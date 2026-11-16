package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/secret"
)

const (
	RoleReadonly = "readonly"
	RoleOperate  = "operate"

	ActionCreate    = "create"
	ActionScaleSpec = "scale_spec"
	ActionScaleDisk = "scale_disk"
	ActionRestart   = "restart"
	ActionSwitch    = "switchover"
	ActionParam     = "modify_param"
	ActionRegister  = "register_platform" // 把云清单登记为平台实例
	ActionRebind    = "rebind_account"    // 已关联平台实例时改云账号归属

	ReconcileLinked    = "linked"
	ReconcileCloudOnly = "cloud_only"
	ReconcileMissing   = "missing"
	ReconcileChanged   = "status_changed"
	ReconcileConflict  = "conflict"
)

// Window 慢日志时间窗。(上次成功, 本次结束]，并向前重叠 overlap。
const (
	// SlowLogAPILookback 是腾讯云慢日志默认可查范围。更早的起点会被接口判为参数不合法。
	SlowLogAPILookback = 7 * 24 * time.Hour
	// SlowLogAPIFallback 是 7 天窗口仍被拒绝时，这一次改拉的最近范围。
	SlowLogAPIFallback = 24 * time.Hour
)

type Window struct {
	From time.Time
	To   time.Time
}

// ClampSlowWindow 把第一次回溯的起点收到云上还能查到的范围内。已落在范围内的增量窗口保持原样。
func ClampSlowWindow(win Window, now time.Time) Window {
	if now.IsZero() {
		now = time.Now()
	}
	floor := now.Add(-SlowLogAPILookback)
	if win.From.Before(floor) {
		win.From = floor
	}
	if !win.From.Before(win.To) {
		win.From = floor
		if !win.From.Before(win.To) {
			win.From = win.To.Add(-time.Minute)
		}
	}
	return win
}

func NextWindow(last, now time.Time, firstLookback, overlap time.Duration) Window {
	if now.IsZero() {
		now = time.Now()
	}
	if last.IsZero() {
		return Window{From: now.Add(-firstLookback), To: now}
	}
	from := last.Add(-overlap)
	if !from.Before(now) {
		from = now.Add(-overlap)
	}
	return Window{From: from, To: now}
}

// SplitWindow 把过长的窗口拆成不超过 max 的多段，段尾相接。
func SplitWindow(w Window, max time.Duration) []Window {
	if max <= 0 || !w.From.Add(max).Before(w.To) {
		return []Window{w}
	}
	var out []Window
	cur := w.From
	for cur.Before(w.To) {
		end := cur.Add(max)
		if end.After(w.To) {
			end = w.To
		}
		out = append(out, Window{From: cur, To: end})
		cur = end
	}
	return out
}

// AdvanceCursor 整段成功才把游标拨到窗口结束。失败时游标停在上一段成功之后。
func AdvanceCursor(prev time.Time, seg Window, ok bool) time.Time {
	if !ok {
		return prev
	}
	return seg.To
}

type BindResult string

const (
	BindFill     BindResult = "fill"
	BindKeep     BindResult = "keep"
	BindConflict BindResult = "conflict"
	BindSkip     BindResult = "skip"
)

// DecideBind 只有唯一匹配且尚未绑定时才写入。已绑定的账号不覆盖。
func DecideBind(currentAccount, incomingAccount string, matches int) BindResult {
	incomingAccount = strings.TrimSpace(incomingAccount)
	currentAccount = strings.TrimSpace(currentAccount)
	if matches != 1 || incomingAccount == "" {
		if matches > 1 {
			return BindSkip
		}
		return BindSkip
	}
	if currentAccount == "" {
		return BindFill
	}
	if currentAccount == incomingAccount {
		return BindKeep
	}
	return BindConflict
}

// IsIntranetNetType 判断云上网络类型是否为内网。
func IsIntranetNetType(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inner", "private", "intranet", "1", "内网":
		return true
	default:
		return false
	}
}

// PickPGIntranet 从 PostgreSQL DBInstanceNetInfo 取内网地址。未标成内网的条目不用。
func PickPGIntranet(nets []any) (host string, port int) {
	for _, item := range nets {
		m, ok := item.(map[string]any)
		if !ok || !IsIntranetNetType(anyStr(m["NetType"])) {
			continue
		}
		host = anyStr(m["Ip"])
		if host == "" {
			host = anyStr(m["Address"])
		}
		port = anyInt(m["Port"])
		if host != "" {
			return host, port
		}
	}
	return "", 0
}

// IntranetFromExtras 从已保存的云厂商原始返回里再解析内网地址。
func IntranetFromExtras(engine string, extras map[string]any) string {
	host, _ := NetFromExtras(engine, extras)
	return host
}

