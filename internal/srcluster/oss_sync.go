package srcluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// OSSObject 列举结果条目。
type OSSObject struct {
	Key  string
	Size int64
}

// SyncRoleResult 单角色同步结果。
type SyncRoleResult struct {
	Status   string                   `json:"status"` // ok | hashing | missing | error
	Message  string                   `json:"message,omitempty"`
	Artifact *store.SrReleaseArtifact `json:"artifact,omitempty"`
}

// SyncArtifactsResult FE/BE 同步摘要。
type SyncArtifactsResult struct {
	FE      SyncRoleResult `json:"fe"`
	BE      SyncRoleResult `json:"be"`
	Message string         `json:"message"`
}

var sha256HexRe = regexp.MustCompile(`(?i)\b([a-f0-9]{64})\b`)

// 小于该体积则同步请求内联算 sha256；更大则后台计算，避免卡住 UI。
const syncHashInlineMaxBytes int64 = 32 << 20

// ListPrefix 列举绑定 OSS 下指定前缀的对象（ListObjectsV2）。
func (e *Engine) ListPrefix(ctx context.Context, prefix string) ([]OSSObject, error) {
	s, err := e.loadOSSPlain()
	if err != nil {
		return nil, err
	}
	return listObjectsV2(ctx, s, prefix)
}

// HashObject 流式 GET 并计算 sha256。
func (e *Engine) HashObject(ctx context.Context, objectKey string) (sum string, size int64, err error) {
	s, err := e.loadOSSPlain()
	if err != nil {
		return "", 0, err
	}
	return hashObjectAWSv4(ctx, s, objectKey)
}

// SyncArtifactsFromOSS 扫描绑定 OSS 并登记 FE/BE 分包。
// 支持两种布局：
//  1) 分层：{prefix}{version}/{fe|be}/{filename}
//  2) 扁平文件名：{prefix}StarRocks-{version}.fe.tar.gz / .be.tar.gz（及同风格变体）
// 大包默认不在请求内整包下载算哈希（会极慢），改为侧车/对象元数据，否则后台算 sha256。
func (e *Engine) SyncArtifactsFromOSS(ctx context.Context, release *store.SrRelease) (*SyncArtifactsResult, error) {
	if e == nil || e.Store == nil {
		return nil, fmt.Errorf("engine 未初始化")
	}
	if release == nil || strings.TrimSpace(release.ID) == "" {
		return nil, fmt.Errorf("版本不存在")
	}
	s, err := e.loadOSSPlain()
	if err != nil {
		return nil, err
	}
	// prefix 下列举一次即可覆盖扁平包与 {version}/fe|be/ 分层对象
	objs, errList := listObjectsV2(ctx, s, s.Prefix)
	if errList != nil {
		return nil, fmt.Errorf("OSS 列举失败: %v", errList)
	}
	ver := strings.Trim(release.Version, "/")
	feDir := fmt.Sprintf("%s%s/fe/", s.Prefix, ver)
	beDir := fmt.Sprintf("%s%s/be/", s.Prefix, ver)
	out := &SyncArtifactsResult{}
	out.FE = e.syncRoleArtifact(ctx, s, release, "fe", objs, feDir)
	out.BE = e.syncRoleArtifact(ctx, s, release, "be", objs, beDir)
	var parts []string
	for _, r := range []SyncRoleResult{out.FE, out.BE} {
		if r.Message != "" {
			parts = append(parts, r.Message)
		}
	}
	out.Message = strings.Join(parts, "；")
	return out, nil
}

