package dbdeploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/srcluster"
	"auto-dba/internal/store"
)

// ReleaseStatus 版本包状态。
const (
	ReleaseStatusReady   = "ready"   // object_key + 64 位 sha256 齐全，可部署
	ReleaseStatusPending = "pending" // 已登记，待上传 / 同步包
	ReleaseStatusHashing = "hashing" // 后台计算 sha256 中
)

var (
	sha256HexFull  = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
	mysqlVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	pgVersionRe    = regexp.MustCompile(`^(\d+)(?:\.(\d+))?$`)
	genericVerRe   = regexp.MustCompile(`^\d+(\.\d+){0,3}([.-][0-9A-Za-z]+)?$`)
)

// syncHashInlineMaxBytes 小于该体积在同步请求内直接算 sha256，否则后台计算。
const syncHashInlineMaxBytes int64 = 32 << 20

// ValidateVersion 按引擎校验版本号格式。
func ValidateVersion(engine, version string) error {
	v := strings.TrimSpace(version)
	if v == "" {
		return fmt.Errorf("版本号必填")
	}
	switch engine {
	case store.DbDeployEngineMySQL:
		m := mysqlVersionRe.FindStringSubmatch(v)
		if m == nil {
			return fmt.Errorf("MySQL 版本号格式为 主.次.修订，如 8.0.36 / 8.4.3 / 5.7.44")
		}
		if m[1] != "5" && m[1] != "8" && m[1] != "9" {
			return fmt.Errorf("暂不支持 MySQL %s.x", m[1])
		}
	case store.DbDeployEnginePostgres:
		m := pgVersionRe.FindStringSubmatch(v)
		if m == nil {
			return fmt.Errorf("PostgreSQL 版本号格式为 主版本.小版本，如 16.4 / 17.2")
		}
		if major := atoiSafe(m[1]); major < 13 {
			return fmt.Errorf("PostgreSQL 需 13 及以上（12 已停止维护），当前 %s", v)
		}
	default:
		if !genericVerRe.MatchString(v) {
			return fmt.Errorf("版本号格式无效：%s", v)
		}
	}
	return nil
}

// MajorVersion MySQL 取「主.次」（8.0 / 8.4 / 5.7），PostgreSQL 取主版本（16）；其他引擎取首段。
func MajorVersion(engine, version string) string {
	v := strings.TrimSpace(version)
	switch engine {
	case store.DbDeployEngineMySQL:
		parts := strings.SplitN(v, ".", 3)
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
		return v
	default:
		if i := strings.Index(v, "."); i > 0 {
			return v[:i]
		}
		return v
	}
}

// ReleaseChannels 各引擎发行通道选项。
func ReleaseChannels(engine string) []string {
	switch engine {
	case store.DbDeployEngineMySQL:
		return []string{"LTS", "Innovation", "GA"}
	case store.DbDeployEnginePostgres:
		return []string{"stable"}
	default:
		return []string{"stable"}
	}
}

// BuildReleaseObjectKey 约定 {prefix}{version}/{arch}/{filename}。
func BuildReleaseObjectKey(prefix, version, arch, filename string) string {
	p := strings.TrimSpace(prefix)
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	if strings.TrimSpace(arch) == "" {
		arch = "x86_64"
	}
	fn := path.Base(strings.TrimSpace(filename))
	if fn == "." || fn == "/" {
		fn = ""
	}
	return fmt.Sprintf("%s%s/%s/%s", p, strings.Trim(strings.TrimSpace(version), "/"), arch, fn)
}

// PackFormatOf 按文件名识别包格式。
func PackFormatOf(filename string) string {
	l := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(l, ".tar.xz") || strings.HasSuffix(l, ".txz"):
		return "tar.xz"
	case strings.HasSuffix(l, ".tar.gz") || strings.HasSuffix(l, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(l, ".tar"):
		return "tar"
	}
	return ""
}

// ReleaseReady object_key 非空、不以 / 结尾，且 sha256 为 64 位十六进制。
func ReleaseReady(r *store.DbDeployRelease) bool {
	if r == nil {
		return false
	}
	key := strings.TrimSpace(r.ObjectKey)
	return key != "" && !strings.HasSuffix(key, "/") && sha256HexFull.MatchString(strings.TrimSpace(r.Checksum))
}