// NetFromExtras 从已保存的云厂商原始返回里解析内网地址和端口。云主机没有端口。
func NetFromExtras(engine string, extras map[string]any) (host string, port int) {
	if len(extras) == 0 {
		return "", 0
	}
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "", EnginePostgres:
		if nets, ok := extras["DBInstanceNetInfo"].([]any); ok {
			return PickPGIntranet(nets)
		}
	case EngineMySQL:
		return anyStr(extras["Vip"]), anyInt(extras["Vport"])
	case EngineRedis:
		return anyStr(extras["WanIp"]), anyInt(extras["Port"])
	case EngineKafka:
		return kafkaNet(extras)
	case EngineHost:
		if ips, ok := extras["PrivateIpAddresses"].([]any); ok && len(ips) > 0 {
			return anyStr(ips[0]), 0
		}
	}
	return "", 0
}

// kafkaNet 先用 Vip，没有再用 VipList。有地址但没有端口时用 9092。
func kafkaNet(extras map[string]any) (string, int) {
	host := anyStr(extras["Vip"])
	port := anyInt(extras["Vport"])
	if host == "" {
		if list, ok := extras["VipList"].([]any); ok {
			for _, item := range list {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if h := anyStr(m["Vip"]); h != "" {
					host = h
					if p := anyInt(m["Vport"]); p > 0 {
						port = p
					}
					break
				}
			}
		}
	}
	if host != "" && port <= 0 {
		port = 9092
	}
	return host, port
}

// KafkaListFields 从已保存的 extras 取出 Kafka 列表要展示的配额和集群类型。
// 已用主题优先读本次写入的 kafka_topic_used，没有时用属性里的 CreatedTopics，再没有用清单里的 TopicNum。
func KafkaListFields(extras map[string]any) (topicLimit, topicUsed, partLimit, partUsed int, instanceType string) {
	if len(extras) == 0 {
		return 0, 0, 0, 0, ""
	}
	topicUsed = anyInt(extras["kafka_topic_used"])
	if topicUsed <= 0 {
		topicUsed = anyInt(extras["CreatedTopics"])
	}
	if topicUsed <= 0 {
		topicUsed = anyInt(extras["TopicNum"])
	}
	return anyInt(extras["kafka_topic_limit"]), topicUsed, anyInt(extras["kafka_partition_limit"]), anyInt(extras["kafka_partition_used"]), anyStr(extras["kafka_instance_type"])
}

// DiskUsedMB 读取同步时写入的 MySQL 磁盘占用，单位 MB。ok 为假表示这次没有采到。
func DiskUsedMB(extras map[string]any) (mb int, ok bool) {
	if len(extras) == 0 {
		return 0, false
	}
	v, exists := extras["disk_used_mb"]
	if !exists || v == nil {
		return 0, false
	}
	return anyInt(v), true
}

// PreserveMySQLDiskUsed 监控暂时失败时保留上次的占用，避免同步把已展示的使用量写成空。
func PreserveMySQLDiskUsed(inst *Instance, prevExtras map[string]any, prevGB int) {
	if inst == nil {
		return
	}
	if _, ok := DiskUsedMB(inst.Extras); ok {
		return
	}
	mb, ok := DiskUsedMB(prevExtras)
	if !ok {
		return
	}
	if inst.Extras == nil {
		inst.Extras = map[string]any{}
	}
	inst.Extras["disk_used_mb"] = mb
	if inst.DiskUsed == 0 && prevGB > 0 {
		inst.DiskUsed = prevGB
	}
}

// SystemDiskFromExtras 从云主机原始返回取系统盘大小（GB）和类型。
func SystemDiskFromExtras(extras map[string]any) (gb int, diskType string) {
	sys, ok := extras["SystemDisk"].(map[string]any)
	if !ok {
		return 0, ""
	}
	return anyInt(sys["DiskSize"]), anyStr(sys["DiskType"])
}

// DataDiskSpec 是云主机一块数据盘的大小和类型，只用于列表展示。
type DataDiskSpec struct {
	GB   int    `json:"gb"`
	Type string `json:"type,omitempty"`
}

// DataDisksFromExtras 从云主机原始返回逐块取出数据盘，不含系统盘，大小为空的跳过。
func DataDisksFromExtras(extras map[string]any) []DataDiskSpec {
	raw, ok := extras["DataDisks"].([]any)
	if !ok {
		return nil
	}
	out := make([]DataDiskSpec, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		gb := anyInt(m["DiskSize"])
		if gb <= 0 {
			continue
		}
		out = append(out, DataDiskSpec{GB: gb, Type: anyStr(m["DiskType"])})
	}
	return out
}