func (e *Engine) syncRoleArtifact(
	ctx context.Context, s *ossRuntime, release *store.SrRelease, role string,
	objs []OSSObject, nestedHint string,
) SyncRoleResult {
	role = strings.ToLower(role)
	var archives []OSSObject
	sidecarByBase := map[string]string{}
	var sha256sumsKey string
	for _, o := range objs {
		key := strings.TrimSpace(o.Key)
		if key == "" || strings.HasSuffix(key, "/") {
			continue
		}
		base := path.Base(key)
		lower := strings.ToLower(base)
		if lower == "sha256sums" || lower == "sha256sum.txt" {
			sha256sumsKey = key
			continue
		}
		if strings.HasSuffix(lower, ".sha256") {
			stem := strings.TrimSuffix(lower, ".sha256")
			sidecarByBase[stem] = key
			sidecarByBase[strings.TrimSuffix(base, ".sha256")] = key
			continue
		}
		if !isArchiveName(lower) {
			continue
		}
		if objectInRoleDir(key, s.Prefix, release.Version, role) || flatNameMatchesVersionRole(lower, release.Version, role) {
			archives = append(archives, o)
		}
	}
	picked := pickArchive(archives, role, release.Version)
	if picked == nil {
		flatHint := fmt.Sprintf("%sStarRocks-%s.%s.tar.gz", s.Prefix, release.Version, role)
		return SyncRoleResult{
			Status:  "missing",
			Message: fmt.Sprintf("%s 未找到安装包（可放 %s 或扁平命名 %s）", role, nestedHint, flatHint),
		}
	}
	filename := path.Base(picked.Key)
	sum := ""
	if sk, ok := sidecarByBase[filename]; ok {
		sum, _ = readSHA256Sidecar(ctx, s, sk)
	}
	if sk, ok := sidecarByBase[strings.ToLower(filename)]; ok && sum == "" {
		sum, _ = readSHA256Sidecar(ctx, s, sk)
	}
	if sum == "" && sha256sumsKey != "" {
		sum, _ = readSHA256FromSums(ctx, s, sha256sumsKey, filename)
	}
	size := picked.Size
	if headSize, metaSum, herr := headObjectMeta(ctx, s, picked.Key); herr == nil {
		if headSize > 0 {
			size = headSize
		}
		if sum == "" && metaSum != "" {
			sum = metaSum
		}
	}
	hashPending := false
	if sum == "" {
		if size > 0 && size <= syncHashInlineMaxBytes {
			var herr error
			sum, size, herr = hashObjectAWSv4(ctx, s, picked.Key)
			if herr != nil {
				return SyncRoleResult{Status: "error", Message: fmt.Sprintf("%s 计算 sha256 失败: %v", role, herr)}
			}
		} else {
			hashPending = true
		}
	}
	art := &store.SrReleaseArtifact{
		ReleaseID: release.ID,
		Role:      role,
		ObjectKey: picked.Key,
		Filename:  filename,
		SHA256:    sum,
		SizeBytes: size,
		Format:    "tar.gz",
	}
	if err := e.Store.UpsertSrReleaseArtifact(art); err != nil {
		return SyncRoleResult{Status: "error", Message: fmt.Sprintf("%s 写入失败: %v", role, err)}
	}
	if hashPending {
		e.scheduleArtifactHash(release.ID, role, picked.Key, filename, size)
		return SyncRoleResult{
			Status:   "hashing",
			Message:  fmt.Sprintf("%s 已登记 %s，sha256 后台计算中（大包可能需数分钟，稍后刷新查看）", role, filename),
			Artifact: art,
		}
	}
	return SyncRoleResult{
		Status:   "ok",
		Message:  fmt.Sprintf("%s 已同步 %s", role, filename),
		Artifact: art,
	}
}

func (e *Engine) scheduleArtifactHash(releaseID, role, objectKey, filename string, knownSize int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		s, err := e.loadOSSPlain()
		if err != nil {
			return
		}
		sum, size, err := hashObjectAWSv4(ctx, s, objectKey)
		if err != nil {
			return
		}
		if knownSize > 0 && size == 0 {
			size = knownSize
		}
		_ = e.Store.UpsertSrReleaseArtifact(&store.SrReleaseArtifact{
			ReleaseID: releaseID,
			Role:      role,
			ObjectKey: objectKey,
			Filename:  filename,
			SHA256:    sum,
			SizeBytes: size,
			Format:    "tar.gz",
		})
	}()
}

func isArchiveName(lower string) bool {
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.bz2")
}

// objectInRoleDir 是否落在 {prefix}{version}/{role}/ 下。
func objectInRoleDir(key, prefix, version, role string) bool {
	p := strings.TrimSpace(prefix)
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	want := fmt.Sprintf("%s%s/%s/", p, strings.Trim(version, "/"), strings.ToLower(role))
	return strings.HasPrefix(key, want)
}

