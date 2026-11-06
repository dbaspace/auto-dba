package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
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
type Window struct {
	From time.Time
	To   time.Time
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
	case EngineHost:
		if ips, ok := extras["PrivateIpAddresses"].([]any); ok && len(ips) > 0 {
			return anyStr(ips[0]), 0
		}
	}
	return "", 0
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
	case "host", "cvm":
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
			return CloudFill{Message: "云实例 ID 与云清单不一致，未修改"}
		}
		return CloudFill{Message: "云清单中没有这个云实例 ID，请先在云资源页立即同步"}
	}
	hits := matchIntranet(host, rows)
	switch len(hits) {
	case 0:
		return CloudFill{Message: "没有对上云资源。请先在云资源页立即同步，或确认连接主机与云上内网地址一致"}
	case 1:
		return filled(vendor, cloudID, region, hits[0], hits)
	default:
		return CloudFill{Message: "连接主机对应多条云资源，未修改"}
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
	Action       string   `json:"action"`
	Vendor       string   `json:"vendor"`
	Engine       string   `json:"engine"`
	AccountID    string   `json:"account_id"`
	CloudID      string   `json:"cloud_instance_id"`
	Region       string   `json:"region"`
	BeforeCPU    int      `json:"before_cpu"`
	BeforeMem    int      `json:"before_memory_mb"`
	BeforeDisk   int      `json:"before_disk_gb"`
	CPU          int      `json:"cpu"`
	MemoryMB     int      `json:"memory_mb"`
	DiskGB       int      `json:"disk_gb"`
	Zone         string   `json:"zone,omitempty"`
	Name         string   `json:"name,omitempty"`
	InstanceType string   `json:"instance_type,omitempty"`
	AdminUser    string   `json:"admin_user,omitempty"`
	PasswordEnc  string   `json:"password_enc,omitempty"`
	Params       []Param  `json:"params,omitempty"`
	Quote        Quote    `json:"quote"`
	Blip         bool     `json:"blip"`
	Impact       string   `json:"impact"`
	TaskID       string   `json:"task_id,omitempty"`
	Approval     Approval `json:"approval"`
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