// EngineOfDBType 把平台实例类型收成云引擎名。
func EngineOfDBType(dbType string) string {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgres", "postgresql", "pg":
		return EnginePostgres
	case "mysql":
		return EngineMySQL
	case "redis":
		return EngineRedis
	case "host", "cvm", "os", "主机":
		return EngineHost
	default:
		return strings.ToLower(strings.TrimSpace(dbType))
	}
}

// PlatformRef 是用来和云上内网地址对照的平台实例。
type PlatformRef struct {
	ID     string
	Host   string
	DBType string
}

// MatchPlatformIDs 返回连接主机与内网地址相同、且引擎一致的平台实例。多地址按逗号拆开，忽略大小写。
func MatchPlatformIDs(intranet, engine string, items []PlatformRef) []string {
	intranet = strings.TrimSpace(intranet)
	engine = strings.ToLower(strings.TrimSpace(engine))
	if intranet == "" || engine == "" {
		return nil
	}
	var ids []string
	for _, it := range items {
		if EngineOfDBType(it.DBType) != engine {
			continue
		}
		if HostListHas(it.Host, intranet) {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// HostListHas 判断连接主机列表里是否有这一段。
func HostListHas(list, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, part := range splitHostList(list) {
		if strings.ToLower(part) == host {
			return true
		}
	}
	return false
}

func splitHostList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, "，", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// InventoryRef 是已经同步进清单的一条云资源。
type InventoryRef struct {
	ID         string
	CloudID    string
	Vendor     string
	Region     string
	Intranet   string
	InstanceID string
}

// CloudFill 是补齐平台实例云厂商、云实例 ID、云 Region 的结果。
type CloudFill struct {
	OK          bool
	Message     string
	Vendor      string
	CloudID     string
	Region      string
	ResourceIDs []string
}

const (
	MsgNoCloudResource = "没有对上云资源。请先在云资源页立即同步，或确认连接主机与云上内网地址一致"
	MsgCloudIDMissing  = "云清单中没有这个云实例 ID，请先在云资源页立即同步"
	MsgCloudIDClash    = "云实例 ID 与云清单不一致，未修改"
	MsgManyResources   = "连接主机对应多条云资源，未修改"
)

// ShouldFallbackTenant 本租户清单没有这条主机或云实例 ID 时，才再查默认租户。已经对上或对上多条时不换租户。
func ShouldFallbackTenant(instanceTenant, defaultTenant string, got CloudFill) bool {
	instanceTenant = strings.TrimSpace(instanceTenant)
	defaultTenant = strings.TrimSpace(defaultTenant)
	if instanceTenant == "" || instanceTenant == defaultTenant || got.OK {
		return false
	}
	return got.Message == MsgNoCloudResource || got.Message == MsgCloudIDMissing
}

// ReconcileConflictNote 对账冲突时说明账号不一致。inventoryCopies 大于 1 表示同一云实例 ID 出现在多个云账号下。
func ReconcileConflictNote(inventoryAccount, instanceAccount string, inventoryCopies int) string {
	inv := strings.TrimSpace(inventoryAccount)
	cur := strings.TrimSpace(instanceAccount)
	if inv == "" {
		inv = "未命名账号"
	}
	if inventoryCopies > 1 {
		if cur != "" {
			return fmt.Sprintf("云账号不一致：同一云实例 ID 在多个云账号下都有清单（实例是 %s，本条清单是 %s）", cur, inv)
		}
		return fmt.Sprintf("云账号不一致：同一云实例 ID 在多个云账号下都有清单（本条清单是 %s）", inv)
	}
	if cur != "" && cur != inv {
		return fmt.Sprintf("云账号不一致：实例是 %s，清单是 %s", cur, inv)
	}
	return "云账号不一致"
}

// FillFromInventory 用云实例 ID 或连接主机对照清单。两条都对上时以云实例 ID 为准。只补空项。
func FillFromInventory(host, vendor, cloudID, region string, rows []InventoryRef) CloudFill {
	vendor = strings.TrimSpace(vendor)
	cloudID = strings.TrimSpace(cloudID)
	region = strings.TrimSpace(region)
	if cloudID != "" {
		var byID []InventoryRef
		for _, row := range rows {
			if strings.EqualFold(strings.TrimSpace(row.CloudID), cloudID) {
				byID = append(byID, row)
			}
		}
		if len(byID) > 0 {
			return filled(vendor, cloudID, region, byID[0], byID)
		}
		if len(matchIntranet(host, rows)) > 0 {
			return CloudFill{Message: MsgCloudIDClash}
		}
		return CloudFill{Message: MsgCloudIDMissing}
	}
	hits := matchIntranet(host, rows)
	switch len(hits) {
	case 0:
		return CloudFill{Message: MsgNoCloudResource}
	case 1:
		return filled(vendor, cloudID, region, hits[0], hits)
	default:
		return CloudFill{Message: MsgManyResources}
	}
}

func filled(vendor, cloudID, region string, src InventoryRef, rows []InventoryRef) CloudFill {
	out := CloudFill{
		OK:      true,
		Vendor:  BlankFill(vendor, src.Vendor),
		CloudID: BlankFill(cloudID, src.CloudID),
		Region:  BlankFill(region, src.Region),
	}
	if out.Vendor == vendor && out.CloudID == cloudID && out.Region == region {
		out.Message = "云信息已与清单一致"
	} else {
		out.Message = "已补齐云厂商、云实例 ID 和云 Region"
	}
	for _, row := range rows {
		if strings.TrimSpace(row.InstanceID) == "" && strings.TrimSpace(row.ID) != "" {
			out.ResourceIDs = append(out.ResourceIDs, row.ID)
		}
	}
	return out
}

func matchIntranet(host string, rows []InventoryRef) []InventoryRef {
	var hits []InventoryRef
	for _, row := range rows {
		if HostListHas(host, row.Intranet) {
			hits = append(hits, row)
		}
	}
	return hits
}

// BlankFill 空值用云清单补上，已有值保留。
func BlankFill(cur, next string) string {
	if strings.TrimSpace(cur) != "" {
		return strings.TrimSpace(cur)
	}
	return strings.TrimSpace(next)
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return s
}

func anyInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	case int:
		return n
	default:
		i, _ := strconv.Atoi(anyStr(v))
		return i
	}
}

type Approval struct {
	By       string `json:"by"`
	At       string `json:"at"`
	Opinion  string `json:"opinion"`
	PolicyID string `json:"policy_id,omitempty"`
	AlertID  string `json:"alert_id,omitempty"`
}

func (a Approval) Valid() bool {
	return strings.TrimSpace(a.By) != "" && strings.TrimSpace(a.At) != "" && strings.TrimSpace(a.Opinion) != ""
}

type Payload struct {
	Action             string   `json:"action"`
	Vendor             string   `json:"vendor"`
	Engine             string   `json:"engine"`
	AccountID          string   `json:"account_id"`
	CloudID            string   `json:"cloud_instance_id"`
	Region             string   `json:"region"`
	BeforeCPU          int      `json:"before_cpu"`
	BeforeMem          int      `json:"before_memory_mb"`
	BeforeDisk         int      `json:"before_disk_gb"`
	CPU                int      `json:"cpu"`
	MemoryMB           int      `json:"memory_mb"`
	DiskGB             int      `json:"disk_gb"`
	Zone               string   `json:"zone,omitempty"`
	StandbyZone        string   `json:"standby_zone,omitempty"`
	Name               string   `json:"name,omitempty"`
	InstanceType       string   `json:"instance_type,omitempty"`
	AdminUser          string   `json:"admin_user,omitempty"`
	PasswordEnc        string   `json:"password_enc,omitempty"`
	ChargeType         string   `json:"charge_type,omitempty"`
	Period             int      `json:"period,omitempty"`
	InstanceCount      int      `json:"instance_count,omitempty"`
	SpecCode           string   `json:"spec_code,omitempty"`
	DBMajorVersion     string   `json:"db_major_version,omitempty"`
	VpcID              string   `json:"vpc_id,omitempty"`
	SubnetID           string   `json:"subnet_id,omitempty"`
	Charset            string   `json:"charset,omitempty"`
	SyncMode           string   `json:"sync_mode,omitempty"`
	SecurityGroupID    string   `json:"security_group_id,omitempty"`
	ProjectID          int      `json:"project_id,omitempty"`
	NeedSupportTDE     bool     `json:"need_support_tde,omitempty"`
	DeletionProtection bool     `json:"deletion_protection,omitempty"`
	Tags               []Tag    `json:"tags,omitempty"`
	Params             []Param  `json:"params,omitempty"`
	Quote              Quote    `json:"quote"`
	Blip               bool     `json:"blip"`
	Impact             string   `json:"impact"`
	TaskID             string   `json:"task_id,omitempty"`
	SwitchTag          int      `json:"switch_tag,omitempty"`
	SwitchStart        string   `json:"switch_start,omitempty"`
	SwitchEnd          string   `json:"switch_end,omitempty"`
	Approval           Approval `json:"approval"`
}

func ParsePayload(raw string) (Payload, error) {
	var p Payload
	if strings.TrimSpace(raw) == "" {
		return p, fmt.Errorf("工单内容为空")
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return p, fmt.Errorf("工单内容不是云变更: %w", err)
	}
	return p, nil
}

func (p Payload) JSON() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func (p Payload) ToCreateInput(password string) CreateInput {
	count := p.InstanceCount
	if count <= 0 {
		count = 1
	}
	period := p.Period
	if period <= 0 {
		period = 1
	}
	charge := strings.TrimSpace(p.ChargeType)
	if charge == "" {
		charge = "POSTPAID_BY_HOUR"
	}
	return CreateInput{
		Region: p.Region, Zone: p.Zone, StandbyZone: p.StandbyZone,
		Name: p.Name, Admin: p.AdminUser, Password: password,
		CPU: p.CPU, MemoryMB: p.MemoryMB, DiskGB: p.DiskGB,
		Period: period, Count: count, SpecCode: p.SpecCode,
		ChargeType: charge, DBMajorVersion: p.DBMajorVersion,
		VpcID: p.VpcID, SubnetID: p.SubnetID,
		Charset: p.Charset, SyncMode: p.SyncMode,
		SecurityGroupID: p.SecurityGroupID, ProjectID: p.ProjectID,
		NeedSupportTDE: p.NeedSupportTDE, DeletionProtection: p.DeletionProtection,
		Tags: p.Tags,
	}
}

func prepaidPeriodOK(n int) bool {
	return (n >= 1 && n <= 12) || n == 24 || n == 36
}

// ValidateCreate 检查 PostgreSQL 和 Kafka 的资源申请。其他引擎仍用原来的短表单。
func ValidateCreate(p Payload) error {
	if p.Action != ActionCreate {
		return nil
	}
	if p.Engine == EngineKafka {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("请填写实例名")
		}
		if _, err := strconv.Atoi(strings.TrimSpace(p.Zone)); err != nil {
			return fmt.Errorf("请填写可用区 ID")
		}
		if p.DiskGB <= 0 {
			return fmt.Errorf("请填写磁盘")
		}
		if strings.TrimSpace(p.VpcID) == "" || strings.TrimSpace(p.SubnetID) == "" {
			return fmt.Errorf("请填写 VPC 和子网")
		}
		return nil
	}
	if p.Engine != "" && p.Engine != EnginePostgres {
		return nil
	}
	if strings.TrimSpace(p.Region) == "" {
		return fmt.Errorf("请选择地域")
	}
	if strings.TrimSpace(p.Zone) == "" || strings.TrimSpace(p.StandbyZone) == "" {
		return fmt.Errorf("请选择主可用区和备可用区")
	}
	if strings.TrimSpace(p.VpcID) == "" || strings.TrimSpace(p.SubnetID) == "" {
		return fmt.Errorf("请选择私有网络和子网")
	}
	if strings.TrimSpace(p.DBMajorVersion) == "" {
		return fmt.Errorf("请选择数据库版本")
	}
	if strings.TrimSpace(p.SpecCode) == "" || p.CPU <= 0 || p.MemoryMB <= 0 {
		return fmt.Errorf("请选择规格")
	}
	if p.DiskGB < 10 || p.DiskGB%10 != 0 {
		return fmt.Errorf("磁盘按 10GB 步进，且不能小于 10GB")
	}
	charge := strings.TrimSpace(p.ChargeType)
	if charge != "PREPAID" && charge != "POSTPAID_BY_HOUR" {
		return fmt.Errorf("请选择计费模式")
	}
	if charge == "PREPAID" && !prepaidPeriodOK(p.Period) {
		return fmt.Errorf("包年包月时长只支持 1 到 12 个月，或 24、36 个月")
	}
	if p.InstanceCount < 0 || p.InstanceCount > 10 {
		return fmt.Errorf("购买数量为 1 到 10")
	}
	charset := strings.TrimSpace(p.Charset)
	if charset != "UTF8" && charset != "LATIN1" {
		return fmt.Errorf("字符集只支持 UTF8 或 LATIN1")
	}
	syncMode := strings.TrimSpace(p.SyncMode)
	if syncMode != "Async" && syncMode != "Semi-sync" {
		return fmt.Errorf("请选择数据复制方式")
	}
	if name := strings.TrimSpace(p.Name); len([]rune(name)) >= 60 {
		return fmt.Errorf("实例名长度要小于 60")
	}
	if err := validAdminName(p.AdminUser); err != nil {
		return err
	}
	if secret.IsEncrypted(p.PasswordEnc) {
		return nil
	}
	if err := validAdminPassword(p.PasswordEnc); err != nil {
		return err
	}
	return nil
}

