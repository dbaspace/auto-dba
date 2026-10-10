package srcluster

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// BuildObjectKey 约定：{prefix}{version}/{role}/{filename}
func BuildObjectKey(prefix, version, role, filename string) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "starrocks/"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	fn := path.Base(strings.TrimSpace(filename))
	if fn == "" || fn == "." {
		fn = fmt.Sprintf("StarRocks-%s-%s.tar.gz", version, role)
	}
	return fmt.Sprintf("%s%s/%s/%s", p, strings.Trim(version, "/"), strings.ToLower(role), fn)
}

// ossRuntime 运行时连接参数（来自配置中心 OSS + SR 制品 prefix）。
type ossRuntime struct {
	Endpoint    string // 规范化后的区域 endpoint（不含 bucket 前缀）
	Bucket      string
	AccessKey   string
	SecretKey   string
	Prefix      string // 制品前缀
	Region      string
	VirtualHost bool // COS/OSS 等公有云须 virtual-host；MinIO 等用 path-style
}

func (e *Engine) loadOSSPlain() (*ossRuntime, error) {
	bind, err := e.Store.GetSrOSSSettings()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bind.OSSSettingID) == "" {
		return nil, fmt.Errorf("请先在集群部署 · StarRocks 设置中选择配置中心的 OSS，并填写制品 prefix")
	}
	return newOSSRuntime(e.Store, e.Secret, bind.OSSSettingID, bind.Prefix, "starrocks/")
}

// newOSSRuntime 由配置中心 OSS + 制品 prefix 构造连接参数；prefix 为空时用 defaultPrefix。
func newOSSRuntime(st *store.DB, secretKey, ossSettingID, prefix, defaultPrefix string) (*ossRuntime, error) {
	plain, err := st.GetOSSSettingPlain(ossSettingID)
	if err != nil || plain == nil {
		return nil, fmt.Errorf("配置中心 OSS 不存在，请到「配置中心 → OSS 配置」维护")
	}
	sk, err := secret.Decrypt(secretKey, plain.SecretEnc)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(plain.Endpoint) == "" || strings.TrimSpace(plain.Bucket) == "" ||
		strings.TrimSpace(plain.AccessKey) == "" || strings.TrimSpace(sk) == "" {
		return nil, fmt.Errorf("所选 OSS（环境 %s）凭证不完整，请到配置中心补全", plain.Environment)
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = defaultPrefix
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	endpoint, host, err := normalizeOSSEndpoint(plain.Endpoint, plain.Bucket)
	if err != nil {
		return nil, err
	}
	return &ossRuntime{
		Endpoint: endpoint, Bucket: strings.Trim(plain.Bucket, "/"),
		AccessKey: plain.AccessKey, SecretKey: sk, Prefix: prefix,
		Region: s3Region(host), VirtualHost: preferVirtualHost(plain.CloudProduct, host),
	}, nil
}

// normalizeOSSEndpoint 补全 scheme，并去掉用户误填的 bucket. 前缀。
func normalizeOSSEndpoint(endpoint, bucket string) (normEndpoint, host string, err error) {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("endpoint 无效")
	}
	host = u.Host
	prefix := strings.ToLower(strings.Trim(bucket, "/")) + "."
	if strings.HasPrefix(strings.ToLower(host), prefix) {
		host = host[len(prefix):]
		u.Host = host
	}
	return strings.TrimRight(u.Scheme+"://"+host, "/"), host, nil
}

// preferVirtualHost 与配置中心 OSS 上传客户端一致：公有云用 virtual-host。
func preferVirtualHost(cloudProduct, host string) bool {
	switch strings.ToLower(strings.TrimSpace(cloudProduct)) {
	case "tencent_cos", "aliyun_oss", "huawei_obs", "aws_s3":
		return true
	case "s3_compatible":
		return false
	}
	h := strings.ToLower(strings.TrimSpace(host))
	if strings.Contains(h, "myqcloud.com") ||
		strings.Contains(h, "aliyuncs.com") ||
		strings.Contains(h, "myhuaweicloud.com") ||
		strings.Contains(h, "amazonaws.com") {
		return true
	}
	return false
}

