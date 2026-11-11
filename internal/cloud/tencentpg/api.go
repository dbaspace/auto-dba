package tencentpg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloud/tencentapi"
)

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
	cloud.Register(cloud.EnginePostgres, Reader{c}, Writer{c})
}

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		i, _ := strconv.Atoi(fmt.Sprint(v))
		return i
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func (r Reader) ListInstances(ctx context.Context, cred cloud.Cred) ([]cloud.Instance, error) {
	var out []cloud.Instance
	offset := 0
	for {
		raw, err := r.call(ctx, cred, "DescribeDBInstances", map[string]any{
			"Limit": 100, "Offset": offset,
		})
		if err != nil {
			return nil, err
		}
		var resp struct {
			Response struct {
				TotalCount    int              `json:"TotalCount"`
				DBInstanceSet []map[string]any `json:"DBInstanceSet"`
			} `json:"Response"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
		for _, row := range resp.Response.DBInstanceSet {
			out = append(out, mapInstance(row, cred.Region))
		}
		offset += len(resp.Response.DBInstanceSet)
		if len(resp.Response.DBInstanceSet) == 0 || offset >= resp.Response.TotalCount {
			break
		}
	}
	return out, nil
}

func mapInstance(row map[string]any, region string) cloud.Instance {
	if region == "" {
		region = str(row["Region"])
	}
	var nets []any
	if raw, ok := row["DBInstanceNetInfo"].([]any); ok {
		nets = raw
	}
	host, port := cloud.PickPGIntranet(nets)
	return cloud.Instance{
		CloudID:  str(row["DBInstanceId"]),
		Name:     str(row["DBInstanceName"]),
		Region:   region,
		Status:   cloud.MapInstanceStatus(str(row["DBInstanceStatus"])),
		CPU:      num(row["DBInstanceCpu"]),
		MemoryMB: memoryMBFromInstanceGB(num(row["DBInstanceMemory"])),
		DiskGB:   num(row["DBInstanceStorage"]),
		Version:  str(row["DBVersion"]),
		Zone:     str(row["Zone"]),
		PayType:  str(row["PayType"]),
		ExpireAt: str(row["ExpireTime"]),
		Host:     host,
		Port:     port,
		Extras:   row,
	}
}

func (r Reader) ListSpecs(ctx context.Context, cred cloud.Cred, zone string) ([]cloud.Spec, error) {
	if strings.TrimSpace(zone) == "" {
		return nil, fmt.Errorf("请先选择可用区")
	}
	raw, err := r.call(ctx, cred, "DescribeProductConfig", map[string]any{"Zone": zone})
	if err != nil {
		return nil, err
	}
	return parseProductSpecs(raw, zone)
}

// parseProductSpecs 读取 DescribeProductConfig。Cpu 和 Memory 在 SpecItemInfoList 里，不在外层。
func parseProductSpecs(raw []byte, zone string) ([]cloud.Spec, error) {
	var resp struct {
		Response struct {
			SpecInfoList []struct {
				Zone             string `json:"Zone"`
				SpecItemInfoList []struct {
					CPU        int    `json:"Cpu"`
					Memory     int    `json:"Memory"`
					MaxStorage int    `json:"MaxStorage"`
					SpecCode   string `json:"SpecCode"`
				} `json:"SpecItemInfoList"`
			} `json:"SpecInfoList"`
		} `json:"Response"`
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("云接口没有返回内容")
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("云接口返回无法解析")
	}
	var out []cloud.Spec
	index := map[string]int{}
	want := strings.TrimSpace(zone)
	for _, info := range resp.Response.SpecInfoList {
		if want != "" && strings.TrimSpace(info.Zone) != "" && info.Zone != want {
			continue
		}
		for _, s := range info.SpecItemInfoList {
			key := fmt.Sprintf("%d-%d", s.CPU, s.Memory)
			if s.CPU == 0 {
				continue
			}
			if i, ok := index[key]; ok {
				nextMax := tighterStorageMax(out[i].MaxStorageGB, s.MaxStorage)
				if (nextMax != out[i].MaxStorageGB || out[i].SpecCode == "") && s.SpecCode != "" {
					out[i].SpecCode = s.SpecCode
				}
				out[i].MaxStorageGB = nextMax
				out[i].Label = specLabel(s.CPU, s.Memory, out[i].MaxStorageGB)
				continue
			}
			index[key] = len(out)
			out = append(out, cloud.Spec{
				CPU: s.CPU, MemoryMB: s.Memory, MaxStorageGB: s.MaxStorage, SpecCode: s.SpecCode,
				Label: specLabel(s.CPU, s.Memory, s.MaxStorage),
			})
		}
	}
	return out, nil
}

func tighterStorageMax(cur, next int) int {
	if next <= 0 {
		return cur
	}
	if cur <= 0 || next < cur {
		return next
	}
	return cur
}

func specLabel(cpu, memoryMB, maxGB int) string {
	label := fmt.Sprintf("%d核 %s", cpu, memoryText(memoryMB))
	if maxGB > 0 {
		label += fmt.Sprintf("，最大存储 %dGB", maxGB)
	}
	return label
}

func (r Reader) QuoteCreate(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Quote, error) {
	raw, err := r.call(ctx, cred, "InquiryPriceCreateDBInstances", createBody(in))
	if err != nil {
		return cloud.Quote{}, err
	}
	return parseQuote(raw), nil
}

func (r Reader) QuoteUpgrade(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Quote, error) {
	raw, err := r.call(ctx, cred, "InquiryPriceUpgradeDBInstance", specBody(in))
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
	for _, k := range []string{"Price", "OriginalPrice", "Amount"} {
		if v, ok := resp.Response[k]; ok {
			q.Amount = float64(num(v))
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
	raw, err := r.call(ctx, cred, "DescribeSlowQueryList", map[string]any{
		"DBInstanceId": cloudID,
		"StartTime":    shanghai(from),
		"EndTime":      shanghai(to),
		"Limit":        limit,
		"Offset":       offset,
	})
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Response struct {
			TotalCount       int              `json:"TotalCount"`
			RawSlowQueryList []map[string]any `json:"RawSlowQueryList"`
			SlowQueryList    []map[string]any `json:"SlowQueryList"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, err
	}
	rows := resp.Response.RawSlowQueryList
	if len(rows) == 0 {
		rows = resp.Response.SlowQueryList
	}
	var out []cloud.SlowLog
	for _, row := range rows {
		sqlText := str(row["NormalQuery"])
		if sqlText == "" {
			sqlText = str(row["RawQuery"])
		}
		at := slowTime(row)
		dur := row["Duration"]
		if dur == nil {
			dur = row["QueryTime"]
		}
		rawRow, _ := json.Marshal(row)
		out = append(out, cloud.SlowLog{
			Time: at, DBName: str(row["DatabaseName"]), User: str(row["UserName"]),
			Client: str(row["ClientAddr"]), DurationMS: durationMS(dur),
			SQL: sqlText, Raw: string(rawRow),
		})
	}
	return out, resp.Response.TotalCount, nil
}

func slowTime(row map[string]any) time.Time {
	for _, key := range []string{"SessionStartTime", "LastTime", "FirstTime", "QueryStartTime", "Timestamp", "CallTime"} {
		if at := parseSlowTime(str(row[key])); !at.IsZero() {
			return at
		}
	}
	return time.Time{}
}

func parseSlowTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " "); i > 0 {
		tail := s[i+1:]
		if len(tail) >= 2 && len(tail) <= 5 && isZoneName(tail) {
			s = strings.TrimSpace(s[:i])
		}
	}
	if at := tencentapi.ParseShanghai(s); !at.IsZero() {
		return at
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000", "2006-01-02T15:04:05"} {
		if at, err := time.Parse(layout, s); err == nil {
			return at
		}
	}
	return time.Time{}
}