func validAdminName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 16 || !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(name) {
		return fmt.Errorf("管理员账号需要 1-16 位字母、数字或下划线")
	}
	if name[0] >= '0' && name[0] <= '9' {
		return fmt.Errorf("管理员账号不能以数字开头")
	}
	lower := strings.ToLower(name)
	if lower == "postgres" || strings.HasPrefix(lower, "pg_") {
		return fmt.Errorf("管理员账号不能是 postgres，也不能以 pg_ 开头")
	}
	return nil
}

func validAdminPassword(password string) error {
	if len(password) < 8 || len(password) > 32 || strings.HasPrefix(password, "/") {
		return fmt.Errorf("管理员密码需要 8-32 位，且不能以 / 开头")
	}
	if !regexp.MustCompile(`[a-z]`).MatchString(password) ||
		!regexp.MustCompile(`[A-Z]`).MatchString(password) ||
		!regexp.MustCompile(`[0-9]`).MatchString(password) ||
		!regexp.MustCompile("[`()~!@#$%^&*\\-+=_|{}\\[\\]:;'<>,.?/]").MatchString(password) {
		return fmt.Errorf("管理员密码需要同时包含大写字母、小写字母、数字和特殊字符")
	}
	return nil
}

// CheckTargetSpec 变配、扩盘的目标不能为 0，也不能小于当前规格。
func CheckTargetSpec(action string, beforeCPU, beforeMem, beforeDisk, cpu, mem, disk int, allowDiskShrink bool) error {
	_, _, err := ScaleDelta("", action, beforeCPU, beforeMem, beforeDisk, cpu, mem, disk, allowDiskShrink)
	return err
}

