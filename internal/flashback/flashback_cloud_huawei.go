package flashback

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const flashbackWALSourceCloudHuawei = "cloud_huawei"

// flashbackHuaweiWALProvider 华为云 RDS 日志列表骨架（需 project_id；完整下载建议官方 SDK）。
type flashbackHuaweiWALProvider struct {
	ak     string
	sk     string
	region string
	client *http.Client
}

func flashbackNewHuaweiWALProvider(ctx context.Context, region string) (*flashbackHuaweiWALProvider, error) {
	ak, sk, err := flashbackCloudVendorCreds(ctx, flashbackVendorHuawei)
	if err != nil {
		return nil, err
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "cn-north-4"
	}
	return &flashbackHuaweiWALProvider{
		ak: ak, sk: sk, region: region,
		client: &http.Client{Timeout: 45 * time.Second},
	}, nil
}

func (p *flashbackHuaweiWALProvider) Vendor() string { return flashbackVendorHuawei }

func (p *flashbackHuaweiWALProvider) ListByTime(ctx context.Context, spec flashbackCloudWALSpec) ([]flashbackCloudWALObject, error) {
	if strings.TrimSpace(spec.InstanceID) == "" {
		return nil, fmt.Errorf("华为云实例 ID 为空，请填写 cloud_instance_id")
	}
	host := fmt.Sprintf("rds.%s.myhuaweicloud.com", p.region)
	path := "/v3/instances/" + url.PathEscape(spec.InstanceID) + "/errorlog"
	q := url.Values{}
	q.Set("start_date", spec.From.UTC().Format("2006-01-02T15:04:05+0000"))
	q.Set("end_date", spec.To.UTC().Format("2006-01-02T15:04:05+0000"))
	full := "https://" + host + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	date := time.Now().UTC().Format("20060102T150405Z")
	req.Header.Set("X-Sdk-Date", date)
	req.Header.Set("X-Project-Id", "PLEASE_SET_HUAWEI_PROJECT_ID")
	req.Header.Set("Authorization", "SDK-HMAC-SHA256 Access="+p.ak+", SignedHeaders=x-sdk-date, Signature="+huaweiStubSign(p.sk, date))
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("华为云 RDS 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("华为云日志列表 HTTP %d（请配置 project_id/权限或换官方 SDK）: %s",
			resp.StatusCode, truncate(string(body), 300))
	}
	var parsed struct {
		ErrorLogList []struct {
			ID       string `json:"id"`
			FileName string `json:"file_name"`
			Status   string `json:"status"`
		} `json:"error_log_list"`
	}
	_ = json.Unmarshal(body, &parsed)
	var out []flashbackCloudWALObject
	for _, it := range parsed.ErrorLogList {
		name := strings.TrimSpace(it.FileName)
		if name == "" {
			name = strings.TrimSpace(it.ID)
		}
		out = append(out, flashbackCloudWALObject{ID: name, Name: name, State: it.Status})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("华为云未返回可用日志包；连通成功但列表为空，请确认已开通日志备份")
	}
	return out, nil
}

func (p *flashbackHuaweiWALProvider) DownloadURL(ctx context.Context, spec flashbackCloudWALSpec, obj flashbackCloudWALObject) (string, error) {
	return "", fmt.Errorf("华为云日志下载 URL 需 project_id + 官方 SDK；对象=%s", obj.Name)
}

func huaweiStubSign(sk, date string) string {
	mac := hmac.New(sha256.New, []byte(sk))
	mac.Write([]byte(date))
	return hex.EncodeToString(mac.Sum(nil))
}

func flashbackFindHuaweiInstanceID(res *ResourceDbsInfo, override string) (id, reason string) {
	if id := strings.TrimSpace(override); id != "" {
		return id, "任务入参 cloud_instance_id=" + id
	}
	if res == nil {
		return "", ""
	}
	for _, k := range []string{"cloud_instance_id", "db_instance_id", "huawei_instance_id"} {
		raw := strings.TrimSpace(flashbackTagRaw(res.Tags, k))
		if raw != "" {
			return raw, "实例标签 " + k + "=" + raw
		}
	}
	return "", ""
}