func isZoneName(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func durationMS(v any) int {
	n := num(v)
	if f, ok := v.(float64); ok && f > 0 && f < 100 {
		return int(f * 1000)
	}
	return n
}

func (r Reader) ListParameters(ctx context.Context, cred cloud.Cred, cloudID string) ([]cloud.Param, error) {
	raw, err := r.call(ctx, cred, "DescribeDBInstanceParameters", map[string]any{"DBInstanceId": cloudID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response struct {
			Detail []struct {
				Name  string `json:"Name"`
				Value string `json:"CurrentValue"`
			} `json:"Detail"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	var out []cloud.Param
	for _, p := range resp.Response.Detail {
		out = append(out, cloud.Param{Name: p.Name, Value: p.Value})
	}
	return out, nil
}

func (r Reader) ListEvents(ctx context.Context, cred cloud.Cred, cloudID string, from, to time.Time) ([]cloud.Event, error) {
	raw, err := r.call(ctx, cred, "DescribeDBErrlogs", map[string]any{
		"DBInstanceId": cloudID,
		"StartTime":    shanghai(from),
		"EndTime":      shanghai(to),
		"Limit":        50,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Response struct {
			Items []map[string]any `json:"Items"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	var out []cloud.Event
	for _, row := range resp.Response.Items {
		msg := str(row["ErrLog"])
		if msg == "" {
			msg = str(row["LogContent"])
		}
		at := tencentapi.ParseShanghai(str(row["ErrTime"]))
		if at.IsZero() {
			at = time.Now()
		}
		out = append(out, cloud.Event{Type: "fault", Message: msg, At: at})
	}
	return out, nil
}

func (r Reader) DescribeTask(ctx context.Context, cred cloud.Cred, cloudID, taskID string) (cloud.Task, error) {
	raw, err := r.call(ctx, cred, "DescribeDBInstanceAttribute", map[string]any{"DBInstanceId": cloudID})
	if err != nil {
		return cloud.Task{}, err
	}
	var resp struct {
		Response struct {
			DBInstance map[string]any `json:"DBInstance"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return cloud.Task{}, err
	}
	st := cloud.MapInstanceStatus(str(resp.Response.DBInstance["DBInstanceStatus"]))
	task := cloud.Task{ID: taskID, Status: "running", Detail: st}
	if st == "running" {
		task.Status = "success"
	}
	if st == "isolated" || st == "offline" {
		task.Status = "failed"
		task.Detail = st
	}
	return task, nil
}

func (w Writer) Create(ctx context.Context, cred cloud.Cred, in cloud.CreateInput) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "CreateInstances", createBody(in))
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) ModifySpec(ctx context.Context, cred cloud.Cred, in cloud.SpecInput) (cloud.Task, error) {
	if in.DiskGB > 0 {
		// 磁盘是否允许缩小由调用方按 Caps 判断。这里只提交目标值。
	}
	raw, err := w.call(ctx, cred, "ModifyDBInstanceSpec", specBody(in))
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Restart(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "RestartDBInstance", map[string]any{"DBInstanceId": cloudID})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) Switchover(ctx context.Context, cred cloud.Cred, cloudID string) (cloud.Task, error) {
	raw, err := w.call(ctx, cred, "SwitchDBInstancePrimary", map[string]any{
		"DBInstanceId": cloudID, "Force": false, "SwitchTag": 0,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func (w Writer) ModifyParameters(ctx context.Context, cred cloud.Cred, cloudID string, params []cloud.Param) (cloud.Task, error) {
	list := make([]map[string]string, 0, len(params))
	for _, p := range params {
		list = append(list, map[string]string{"Name": p.Name, "ExpectedValue": p.Value})
	}
	raw, err := w.call(ctx, cred, "ModifyDBInstanceParameters", map[string]any{
		"DBInstanceId": cloudID, "ParamList": list,
	})
	if err != nil {
		return cloud.Task{}, err
	}
	return parseTask(raw), nil
}

func createBody(in cloud.CreateInput) map[string]any {
	count := in.Count
	if count <= 0 {
		count = 1
	}
	period := in.Period
	if period <= 0 {
		period = 1
	}
	charge := in.ChargeType
	if charge == "" {
		charge = "POSTPAID_BY_HOUR"
	}
	if charge == "POSTPAID_BY_HOUR" {
		period = 1
	}
	charset := in.Charset
	if charset == "" {
		charset = "UTF8"
	}
	syncMode := in.SyncMode
	if syncMode == "" {
		syncMode = "Semi-sync"
	}
	tde := 0
	if in.NeedSupportTDE {
		tde = 1
	}
	body := map[string]any{
		"Zone": in.Zone, "SpecCode": in.SpecCode, "Storage": in.DiskGB,
		"InstanceCount": count, "Period": period, "Charset": charset,
		"AdminName": in.Admin, "AdminPassword": in.Password,
		"InstanceChargeType": charge, "DBEngine": "postgresql",
		"StorageType": "PHYSICAL_LOCAL_SSD", "SyncMode": syncMode,
		"NeedSupportTDE": tde, "DeletionProtection": in.DeletionProtection,
	}
	if in.DBMajorVersion != "" {
		body["DBMajorVersion"] = in.DBMajorVersion
	}
	if in.VpcID != "" {
		body["VpcId"] = in.VpcID
	}
	if in.SubnetID != "" {
		body["SubnetId"] = in.SubnetID
	}
	if strings.TrimSpace(in.Name) != "" {
		body["Name"] = in.Name
	}
	if in.StandbyZone != "" {
		body["DBNodeSet"] = []map[string]string{
			{"Role": "Primary", "Zone": in.Zone},
			{"Role": "Standby", "Zone": in.StandbyZone},
		}
	}
	if in.SecurityGroupID != "" {
		body["SecurityGroupIds"] = []string{in.SecurityGroupID}
	}
	if in.ProjectID > 0 {
		body["ProjectId"] = in.ProjectID
	}
	var tags []map[string]string
	for _, tag := range in.Tags {
		if strings.TrimSpace(tag.Key) == "" {
			continue
		}
		tags = append(tags, map[string]string{"TagKey": tag.Key, "TagValue": tag.Value})
	}
	if len(tags) > 0 {
		body["TagList"] = tags
	}
	return body
}

func specBody(in cloud.SpecInput) map[string]any {
	body := map[string]any{"DBInstanceId": in.CloudID}
	if in.MemoryMB > 0 {
		body["Memory"] = memoryGB(in.MemoryMB)
	}
	if in.DiskGB > 0 {
		body["Storage"] = in.DiskGB
	}
	if in.CPU > 0 {
		body["Cpu"] = in.CPU
	}
	body["SwitchTag"] = in.SwitchTag
	if in.SwitchTag == 1 {
		body["SwitchStartTime"] = strings.TrimSpace(in.SwitchStart)
		body["SwitchEndTime"] = strings.TrimSpace(in.SwitchEnd)
	}
	return body
}

// DescribeDBInstances 的 DBInstanceMemory 单位是 GB。规格和平台内部用 MB。
func memoryMBFromInstanceGB(gb int) int {
	if gb <= 0 {
		return 0
	}
	return gb * 1024
}

// 创建和变配请求的 Memory 单位是 GB。
func memoryGB(mb int) int {
	if mb <= 0 {
		return 0
	}
	gb := mb / 1024
	if gb == 0 {
		return 1
	}
	return gb
}

func memoryText(mb int) string {
	if mb > 0 && mb%1024 == 0 {
		return fmt.Sprintf("%dGB", mb/1024)
	}
	return fmt.Sprintf("%dMB", mb)
}

func parseTask(raw []byte) cloud.Task {
	var resp struct {
		Response struct {
			TaskId        any      `json:"TaskId"`
			DBInstanceId  string   `json:"DBInstanceId"`
			DBInstanceIds []string `json:"DBInstanceIdSet"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	id := str(resp.Response.TaskId)
	cloudID := resp.Response.DBInstanceId
	if cloudID == "" && len(resp.Response.DBInstanceIds) > 0 {
		cloudID = resp.Response.DBInstanceIds[0]
	}
	return cloud.Task{ID: id, Status: "running", Detail: cloudID}
}
