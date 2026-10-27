package tencentredis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloud/tencentapi"
)

const (
	apiHost    = "redis.tencentcloudapi.com"
	apiVersion = "2018-04-12"
	apiService = "redis"
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
	// Redis 无主备切换/参数工单接口按文档收敛；扩容只升内存；无慢日志。
	return cloud.Caps{ScaleDiskDown: false, Switchover: false, Parameters: false, Restart: false, Create: true, SlowLog: false}
}
func (Reader) Caps() cloud.Caps { return caps() }
func (Writer) Caps() cloud.Caps { return caps() }

func init() {
	c := &Client{}
	cloud.Register(cloud.EngineRedis, Reader{c}, Writer{c})
}

func (r Reader) ListInstances(ctx context.Context, cred cloud.Cred) ([]cloud.Instance, error) {
	var out []cloud.Instance
	offset := 0
	for {
		raw, err := r.call(ctx, cred, "DescribeInstances", map[string]any{"Limit": 100, "Offset": offset})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Response struct {
				TotalCount  int              `json:"TotalCount"`
				InstanceSet []map[string]any `json:"InstanceSet"`
			} `json:"Response"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
		for _, row := range resp.Response.InstanceSet {
			out = append(out, mapInstance(row, cred.Region))
		}
		offset += len(resp.Response.InstanceSet)
		if len(resp.Response.InstanceSet) == 0 || offset >= resp.Response.TotalCount {
			break
		}
	}
	return out, nil
}

func mapInstance(row map[string]any, region string) cloud.Instance {
	if region == "" {
		region = tencentapi.Str(row["Region"])
	}
	status := mapStatus(tencentapi.Num(row["Status"]))
	mem := tencentapi.Num(row["Size"]) // MB
	if mem == 0 {
		mem = tencentapi.Num(row["RedisShardSize"])
	}
	return cloud.Instance{
		CloudID:  tencentapi.Str(row["InstanceId"]),
		Name:     tencentapi.Str(row["InstanceName"]),
		Region:   region,
		Status:   status,
		MemoryMB: mem,
		Version:  tencentapi.Str(row["CurrentRedisVersion"]),
		Zone:     tencentapi.Str(row["Zone"]),
		PayType:  tencentapi.Str(row["BillingMode"]),
		ExpireAt: tencentapi.Str(row["DeadlineTime"]),
		Host:     tencentapi.Str(row["WanIp"]),
		Port:     tencentapi.Num(row["Port"]),
		Extras:   row,
	}
}

func mapStatus(n int) string {
	// 0：待初始化；1：流程中；2：运行中；-2：已隔离；-3：待删除
	switch n {
	case 2:
		return "running"
	case 1, 0:
		return "changing"
	case -2:
		return "isolated"
	case -3:
		return "offline"
	default:
		return fmt.Sprintf("%d", n)
	}
}

func (r Reader) ListSpecs(ctx context.Context, cred cloud.Cred, zone string) ([]cloud.Spec, error) {
	raw, err := r.call(ctx, cred, "DescribeProductInfo", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response struct {
			RegionSet []struct {
				ZoneSet []struct {
					ZoneName string `json:"ZoneName"`
					TypeSet  []struct {
						TypeName  string `json:"TypeName"`
						ShardSize []int  `json:"ShardSize"`
						Size      []int  `json:"Size"`
					} `json:"TypeSet"`
				} `json:"ZoneSet"`
			} `json:"RegionSet"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	var out []cloud.Spec
	seen := map[int]bool{}
	for _, reg := range resp.Response.RegionSet {
		for _, z := range reg.ZoneSet {
			if zone != "" && z.ZoneName != zone {
				continue
			}
			for _, typ := range z.TypeSet {
				for _, sz := range typ.Size {
					if sz <= 0 || seen[sz] {
						continue
					}
					seen[sz] = true
					out = append(out, cloud.Spec{MemoryMB: sz, Label: fmt.Sprintf("%dMB", sz)})
				}
				for _, sz := range typ.ShardSize {
					if sz <= 0 || seen[sz] {
						continue
					}
					seen[sz] = true
					out = append(out, cloud.Spec{MemoryMB: sz, Label: fmt.Sprintf("%dMB", sz)})
				}
			}
		}
	}
	return out, nil
}

func (r Reader) QuoteCreate(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Quote, error) {
	return cloud.Quote{}, nil
}

func (r Reader) QuoteUpgrade(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Quote, error) {
	return cloud.Quote{}, nil
}

func (r Reader) ListSlowLogs(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time, offset, limit int) ([]cloud.SlowLog, int, error) {
	return nil, 0, fmt.Errorf("%w：Redis 慢日志", cloud.ErrUnsupported)
}

func (r Reader) ListParameters(ctx context.Context, cred cloud.Cred, cloudID string) ([]cloud.Param, error) {
	return nil, fmt.Errorf("%w：Redis 参数", cloud.ErrUnsupported)
}

func (r Reader) ListEvents(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time) ([]cloud.Event, error) {
	return nil, nil
}

func (r Reader) DescribeTask(ctx context.Context, cred cloud.Cred, cloudID, taskID string) (cloud.Task, error) {
	raw, err := r.call(ctx, cred, "DescribeInstanceStatus", map[string]any{"InstanceId": cloudID})
	if err != nil {
		// 回退到列表状态
		list, e := r.ListInstances(ctx, cred)
		if e != nil {
			return cloud.Task{}, err
		}
		for _, it := range list {
			if it.CloudID == cloudID {
				st := "running"
				if it.Status != "running" {
					st = "running"
					if it.Status == "isolated" || it.Status == "offline" {
						return cloud.Task{ID: taskID, Status: "failed", Detail: it.Status}, nil
					}
					if it.Status == "changing" {
						return cloud.Task{ID: taskID, Status: "running", Detail: it.Status}, nil
					}
				}
				return cloud.Task{ID: taskID, Status: "success", Detail: st}, nil
			}
		}
		return cloud.Task{ID: taskID, Status: "failed", Detail: "实例不存在"}, nil
	}
	var resp struct {
		Response struct {
			Status int `json:"Status"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	st := mapStatus(resp.Response.Status)
	task := cloud.Task{ID: taskID, Status: "running", Detail: st}
	if st == "running" {
		task.Status = "success"
	}
	if st == "isolated" || st == "offline" {
		task.Status = "failed"
	}
	return task, nil
}

func (w Writer) Create(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Task, error) {
	mem := in.MemoryMB
	if mem <= 0 {
		mem = 1024
	}
	raw, err := w.call(ctx, cred, "CreateInstances", map[string]any{
		"ZoneId": 0, "TypeId": 2, "MemSize": mem, "GoodsNum": 1, "Period": max(in.Period, 1),
		"BillingMode": 0, "Password": in.Password, "InstanceName": in.Name,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) ModifySpec(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Task, error) {
	if in.MemoryMB <= 0 {
		return cloud.Task{}, fmt.Errorf("Redis 变配需要目标内存 MB")
	}
	raw, err := w.call(ctx, cred, "UpgradeInstance", map[string]any{
		"InstanceId": in.CloudID, "MemSize": in.MemoryMB,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Restart(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Redis 重启", cloud.ErrUnsupported)
}

func (w Writer) Switchover(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Redis 主备切换", cloud.ErrUnsupported)
}

func (w Writer) ModifyParameters(ctx context.Context, cred cloud.Cred, cloudID string, params []cloud.Param) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：Redis 改参数", cloud.ErrUnsupported)
}

func parseTask(raw []byte) cloud.Task {
	var resp struct {
		Response struct {
			TaskId      any      `json:"TaskId"`
			InstanceIds []string `json:"InstanceIds"`
			DealId      string   `json:"DealId"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	id := tencentapi.Str(resp.Response.TaskId)
	if id == "" {
		id = resp.Response.DealId
	}
	detail := ""
	if len(resp.Response.InstanceIds) > 0 {
		detail = resp.Response.InstanceIds[0]
	}
	return cloud.Task{ID: id, Status: "running", Detail: detail}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
