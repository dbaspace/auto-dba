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
	inst := cloud.Instance{
		CloudID: tencentapi.Str(row["InstanceId"]),
		Name:    tencentapi.Str(row["InstanceName"]),
		Region:  region,
		Status:  mapStatus(tencentapi.Num(row["Status"])),
		Version: tencentapi.Str(row["Version"]),
		Zone:    tencentapi.Str(row["ZoneId"]),
		Extras:  row,
	}
	applyKafkaFacts(&inst)
	return inst
}

// Enrich 用实例属性补磁盘容量、主题和分区配额。清单里已有的版本不被覆盖。
func (r Reader) Enrich(ctx context.Context, cred cloud.Cred, inst *cloud.Instance) error {
	if inst == nil || strings.TrimSpace(inst.CloudID) == "" {
		return nil
	}
	raw, err := r.call(ctx, cred, "DescribeInstanceAttributes", map[string]any{"InstanceId": inst.CloudID})
	if err != nil {
		return err
	}
	var resp struct {
		Response struct {
			Result map[string]any `json:"Result"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	if len(resp.Response.Result) == 0 {
		return fmt.Errorf("实例属性为空")
	}
	if inst.Extras == nil {
		inst.Extras = map[string]any{}
	}
	for k, v := range resp.Response.Result {
		if v == nil || v == "" {
			continue
		}
		inst.Extras[k] = v
	}
	applyKafkaFacts(inst)
	return nil
}

func applyKafkaFacts(inst *cloud.Instance) {
	if inst == nil {
		return
	}
	row := inst.Extras
	if row == nil {
		row = map[string]any{}
		inst.Extras = row
	}
	if disk := kafkaDiskGB(row); disk > 0 {
		inst.DiskGB = disk
	}
	if inst.Version == "" {
		if v := tencentapi.Str(row["Version"]); v != "" {
			inst.Version = v
		}
	}
	host, port := cloud.NetFromExtras(cloud.EngineKafka, row)
	if host != "" {
		row["Vip"] = host
		inst.Host = host
	}
	if port > 0 {
		row["Vport"] = port
		inst.Port = port
	}
	if n := kafkaTopicLimit(row); n > 0 {
		row["kafka_topic_limit"] = n
	}
	if n := kafkaTopicUsed(row); n > 0 {
		row["kafka_topic_used"] = n
	}
	if n := kafkaPartitionLimit(row); n > 0 {
		row["kafka_partition_limit"] = n
	}
	if n := kafkaPartitionUsed(row); n > 0 {
		row["kafka_partition_used"] = n
	}
	if typ := tencentapi.Str(row["InstanceType"]); typ != "" {
		row["kafka_instance_type"] = typ
	}
}

func kafkaDiskGB(row map[string]any) int {
	if n := tencentapi.Num(row["DiskSize"]); n > 0 {
		return n
	}
	cfg, ok := row["DynamicDiskConfig"].(map[string]any)
	if !ok {
		return 0
	}
	return tencentapi.Num(cfg["DiskQuota"])
}

func kafkaTopicUsed(row map[string]any) int {
	if n := tencentapi.Num(row["CreatedTopics"]); n > 0 {
		return n
	}
	return tencentapi.Num(row["TopicNum"])
}

func kafkaTopicLimit(row map[string]any) int {
	if n := tencentapi.Num(row["MaxTopicNumber"]); n > 0 {
		return n
	}
	created := tencentapi.Num(row["CreatedTopics"])
	remain := tencentapi.Num(row["RemainingTopics"])
	if created > 0 || remain > 0 {
		return created + remain
	}
	return 0
}

func kafkaPartitionLimit(row map[string]any) int {
	if n := tencentapi.Num(row["MaxPartitionNumber"]); n > 0 {
		return n
	}
	created := tencentapi.Num(row["CreatedPartitions"])
	remain := tencentapi.Num(row["RemainingPartitions"])
	if created > 0 || remain > 0 {
		return created + remain
	}
	return 0
}

func kafkaPartitionUsed(row map[string]any) int {
	if n := tencentapi.Num(row["CreatedPartitions"]); n > 0 {
		return n
	}
	return tencentapi.Num(row["PartitionNum"])
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