// flexibleScale 是 PostgreSQL、MySQL。目标规格可以不填，只把变化的项交给云接口。
func flexibleScale(engine string) bool {
	return engine == EnginePostgres || engine == EngineMySQL
}

// ScaleDelta 比较变更前后。specChanged、diskChanged 表示要传给云接口的项。
func ScaleDelta(engine, action string, beforeCPU, beforeMem, beforeDisk, cpu, mem, disk int, allowDiskShrink bool) (specChanged, diskChanged bool, err error) {
	switch action {
	case ActionScaleDisk:
		if disk <= 0 {
			return false, false, fmt.Errorf("目标磁盘不能为 0")
		}
		if err := CheckDisk(action, beforeDisk, disk, allowDiskShrink); err != nil {
			return false, false, err
		}
		return false, true, nil
	case ActionScaleSpec:
		if !flexibleScale(engine) {
			if cpu <= 0 || mem <= 0 {
				return false, false, fmt.Errorf("请填写目标规格")
			}
			if (beforeCPU > 0 && cpu < beforeCPU) || (beforeMem > 0 && mem < beforeMem) {
				return false, false, fmt.Errorf("目标规格不能小于当前规格")
			}
			if beforeDisk > 0 && disk <= 0 {
				return false, false, fmt.Errorf("目标磁盘不能为 0")
			}
			if err := CheckDisk(action, beforeDisk, disk, allowDiskShrink); err != nil {
				return false, false, err
			}
			return true, disk > 0 && beforeDisk > 0 && disk != beforeDisk, nil
		}
		if (cpu > 0) != (mem > 0) {
			return false, false, fmt.Errorf("请同时填写核数和内存")
		}
		specChanged = cpu > 0 && (cpu != beforeCPU || mem != beforeMem)
		if specChanged && ((beforeCPU > 0 && cpu < beforeCPU) || (beforeMem > 0 && mem < beforeMem)) {
			return false, false, fmt.Errorf("目标规格不能小于当前规格")
		}
		if disk > 0 && beforeDisk > 0 && disk != beforeDisk {
			if err := CheckDisk(action, beforeDisk, disk, allowDiskShrink); err != nil {
				return false, false, err
			}
			diskChanged = true
		} else if beforeDisk == 0 && disk > 0 {
			diskChanged = true
		}
		if !specChanged && !diskChanged {
			return false, false, fmt.Errorf("没有需要执行的变更")
		}
		return specChanged, diskChanged, nil
	default:
		return false, false, nil
	}
}