// s3URL 构造请求 URL。objectKey 为空表示桶根（List/HEAD bucket）。
// 返回 fullURL、用于签名的 host、canonical URI（已 PathEscape）。
func (s *ossRuntime) s3URL(objectKey string, query url.Values) (fullURL, host, canonicalURI string, err error) {
	if s == nil {
		return "", "", "", fmt.Errorf("OSS 未配置")
	}
	endpoint, baseHost, err := normalizeOSSEndpoint(s.Endpoint, s.Bucket)
	if err != nil {
		return "", "", "", err
	}
	keyEsc := escapeObjectPath(objectKey)
	u, _ := url.Parse(endpoint)
	scheme := "https"
	if u != nil && u.Scheme != "" {
		scheme = u.Scheme
	}
	if s.VirtualHost {
		host = s.Bucket + "." + baseHost
		if keyEsc == "" {
			canonicalURI = "/"
		} else {
			canonicalURI = "/" + keyEsc
		}
		fullURL = scheme + "://" + host + canonicalURI
	} else {
		host = baseHost
		if keyEsc == "" {
			canonicalURI = "/" + url.PathEscape(s.Bucket)
		} else {
			canonicalURI = "/" + url.PathEscape(s.Bucket) + "/" + keyEsc
		}
		fullURL = scheme + "://" + host + canonicalURI
	}
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	return fullURL, host, canonicalURI, nil
}

