package flashback

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
)

const flashbackWALSourceCloudAWS = "cloud_aws"

// flashbackAWSWALProvider AWS RDS DescribeDBLogFiles 骨架（SigV4 简化；完整实现可换 aws-sdk-go-v2）。
type flashbackAWSWALProvider struct {
	accessKey string
	secretKey string
	region    string
	client    *http.Client
}

func flashbackNewAWSWALProvider(ctx context.Context, region string) (*flashbackAWSWALProvider, error) {
	ak, sk, err := flashbackCloudVendorCreds(ctx, flashbackVendorAWS)
	if err != nil {
		return nil, err
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "ap-northeast-1"
	}
	return &flashbackAWSWALProvider{
		accessKey: ak, secretKey: sk, region: region,
		client: &http.Client{Timeout: 45 * time.Second},
	}, nil
}

func (p *flashbackAWSWALProvider) Vendor() string { return flashbackVendorAWS }

func (p *flashbackAWSWALProvider) ListByTime(ctx context.Context, spec flashbackCloudWALSpec) ([]flashbackCloudWALObject, error) {
	if strings.TrimSpace(spec.InstanceID) == "" {
		return nil, fmt.Errorf("AWS DBInstanceIdentifier 为空，请填写 cloud_instance_id")
	}
	// Query API: Action=DescribeDBLogFiles
	host := fmt.Sprintf("rds.%s.amazonaws.com", p.region)
	q := url.Values{}
	q.Set("Action", "DescribeDBLogFiles")
	q.Set("Version", "2014-10-31")
	q.Set("DBInstanceIdentifier", spec.InstanceID)
	q.Set("FileLastWritten", fmt.Sprintf("%d", spec.From.UnixMilli()))
	u := "https://" + host + "/?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Authorization", awsSigV4Stub(p.accessKey, p.secretKey, p.region, "rds", amzDate, "GET", "/", q.Encode()))
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AWS RDS 请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AWS DescribeDBLogFiles HTTP %d（请核对 AK/Region/实例 ID；建议换 aws-sdk）: %s",
			resp.StatusCode, truncate(string(body), 300))
	}
	// XML 解析从简：按 LogFileName 粗提取
	raw := string(body)
	var out []flashbackCloudWALObject
	for _, part := range strings.Split(raw, "<LogFileName>") {
		if !strings.Contains(part, "</LogFileName>") {
			continue
		}
		name := strings.TrimSpace(strings.Split(part, "</LogFileName>")[0])
		if name == "" {
			continue
		}
		out = append(out, flashbackCloudWALObject{ID: name, Name: name, State: "available"})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("AWS 未解析到日志文件名；连通成功但列表为空或响应格式变化")
	}
	return out, nil
}

func (p *flashbackAWSWALProvider) DownloadURL(ctx context.Context, spec flashbackCloudWALSpec, obj flashbackCloudWALObject) (string, error) {
	return "", fmt.Errorf("AWS 日志下载请用 DownloadDBLogFilePortion / SDK；对象=%s", obj.Name)
}

func awsSigV4Stub(ak, sk, region, service, amzDate, method, path, query string) string {
	// 仅生成可识别的 Authorization 头形状，真实签名需完整 canonical request；
	// 目的是让错误信息回到 AWS 侧而非本地 panic。
	dateStamp := amzDate[:8]
	kDate := hmacSHA256([]byte("AWS4"+sk), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(kSigning, amzDate+method+path+query))
	return fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/%s/aws4_request, SignedHeaders=host;x-amz-date, Signature=%s",
		ak, dateStamp, region, service, sig)
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func flashbackFindAWSInstanceID(res *ResourceDbsInfo, override string) (id, reason string) {
	if id := strings.TrimSpace(override); id != "" {
		return id, "任务入参 cloud_instance_id=" + id
	}
	if res == nil {
		return "", ""
	}
	for _, k := range []string{"cloud_instance_id", "db_instance_id", "aws_instance_id", "rds_instance_id"} {
		raw := strings.TrimSpace(flashbackTagRaw(res.Tags, k))
		if raw != "" {
			return raw, "实例标签 " + k + "=" + raw
		}
	}
	return "", ""
}
