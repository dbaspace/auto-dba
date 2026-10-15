package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// AlertTier 单规则多档阈值（按当前值取最高命中档）。
type AlertTier struct {
	Op       string  `json:"op"` // >= > <= <
	Value    float64 `json:"value"`
	Severity string  `json:"severity"`
	For      string  `json:"for,omitempty"`
}

// AlertNotifyPolicy 规则级频控与未处理续报（仅 trigger/repeat；resolve 不受此限）。
type AlertNotifyPolicy struct {
	MaxFirePerWindow int      `json:"max_fire_per_window"` // 窗口内最多触发/续报次数，0=不限制
	Window           string   `json:"window"`              // 如 15m
	RepeatInterval   string   `json:"repeat_interval"`     // 如 5m；空或 0 关闭
	RepeatOnHandles  []string `json:"repeat_on_handles"`   // 默认 unclaimed
	// InheritGlobal 为 true 时使用全局默认（通知页配置），忽略本规则窗口/次数/续报。
	InheritGlobal bool `json:"inherit_global,omitempty"`
}

const (
	DefaultAlertNotifyMaxFire = 3
	DefaultAlertNotifyWindow  = "15m"
	DefaultAlertNotifyRepeat  = "5m"
)

var (
	globalNotifyMu   sync.RWMutex
	globalNotifyPol  = builtinDefaultAlertNotifyPolicy()
)

func builtinDefaultAlertNotifyPolicy() AlertNotifyPolicy {
	return AlertNotifyPolicy{
		MaxFirePerWindow: DefaultAlertNotifyMaxFire,
		Window:           DefaultAlertNotifyWindow,
		RepeatInterval:   DefaultAlertNotifyRepeat,
		RepeatOnHandles:  []string{AlertHandleUnclaimed},
	}
}

// SyncGlobalAlertNotifyPolicy 由通知配置加载/保存时调用。
func SyncGlobalAlertNotifyPolicy(maxFire int, window, repeat string) {
	p := AlertNotifyPolicy{
		MaxFirePerWindow: maxFire,
		Window:           window,
		RepeatInterval:   repeat,
		RepeatOnHandles:  []string{AlertHandleUnclaimed},
	}
	p = NormalizeAlertNotifyPolicy(&p)
	globalNotifyMu.Lock()
	globalNotifyPol = p
	globalNotifyMu.Unlock()
}

// GlobalAlertNotifyPolicy 返回当前全局默认（副本）。
func GlobalAlertNotifyPolicy() AlertNotifyPolicy {
	globalNotifyMu.RLock()
	defer globalNotifyMu.RUnlock()
	return globalNotifyPol
}

func DefaultAlertNotifyPolicy() AlertNotifyPolicy {
	return GlobalAlertNotifyPolicy()
}

// ResolveAlertNotifyPolicy 规则显式配置优先；InheritGlobal 或空策略用全局。
func ResolveAlertNotifyPolicy(rule AlertNotifyPolicy) AlertNotifyPolicy {
	if rule.InheritGlobal {
		g := GlobalAlertNotifyPolicy()
		if len(rule.RepeatOnHandles) > 0 {
			g.RepeatOnHandles = append([]string(nil), rule.RepeatOnHandles...)
		}
		return g
	}
	return NormalizeAlertNotifyPolicy(&rule)
}

func NormalizeAlertTiers(tiers []AlertTier) ([]AlertTier, error) {
	if len(tiers) == 0 {
		return nil, nil
	}
	out := make([]AlertTier, 0, len(tiers))
	for _, t := range tiers {
		t.Op = strings.TrimSpace(t.Op)
		if t.Op == "" {
			t.Op = ">="
		}
		switch t.Op {
		case ">=", ">", "<=", "<":
		default:
			return nil, fmt.Errorf("不支持的阈值运算符：%s", t.Op)
		}
		if !ValidAlertSeverity(t.Severity) {
			return nil, fmt.Errorf("非法级别：%s", t.Severity)
		}
		if t.For != "" {
			if _, err := ParseAlertDuration(t.For); err != nil {
				return nil, fmt.Errorf("档位 for 无效：%w", err)
			}
		}
		out = append(out, t)
	}
	return out, nil
}

