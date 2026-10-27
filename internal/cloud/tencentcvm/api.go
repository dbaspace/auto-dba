package tencentcvm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloud/tencentapi"
)

const (
	apiHost    = "cvm.tencentcloudapi.com"
	apiVersion = "2017-03-12"
	apiService = "cvm"
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
	// 主机：可重启、变配；无数据库主备切换/参数/慢日志；本期不开放创建（需镜像/VPC 等复杂参数）。
	return cloud.Caps{ScaleDiskDown: false, Switchover: false, Parameters: false, Restart: true, Create: false, SlowLog: false}
}
func (Reader) Caps() cloud.Caps { return caps() }
func (Writer) Caps() cloud.Caps { return caps() }

func init() {
	c := &Client{}
	cloud.Register(cloud.EngineHost, Reader{c}, Writer{c})
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
		region = tencentapi.Str(row["Placement"])
	}
	if place, ok := row["Placement"].(map[string]any); ok {
		if region == "" || strings.Contains(region, "map[") {
			region = tencentapi.Str(place["Zone"])
		}
	}
	zone := ""
	if place, ok := row["Placement"].(map[string]any); ok {
		zone = tencentapi.Str(place["Zone"])
	}
	cpu := tencentapi.Num(row["CPU"])
	mem := tencentapi.Num(row["Memory"]) * 1024 // API 多为 GB
	if mem == 0 {
		mem = tencentapi.Num(row["Memory"])
	}
	host := ""
	if ips, ok := row["PrivateIpAddresses"].([]any); ok && len(ips) > 0 {
		host = tencentapi.Str(ips[0])
	}
	disk := 0
	if disks, ok := row["DataDisks"].([]any); ok {
		for _, d := range disks {
			if m, ok := d.(map[string]any); ok {
				disk += tencentapi.Num(m["DiskSize"])
			}
		}
	}
	if sys, ok := row["SystemDisk"].(map[string]any); ok {
		disk += tencentapi.Num(sys["DiskSize"])
	}
	st := strings.ToUpper(tencentapi.Str(row["InstanceState"]))
	status := strings.ToLower(st)
	switch st {
	case "RUNNING":
		status = "running"
	case "PENDING", "STARTING", "REBOOTING", "STOPPING":
		status = "changing"
	case "STOPPED":
		status = "stopped"
	case "SHUTDOWN", "TERMINATING":
		status = "offline"
	}
	return cloud.Instance{
		CloudID:  tencentapi.Str(row["InstanceId"]),
		Name:     tencentapi.Str(row["InstanceName"]),
		Region:   region,
		Status:   status,
		CPU:      cpu,
		MemoryMB: mem,
		DiskGB:   disk,
		Version:  tencentapi.Str(row["InstanceType"]),
		Zone:     zone,
		PayType:  tencentapi.Str(row["InstanceChargeType"]),
		ExpireAt: tencentapi.Str(row["ExpiredTime"]),
		Host:     host,
		Extras:   row,
	}
}

func (r Reader) ListSpecs(ctx context.Context, cred cloud.Cred, zone string) ([]cloud.Spec, error) {
	body := map[string]any{}
	if zone != "" {
		body["Filters"] = []map[string]any{{"Name": "zone", "Values": []string{zone}}}
	}
	raw, err := r.call(ctx, cred, "DescribeInstanceTypeConfigs", body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response struct {
			InstanceTypeConfigSet []struct {
				InstanceType string `json:"InstanceType"`
				CPU          int    `json:"CPU"`
				Memory       int    `json:"Memory"`
			} `json:"InstanceTypeConfigSet"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	var out []cloud.Spec
	for _, s := range resp.Response.InstanceTypeConfigSet {
		if s.CPU == 0 {
			continue
		}
		out = append(out, cloud.Spec{
			CPU: s.CPU, MemoryMB: s.Memory * 1024,
			Label: fmt.Sprintf("%s · %d核 %dGB", s.InstanceType, s.CPU, s.Memory),
		})
	}
	return out, nil
}

func (r Reader) QuoteCreate(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Quote, error) {
	return cloud.Quote{}, fmt.Errorf("%w：主机申请", cloud.ErrUnsupported)
}

func (r Reader) QuoteUpgrade(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Quote, error) {
	return cloud.Quote{}, nil
}

func (r Reader) ListSlowLogs(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time, offset, limit int) ([]cloud.SlowLog, int, error) {
	return nil, 0, fmt.Errorf("%w：主机慢日志", cloud.ErrUnsupported)
}

func (r Reader) ListParameters(ctx context.Context, cred cloud.Cred, cloudID string) ([]cloud.Param, error) {
	return nil, fmt.Errorf("%w：主机参数", cloud.ErrUnsupported)
}

func (r Reader) ListEvents(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time) ([]cloud.Event, error) {
	return nil, nil
}

func (r Reader) DescribeTask(ctx context.Context, cred cloud.Cred, cloudID, taskID string) (cloud.Task, error) {
	raw, err := r.call(ctx, cred, "DescribeInstances", map[string]any{
		"InstanceIds": []string{cloudID},
	})
	if err != nil {
		return cloud.Task{}, err
	}
	var resp struct {
		Response struct {
			InstanceSet []map[string]any `json:"InstanceSet"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	if len(resp.Response.InstanceSet) == 0 {
		return cloud.Task{ID: taskID, Status: "failed", Detail: "实例不存在"}, nil
	}
	st := strings.ToUpper(tencentapi.Str(resp.Response.InstanceSet[0]["InstanceState"]))
	task := cloud.Task{ID: taskID, Status: "running", Detail: st}
	if st == "RUNNING" {
		task.Status = "success"
	}
	if st == "SHUTDOWN" || st == "TERMINATING" {
		task.Status = "failed"
	}
	return task, nil
}

func (w Writer) Create(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：主机申请（需镜像与网络参数，本期不做）", cloud.ErrUnsupported)
}

func (w Writer) ModifySpec(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Task, error) {
	if in.InstanceType == "" {
		return cloud.Task{}, fmt.Errorf("主机变配需要 InstanceType")
	}
	raw, err := w.call(ctx, cred, "ResetInstancesType", map[string]any{
		"InstanceIds":  []string{in.CloudID},
		"InstanceType": in.InstanceType,
		"ForceStop":    false,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Restart(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "RebootInstances", map[string]any{
		"InstanceIds": []string{cloudID}, "ForceReboot": false,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Switchover(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：主机主备切换", cloud.ErrUnsupported)
}

func (w Writer) ModifyParameters(ctx context.Context, cred cloud.Cred, cloudID string, params []cloud.Param) (cloud.Task, error) {
	return cloud.Task{}, fmt.Errorf("%w：主机改参数", cloud.ErrUnsupported)
}

func parseTask(raw []byte) cloud.Task {
	var resp struct {
		Response struct {
			RequestId string `json:"RequestId"`
			TaskId    any    `json:"TaskId"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	id := tencentapi.Str(resp.Response.TaskId)
	if id == "" {
		id = resp.Response.RequestId
	}
	return cloud.Task{ID: id, Status: "running"}
}
