package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	AlertScopeModeGlobal    = "global"
	AlertScopeModeInstances = "instances"
	AlertScopeModeCluster   = "cluster"
	AlertScopeModeTags      = "tags"

	AlertMatcherKeyAuto            = "auto"
	AlertMatcherKeyInstance        = "instance"
	AlertMatcherKeyDBInstanceName  = "d_b_instance_name"
	AlertMatcherKeyDomain          = "domain"

	AlertScopeToken = "__SCOPE__"
	// MaxAlertScopeInstanceIDs 作用域可保存的实例 ID 上限（折叠前）；过大请用标签。
	MaxAlertScopeInstanceIDs = 1000
	// MaxAlertScopeInstances regex 交替取值硬上限；无法折叠时超过则拒绝。
	MaxAlertScopeInstances = 200
	// AlertScopeCollapseAt 达到该数量时优先折叠为公共 Prom 标签（idc/env），避免超长 =~ 正则。
	AlertScopeCollapseAt = 50
	// MaxAlertSeriesPerEval 单规则单轮评估最多处理的 series，防止告警风暴拖垮写库。
	MaxAlertSeriesPerEval = 500
)

// AlertScopeCluster 监控域 idc + domain。
type AlertScopeCluster struct {
	IDC    string `json:"idc"`
	Domain string `json:"domain"`
}

// AlertScope 规则作用域；编译后 Matcher 供 UI 展示。
// MatcherKey 决定勾选实例时注入的 label：auto|instance|d_b_instance_name|domain|自定义。
type AlertScope struct {
	Mode        string             `json:"mode"`
	MatcherKey  string             `json:"matcher_key,omitempty"`
	InstanceIDs []string           `json:"instance_ids,omitempty"`
	Cluster     *AlertScopeCluster `json:"cluster,omitempty"`
	Tags        []string           `json:"tags,omitempty"`
	Matcher     string             `json:"matcher,omitempty"`
}

func DefaultAlertScope() AlertScope {
	return AlertScope{Mode: AlertScopeModeGlobal, MatcherKey: AlertMatcherKeyAuto}
}

func NormalizeAlertScope(s *AlertScope) error {
	if s == nil {
		return errors.New("scope 不能为空")
	}
	s.Mode = strings.ToLower(strings.TrimSpace(s.Mode))
	if s.Mode == "" {
		s.Mode = AlertScopeModeGlobal
	}
	s.MatcherKey = strings.TrimSpace(s.MatcherKey)
	if s.MatcherKey == "" {
		s.MatcherKey = AlertMatcherKeyAuto
	}
	if err := validateMatcherKey(s.MatcherKey); err != nil {
		return err
	}
	switch s.Mode {
	case AlertScopeModeGlobal:
		s.InstanceIDs = nil
		s.Cluster = nil
		s.Tags = nil
	case AlertScopeModeInstances:
		s.InstanceIDs = uniqueTrimmed(s.InstanceIDs)
		if len(s.InstanceIDs) == 0 {
			return errors.New("请勾选至少一个实例")
		}
		if len(s.InstanceIDs) > MaxAlertScopeInstanceIDs {
			return fmt.Errorf("勾选实例过多（%d，上限 %d）。请改用「按标签/按集群」；同环境数百台可依赖 idc/env 折叠",
				len(s.InstanceIDs), MaxAlertScopeInstanceIDs)
		}
		s.Cluster = nil
		s.Tags = nil
	case AlertScopeModeCluster:
		if s.Cluster == nil {
			return errors.New("请填写集群 idc 与 domain")
		}
		s.Cluster.IDC = strings.TrimSpace(s.Cluster.IDC)
		s.Cluster.Domain = strings.TrimSpace(s.Cluster.Domain)
		if s.Cluster.IDC == "" || s.Cluster.Domain == "" {
			return errors.New("集群需同时填写 idc 与 domain")
		}
		s.InstanceIDs = nil
		s.Tags = nil
	case AlertScopeModeTags:
		s.Tags = uniqueTrimmed(s.Tags)
		if len(s.Tags) == 0 {
			return errors.New("请至少填写一个标签（key=value）")
		}
		for _, t := range s.Tags {
			if !strings.Contains(t, "=") {
				return fmt.Errorf("标签格式应为 key=value：%s", t)
			}
		}
		s.InstanceIDs = nil
		s.Cluster = nil
	default:
		return fmt.Errorf("不支持的作用域 mode：%s", s.Mode)
	}
	return nil
}

