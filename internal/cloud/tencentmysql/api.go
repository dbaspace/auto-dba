package tencentmysql

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
	apiHost    = "cdb.tencentcloudapi.com"
	apiVersion = "2017-03-20"
	apiService = "cdb"
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
	return cloud.Caps{ScaleDiskDown: false, Switchover: true, Parameters: true, Restart: true, Create: true, SlowLog: true}
}
func (Reader) Caps() cloud.Caps { return caps() }
func (Writer) Caps() cloud.Caps { return caps() }

func init() {
	c := &Client{}
	cloud.Register(cloud.EngineMySQL, Reader{c}, Writer{c})
}

func (r Reader) ListInstances(ctx context.Context, cred cloud.Cred) ([]cloud.Instance, error) {
	var out []cloud.Instance
	offset := 0
	for {
		raw, err := r.call(ctx, cred, "DescribeDBInstances", map[string]any{"Limit": 100, "Offset": offset})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Response struct {
				TotalCount int              `json:"TotalCount"`
				Items      []map[string]any `json:"Items"`
			} `json:"Response"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
		for _, row := range resp.Response.Items {
			out = append(out, mapInstance(row, cred.Region))
		}
		offset += len(resp.Response.Items)
		if len(resp.Response.Items) == 0 || offset >= resp.Response.TotalCount {
			break
		}
	}
	return out, nil
}

func mapInstance(row map[string]any, region string) cloud.Instance {
	if region == "" {
		region = tencentapi.Str(row["Region"])
	}
	status := strings.ToLower(tencentapi.Str(row["Status"]))
	if status == "" {
		status = cloud.MapInstanceStatus(tencentapi.Str(row["InstanceType"]))
	}
	switch status {
	case "1", "running":
		status = "running"
	case "4", "isolated":
		status = "isolated"
	case "5", "offline":
		status = "offline"
	}
	return cloud.Instance{
		CloudID:  tencentapi.Str(row["InstanceId"]),
		Name:     tencentapi.Str(row["InstanceName"]),
		Region:   region,
		Status:   status,
		CPU:      tencentapi.Num(row["Cpu"]),
		MemoryMB: tencentapi.Num(row["Memory"]),
		DiskGB:   tencentapi.Num(row["Volume"]),
		Version:  tencentapi.Str(row["EngineVersion"]),
		Zone:     tencentapi.Str(row["Zone"]),
		PayType:  tencentapi.Str(row["PayType"]),
		ExpireAt: tencentapi.Str(row["DeadlineTime"]),
		Host:     tencentapi.Str(row["Vip"]),
		Port:     tencentapi.Num(row["Vport"]),
		Extras:   row,
	}
}

func (r Reader) ListSpecs(ctx context.Context, cred cloud.Cred, zone string) ([]cloud.Spec, error) {
	raw, err := r.call(ctx, cred, "DescribeDBZoneConfig", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response struct {
			Items []struct {
				Zone    string `json:"Zone"`
				Configs []struct {
					Device string `json:"Device"`
					Cpu    int    `json:"Cpu"`
					Memory int    `json:"Memory"`
				} `json:"Configs"`
			} `json:"Items"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	var out []cloud.Spec
	seen := map[string]bool{}
	for _, z := range resp.Response.Items {
		if zone != "" && z.Zone != zone {
			continue
		}
		for _, c := range z.Configs {
			key := fmt.Sprintf("%d-%d", c.Cpu, c.Memory)
			if c.Cpu == 0 || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, cloud.Spec{CPU: c.Cpu, MemoryMB: c.Memory, Label: fmt.Sprintf("%d核 %dMB", c.Cpu, c.Memory)})
		}
	}
	return out, nil
}

func (r Reader) QuoteCreate(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Quote, error) {
	raw, err := r.call(ctx, cred, "DescribeDBPrice", map[string]any{
		"Zone": in.Zone, "GoodsNum": 1, "Memory": in.MemoryMB, "Volume": in.DiskGB, "Period": max(in.Period, 1),
	})
	if err != nil {
		return cloud.Quote{}, err
	}
	return parseQuote(raw), nil
}

func (r Reader) QuoteUpgrade(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Quote, error) {
	raw, err := r.call(ctx, cred, "DescribeDBPrice", map[string]any{
		"InstanceId": in.CloudID, "Memory": in.MemoryMB, "Volume": in.DiskGB,
	})
	if err != nil {
		return cloud.Quote{}, err
	}
	return parseQuote(raw), nil
}