var releaseHashing sync.Map // releaseID -> struct{}

// DecorateRelease 填充展示用的大版本与状态。
func DecorateRelease(r *store.DbDeployRelease) {
	if r == nil {
		return
	}
	r.MajorVersion = MajorVersion(r.Engine, r.Version)
	switch {
	case ReleaseReady(r):
		r.Status = ReleaseStatusReady
	default:
		if _, ok := releaseHashing.Load(r.ID); ok {
			r.Status = ReleaseStatusHashing
		} else {
			r.Status = ReleaseStatusPending
		}
	}
}

// artifactPrefix 引擎 OSS 绑定的制品前缀，未填时 db-artifacts/{engine}/。
func artifactPrefix(bind *store.DbDeployOSSBind, engine string) string {
	p := ""
	if bind != nil {
		p = strings.TrimSpace(bind.Prefix)
	}
	if p == "" {
		p = "db-artifacts/" + engine + "/"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func (s *Service) ossClient(engine string) (*srcluster.OSSClient, string, error) {
	bind, err := s.Store.GetDbDeployOSSBind(engine)
	if err != nil {
		return nil, "", err
	}
	prefix := artifactPrefix(bind, engine)
	cli, err := srcluster.NewOSSClient(s.Store, s.Secret, bind.OSSSettingID, prefix)
	if err != nil {
		return nil, "", err
	}
	return cli, prefix, nil
}

// PrepareRelease 登记 / 修改前：校验版本号（checkVersion 为 false 时跳过，兼容历史记录），
// 补齐 arch、pack_format，object_key 只给了文件名或为空时按约定生成。
func (s *Service) PrepareRelease(r *store.DbDeployRelease, checkVersion bool) error {
	if r == nil {
		return fmt.Errorf("版本为空")
	}
	r.Engine = strings.ToLower(strings.TrimSpace(r.Engine))
	if r.Engine == "pg" || r.Engine == "postgresql" {
		r.Engine = store.DbDeployEnginePostgres
	}
	r.Version = strings.TrimSpace(r.Version)
	if r.Version == "" {
		return fmt.Errorf("版本号必填")
	}
	if checkVersion {
		if err := ValidateVersion(r.Engine, r.Version); err != nil {
			return err
		}
	}
	if strings.TrimSpace(r.Arch) == "" {
		r.Arch = "x86_64"
	}
	r.Checksum = strings.ToLower(strings.TrimSpace(r.Checksum))
	if r.Checksum != "" && !sha256HexFull.MatchString(r.Checksum) {
		return fmt.Errorf("sha256 须为 64 位十六进制")
	}
	key := strings.TrimSpace(r.ObjectKey)
	if key == "" || !strings.Contains(key, "/") {
		fn := key
		if fn == "" {
			fn = strings.TrimSpace(r.Filename)
		}
		if fn != "" {
			bind, _ := s.Store.GetDbDeployOSSBind(r.Engine)
			key = BuildReleaseObjectKey(artifactPrefix(bind, r.Engine), r.Version, r.Arch, fn)
		}
	}
	r.ObjectKey = key
	if r.Filename == "" && key != "" && !strings.HasSuffix(key, "/") {
		r.Filename = path.Base(key)
	}
	if f := PackFormatOf(r.Filename); f != "" {
		r.PackFormat = f
	}
	return nil
}

// UploadRelease 上传安装包到 OSS 并回填 object_key / sha256 / 大小。
func (s *Service) UploadRelease(ctx context.Context, rel *store.DbDeployRelease, filename string, body io.Reader, operator string) (*store.DbDeployRelease, error) {
	fn := path.Base(strings.TrimSpace(filename))
	if fn == "" || fn == "." || PackFormatOf(fn) == "" {
		return nil, fmt.Errorf("仅支持 .tar.gz / .tgz / .tar.xz / .tar 安装包")
	}
	cli, prefix, err := s.ossClient(rel.Engine)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "db-release-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), body)
	_ = tmp.Close()
	if err != nil {
		return nil, err
	}
	rf, err := os.Open(tmpPath)
	if err != nil {
		return nil, err
	}
	defer rf.Close()
	key := BuildReleaseObjectKey(prefix, rel.Version, rel.Arch, fn)
	if err := cli.Upload(ctx, key, rf, n, "application/octet-stream"); err != nil {
		return nil, err
	}
	rel.ObjectKey, rel.Filename, rel.SizeBytes = key, fn, n
	rel.Checksum = hex.EncodeToString(h.Sum(nil))
	rel.PackFormat = PackFormatOf(fn)
	rel.UpdatedBy = operator
	if err := s.Store.UpsertDbDeployRelease(rel); err != nil {
		return nil, err
	}
	DecorateRelease(rel)
	return rel, nil
}

// SyncReleaseResult 从 OSS 同步结果。
type SyncReleaseResult struct {
	Status  string                 `json:"status"` // ok | hashing | missing
	Message string                 `json:"message"`
	Release *store.DbDeployRelease `json:"release,omitempty"`
}

// SyncReleaseFromOSS 扫描 {prefix}{version}/{arch}/ 下的安装包并登记；sha256 取侧车 / SHA256SUMS / 元数据，否则小包当场算、大包后台算。
func (s *Service) SyncReleaseFromOSS(ctx context.Context, rel *store.DbDeployRelease, operator string) (*SyncReleaseResult, error) {
	cli, prefix, err := s.ossClient(rel.Engine)
	if err != nil {
		return nil, err
	}
	dir := BuildReleaseObjectKey(prefix, rel.Version, rel.Arch, "")
	objs, err := cli.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	var pkgs []srcluster.OSSObject
	for _, o := range objs {
		if PackFormatOf(o.Key) != "" && path.Dir(o.Key)+"/" == dir {
			pkgs = append(pkgs, o)
		}
	}
	if len(pkgs) == 0 {
		return &SyncReleaseResult{Status: "missing", Message: fmt.Sprintf("OSS 目录 %s 下没有安装包（.tar.gz / .tar.xz）", dir)}, nil
	}
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].Size > pkgs[j].Size })
	pkg := pkgs[0]
	if strings.TrimSpace(rel.Filename) != "" {
		for _, p := range pkgs {
			if path.Base(p.Key) == rel.Filename {
				pkg = p
				break
			}
		}
	}
	rel.ObjectKey, rel.Filename, rel.SizeBytes = pkg.Key, path.Base(pkg.Key), pkg.Size
	rel.PackFormat = PackFormatOf(rel.Filename)
	rel.UpdatedBy = operator
	sum := cli.KnownSHA256(ctx, pkg.Key, objs)
	status, msg := "ok", "已同步："+rel.Filename
	switch {
	case sum != "":
		rel.Checksum = sum
	case pkg.Size > 0 && pkg.Size <= syncHashInlineMaxBytes:
		if sum, _, err = cli.Hash(ctx, pkg.Key); err != nil {
			return nil, fmt.Errorf("计算 sha256: %w", err)
		}
		rel.Checksum = sum
	default:
		rel.Checksum = ""
		status, msg = "hashing", "已登记 "+rel.Filename+"，包较大，正在后台计算 sha256（也可上传 .sha256 侧车文件后重新同步）"
	}
	if err := s.Store.UpsertDbDeployRelease(rel); err != nil {
		return nil, err
	}
	if status == "hashing" {
		s.scheduleReleaseHash(rel.ID, rel.Engine, pkg.Key)
	}
	DecorateRelease(rel)
	return &SyncReleaseResult{Status: status, Message: msg, Release: rel}, nil
}

func (s *Service) scheduleReleaseHash(releaseID, engine, objectKey string) {
	if _, loaded := releaseHashing.LoadOrStore(releaseID, struct{}{}); loaded {
		return
	}
	go func() {
		defer releaseHashing.Delete(releaseID)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		cli, _, err := s.ossClient(engine)
		if err != nil {
			log.Printf("[db-deploy] release %s hash: %v", releaseID, err)
			return
		}
		sum, size, err := cli.Hash(ctx, objectKey)
		if err != nil {
			log.Printf("[db-deploy] release %s hash: %v", releaseID, err)
			return
		}
		rel, err := s.Store.GetDbDeployRelease(releaseID)
		if err != nil || rel == nil || rel.ObjectKey != objectKey {
			return
		}
		rel.Checksum, rel.SizeBytes = sum, size
		if err := s.Store.UpsertDbDeployRelease(rel); err != nil {
			log.Printf("[db-deploy] release %s save hash: %v", releaseID, err)
		}
	}()
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
