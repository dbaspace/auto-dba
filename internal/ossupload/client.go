package ossupload

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client S3 兼容 Multipart（SigV4；按云产品选 virtual-host 或 path-style）。
type Client struct {
	Endpoint    string
	Bucket      string
	Region      string
	AccessKey   string
	SecretKey   string
	VirtualHost bool // true: https://bucket.host/key；false: https://host/bucket/key
	HTTP        *http.Client
}

func NewClient(endpoint, bucket, accessKey, secretKey, cloudProduct string) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	bucket = strings.TrimSpace(bucket)
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("OSS 缺少 endpoint/bucket/凭证")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("OSS endpoint 无效")
	}
	host := u.Host
	// 用户若已填 bucket.cos.xxx 形态，剥掉桶名前缀，避免重复拼接
	prefix := strings.ToLower(bucket) + "."
	if strings.HasPrefix(strings.ToLower(host), prefix) {
		host = host[len(prefix):]
		u.Host = host
		endpoint = u.Scheme + "://" + host
	}
	vh := preferVirtualHost(cloudProduct, host)
	return &Client{
		Endpoint:    strings.TrimRight(endpoint, "/"),
		Bucket:      strings.Trim(bucket, "/"),
		Region:      regionFromHost(host),
		AccessKey:   accessKey,
		SecretKey:   secretKey,
		VirtualHost: vh,
		HTTP:        &http.Client{Timeout: 2 * time.Hour},
	}, nil
}

// preferVirtualHost 公有云强制/优先 virtual-host；MinIO 等兼容端点用 path-style。
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

func (c *Client) objectURL(objectKey string, query url.Values) string {
	keyPath := escapeObjectPath(objectKey)
	var raw string
	if c.VirtualHost {
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Host == "" {
			raw = c.Endpoint + "/" + keyPath
		} else {
			base := u.Scheme + "://" + c.Bucket + "." + u.Host
			if keyPath == "" {
				raw = base + "/"
			} else {
				raw = base + "/" + keyPath
			}
		}
	} else {
		path := "/" + url.PathEscape(c.Bucket)
		if keyPath != "" {
			path += "/" + keyPath
		}
		raw = c.Endpoint + path
	}
	if len(query) > 0 {
		raw += "?" + query.Encode()
	}
	return raw
}

func escapeObjectPath(objectKey string) string {
	trimmed := strings.Trim(strings.TrimLeft(objectKey, "/"), "/")
	if trimmed == "" {
		return ""
	}
	segs := strings.Split(trimmed, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// CreateMultipartUpload 初始化分片上传，返回 UploadId。
func (c *Client) CreateMultipartUpload(ctx context.Context, objectKey string) (string, error) {
	q := url.Values{}
	q.Set("uploads", "")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.objectURL(objectKey, q), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if err := c.sign(req, sha256HexBytes(nil)); err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("CreateMultipartUpload HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("解析 UploadId: %w", err)
	}
	if strings.TrimSpace(out.UploadID) == "" {
		return "", fmt.Errorf("CreateMultipartUpload 未返回 UploadId")
	}
	return out.UploadID, nil
}

// UploadPart 上传第 partNumber 片（从 1 起），返回 ETag。
func (c *Client) UploadPart(ctx context.Context, objectKey, uploadID string, partNumber int, body io.Reader, size int64) (string, error) {
	q := url.Values{}
	q.Set("partNumber", fmt.Sprintf("%d", partNumber))
	q.Set("uploadId", uploadID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.objectURL(objectKey, q), body)
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	// 大文件分片用 UNSIGNED-PAYLOAD，避免整片入内存再算哈希。
	if err := c.sign(req, "UNSIGNED-PAYLOAD"); err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("UploadPart HTTP %d: %s", resp.StatusCode, string(b))
	}
	etag := strings.Trim(resp.Header.Get("ETag"), "\"")
	if etag == "" {
		return "", fmt.Errorf("UploadPart 未返回 ETag")
	}
	return etag, nil
}

type completePart struct {
	XMLName    xml.Name `xml:"Part"`
	PartNumber int      `xml:"PartNumber"`
	ETag       string   `xml:"ETag"`
}

type completeMultipart struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []completePart `xml:"Part"`
}