func NormalizeAlertNotifyPolicy(p *AlertNotifyPolicy) AlertNotifyPolicy {
	if p == nil {
		return DefaultAlertNotifyPolicy()
	}
	out := *p
	if out.InheritGlobal {
		// 持久化时保留 inherit，运行时再用 ResolveAlertNotifyPolicy 展开
		if len(out.RepeatOnHandles) == 0 {
			out.RepeatOnHandles = []string{AlertHandleUnclaimed}
		}
		out.MaxFirePerWindow = 0
		out.Window = ""
		out.RepeatInterval = ""
		return out
	}
	if out.MaxFirePerWindow < 0 {
		out.MaxFirePerWindow = 0
	}
	if strings.TrimSpace(out.Window) == "" {
		out.Window = DefaultAlertNotifyWindow
	}
	if _, err := ParseAlertDuration(out.Window); err != nil {
		out.Window = DefaultAlertNotifyWindow
	}
	ri := strings.TrimSpace(out.RepeatInterval)
	if ri == "" || ri == "0" || ri == "0s" || ri == "0m" {
		out.RepeatInterval = ""
	} else if _, err := ParseAlertDuration(ri); err != nil {
		out.RepeatInterval = DefaultAlertNotifyRepeat
	} else {
		out.RepeatInterval = ri
	}
	if len(out.RepeatOnHandles) == 0 {
		out.RepeatOnHandles = []string{AlertHandleUnclaimed}
	}
	return out
}

func encodeAlertTiers(tiers []AlertTier) string {
	if len(tiers) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(tiers)
	return string(b)
}

func decodeAlertTiers(raw string) []AlertTier {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []AlertTier
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func encodeAlertNotifyPolicy(p AlertNotifyPolicy) string {
	b, _ := json.Marshal(p)
	if len(b) == 0 {
		return "{}"
	}
	return string(b)
}

func decodeAlertNotifyPolicy(raw string) AlertNotifyPolicy {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return AlertNotifyPolicy{InheritGlobal: true}
	}
	var p AlertNotifyPolicy
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return AlertNotifyPolicy{InheritGlobal: true}
	}
	// 旧默认 1h/30m 视为「未定制」，改为继承全局（便于全局 15m 生效）
	if !p.InheritGlobal && isLegacyDefaultNotifyPolicy(p) {
		p.InheritGlobal = true
	}
	return p
}

func isLegacyDefaultNotifyPolicy(p AlertNotifyPolicy) bool {
	w := strings.TrimSpace(p.Window)
	ri := strings.TrimSpace(p.RepeatInterval)
	if w != "1h" {
		return false
	}
	if ri != "30m" && ri != "" {
		return false
	}
	if p.MaxFirePerWindow != 0 && p.MaxFirePerWindow != 3 {
		return false
	}
	return true
}

func severityRank(s string) int {
	switch s {
	case AlertSeverityP0:
		return 4
	case AlertSeverityP1:
		return 3
	case AlertSeverityP2:
		return 2
	case AlertSeverityP3:
		return 1
	default:
		return 0
	}
}

func tierMatches(t AlertTier, v float64) bool {
	switch t.Op {
	case ">=":
		return v >= t.Value
	case ">":
		return v > t.Value
	case "<=":
		return v <= t.Value
	case "<":
		return v < t.Value
	default:
		return false
	}
}

