package flashback

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	flashbackWALSourceCloudAliyun = "cloud_aliyun"
	flashbackAliyunAPIHost        = "rds.aliyuncs.com"
	flashbackAliyunAPIVersion     = "2014-08-15"
)

// flashbackAliyunWALProvider 阿里云 RDS PostgreSQL 日志备份下载（DescribeBinlogFiles 风格 RPC）。
// 若实例为 PolarDB/专用 API，可后续按产品拆分；当前以 RDS OpenAPI 为准。
type flashbackAliyunWALProvider struct {
	accessKeyID     string
	accessKeySecret string
	region          string
	client          *http.Client
}

func flashbackNewAliyunWALProvider(ctx context.Context, region string) (*flashbackAliyunWALProvider, error) {
	id, key, err := flashbackCloudVendorCreds(ctx, flashbackVendorAliyun)
	if err != nil {
		return nil, err
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "cn-hangzhou"
	}
	return &flashbackAliyunWALProvider{
		accessKeyID:     id,
		accessKeySecret: key,
		region:          region,
		client:          &http.Client{Timeout: 45 * time.Second},
	}, nil
}

func (p *flashbackAliyunWALProvider) Vendor() string { return flashbackVendorAliyun }

func (p *flashbackAliyunWALProvider) ListByTime(ctx context.Context, spec flashbackCloudWALSpec) ([]flashbackCloudWALObject, error) {
	if strings.TrimSpace(spec.InstanceID) == "" {
		return nil, fmt.Errorf("阿里云 DBInstanceId 为空，请填写 cloud_instance_id（如 rm-xxxx）")
	}
	params := map[string]string{
		"Action":       "DescribeBinlogFiles",
		"Version":      flashbackAliyunAPIVersion,
		"DBInstanceId": spec.InstanceID,
		"StartTime":    spec.From.UTC().Format("2006-01-02T15:04Z"),
		"EndTime":      spec.To.UTC().Format("2006-01-02T15:04Z"),
		"PageSize":     "100",
		"PageNumber":   "1",
		"RegionId":     p.region,
	}
	body, err := p.rpc(ctx, params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items struct {
			BinLogFile []struct {
				LogFileName      string `json:"LogFileName"`
				FileSize         int64  `json:"FileSize"`
				LogBeginTime     string `json:"LogBeginTime"`
				LogEndTime       string `json:"LogEndTime"`
				DownloadLink     string `json:"DownloadLink"`
				LinkExpiredTime  string `json:"LinkExpiredTime"`
				HostInstanceID   string `json:"HostInstanceID"`
			} `json:"BinLogFile"`
		} `json:"Items"`
		Message   string `json:"Message"`
		Code      string `json:"Code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析阿里云日志列表失败: %w; body=%s", err, truncate(string(body), 400))
	}
	if resp.Code != "" && !strings.EqualFold(resp.Code, "200") {
		return nil, fmt.Errorf("阿里云 DescribeBinlogFiles: %s %s", resp.Code, resp.Message)
	}
	var out []flashbackCloudWALObject
	for _, f := range resp.Items.BinLogFile {
		st, _ := time.Parse(time.RFC3339, normalizeAliyunTime(f.LogBeginTime))
		et, _ := time.Parse(time.RFC3339, normalizeAliyunTime(f.LogEndTime))
		out = append(out, flashbackCloudWALObject{
			ID:         f.LogFileName,
			Name:       f.LogFileName,
			Size:       f.FileSize,
			StartTime:  st,
			FinishTime: et,
			State:      "Success",
		})
	}
	return out, nil
}

func (p *flashbackAliyunWALProvider) DownloadURL(ctx context.Context, spec flashbackCloudWALSpec, obj flashbackCloudWALObject) (string, error) {
	// DescribeBinlogFiles 已含 DownloadLink；再查一次取最新链接
	list, err := p.ListByTime(ctx, spec)
	if err != nil {
		return "", err
	}
	for _, it := range list {
		if it.Name == obj.Name || it.ID == obj.ID {
			// 二次请求拿 DownloadLink：带 FileName 过滤
			params := map[string]string{
				"Action":       "DescribeBinlogFiles",
				"Version":      flashbackAliyunAPIVersion,
				"DBInstanceId": spec.InstanceID,
				"StartTime":    spec.From.UTC().Format("2006-01-02T15:04Z"),
				"EndTime":      spec.To.UTC().Format("2006-01-02T15:04Z"),
				"PageSize":     "100",
				"PageNumber":   "1",
				"RegionId":     p.region,
			}
			body, err := p.rpc(ctx, params)
			if err != nil {
				return "", err
			}
			var resp struct {
				Items struct {
					BinLogFile []struct {
						LogFileName  string `json:"LogFileName"`
						DownloadLink string `json:"DownloadLink"`
					} `json:"BinLogFile"`
				} `json:"Items"`
			}
			_ = json.Unmarshal(body, &resp)
			for _, f := range resp.Items.BinLogFile {
				if f.LogFileName == obj.Name || f.LogFileName == obj.ID {
					if strings.TrimSpace(f.DownloadLink) == "" {
						return "", fmt.Errorf("阿里云未返回 DownloadLink: %s", obj.Name)
					}
					return f.DownloadLink, nil
				}
			}
		}
	}
	return "", fmt.Errorf("未找到日志包下载链接: %s", obj.Name)
}

func (p *flashbackAliyunWALProvider) rpc(ctx context.Context, params map[string]string) ([]byte, error) {
	params["Format"] = "JSON"
	params["AccessKeyId"] = p.accessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = fmt.Sprintf("%d", time.Now().UnixNano())
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	params["Signature"] = flashbackAliyunSign("GET", params, p.accessKeySecret)

	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	u := "https://" + flashbackAliyunAPIHost + "/?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("阿里云 HTTP %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	return body, nil
}

func flashbackAliyunSign(method string, params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(percentEncode(k))
		b.WriteByte('=')
		b.WriteString(percentEncode(params[k]))
	}
	stringToSign := method + "&" + percentEncode("/") + "&" + percentEncode(b.String())
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func percentEncode(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(s), "+", "%20"), "*", "%2A"), "%7E", "~")
}

func normalizeAliyunTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, "Z") {
		return s
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s+"Z"); err == nil {
		return t.Format(time.RFC3339)
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func flashbackFindAliyunInstanceID(res *ResourceDbsInfo, override string) (id, reason string) {
	if id := strings.TrimSpace(override); id != "" && (strings.HasPrefix(id, "rm-") || strings.HasPrefix(id, "pgm-")) {
		return id, "任务入参 cloud_instance_id=" + id
	}
	if res == nil {
		return "", ""
	}
	for _, k := range []string{"cloud_instance_id", "db_instance_id", "aliyun_instance_id", "pg"} {
		raw := strings.TrimSpace(flashbackTagRaw(res.Tags, k))
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "rm-") || strings.HasPrefix(raw, "pgm-") {
			return raw, "实例标签 " + k + "=" + raw
		}
	}
	return "", ""
}
