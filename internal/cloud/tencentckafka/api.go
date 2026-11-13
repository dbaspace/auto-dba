package tencentckafka

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloud/tencentapi"
)

const (
	apiHost    = "ckafka.tencentcloudapi.com"
	apiVersion = "2019-08-19"
	apiService = "ckafka"
	pageLimit  = 20
	bandwidth  = 20
)

type Client struct {
	Host string
	Do   func(*http.Request) (*http.Response, error)
}

func (c *Client) api() *tencentapi.Client {
	host := apiHost
	if c != nil && c.Host != "" {
		host = c.Host
	}
	var do func(*http.Request) (*http.Response, error)
	if c != nil {
		do = c.Do
	}
	return &tencentapi.Client{Host: host, Service: apiService, Version: apiVersion, Do: do}
}

func (c *Client) call(ctx context.Context, cred cloud.Cred, action string, payload any) ([]byte, error) {
	return c.api().Call(ctx, cred, action, payload)
}

type Reader struct{ *Client }
type Writer struct{ *Client }

func (Reader) Vendor() string { return cloud.VendorTencent }
func (Writer) Vendor() string { return cloud.VendorTencent }

func caps() cloud.Caps {
	return cloud.Caps{Create: true}
}

func (Reader) Caps() cloud.Caps { return caps() }
func (Writer) Caps() cloud.Caps { return caps() }

func init() {
	c := &Client{}
	cloud.Register(cloud.EngineKafka, Reader{c}, Writer{c})
}

func (r Reader) ListInstances(ctx context.Context, cred cloud.Cred) ([]cloud.Instance, error) {
	var out []cloud.Instance
	offset := 0
	for {
		raw, err := r.call(ctx, cred, "DescribeInstances", map[string]any{"Limit": pageLimit, "Offset": offset})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Response struct {
				Result struct {
					TotalCount   int              `json:"TotalCount"`
					InstanceList []map[string]any `json:"InstanceList"`
				} `json:"Result"`
			} `json:"Response"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
		rows := resp.Response.Result.InstanceList
		for _, row := range rows {
			out = append(out, mapInstance(row, cred.Region))
		}
		offset += len(rows)
		total := resp.Response.Result.TotalCount
		if len(rows) == 0 || (total > 0 && offset >= total) {
			break
		}
	}
	return out, nil
}

func mapInstance(row map[string]any, region string) cloud.Instance {
	if region == "" {
		region = tencentapi.Str(row["Region"])
	}
	port := tencentapi.Num(row["Vport"])
	return cloud.Instance{
		CloudID: tencentapi.Str(row["InstanceId"]),
		Name:    tencentapi.Str(row["InstanceName"]),
		Region:  region,
		Status:  mapStatus(tencentapi.Num(row["Status"])),
		DiskGB:  tencentapi.Num(row["DiskSize"]),
		Version: tencentapi.Str(row["Version"]),
		Zone:    tencentapi.Str(row["ZoneId"]),
		Host:    tencentapi.Str(row["Vip"]),
		Port:    port,
		Extras:  row,
	}
}

func mapStatus(n int) string {
	// 0 创建中，1 运行中，2 删除中，5 隔离中，-1 创建失败。
	switch n {
	case 1:
		return "running"
	case 0, 2:
		return "changing"
	case 5:
		return "isolated"
	case -1:
		return "offline"
	default:
		return fmt.Sprintf("%d", n)
	}
}

func (r Reader) ListSpecs(ctx context.Context, cred cloud.Cred, zone string) ([]cloud.Spec, error) {
	return nil, fmt.Errorf("%w：Kafka 规格", cloud.ErrUnsupported)
}

func (r Reader) QuoteCreate(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Quote, error) {
	return cloud.Quote{Detail: "将创建 CKafka 实例"}, nil
}

func (r Reader) QuoteUpgrade(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Quote, error) {
	return cloud.Quote{}, fmt.Errorf("%w：Kafka 变配", cloud.ErrUnsupported)
}

func (r Reader) ListSlowLogs(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time, offset, limit int) ([]cloud.SlowLog, int, error) {
	return nil, 0, fmt.Errorf("%w：Kafka 慢日志", cloud.ErrUnsupported)
}

func (r Reader) ListParameters(ctx context.Context, cred cloud.Cred, cloudID string) ([]cloud.Param, error) {
	return nil, fmt.Errorf("%w：Kafka 参数", cloud.ErrUnsupported)
}

func (r Reader) ListEvents(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time) ([]cloud.Event, error) {
	return nil, nil
}

func (r Reader) DescribeTask(ctx context.Context, cred cloud.Cred, cloudID, taskID string) (cloud.Task, error) {
	list, err := r.ListInstances(ctx, cred)
	if err != nil {
		return cloud.Task{}, err
	}
	for _, it := range list {
		if it.CloudID != cloudID {
			continue
		}
		task := cloud.Task{ID: taskID, Status: "running", Detail: it.Status}
		switch it.Status {
		case "running":
			task.Status = "success"
		case "isolated", "offline":
			task.Status = "failed"
		}
		return task, nil
	}
	return cloud.Task{ID: taskID, Status: "running", Detail: "实例尚未出现在清单"}, nil
}

func (w Writer) Create(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Task, error) {
	zoneID, err := strconv.Atoi(strings.TrimSpace(in.Zone))
	if err != nil || zoneID <= 0 {
		return cloud.Task{}, fmt.Errorf("请填写可用区 ID")
	}
	if in.DiskGB <= 0 {
		return cloud.Task{}, fmt.Errorf("请填写磁盘")
	}
	if strings.TrimSpace(in.Name) == "" {
		return cloud.Task{}, fmt.Errorf("请填写实例名")
	}
	if strings.TrimSpace(in.VpcID) == "" || strings.TrimSpace(in.SubnetID) == "" {
		return cloud.Task{}, fmt.Errorf("请填写 VPC 和子网")
	}
	raw, err := w.call(ctx, cred, "CreateInstance", map[string]any{
		"InstanceName":     in.Name,
		"BandWidth":        bandwidth,
		"DiskSize":         in.DiskGB,
		"ZoneId":           zoneID,
		"VpcId":            in.VpcID,
		"SubnetId":         in.SubnetID,
		"DiskType":         "CLOUD_BASIC",
		"MsgRetentionTime": 24,
		"RenewFlag":        0,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseCreate(raw), nil
}

func (w Writer) ModifySpec(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Kafka 变配", cloud.ErrUnsupported)
}

func (w Writer) Restart(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Kafka 重启", cloud.ErrUnsupported)
}

func (w Writer) Switchover(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Kafka 主备切换", cloud.ErrUnsupported)
}

func (w Writer) ModifyParameters(ctx context.Context, cred cloud.Cred, cloudID string, params []cloud.Param) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Kafka 改参数", cloud.ErrUnsupported)
}

func parseCreate(raw []byte) cloud.Task {
	var resp struct {
		Response struct {
			Result struct {
				Data struct {
					FlowId     any    `json:"FlowId"`
					InstanceId string `json:"InstanceId"`
				} `json:"Data"`
			} `json:"Result"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	flow := tencentapi.Str(resp.Response.Result.Data.FlowId)
	inst := strings.TrimSpace(resp.Response.Result.Data.InstanceId)
	if flow == "" {
		flow = inst
	}
	return cloud.Task{ID: flow, Detail: inst, Status: "running"}
}