// flatNameMatchesVersionRole 匹配扁平命名，如 StarRocks-3.2.10.fe.tar.gz / StarRocks-3.2.10-be.tar.gz。
func flatNameMatchesVersionRole(lowerName, version, role string) bool {
	ver := strings.ToLower(strings.TrimSpace(version))
	role = strings.ToLower(strings.TrimSpace(role))
	if ver == "" || role == "" {
		return false
	}
	n := lowerName
	patterns := []string{
		fmt.Sprintf("-%s.%s.tar.gz", ver, role),
		fmt.Sprintf("-%s-%s.tar.gz", ver, role),
		fmt.Sprintf("_%s_%s.tar.gz", ver, role),
		fmt.Sprintf("-%s.%s.tgz", ver, role),
		fmt.Sprintf("-%s-%s.tgz", ver, role),
	}
	for _, p := range patterns {
		if strings.HasSuffix(n, p) || strings.Contains(n, p) {
			return true
		}
	}
	// 宽松：版本完整段 + 角色标记（避免 3.2.1 误匹配 3.2.10）
	if !versionTokenInName(n, ver) {
		return false
	}
	markers := []string{"." + role + ".", "-" + role + ".", "_" + role + ".", "." + role + ".tar", "-" + role + ".tar"}
	for _, m := range markers {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

func versionTokenInName(lowerName, ver string) bool {
	idx := strings.Index(lowerName, ver)
	for idx >= 0 {
		beforeOK := idx == 0 || !isVersionTokenChar(lowerName[idx-1])
		after := idx + len(ver)
		afterOK := after >= len(lowerName) || !isVersionTokenChar(lowerName[after])
		if beforeOK && afterOK {
			return true
		}
		next := strings.Index(lowerName[idx+1:], ver)
		if next < 0 {
			return false
		}
		idx = idx + 1 + next
	}
	return false
}

func isVersionTokenChar(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.'
}

func pickArchive(archives []OSSObject, role, version string) *OSSObject {
	if len(archives) == 0 {
		return nil
	}
	role = strings.ToLower(role)
	ver := strings.ToLower(strings.TrimSpace(version))
	score := func(o OSSObject) int {
		n := strings.ToLower(path.Base(o.Key))
		sc := 0
		if flatNameMatchesVersionRole(n, ver, role) {
			sc += 200
		}
		if strings.Contains(o.Key, "/"+role+"/") {
			sc += 150
		}
		if ver != "" && versionTokenInName(n, ver) {
			sc += 50
		}
		if strings.HasSuffix(n, ".tar.gz") {
			sc += 10
		} else if strings.HasSuffix(n, ".tgz") {
			sc += 8
		}
		return sc
	}
	sort.SliceStable(archives, func(i, j int) bool {
		si, sj := score(archives[i]), score(archives[j])
		if si != sj {
			return si > sj
		}
		return archives[i].Size > archives[j].Size
	})
	return &archives[0]
}

func readSHA256Sidecar(ctx context.Context, s *ossRuntime, key string) (string, error) {
	body, err := getObjectBytes(ctx, s, key, 4096)
	if err != nil {
		return "", err
	}
	m := sha256HexRe.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return "", fmt.Errorf("侧车无有效 sha256")
	}
	return strings.ToLower(m[1]), nil
}

func readSHA256FromSums(ctx context.Context, s *ossRuntime, sumsKey, filename string) (string, error) {
	body, err := getObjectBytes(ctx, s, sumsKey, 1<<20)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, filename) {
			continue
		}
		m := sha256HexRe.FindStringSubmatch(line)
		if len(m) >= 2 {
			return strings.ToLower(m[1]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS 中无 %s", filename)
}

type listBucketResult struct {
	XMLName     xml.Name       `xml:"ListBucketResult"`
	Contents    []listContent  `xml:"Contents"`
	IsTruncated bool           `xml:"IsTruncated"`
	NextToken   string         `xml:"NextContinuationToken"`
}

type listContent struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
}

func listObjectsV2(ctx context.Context, s *ossRuntime, prefix string) ([]OSSObject, error) {
	var out []OSSObject
	token := ""
	client := &http.Client{Timeout: 60 * time.Second}
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		q.Set("prefix", prefix)
		q.Set("max-keys", "1000")
		if token != "" {
			q.Set("continuation-token", token)
		}
		listURL, host, _, err := s.s3URL("", q)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
		if err != nil {
			return nil, err
		}
		req.Host = host
		signAWSv4Header(req, s.Region, s.AccessKey, s.SecretKey, "UNSIGNED-PAYLOAD")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("ListObjects HTTP %d: %s", resp.StatusCode, string(body))
		}
		var parsed listBucketResult
		if err := xml.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("解析 ListObjects: %w", err)
		}
		for _, c := range parsed.Contents {
			out = append(out, OSSObject{Key: c.Key, Size: c.Size})
		}
		if !parsed.IsTruncated || strings.TrimSpace(parsed.NextToken) == "" {
			break
		}
		token = parsed.NextToken
	}
	return out, nil
}

func getObjectBytes(ctx context.Context, s *ossRuntime, objectKey string, limit int64) ([]byte, error) {
	raw, host, _, err := s.s3URL(objectKey, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Host = host
	signAWSv4Header(req, s.Region, s.AccessKey, s.SecretKey, "UNSIGNED-PAYLOAD")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GET HTTP %d: %s", resp.StatusCode, string(b))
	}
	if limit <= 0 {
		limit = 1 << 20
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func headObjectMeta(ctx context.Context, s *ossRuntime, objectKey string) (size int64, metaSHA string, err error) {
	raw, host, _, err := s.s3URL(objectKey, nil)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return 0, "", err
	}
	req.Host = host
	signAWSv4Header(req, s.Region, s.AccessKey, s.SecretKey, "UNSIGNED-PAYLOAD")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, "", fmt.Errorf("HEAD HTTP %d", resp.StatusCode)
	}
	size = resp.ContentLength
	for _, hk := range []string{
		"x-amz-meta-sha256", "X-Amz-Meta-Sha256",
		"x-cos-meta-sha256", "X-Cos-Meta-Sha256",
		"x-oss-meta-sha256", "X-Oss-Meta-Sha256",
	} {
		if v := strings.TrimSpace(resp.Header.Get(hk)); v != "" {
			if m := sha256HexRe.FindStringSubmatch(v); len(m) >= 2 {
				return size, strings.ToLower(m[1]), nil
			}
		}
	}
	return size, "", nil
}

func hashObjectAWSv4(ctx context.Context, s *ossRuntime, objectKey string) (string, int64, error) {
	raw, host, _, err := s.s3URL(objectKey, nil)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", 0, err
	}
	req.Host = host
	signAWSv4Header(req, s.Region, s.AccessKey, s.SecretKey, "UNSIGNED-PAYLOAD")
	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", 0, fmt.Errorf("GET HTTP %d: %s", resp.StatusCode, string(b))
	}
	h := sha256.New()
	n, err := io.Copy(h, resp.Body)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