func validateMatcherKey(key string) error {
	k := strings.TrimSpace(key)
	if k == "" || k == AlertMatcherKeyAuto {
		return nil
	}
	// Prometheus label name: [a-zA-Z_][a-zA-Z0-9_]*
	if len(k) == 0 || (k[0] >= '0' && k[0] <= '9') {
		return fmt.Errorf("非法 matcher_key：%s", key)
	}
	for _, c := range k {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return fmt.Errorf("非法 matcher_key：%s", key)
	}
	return nil
}

func isPostgresDBType(dbType string) bool {
	t := strings.ToLower(strings.TrimSpace(dbType))
	return t == "postgres" || t == "postgresql" || t == "pg"
}

func resolveMatcherKey(key string, list []Instance) (string, error) {
	k := strings.TrimSpace(key)
	if k == "" || k == AlertMatcherKeyAuto {
		if len(list) == 0 {
			return AlertMatcherKeyInstance, nil
		}
		pg, nonPG := 0, 0
		for _, it := range list {
			if isPostgresDBType(it.DBType) {
				pg++
			} else {
				nonPG++
			}
		}
		if pg > 0 && nonPG > 0 {
			return "", errors.New("勾选了 postgres 与非 postgres 实例，请分拆规则或手动指定 matcher_key")
		}
		if pg > 0 {
			return AlertMatcherKeyDBInstanceName, nil
		}
		return AlertMatcherKeyInstance, nil
	}
	return k, nil
}

// InjectScopeMatchers 将 __SCOPE__ 替换为 matcher，并清理多余逗号 / 空选择器。
func InjectScopeMatchers(tmpl, matcher string) string {
	sel := strings.TrimSpace(matcher)
	out := strings.ReplaceAll(tmpl, AlertScopeToken, sel)
	out = strings.ReplaceAll(out, "{,", "{")
	out = strings.ReplaceAll(out, ",}", "}")
	out = strings.ReplaceAll(out, ",,", ",")
	out = strings.ReplaceAll(out, "{}", "")
	return strings.TrimSpace(out)
}

func encodeAlertScope(s AlertScope) string {
	b, _ := json.Marshal(s)
	if len(b) == 0 {
		return `{"mode":"global"}`
	}
	return string(b)
}

func decodeAlertScope(raw string) AlertScope {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultAlertScope()
	}
	var s AlertScope
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return DefaultAlertScope()
	}
	if strings.TrimSpace(s.Mode) == "" {
		s.Mode = AlertScopeModeGlobal
	}
	return s
}