func parseQuote(raw []byte) cloud.Quote {
	var resp struct {
		Response map[string]any `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	q := cloud.Quote{}
	for _, k := range []string{"Price", "OriginalPrice", "RealTotalCost"} {
		if v, ok := resp.Response[k]; ok {
			q.Amount = float64(tencentapi.Num(v))
			if f, ok := v.(float64); ok {
				q.Amount = f
			}
		}
	}
	b, _ := json.Marshal(resp.Response)
	q.Detail = string(b)
	return q
}

func (r Reader) ListSlowLogs(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time, offset, limit int) ([]cloud.SlowLog, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	raw, err := r.call(ctx, cred, "DescribeSlowLogData", map[string]any{
		"InstanceId": cloudID,
		"StartTime":  from.Unix(),
		"EndTime":    to.Unix(),
		"Offset":     offset,
		"Limit":      limit,
	})
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Response struct {
			TotalCount int              `json:"TotalCount"`
			Items      []map[string]any `json:"Items"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, err
	}
	var out []cloud.SlowLog
	for _, row := range resp.Response.Items {
		sqlText := tencentapi.Str(row["SqlText"])
		at := time.Unix(int64(tencentapi.Num(row["Timestamp"])), 0)
		if at.IsZero() {
			at = tencentapi.ParseShanghai(tencentapi.Str(row["Timestamp"]))
		}
		rawRow, _ := json.Marshal(row)
		out = append(out, cloud.SlowLog{
			Time: at, DBName: tencentapi.Str(row["Database"]), User: tencentapi.Str(row["UserName"]),
			Client: tencentapi.Str(row["UserHost"]), DurationMS: tencentapi.Num(row["QueryTime"]) * 1000,
			SQL: sqlText, Raw: string(rawRow),
		})
	}
	return out, resp.Response.TotalCount, nil
}

func (r Reader) ListParameters(ctx context.Context, cred cloud.Cred, cloudID string) ([]cloud.Param, error) {
	raw, err := r.call(ctx, cred, "DescribeInstanceParams", map[string]any{"InstanceId": cloudID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response struct {
			Items []struct {
				Name  string `json:"Name"`
				Value string `json:"CurrentValue"`
			} `json:"Items"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	var out []cloud.Param
	for _, p := range resp.Response.Items {
		out = append(out, cloud.Param{Name: p.Name, Value: p.Value})
	}
	return out, nil
}

func (r Reader) ListEvents(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time) ([]cloud.Event, error) {
	return nil, nil
}

func (r Reader) DescribeTask(ctx context.Context, cred cloud.Cred, cloudID, taskID string) (cloud.Task, error) {
	raw, err := r.call(ctx, cred, "DescribeDBInstances", map[string]any{
		"InstanceIds": []string{cloudID},
	})
	if err != nil {
		return cloud.Task{}, err
	}
	var resp struct {
		Response struct {
			Items []map[string]any `json:"Items"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	if len(resp.Response.Items) == 0 {
		return cloud.Task{ID: taskID, Status: "failed", Detail: "实例不存在"}, nil
	}
	st := strings.ToLower(tencentapi.Str(resp.Response.Items[0]["Status"]))
	task := cloud.Task{ID: taskID, Status: "running", Detail: st}
	if st == "1" || st == "running" {
		task.Status = "success"
	}
	if st == "4" || st == "5" || st == "isolated" || st == "offline" {
		task.Status = "failed"
	}
	return task, nil
}

func (w Writer) Create(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "CreateDBInstanceHour", map[string]any{
		"GoodsNum": 1, "Memory": in.MemoryMB, "Volume": in.DiskGB, "Zone": in.Zone,
		"EngineVersion": "8.0", "InstanceName": in.Name, "Password": in.Password,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) ModifySpec(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Task, error) {
	body := map[string]any{"InstanceId": in.CloudID}
	if in.MemoryMB > 0 {
		body["Memory"] = in.MemoryMB
	}
	if in.DiskGB > 0 {
		body["Volume"] = in.DiskGB
	}
	if in.CPU > 0 {
		body["Cpu"] = in.CPU
	}
	raw, err := w.call(ctx, cred, "UpgradeDBInstance", body)
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Restart(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "RestartDBInstances", map[string]any{"InstanceIds": []string{cloudID}})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Switchover(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "SwitchDBInstanceMasterSlave", map[string]any{
		"InstanceId": cloudID, "DstSlave": "first", "ForceSwitch": false,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) ModifyParameters(ctx context.Context, cred cloud.Cred, cloudID string, params []cloud.Param) (cloud.Task, error) {
	list := make([]map[string]string, 0, len(params))
	for _, p := range params {
		list = append(list, map[string]string{"Name": p.Name, "CurrentValue": p.Value})
	}
	raw, err := w.call(ctx, cred, "ModifyInstanceParam", map[string]any{"InstanceIds": []string{cloudID}, "ParamList": list})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func parseTask(raw []byte) cloud.Task {
	var resp struct {
		Response struct {
			AsyncRequestId string   `json:"AsyncRequestId"`
			InstanceIds    []string `json:"InstanceIds"`
			DealIds        []string `json:"DealIds"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	id := resp.Response.AsyncRequestId
	if id == "" && len(resp.Response.DealIds) > 0 {
		id = resp.Response.DealIds[0]
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