// SpecForAPI 只保留这次要执行的 CPU、内存或磁盘。未变化的项为 0，云请求会省略。
func SpecForAPI(p Payload, specChanged, diskChanged bool) SpecInput {
	in := SpecInput{
		CloudID: p.CloudID, InstanceType: p.InstanceType,
		SwitchTag: p.SwitchTag, SwitchStart: strings.TrimSpace(p.SwitchStart), SwitchEnd: strings.TrimSpace(p.SwitchEnd),
	}
	if p.Action == ActionScaleDisk {
		in.DiskGB = p.DiskGB
		return in
	}
	if p.Action == ActionScaleSpec && flexibleScale(p.Engine) {
		if specChanged {
			in.CPU = p.CPU
			in.MemoryMB = p.MemoryMB
		}
		if diskChanged {
			in.DiskGB = p.DiskGB
		}
		return in
	}
	in.CPU = p.CPU
	in.MemoryMB = p.MemoryMB
	in.DiskGB = p.DiskGB
	return in
}

// ImpactFor 只扩盘时说明不调整规格。
func ImpactFor(p Payload, allowDiskShrink bool) (bool, string) {
	specChanged, diskChanged, err := ScaleDelta(p.Engine, p.Action, p.BeforeCPU, p.BeforeMem, p.BeforeDisk, p.CPU, p.MemoryMB, p.DiskGB, allowDiskShrink)
	if err != nil {
		return ImpactOf(p.Action, p.BeforeCPU, p.BeforeMem, p.CPU, p.MemoryMB)
	}
	if p.Action == ActionScaleDisk || (p.Action == ActionScaleSpec && diskChanged && !specChanged) {
		return false, "只扩容磁盘"
	}
	blip, text := ImpactOf(p.Action, p.BeforeCPU, p.BeforeMem, p.CPU, p.MemoryMB)
	if p.Action == ActionScaleSpec && specChanged && diskChanged {
		text += "，并扩容磁盘"
	}
	return blip, text
}