func promRegexAlt(vals []string) string {
	clean := uniqueTrimmed(vals)
	sort.Strings(clean)
	parts := make([]string, 0, len(clean))
	for _, v := range clean {
		// QuoteMeta → \. ；PromQL 双引号字符串需写成 \\. 才能到达正则引擎
		parts = append(parts, strings.ReplaceAll(regexp.QuoteMeta(v), `\`, `\\`))
	}
	return strings.Join(parts, "|")
}

func instancePromEndpoints(inst Instance) (domains, endpoints []string) {
	seenD, seenE := map[string]struct{}{}, map[string]struct{}{}
	addD := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seenD[v]; ok {
			return
		}
		seenD[v] = struct{}{}
		domains = append(domains, v)
	}
	addE := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seenE[v]; ok {
			return
		}
		seenE[v] = struct{}{}
		endpoints = append(endpoints, v)
	}
	for _, d := range append([]string{inst.DomainName}, inst.Domains...) {
		addD(d)
		if d != "" && inst.Port > 0 {
			addE(fmt.Sprintf("%s:%d", d, inst.Port))
		}
	}
	host := strings.TrimSpace(inst.Host)
	if host != "" {
		if inst.Port > 0 {
			addE(fmt.Sprintf("%s:%d", host, inst.Port))
		} else {
			addE(host)
		}
	}
	for _, raw := range inst.Tags {
		k, v := splitAlertTagKV(raw)
		if strings.EqualFold(k, "domain") {
			addD(v)
		}
	}
	return domains, endpoints
}

func splitAlertTagKV(raw string) (key, val string) {
	raw = strings.TrimSpace(raw)
	i := strings.IndexByte(raw, '=')
	if i <= 0 {
		return "", ""
	}
	return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
}

func matcherFromInstances(list []Instance) (string, error) {
	return matcherFromInstancesWithKey(list, AlertMatcherKeyInstance)
}

func instanceLabelValues(list []Instance, key string) []string {
	var vals []string
	for _, it := range list {
		switch key {
		case AlertMatcherKeyDBInstanceName:
			if n := strings.TrimSpace(it.Name); n != "" {
				vals = append(vals, n)
			}
			if d := strings.TrimSpace(it.DomainName); d != "" {
				vals = append(vals, d)
			}
			for _, d := range it.Domains {
				if x := strings.TrimSpace(d); x != "" {
					vals = append(vals, x)
				}
			}
		case AlertMatcherKeyDomain:
			d, _ := instancePromEndpoints(it)
			vals = append(vals, d...)
		default: // instance 或自定义：尽量收集 name/domain/host:port
			if n := strings.TrimSpace(it.Name); n != "" && key != AlertMatcherKeyInstance {
				vals = append(vals, n)
			}
			d, e := instancePromEndpoints(it)
			vals = append(vals, d...)
			vals = append(vals, e...)
			if key != AlertMatcherKeyInstance {
				if h := strings.TrimSpace(it.Host); h != "" {
					vals = append(vals, h)
				}
			}
		}
	}
	return uniqueTrimmed(vals)
}

func matcherFromInstancesWithKey(list []Instance, matcherKey string) (string, error) {
	if len(list) == 0 {
		return "", errors.New("作用域内无可用实例")
	}
	// 大批量时优先折叠为短标签 matcher（可超过硬上限，例如同 idc 下 500 台）
	if len(list) >= AlertScopeCollapseAt {
		if m, ok := collapseInstancesToLabelMatcher(list); ok {
			return m, nil
		}
	}
	if len(list) > MaxAlertScopeInstances {
		return "", fmt.Errorf("勾选实例过多（%d，上限 %d）且无法折叠为统一 idc/env。请改用「按标签/按集群」或按环境拆分规则",
			len(list), MaxAlertScopeInstances)
	}
	key, err := resolveMatcherKey(matcherKey, list)
	if err != nil {
		return "", err
	}
	vals := instanceLabelValues(list, key)
	if key == AlertMatcherKeyInstance {
		// instance 标签：domain + host:port（不含 name，避免与 job 混淆）
		vals = nil
		for _, it := range list {
			d, e := instancePromEndpoints(it)
			vals = append(vals, d...)
			vals = append(vals, e...)
		}
		vals = uniqueTrimmed(vals)
	}
	if len(vals) == 0 {
		return "", fmt.Errorf("所选实例缺少可用于 %s 的名称/域名/地址", key)
	}
	if len(vals) > MaxAlertScopeInstances {
		return "", fmt.Errorf("生成的匹配值过多（%d，上限 %d）。请改用「按标签/按集群」",
			len(vals), MaxAlertScopeInstances)
	}
	// 不对 d_b_instance_name 自动附加 idc：实例表 IDC 常与云监控 label 不一致，会导致试跑 0 命中。
	return fmt.Sprintf(`%s=~"%s"`, key, promRegexAlt(vals)), nil
}

// collapseInstancesToLabelMatcher 当大批量实例共享同一 idc/env 时，用短 matcher 替代超长 regex。
func collapseInstancesToLabelMatcher(list []Instance) (string, bool) {
	if len(list) == 0 {
		return "", false
	}
	idc0 := strings.TrimSpace(list[0].IDC)
	sameIDC := idc0 != ""
	env0 := ""
	sameEnv := true
	for i, it := range list {
		if sameIDC && strings.TrimSpace(it.IDC) != idc0 {
			sameIDC = false
		}
		ev := ""
		for _, raw := range it.Tags {
			k, v := splitAlertTagKV(raw)
			if strings.EqualFold(k, "env") && v != "" {
				ev = v
				break
			}
		}
		if i == 0 {
			env0 = ev
		}
		if env0 == "" || ev != env0 {
			sameEnv = false
		}
	}
	if sameIDC {
		return fmt.Sprintf(`idc=%q`, idc0), true
	}
	if sameEnv && env0 != "" {
		return fmt.Sprintf(`env=%q`, env0), true
	}
	return "", false
}

func matcherFromPromFriendlyTags(tags []string) (string, bool) {
	allowed := map[string]bool{"idc": true, "domain": true, "env": true, "job": true, "service": true, "app": true}
	var parts []string
	for _, t := range tags {
		k, v := splitAlertTagKV(t)
		if k == "" || v == "" || !allowed[strings.ToLower(k)] {
			continue
		}
		parts = append(parts, fmt.Sprintf(`%s=%q`, k, v))
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, ","), true
}

func instanceHasAllTags(it Instance, tags []string) bool {
	set := map[string]struct{}{}
	for _, t := range it.Tags {
		set[strings.TrimSpace(t)] = struct{}{}
	}
	// 也把 idc/domain 字段视作标签
	if idc := strings.TrimSpace(it.IDC); idc != "" {
		set["idc="+idc] = struct{}{}
	}
	if d := strings.TrimSpace(it.DomainName); d != "" {
		set["domain="+d] = struct{}{}
	}
	for _, want := range tags {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		if _, ok := set[want]; ok {
			continue
		}
		// 大小写不敏感扫描
		found := false
		for have := range set {
			if strings.EqualFold(have, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// BuildScopeMatcher 根据 scope 生成注入用的 matcher（不含花括号）。
func (d *DB) BuildScopeMatcher(scope AlertScope) (matcher string, err error) {
	if err := NormalizeAlertScope(&scope); err != nil {
		return "", err
	}
	switch scope.Mode {
	case AlertScopeModeGlobal:
		return "", nil
	case AlertScopeModeCluster:
		return fmt.Sprintf(`idc=%q,domain=%q`, scope.Cluster.IDC, scope.Cluster.Domain), nil
	case AlertScopeModeInstances:
		list := make([]Instance, 0, len(scope.InstanceIDs))
		for _, id := range scope.InstanceIDs {
			it, err := d.GetInstance(id)
			if err != nil {
				return "", err
			}
			if it != nil {
				list = append(list, *it)
			}
		}
		return matcherFromInstancesWithKey(list, scope.MatcherKey)
	case AlertScopeModeTags:
		if m, ok := matcherFromPromFriendlyTags(scope.Tags); ok {
			// 若标签可直接作为 Prom label，且实例很多时优先走标签 matcher
			all, err := d.ListInstances("", "")
			if err != nil {
				return "", err
			}
			matched := make([]Instance, 0)
			for _, it := range all {
				if instanceHasAllTags(it, scope.Tags) {
					matched = append(matched, it)
				}
			}
			if len(matched) == 0 {
				// 无实例命中时仍用标签 matcher（依赖采集 relabel）
				return m, nil
			}
			if len(matched) > MaxAlertScopeInstances {
				return m, nil
			}
			return matcherFromInstancesWithKey(matched, scope.MatcherKey)
		}
		all, err := d.ListInstances("", "")
		if err != nil {
			return "", err
		}
		matched := make([]Instance, 0)
		for _, it := range all {
			if instanceHasAllTags(it, scope.Tags) {
				matched = append(matched, it)
			}
		}
		return matcherFromInstancesWithKey(matched, scope.MatcherKey)
	default:
		return "", fmt.Errorf("不支持的作用域 mode：%s", scope.Mode)
	}
}

// CompileAlertRuleScope 写入 CompiledExpr / CompileError / Scope.Matcher。
func (d *DB) CompileAlertRuleScope(r *AlertRule) error {
	if r == nil {
		return errors.New("rule 不能为空")
	}
	scope := r.Scope
	if strings.TrimSpace(scope.Mode) == "" {
		scope = DefaultAlertScope()
	}
	if err := NormalizeAlertScope(&scope); err != nil {
		r.CompileError = err.Error()
		r.CompiledExpr = ""
		r.CompiledAt = nowRFC3339(time.Now())
		r.Scope = scope
		return err
	}
	tmpl := strings.TrimSpace(r.MetricExpr)
	if tmpl == "" {
		tmpl = strings.TrimSpace(r.Expr)
	}
	if scope.Mode != AlertScopeModeGlobal && !strings.Contains(tmpl, AlertScopeToken) {
		err := fmt.Errorf("非全局作用域时，PromQL 模板须包含 %s", AlertScopeToken)
		r.CompileError = err.Error()
		r.CompiledExpr = ""
		r.CompiledAt = nowRFC3339(time.Now())
		r.Scope = scope
		return err
	}
	matcher, err := d.BuildScopeMatcher(scope)
	if err != nil {
		r.CompileError = err.Error()
		r.CompiledExpr = ""
		r.CompiledAt = nowRFC3339(time.Now())
		r.Scope = scope
		return err
	}
	scope.Matcher = matcher
	compiled := InjectScopeMatchers(tmpl, matcher)
	if scope.Mode == AlertScopeModeGlobal && !strings.Contains(tmpl, AlertScopeToken) {
		compiled = tmpl
	}
	r.Scope = scope
	r.CompiledExpr = compiled
	r.CompileError = ""
	r.CompiledAt = nowRFC3339(time.Now())
	if scope.Mode == AlertScopeModeCluster && scope.Cluster != nil && r.Cluster == "" {
		r.Cluster = scope.Cluster.Domain
	}
	return nil
}

// RefreshAllAlertRuleCompiled 重编译全部规则（作用域成员变化时调用）。
func (d *DB) RefreshAllAlertRuleCompiled() (int, error) {
	list, err := d.ListAlertRules()
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range list {
		r := list[i]
		_ = d.CompileAlertRuleScope(&r)
		if err := d.saveAlertRuleCompiled(&r); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (d *DB) saveAlertRuleCompiled(r *AlertRule) error {
	_, err := d.Exec(`UPDATE tbl_alert_rules SET scope_json=?, compiled_expr=?, compiled_at=?, compile_error=?, cluster=?, updated_at=? WHERE id=?`,
		encodeAlertScope(r.Scope), r.CompiledExpr, r.CompiledAt, r.CompileError, r.Cluster, nowRFC3339(time.Now()), r.ID)
	return err
}

// PatchAlertRuleStats 仅更新健康度/触发计数，避免评估热路径全量 Save。
func (d *DB) PatchAlertRuleStats(id string, health, triggered7d int) error {
	_, err := d.Exec(`UPDATE tbl_alert_rules SET health=?, triggered_7d=?, updated_at=? WHERE id=?`,
		health, triggered7d, nowRFC3339(time.Now()), id)
	return err
}

// EvalExpr 返回 Ruler/试跑应使用的表达式。
func (r *AlertRule) EvalExpr() string {
	if r == nil {
		return ""
	}
	if e := strings.TrimSpace(r.CompiledExpr); e != "" {
		return e
	}
	// 兼容未编译的旧规则：全局直用 expr
	if r.Scope.Mode == "" || r.Scope.Mode == AlertScopeModeGlobal {
		return strings.TrimSpace(r.Expr)
	}
	return ""
}