// CompleteMultipartUpload 合并分片。
func (c *Client) CompleteMultipartUpload(ctx context.Context, objectKey, uploadID string, parts []PartRef) error {
	sorted := append([]PartRef(nil), parts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PartNumber < sorted[j].PartNumber })
	payload := completeMultipart{}
	for _, p := range sorted {
		etag := p.ETag
		if !strings.HasPrefix(etag, "\"") {
			etag = "\"" + etag + "\""
		}
		payload.Parts = append(payload.Parts, completePart{PartNumber: p.PartNumber, ETag: etag})
	}
	raw, err := xml.Marshal(payload)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("uploadId", uploadID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.objectURL(objectKey, q), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	req.ContentLength = int64(len(raw))
	sum := sha256HexBytes(raw)
	if err := c.sign(req, sum); err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("CompleteMultipartUpload HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// AbortMultipartUpload 取消分片上传。
func (c *Client) AbortMultipartUpload(ctx context.Context, objectKey, uploadID string) error {
	q := url.Values{}
	q.Set("uploadId", uploadID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.objectURL(objectKey, q), nil)
	if err != nil {
		return err
	}
	if err := c.sign(req, sha256HexBytes(nil)); err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("AbortMultipartUpload HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// HeadObject 返回对象 Content-Length。
func (c *Client) HeadObject(ctx context.Context, objectKey string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.objectURL(objectKey, nil), nil)
	if err != nil {
		return 0, err
	}
	if err := c.sign(req, sha256HexBytes(nil)); err != nil {
		return 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HeadObject HTTP %d", resp.StatusCode)
	}
	return resp.ContentLength, nil
}

// GetObject 下载对象流（调用方关闭 Body）。
func (c *Client) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.objectURL(objectKey, nil), nil)
	if err != nil {
		return nil, 0, err
	}
	if err := c.sign(req, sha256HexBytes(nil)); err != nil {
		return nil, 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("GetObject HTTP %d: %s", resp.StatusCode, string(b))
	}
	return resp.Body, resp.ContentLength, nil
}

// VerifySHA256 流式下载并校验 SHA-256 与长度。
func (c *Client) VerifySHA256(ctx context.Context, objectKey string, expectSize int64, expectSHA256 string) error {
	rc, size, err := c.GetObject(ctx, objectKey)
	if err != nil {
		return err
	}
	defer rc.Close()
	if expectSize > 0 && size > 0 && size != expectSize {
		return fmt.Errorf("对象长度不符: got=%d want=%d", size, expectSize)
	}
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return fmt.Errorf("下载校验失败: %w", err)
	}
	if expectSize > 0 && n != expectSize {
		return fmt.Errorf("下载长度不符: got=%d want=%d", n, expectSize)
	}
	got := hex.EncodeToString(h.Sum(nil))
	want := strings.ToLower(strings.TrimSpace(expectSHA256))
	if got != want {
		return fmt.Errorf("SHA-256 不符: got=%s want=%s", got, want)
	}
	return nil
}

// PartRef Complete 用分片引用。
type PartRef struct {
	PartNumber int
	ETag       string
}

func (c *Client) sign(req *http.Request, payloadHash string) error {
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
	canonicalQuery := canonicalQueryString(req.URL.Query())
	canonicalHeaders := "host:" + host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + c.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")
	signingKey := awsSignKey(c.SecretKey, dateStamp, c.Region, "s3")
	sig := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.AccessKey, scope, signedHeaders, sig,
	))
	return nil
}

func canonicalQueryString(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vs := q[k]
		if len(vs) == 0 {
			parts = append(parts, url.QueryEscape(k)+"=")
			continue
		}
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	// AWS 要求空格编码为 %20 而非 +
	return strings.ReplaceAll(strings.Join(parts, "&"), "+", "%20")
}

func regionFromHost(host string) string {
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

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
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
