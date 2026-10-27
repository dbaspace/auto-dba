package tencentapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/cloud"
)

// Client 腾讯云 TC3 公共调用。各产品包填 Host/Service/Version。
type Client struct {
	Host    string
	Service string
	Version string
	Do      func(*http.Request) (*http.Response, error)
}

func (c *Client) Call(ctx context.Context, cred cloud.Cred, action string, payload any) ([]byte, error) {
	if c == nil || c.Host == "" || c.Service == "" || c.Version == "" {
		return nil, fmt.Errorf("tencentapi: client 未配置")
	}
	if err := cloud.DefaultLimiter().Wait(ctx, cred.AccountID); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+c.Host+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range TC3Headers(cred.SecretID, cred.SecretKey, c.Service, c.Host, action, c.Version, cred.Region, body, time.Now().UTC()) {
		req.Header.Set(k, v)
	}
	do := c.Do
	if do == nil {
		do = (&http.Client{Timeout: 45 * time.Second}).Do
	}
	resp, err := do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s HTTP %d: %s", action, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var probe struct {
		Response struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.Response.Error != nil && probe.Response.Error.Code != "" {
		err := fmt.Errorf("%s: %s %s", action, probe.Response.Error.Code, probe.Response.Error.Message)
		if probe.Response.Error.Code == "RequestLimitExceeded" {
			return nil, fmt.Errorf("%w: %s", cloud.ErrRateLimit, err.Error())
		}
		return nil, err
	}
	return raw, nil
}

func TC3Headers(secretID, secretKey, service, host, action, version, region string, payload []byte, ts time.Time) map[string]string {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	ts = ts.UTC()
	timestamp := fmt.Sprintf("%d", ts.Unix())
	date := ts.Format("2006-01-02")
	hashedPayload := sha256Hex(payload)
	canonicalHeaders := "content-type:application/json\nhost:" + host + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + hashedPayload
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + timestamp + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	sig := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	return map[string]string{
		"Content-Type":   "application/json",
		"Host":           host,
		"X-TC-Action":    action,
		"X-TC-Version":   version,
		"X-TC-Timestamp": timestamp,
		"X-TC-Region":    region,
		"Authorization":  "TC3-HMAC-SHA256 Credential=" + secretID + "/" + credentialScope + ", SignedHeaders=" + signedHeaders + ", Signature=" + sig,
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(msg))
	return m.Sum(nil)
}

func Shanghai(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	if t.IsZero() {
		t = time.Now()
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}

func ParseShanghai(s string) time.Time {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(s), loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

func Num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i := 0
		fmt.Sscan(strings.TrimSpace(n), &i)
		return i
	default:
		i := 0
		fmt.Sscan(fmt.Sprint(v), &i)
		return i
	}
}

func Str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