// PickAlertTier 在命中档位中取级别最高者；无命中返回 nil。
func PickAlertTier(tiers []AlertTier, value float64) *AlertTier {
	var hit []AlertTier
	for _, t := range tiers {
		if tierMatches(t, value) {
			hit = append(hit, t)
		}
	}
	if len(hit) == 0 {
		return nil
	}
	sort.Slice(hit, func(i, j int) bool {
		ri, rj := severityRank(hit[i].Severity), severityRank(hit[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return hit[i].Value > hit[j].Value
	})
	t := hit[0]
	return &t
}

func (r *AlertRule) HasTiers() bool {
	return r != nil && len(r.Tiers) > 0
}

// TierForDuration 档位 for，缺省用规则 for。
func (r *AlertRule) TierForDuration(t *AlertTier) string {
	if t != nil && strings.TrimSpace(t.For) != "" {
		return t.For
	}
	if r != nil {
		return r.ForDuration
	}
	return "5m"
}

// NotifyWindowElapsed 距窗口起点是否已过期（用于重置计数）。
func NotifyWindowElapsed(lastNotifiedAt, window string, now time.Time) bool {
	if strings.TrimSpace(lastNotifiedAt) == "" {
		return true
	}
	w, err := ParseAlertDuration(window)
	if err != nil || w <= 0 {
		w = time.Hour
	}
	t, err := time.Parse(time.RFC3339, lastNotifiedAt)
	if err != nil {
		return true
	}
	return !now.Before(t.Add(w))
}

func HandleInList(handle string, list []string) bool {
	for _, h := range list {
		if strings.EqualFold(strings.TrimSpace(h), handle) {
			return true
		}
	}
	return false
}

// AlertNotifyDecision 是否应外发/续报。
type AlertNotifyDecision struct {
	Should bool
	Reason string // trigger | repeat | "" 
	Skip   string // rate_limited | silenced | handle | not_due | ""
}

// DecideAlertNotify 根据频控与未处理续报策略决定是否通知（仅 trigger/repeat）。
// justFired：本轮由 pending→firing 或新建即 firing。
func DecideAlertNotify(a *Alert, policy AlertNotifyPolicy, now time.Time, justFired bool) AlertNotifyDecision {
	policy = ResolveAlertNotifyPolicy(policy)
	if a == nil || a.Status != AlertStatusFiring {
		return AlertNotifyDecision{Skip: "not_firing"}
	}
	if a.Handle == AlertHandleSilenced || a.Handle == AlertHandleResolved {
		return AlertNotifyDecision{Skip: "silenced"}
	}
	count := a.NotifyCount
	if NotifyWindowElapsed(a.LastNotifiedAt, policy.Window, now) {
		count = 0
	}
	if policy.MaxFirePerWindow > 0 && count >= policy.MaxFirePerWindow {
		return AlertNotifyDecision{Skip: "rate_limited"}
	}
	if justFired {
		return AlertNotifyDecision{Should: true, Reason: "trigger"}
	}
	if policy.RepeatInterval == "" {
		return AlertNotifyDecision{Skip: "not_due"}
	}
	if !HandleInList(a.Handle, policy.RepeatOnHandles) {
		return AlertNotifyDecision{Skip: "handle"}
	}
	ri, err := ParseAlertDuration(policy.RepeatInterval)
	if err != nil || ri <= 0 {
		return AlertNotifyDecision{Skip: "not_due"}
	}
	if strings.TrimSpace(a.LastNotifiedAt) == "" {
		return AlertNotifyDecision{Should: true, Reason: "repeat"}
	}
	t, err := time.Parse(time.RFC3339, a.LastNotifiedAt)
	if err != nil {
		return AlertNotifyDecision{Should: true, Reason: "repeat"}
	}
	if now.Before(t.Add(ri)) {
		return AlertNotifyDecision{Skip: "not_due"}
	}
	return AlertNotifyDecision{Should: true, Reason: "repeat"}
}

// ApplyAlertNotifyMark 写入告警行通知计数与时间（调用方随后 SaveAlert）。
// 指纹级预算请用 DB.ApplyFingerprintNotifyMark。
func ApplyAlertNotifyMark(a *Alert, policy AlertNotifyPolicy, now time.Time) {
	if a == nil {
		return
	}
	policy = ResolveAlertNotifyPolicy(policy)
	if NotifyWindowElapsed(a.LastNotifiedAt, policy.Window, now) {
		a.NotifyCount = 1
	} else {
		a.NotifyCount++
	}
	a.LastNotifiedAt = now.UTC().Format(time.RFC3339)
}

// FormatTierThreshold 档位阈值展示。
func FormatTierThreshold(t *AlertTier) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%s %g", t.Op, t.Value)
}