func escapeObjectPath(objectKey string) string {
	trimmed := strings.Trim(strings.TrimLeft(objectKey, "/"), "/")
	if trimmed == "" {
		return ""
	}
	segs := strings.Split(trimmed, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// ArtifactPrefix 返回当前绑定的制品 prefix。
func (e *Engine) ArtifactPrefix() string {
	bind, err := e.Store.GetSrOSSSettings()
	if err != nil || bind == nil || strings.TrimSpace(bind.Prefix) == "" {
		return "starrocks/"
	}
	p := bind.Prefix
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// PresignGETURL 导出短时 GET 签名（供 db-deploy 等复用）。
func PresignGETURL(endpoint, bucket, objectKey, ak, sk string, expire time.Duration) (string, error) {
	norm, host, err := normalizeOSSEndpoint(endpoint, bucket)
	if err != nil {
		return "", err
	}
	rt := &ossRuntime{
		Endpoint: norm, Bucket: strings.Trim(bucket, "/"),
		AccessKey: ak, SecretKey: sk,
		Region: s3Region(host), VirtualHost: preferVirtualHost("", host),
	}
	return presignAWSv4GET(rt, objectKey, expire)
}

// PresignGET 签发短时 GET URL（AWS S3 V4 query；公有云 virtual-host）。
func (e *Engine) PresignGET(ctx context.Context, objectKey string, expire time.Duration) (string, error) {
	s, err := e.loadOSSPlain()
	if err != nil {
		return "", err
	}
	if expire <= 0 {
		expire = 2 * time.Hour
	}
	return PresignGETURL(s.Endpoint, s.Bucket, objectKey, s.AccessKey, s.SecretKey, expire)
}

// UploadObject 控制面流式上传制品到 OSS。
func (e *Engine) UploadObject(ctx context.Context, objectKey string, body io.Reader, size int64, contentType string) error {
	s, err := e.loadOSSPlain()
	if err != nil {
		return err
	}
	return putAWSv4(ctx, s, objectKey, body, size, contentType)
}

// TestOSS 连通性（HEAD bucket，使用所选配置中心 OSS）。
func (e *Engine) TestOSS(ctx context.Context) (bool, string) {
	s, err := e.loadOSSPlain()
	if err != nil {
		return false, err.Error()
	}
	raw, host, _, err := s.s3URL("", nil)
	if err != nil {
		return false, err.Error()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return false, err.Error()
	}
	req.Host = host
	signAWSv4Header(req, s.Region, s.AccessKey, s.SecretKey, "UNSIGNED-PAYLOAD")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		style := "path-style"
		if s.VirtualHost {
			style = "virtual-host"
		}
		return true, fmt.Sprintf("OSS 可达 HTTP %d（bucket=%s, %s）", resp.StatusCode, s.Bucket, style)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return false, fmt.Sprintf("HTTP %d %s", resp.StatusCode, string(b))
}

// ResolveInstallSpec 按集群版本+角色解析装包参数（签名 URL）。
func (e *Engine) ResolveInstallSpec(ctx context.Context, cl *store.SrCluster, role string) (downloadURL, sha256, format, filename string, err error) {
	if cl == nil {
		return "", "", "", "", fmt.Errorf("集群为空")
	}
	if err := e.ensureClusterRelease(cl); err != nil {
		return "", "", "", "", err
	}
	rid := strings.TrimSpace(cl.ReleaseID)
	ver := strings.TrimSpace(cl.Version)
	art, err := e.Store.GetSrReleaseArtifact(rid, role)
	if err != nil || !artifactInstallReady(art) {
		if err2 := artifactGapError(ver, rid, role, art); err2 != nil {
			return "", "", "", "", err2
		}
		return "", "", "", "", fmt.Errorf("版本 %s（release_id=%s）缺少可用的 %s 分包", ver, rid, role)
	}
	if e.Cfg.DryRun {
		return "dry-run://" + art.ObjectKey, art.SHA256, art.Format, art.Filename, nil
	}
	urlStr, err := e.PresignGET(ctx, art.ObjectKey, 2*time.Hour)
	if err != nil {
		return "", "", "", "", err
	}
	return urlStr, art.SHA256, art.Format, art.Filename, nil
}

func presignAWSv4GET(s *ossRuntime, objectKey string, expire time.Duration) (string, error) {
	expSec := int(expire.Seconds())
	if expSec < 60 {
		expSec = 3600
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := dateStamp + "/" + s.Region + "/s3/aws4_request"
	creds := s.AccessKey + "/" + scope
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", creds)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", expSec))
	q.Set("X-Amz-SignedHeaders", "host")
	fullURL, host, canonicalURI, err := s.s3URL(objectKey, q)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(fullURL)
	if err != nil {
		return "", err
	}
	canonicalQuery := u.Query().Encode()
	canonicalRequest := strings.Join([]string{
		"GET",
		canonicalURI,
		canonicalQuery,
		"host:" + host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")
	sig := hex.EncodeToString(hmacSHA256(awsSignKey(s.SecretKey, dateStamp, s.Region, "s3"), stringToSign))
	q.Set("X-Amz-Signature", sig)
	base := strings.SplitN(fullURL, "?", 2)[0]
	return base + "?" + q.Encode(), nil
}

func putAWSv4(ctx context.Context, s *ossRuntime, objectKey string, body io.Reader, size int64, contentType string) error {
	putURL, host, _, err := s.s3URL(objectKey, nil)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, body)
	if err != nil {
		return err
	}
	req.Host = host
	if size > 0 {
		req.ContentLength = size
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	signAWSv4Header(req, s.Region, s.AccessKey, s.SecretKey, "UNSIGNED-PAYLOAD")
	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("OSS PUT HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func signAWSv4Header(req *http.Request, region, accessKey, secretKey, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	req.Header.Set("Host", host)
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := ""
	if req.URL.RawQuery != "" {
		// Values.Encode 按 key 排序，满足 SigV4 对 query 的要求（无空格场景与 +/%20 差异可忽略）
		canonicalQuery = req.URL.Query().Encode()
	}
	canonicalHeaders := "host:" + host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method, canonicalURI, canonicalQuery, canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, sha256Hex(canonicalRequest)}, "\n")
	sig := hex.EncodeToString(hmacSHA256(awsSignKey(secretKey, dateStamp, region, "s3"), stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders, sig,
	))
}

func s3Region(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	parts := strings.Split(h, ".")
	if strings.Contains(h, "aliyuncs.com") && len(parts) > 0 && strings.HasPrefix(parts[0], "oss-") {
		return strings.TrimPrefix(parts[0], "oss-")
	}
	if strings.Contains(h, "myqcloud.com") && len(parts) > 1 && parts[0] == "cos" {
		return parts[1]
	}
	if strings.Contains(h, "myhuaweicloud.com") && len(parts) > 1 {
		return parts[1]
	}
	if strings.Contains(h, "amazonaws.com") && len(parts) > 1 && (parts[0] == "s3" || strings.HasPrefix(parts[0], "s3-")) {
		if parts[0] == "s3" {
			return parts[1]
		}
		return strings.TrimPrefix(parts[0], "s3-")
	}
	return "us-east-1"
}

func pathEscapeAWS(p string) string {
	return escapeObjectPath(strings.TrimLeft(p, "/"))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(data))
	return h.Sum(nil)
}

func awsSignKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}
