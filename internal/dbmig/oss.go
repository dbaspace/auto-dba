package dbmig

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/store"
)

func TestOSS(ctx context.Context, cfg store.MigrationOSSSettings) (ok bool, msg string) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	bucket := strings.TrimSpace(cfg.Bucket)
	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if endpoint == "" {
		return false, "请填写 OSS 访问地址"
	}
	if bucket == "" {
		return false, "请填写存储桶名称"
	}
	if accessKey == "" || secretKey == "" {
		return false, "请填写 AccessKey ID / Secret"
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return false, "OSS 访问地址无效"
	}
	region := ossRegion(u.Host)
	candidates := []string{
		strings.TrimRight(endpoint, "/") + "/" + url.PathEscape(bucket),
		u.Scheme + "://" + bucket + "." + u.Host + "/",
	}
	client := &http.Client{Timeout: 12 * time.Second}
	var last string
	for _, raw := range candidates {
		ok, last = headOSS(ctx, client, raw, region, accessKey, secretKey)
		if ok {
			return true, "OSS 连接成功"
		}
	}
	if last == "" {
		last = "连接失败"
	}
	return false, last
}

func ossRegion(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	parts := strings.Split(h, ".")
	if strings.Contains(h, "aliyuncs.com") && len(parts) > 0 && strings.HasPrefix(parts[0], "oss-") {
		return strings.TrimPrefix(parts[0], "oss-")
	}
	if strings.Contains(h, "myqcloud.com") && len(parts) > 1 && parts[0] == "cos" {
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

func headOSS(ctx context.Context, client *http.Client, raw, region, accessKey, secretKey string) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return false, err.Error()
	}
	signAWSv4(req, region, accessKey, secretKey)
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		// 404 表示鉴权已通过但桶不存在/路径风格不匹配，仍视为连通
		if resp.StatusCode == http.StatusNotFound {
			return true, "OSS 连接成功（桶可能不存在，请核对名称）"
		}
		return true, "OSS 连接成功"
	case http.StatusForbidden, http.StatusUnauthorized:
		return false, "鉴权失败，请核对 AccessKey / Secret / 桶权限"
	default:
		return false, fmt.Sprintf("连接失败：HTTP %d", resp.StatusCode)
	}
}

func signAWSv4(req *http.Request, region, accessKey, secretKey string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex("")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	req.Header.Set("Host", host)
	canonicalHeaders := "host:" + host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")
	signingKey := awsSignKey(secretKey, dateStamp, region, "s3")
	sig := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders, sig,
	))
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	var b strings.Builder
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + strings.TrimPrefix(b.String(), "/")
	}
	if b.Len() == 0 {
		return "/"
	}
	if strings.HasSuffix(path, "/") && !strings.HasSuffix(b.String(), "/") {
		b.WriteByte('/')
	}
	return b.String()
}

func awsSignKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(data))
	return m.Sum(nil)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