// MaxStorageGB 按 CPU 和内存找到规格的最大存储。没有这档规格或云上没给上限时返回 0。
func MaxStorageGB(specs []Spec, cpu, memoryMB int) int {
	for _, s := range specs {
		if s.CPU == cpu && s.MemoryMB == memoryMB {
			return s.MaxStorageGB
		}
	}
	return 0
}

// DiskForStorageCheck 变配没另填磁盘时用当前磁盘。
func DiskForStorageCheck(action string, diskGB, beforeDisk int) int {
	if action == ActionScaleSpec && diskGB <= 0 {
		return beforeDisk
	}
	return diskGB
}

// CheckSpecStorage 磁盘超过规格最大存储时拒绝。上限为 0 表示云上没返回，不拦截。
func CheckSpecStorage(diskGB, maxStorageGB int) error {
	if diskGB <= 0 || maxStorageGB <= 0 || diskGB <= maxStorageGB {
		return nil
	}
	return fmt.Errorf("当前磁盘 %dGB，该规格最大支持 %dGB", diskGB, maxStorageGB)
}

var switchClock = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d:[0-5]\d$`)

// CheckSwitch PostgreSQL 变配、扩盘的切换时间。0 升级完成时，1 指定时间，2 维护时间内。缺省按 0。
func CheckSwitch(engine, action string, tag int, start, end string) error {
	if engine != "" && engine != EnginePostgres {
		return nil
	}
	if action != ActionScaleSpec && action != ActionScaleDisk {
		return nil
	}
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	switch tag {
	case 0, 2:
		return nil
	case 1:
		if !switchClock.MatchString(start) || !switchClock.MatchString(end) || start >= end {
			return fmt.Errorf("指定切换时间需要开始早于结束，格式为 HH:mm:ss")
		}
		return nil
	default:
		return fmt.Errorf("切换时间无效")
	}
}

// SwitchText 审批页用的切换时间说明。
func SwitchText(tag int, start, end string) string {
	switch tag {
	case 1:
		return fmt.Sprintf("指定时间 %s–%s", strings.TrimSpace(start), strings.TrimSpace(end))
	case 2:
		return "维护时间内"
	default:
		return "升级完成时"
	}
}

// CopyCurrentSpec 不改规格的动作沿用当前规格，避免把变更后写成 0。
func CopyCurrentSpec(p *Payload) {
	if p == nil {
		return
	}
	switch p.Action {
	case ActionScaleSpec, ActionScaleDisk, ActionCreate:
		return
	}
	if p.CPU == 0 {
		p.CPU = p.BeforeCPU
	}
	if p.MemoryMB == 0 {
		p.MemoryMB = p.BeforeMem
	}
	if p.DiskGB == 0 {
		p.DiskGB = p.BeforeDisk
	}
}

// CheckDisk 腾讯云磁盘只能变大。其他云由 Caps.ScaleDiskDown 决定。
func CheckDisk(action string, before, target int, allowShrink bool) error {
	if action != ActionScaleDisk && action != ActionScaleSpec && action != ActionCreate {
		return nil
	}
	if action == ActionScaleDisk || (action == ActionScaleSpec && target > 0 && before > 0) {
		if target < before && !allowShrink {
			return fmt.Errorf("磁盘只能扩容，目标 %d GB 小于当前 %d GB", target, before)
		}
		if action == ActionScaleDisk && target == before {
			return fmt.Errorf("磁盘容量没有变化")
		}
	}
	return nil
}

// ImpactOf 降配、重启、切换默认会闪断。
func ImpactOf(action string, beforeCPU, beforeMem, cpu, mem int) (blip bool, text string) {
	switch action {
	case ActionRestart:
		return true, "重启会造成闪断"
	case ActionSwitch:
		return true, "主备切换会造成闪断"
	case ActionScaleSpec:
		if (cpu > 0 && cpu < beforeCPU) || (mem > 0 && mem < beforeMem) {
			return true, "降配会造成闪断"
		}
		return false, "升配期间实例可能短暂不可用，审批前请确认业务窗口"
	case ActionScaleDisk:
		return false, "磁盘扩容不缩小容量"
	case ActionParam:
		return true, "修改参数可能触发重启并造成闪断"
	case ActionCreate:
		return false, "新建实例，不影响已有实例"
	case ActionRegister:
		return false, "审批通过后才会把云实例登记到平台资源"
	case ActionRebind:
		return false, "审批通过后才会修改平台实例绑定的云账号"
	default:
		return false, ""
	}
}

type Policy struct {
	Enabled     bool
	Severity    string
	RuleKeyword string
	Action      string
	CapCPU      int
	CapMemoryMB int
	DiskStepGB  int
	CooldownMin int
	DailyMax    int
	Window      string
	Mode        string
}

func (p Policy) Hit(severity, title, rule string) bool {
	if !p.Enabled {
		return false
	}
	if s := strings.TrimSpace(p.Severity); s != "" && !strings.EqualFold(s, severity) {
		return false
	}
	kw := strings.TrimSpace(p.RuleKeyword)
	if kw == "" {
		return true
	}
	blob := title + " " + rule
	return strings.Contains(strings.ToLower(blob), strings.ToLower(kw))
}

// HealDecision 返回是否执行，以及跳过原因。自动策略不允许降配。
func HealDecision(p Policy, now, last time.Time, today int, recentSwitch bool, beforeCPU, beforeMem, targetCPU, targetMem, beforeDisk, targetDisk int) (bool, string) {
	if !p.Enabled {
		return false, "策略未开启"
	}
	if !inWindow(now, p.Window) {
		return false, "不在维护窗口"
	}
	if p.CooldownMin > 0 && !last.IsZero() && now.Sub(last) < time.Duration(p.CooldownMin)*time.Minute {
		return false, "冷却时间内"
	}
	if p.DailyMax > 0 && today >= p.DailyMax {
		return false, "已达当日次数上限"
	}
	switch p.Action {
	case ActionScaleSpec:
		if targetCPU > p.CapCPU && p.CapCPU > 0 || targetMem > p.CapMemoryMB && p.CapMemoryMB > 0 {
			return false, "超过升配上限"
		}
		if (targetCPU > 0 && targetCPU < beforeCPU) || (targetMem > 0 && targetMem < beforeMem) {
			return false, "自动自愈不允许降配"
		}
	case ActionScaleDisk:
		if targetDisk <= beforeDisk {
			return false, "磁盘只能扩容"
		}
	case ActionSwitch:
		if recentSwitch {
			return false, "云侧近期已切换，跳过"
		}
	case ActionRestart:
	default:
		return false, "该动作不能自动执行"
	}
	return true, ""
}

func inWindow(now time.Time, window string) bool {
	window = strings.TrimSpace(window)
	if window == "" {
		return true
	}
	parts := strings.Split(window, "-")
	if len(parts) != 2 {
		return true
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.Local
	}
	n := now.In(loc)
	start, err1 := clock(n, parts[0])
	end, err2 := clock(n, parts[1])
	if err1 != nil || err2 != nil {
		return true
	}
	if !end.After(start) {
		return !n.Before(start) || !n.After(end)
	}
	return !n.Before(start) && !n.After(end)
}

func clock(day time.Time, hm string) (time.Time, error) {
	hm = strings.TrimSpace(hm)
	t, err := time.Parse("15:04", hm)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, day.Location()), nil
}

func Fingerprint(sql string) string {
	s := strings.Join(strings.Fields(strings.ToLower(sql)), " ")
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func MapInstanceStatus(raw string) string {
	switch strings.TrimSpace(raw) {
	case "1":
		return "creating"
	case "2", "running":
		return "running"
	case "3":
		return "limited"
	case "4", "isolated":
		return "isolated"
	case "7", "9", "10", "12", "13", "14", "15":
		return "changing"
	case "8":
		return "offline"
	default:
		if raw == "" {
			return "unknown"
		}
		return raw
	}
}
