package alert

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const (
	KindAlert   = "alert"
	KindRecover = "recover"
	KindChange  = "change"
	KindInspect = "inspect"

	SubTrigger = "trigger"
	SubResolve = "resolve"
	SubTicket  = "ticket"
	SubHA      = "ha"
	SubConfig  = "config"
	SubDNS     = "dns"
	SubAWR     = "awr"

	maxWindow = 7 * 24 * time.Hour
)

// ActivityEvent 实时动态里的一条事件。各来源先映射成它，再按时间归并。
type ActivityEvent struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Sub      string            `json:"sub"`
	Time     int64             `json:"time"`
	Severity string            `json:"severity,omitempty"`
	Title    string            `json:"title"`
	Metric   string            `json:"metric,omitempty"`
	Detail   string            `json:"detail,omitempty"`
	Actor    string            `json:"actor,omitempty"`
	Badge    string            `json:"badge,omitempty"`
	Instance *ActivityInstance `json:"instance,omitempty"`
	Links    []ActivityLink    `json:"links,omitempty"`
}

type ActivityInstance struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	DBType  string `json:"db_type,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	Version string `json:"version,omitempty"`
}

type ActivityLink struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type ActivitySummary struct {
	Total  int `json:"total"`
	Firing int `json:"firing"`
}

// InstRef 用来把 DNS 记录值匹配到实例。不携带账号口令。
type InstRef struct {
	ID       string
	Name     string
	DBType   string
	Host     string
	Domain   string
	Domains  []string
	Port     int
	TenantID string
}

func (r InstRef) ToInstance() *ActivityInstance {
	if r.ID == "" && r.Name == "" {
		return nil
	}
	return &ActivityInstance{ID: r.ID, Name: r.Name, DBType: r.DBType, Host: r.Host, Port: r.Port}
}

// ParseWindow since/until 为 Unix 秒或 RFC3339。缺省近 24 小时，最长 7 天。
// before 是翻页上界（不含）。
func ParseWindow(sinceRaw, untilRaw string, before int64, now time.Time) (since, until time.Time) {
	until = now
	if t, ok := parseBound(untilRaw); ok {
		until = t
	}
	if before > 0 {
		b := time.Unix(before, 0)
		if b.Before(until) {
			until = b
		}
	}
	since = until.Add(-24 * time.Hour)
	if t, ok := parseBound(sinceRaw); ok {
		since = t
	}
	if until.Sub(since) > maxWindow {
		since = until.Add(-maxWindow)
	}
	if !since.Before(until) {
		since = until.Add(-time.Second)
	}
	return since, until
}

func parseBound(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if n, err := parseInt64(raw); err == nil && n > 1_000_000_000 {
		return time.Unix(n, 0), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errNotInt
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

var errNotInt = errStr("not int")

type errStr string

func (e errStr) Error() string { return string(e) }

// ParseEventTime 把库存的 RFC3339 文本转成 Unix 秒。
func ParseEventTime(s string) int64 {
	t, ok := parseBound(s)
	if !ok {
		return 0
	}
	return t.Unix()
}

// MergeActivity 多来源按时间倒序取前 limit 条。满页时 nextBefore 为最后一条的时间，供继续往前翻。
func MergeActivity(groups [][]ActivityEvent, limit int) (page []ActivityEvent, nextBefore int64) {
	if limit <= 0 {
		limit = 50
	}
	var all []ActivityEvent
	for _, g := range groups {
		all = append(all, g...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Time != all[j].Time {
			return all[i].Time > all[j].Time
		}
		return all[i].ID < all[j].ID
	})
	if len(all) > limit {
		all = all[:limit]
	}
	if len(all) == limit {
		nextBefore = all[len(all)-1].Time
	}
	if all == nil {
		all = []ActivityEvent{}
	}
	return all, nextBefore
}

// WantKind kinds 为空表示全要。
func WantKind(kinds []string, kind string) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if strings.EqualFold(strings.TrimSpace(k), kind) {
			return true
		}
	}
	return false
}

func ParseKinds(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		switch p {
		case KindAlert, KindRecover, KindChange, KindInspect:
			out = append(out, p)
		}
	}
	return out
}

// LabelInstanceID 从告警 labels 取出实例 ID。
func LabelInstanceID(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	for _, k := range []string{"instance_id", "instanceId"} {
		if id := strings.TrimSpace(labels[k]); id != "" {
			return id
		}
	}
	if strings.EqualFold(labels["ci_type"], "instance") {
		return strings.TrimSpace(labels["ci_id"])
	}
	return ""
}

func DecodeLabels(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// AlertInput 一条告警触发或恢复事件。
type AlertInput struct {
	EventID   string
	Action    string
	Detail    string
	Operator  string
	EventAt   string
	AlertID   string
	Severity  string
	Title     string
	Handle    string
	Owner     string
	CurValue  string
	Threshold string
	Labels    map[string]string
	Instance  *ActivityInstance
}

func MapAlert(in AlertInput) (ActivityEvent, bool) {
	kind := KindAlert
	sub := SubTrigger
	switch in.Action {
	case "trigger":
	case "resolve":
		kind = KindRecover
		sub = SubResolve
	default:
		return ActivityEvent{}, false
	}
	ev := ActivityEvent{
		ID: in.EventID, Kind: kind, Sub: sub, Time: ParseEventTime(in.EventAt),
		Severity: in.Severity, Title: strings.TrimSpace(in.Title),
		Detail: clip(in.Detail, 180), Actor: in.Operator, Instance: in.Instance,
	}
	if ev.ID == "" {
		ev.ID = in.AlertID + ":" + in.Action
	}
	ev.ID = "alert:" + ev.ID
	if in.Threshold != "" {
		ev.Metric = "阈值 " + in.Threshold
	}
	if in.CurValue != "" {
		cur := "当前 " + in.CurValue
		if ev.Detail == "" {
			ev.Detail = cur
		} else if !strings.Contains(ev.Detail, in.CurValue) {
			ev.Detail = cur + "，" + ev.Detail
		}
	}
	ev.Badge = handleBadge(in.Handle, in.Owner)
	path := "/ops/alerts?id=" + in.AlertID
	if kind == KindRecover {
		ev.Links = []ActivityLink{{Label: "详情", Path: "/ops/alerts/history?alert_id=" + in.AlertID}}
	} else {
		ev.Links = []ActivityLink{{Label: "处理", Path: path}, {Label: "详情", Path: path}}
	}
	return ev, ev.Time > 0
}

func handleBadge(handle, owner string) string {
	owner = strings.TrimSpace(owner)
	switch handle {
	case "acknowledged":
		if owner == "" {
			return "已认领"
		}
		return "已认领 · " + owner
	case "processing":
		if owner == "" {
			return "处理中"
		}
		return "处理中 · " + owner
	case "silenced":
		return "已静默"
	default:
		return ""
	}
}

// TicketInput 已执行或失败的工单。
type TicketInput struct {
	ID, TicketNo, IssueType, Description, Status, Submitter, UpdatedAt string
	Instance                                                           *ActivityInstance
}

func MapTicket(in TicketInput) (ActivityEvent, bool) {
	if in.Status != "executed" && in.Status != "failed" {
		return ActivityEvent{}, false
	}
	title := issueTypeText(in.IssueType)
	if d := clip(oneLine(in.Description), 40); d != "" {
		title += " · " + d
	}
	st := "已执行"
	if in.Status == "failed" {
		st = "执行失败"
	}
	ev := ActivityEvent{
		ID: "ticket:" + in.ID, Kind: KindChange, Sub: SubTicket, Time: ParseEventTime(in.UpdatedAt),
		Title: title, Detail: "工单 " + in.TicketNo + "，" + st, Actor: in.Submitter, Instance: in.Instance,
		Links: []ActivityLink{{Label: "详情", Path: "/ops/tickets/" + in.ID}},
	}
	return ev, ev.Time > 0 && in.ID != ""
}

func issueTypeText(t string) string {
	switch t {
	case "ddl":
		return "DDL"
	case "dml":
		return "数据库操作"
	case "dql":
		return "数据导出"
	case "dcl":
		return "账号权限"
	case "kafka":
		return "Kafka 变更"
	case "cdc_sync":
		return "CDC"
	case "archive":
		return "数据归档"
	case "flashback":
		return "闪回"
	case "migration":
		return "数据迁移"
	case "createdb":
		return "新增数据库"
	case "ha":
		return "高可用变更"
	case "redis":
		return "Redis 变更"
	case "sr_cluster":
		return "StarRocks 集群变更"
	case "db_deploy":
		return "集群部署"
	case "dns_change":
		return "DNS 变更"
	case "k8s_change":
		return "K8s 变更"
	case "backup_restore":
		return "备份恢复"
	default:
		if t == "" {
			return "工单"
		}
		return t
	}
}

// HAInput 已完成或失败的高可用任务。
type HAInput struct {
	ID, Action, Status, Operator, UpdatedAt, ClusterName string
	Instance                                             *ActivityInstance
}

func MapHA(in HAInput) (ActivityEvent, bool) {
	if in.Status != "executed" && in.Status != "failed" {
		return ActivityEvent{}, false
	}
	st := "已完成"
	if in.Status == "failed" {
		st = "失败"
	}
	title := "高可用 " + haActionText(in.Action)
	if in.ClusterName != "" {
		title += " · " + in.ClusterName
	}
	ev := ActivityEvent{
		ID: "ha:" + in.ID, Kind: KindChange, Sub: SubHA, Time: ParseEventTime(in.UpdatedAt),
		Title: title, Detail: st, Actor: in.Operator, Instance: in.Instance,
		Links: []ActivityLink{{Label: "详情", Path: "/ops/ha"}},
	}
	return ev, ev.Time > 0 && in.ID != ""
}

func haActionText(a string) string {
	switch a {
	case "switchover":
		return "主从切换"
	case "enter_maint":
		return "进入维护"
	case "exit_maint":
		return "退出维护"
	default:
		if a == "" {
			return "变更"
		}
		return a
	}
}

// ConfigInput 实例配置审计。不带变更前后的 JSON，避免把口令带进动态。
type ConfigInput struct {
	ID, Action, Actor, Source, CreatedAt string
	Instance                             *ActivityInstance
}

func MapConfig(in ConfigInput) (ActivityEvent, bool) {
	ev := ActivityEvent{
		ID: "config:" + in.ID, Kind: KindChange, Sub: SubConfig, Time: ParseEventTime(in.CreatedAt),
		Title: "实例配置 " + configActionText(in.Action), Actor: in.Actor, Instance: in.Instance,
		Detail: "来源 " + defaultStr(in.Source, "api"),
		Links:  []ActivityLink{{Label: "审计", Path: "/cmdb/audit"}},
	}
	return ev, ev.Time > 0 && in.ID != ""
}

func configActionText(a string) string {
	switch a {
	case "create":
		return "新建"
	case "update":
		return "修改"
	case "delete":
		return "删除"
	case "import":
		return "导入"
	case "sync":
		return "同步"
	default:
		if a == "" {
			return "变更"
		}
		return a
	}
}

// DNSInput 一个 DNS 变更批次。
type DNSInput struct {
	BatchID, Summary, Actor, Reason string
	Count                           int
	Created                         int64
	Instance                        *ActivityInstance
}

func MapDNS(in DNSInput) ActivityEvent {
	title := strings.TrimSpace(in.Summary)
	if title == "" {
		title = "DNS 变更"
	}
	detail := ""
	if in.Count > 1 {
		detail = "共 " + itoa(in.Count) + " 项"
	}
	if r := strings.TrimSpace(in.Reason); r != "" {
		if detail != "" {
			detail += "，"
		}
		detail += r
	}
	return ActivityEvent{
		ID: "dns:" + in.BatchID, Kind: KindChange, Sub: SubDNS, Time: in.Created,
		Title: title, Detail: clip(detail, 180), Actor: in.Actor, Instance: in.Instance,
		Links: []ActivityLink{{Label: "详情", Path: "/dns/changes"}},
	}
}

// AWRInput 按天聚合的巡检。
type AWRInput struct {
	Date         string
	Total        int
	Anomaly      int
	At           int64
	Instance     *ActivityInstance
	InstanceOnly bool
}

func MapAWR(in AWRInput) (ActivityEvent, bool) {
	if in.Total <= 0 || in.At <= 0 {
		return ActivityEvent{}, false
	}
	title := "AWR 日报完成 " + itoa(in.Total) + " 个实例"
	detail := "全部正常"
	if in.Anomaly > 0 {
		detail = itoa(in.Anomaly) + " 个存在告警项，已生成巡检报告"
	}
	if in.InstanceOnly {
		title = "AWR 日报完成"
	}
	id := "awr:" + in.Date
	if in.Instance != nil && in.Instance.ID != "" {
		id += ":" + in.Instance.ID
	}
	return ActivityEvent{
		ID: id, Kind: KindInspect, Sub: SubAWR, Time: in.At,
		Title: title, Detail: detail, Instance: in.Instance,
		Links: []ActivityLink{{Label: "报告", Path: "/ops/sql-insight"}},
	}, true
}

// MatchInstance 用记录值（IP、域名）命中实例。多个命中时取先登记的那个。
func MatchInstance(refs []InstRef, values []string) *InstRef {
	if len(refs) == 0 || len(values) == 0 {
		return nil
	}
	idx := map[string]int{}
	add := func(k string, i int) {
		k = strings.ToLower(strings.TrimSpace(k))
		if len(k) < 3 {
			return
		}
		if _, ok := idx[k]; !ok {
			idx[k] = i
		}
	}
	for i, r := range refs {
		add(r.Host, i)
		add(r.Domain, i)
		for _, d := range r.Domains {
			add(d, i)
		}
	}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if i, ok := idx[v]; ok {
			r := refs[i]
			return &r
		}
	}
	return nil
}

// SnapValues 从变更前后 JSON 里取出可匹配的值。
func SnapValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var out []string
	for _, k := range []string{"data", "name", "mail_host", "origin"} {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func defaultStr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
