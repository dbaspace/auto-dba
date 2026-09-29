package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

func uploadS3(ctx context.Context, st *store.BackupStorage, encKey, localPath, objectKey string) error {
	endpoint := strings.TrimSpace(st.Endpoint)
	bucket := strings.TrimSpace(st.Bucket)
	ak := strings.TrimSpace(st.AccessKey)
	sk := ""
	if st.SecretKeyEnc != "" {
		plain, err := secret.Decrypt(encKey, st.SecretKeyEnc)
		if err != nil {
			return fmt.Errorf("解密 SecretKey 失败: %w", err)
		}
		sk = plain
	}
	if endpoint == "" || bucket == "" || ak == "" || sk == "" {
		return fmt.Errorf("S3 存储缺少 endpoint/bucket/凭证")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return fmt.Errorf("S3 endpoint 无效")
	}
	region := s3Region(u.Host)
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	keyPath := "/" + strings.Trim(bucket, "/") + "/" + strings.TrimLeft(objectKey, "/")
	putURL := strings.TrimRight(endpoint, "/") + keyPath
	// path-style
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, f)
	if err != nil {
		return err
	}
	req.ContentLength = fi.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	payloadHash := "UNSIGNED-PAYLOAD"
	signAWSv4Put(req, region, ak, sk, payloadHash)
	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("S3 PUT HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
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
	if strings.Contains(h, "amazonaws.com") && len(parts) > 1 && (parts[0] == "s3" || strings.HasPrefix(parts[0], "s3-")) {
		if parts[0] == "s3" {
			return parts[1]
		}
		return strings.TrimPrefix(parts[0], "s3-")
	}
	return "us-east-1"
}

func signAWSv4Put(req *http.Request, region, accessKey, secretKey, payloadHash string) {
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
	canonicalHeaders := "host:" + host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		"",
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
