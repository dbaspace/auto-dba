package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/aiops"
	"auto-dba/internal/alert"
	"auto-dba/internal/middleware"
	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

type AlertHandler struct {
	Store  *store.DB
	Secret string
	AIOps  *aiops.Service
}

func (h *AlertHandler) aiopsSvc() *aiops.Service {
	if h.AIOps != nil {
		return h.AIOps
	}
	h.AIOps = aiops.New(h.Store)
	return h.AIOps
}

func (h *AlertHandler) List(c *gin.Context) {
	f := store.AlertFilter{
		Tab:      c.DefaultQuery("tab", "active"),
		Severity: strings.TrimSpace(c.Query("severity")),
		Service:  strings.TrimSpace(c.Query("service")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Since:    strings.TrimSpace(c.Query("since")),
		TenantID: middleware.TenantID(c),
	}
	if f.Tab == "history" {
		rows, err := h.Store.ListAlertHistoryRows(f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
		return
	}
	list, err := h.Store.ListAlerts(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AlertHandler) Stats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.Store.AlertStatsInTenant(effectiveListTenantID(h.Store, c))})
}

func (h *AlertHandler) Get(c *gin.Context) {
	a, err := h.Store.GetAlertInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "告警不存在"})
		return
	}
	events, _ := h.Store.ListAlertEvents(a.ID)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"alert": a, "events": events}})
}

func (h *AlertHandler) Events(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	events, err := h.Store.ListAlertEvents(a.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": events})
}

// ContextBundle AIOps Phase0：告警诊断上下文包（CMDB / 案例 / 历史 / 漂移等）。
func (h *AlertHandler) ContextBundle(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	bundle, err := h.aiopsSvc().BuildAlertContextBundle(c.Request.Context(), a.ID, effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if bundle == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "告警不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": bundle})
}

func (h *AlertHandler) loadAlert(c *gin.Context) *store.Alert {
	a, err := h.Store.GetAlertInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "告警不存在"})
		return nil
	}
	return a
}

func (h *AlertHandler) Claim(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	if a.Status == store.AlertStatusResolved {
		c.JSON(http.StatusBadRequest, gin.H{"message": "告警已恢复"})
		return
	}
	user := middleware.Username(c)
	a.Owner = user
	a.Handle = store.AlertHandleAcknowledged
	if err := h.Store.SaveAlert(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddAlertEvent(&store.AlertEvent{AlertID: a.ID, Action: "claim", Detail: "告警已认领", Operator: user})
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AlertHandler) Handover(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	var b struct {
		Owner string `json:"owner"`
		Note  string `json:"note"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	owner := strings.TrimSpace(b.Owner)
	if owner == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定接收人"})
		return
	}
	user := middleware.Username(c)
	old := a.Owner
	a.Owner = owner
	a.Handle = store.AlertHandleAcknowledged
	if err := h.Store.SaveAlert(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	msg := "转派：" + old + " → " + owner
	if n := strings.TrimSpace(b.Note); n != "" {
		msg += "；" + n
	}
	_ = h.Store.AddAlertEvent(&store.AlertEvent{AlertID: a.ID, Action: "handover", Detail: msg, Operator: user})
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AlertHandler) Processing(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	user := middleware.Username(c)
	if a.Owner == "" {
		a.Owner = user
	}
	a.Handle = store.AlertHandleProcessing
	if err := h.Store.SaveAlert(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddAlertEvent(&store.AlertEvent{AlertID: a.ID, Action: "processing", Detail: "进入处理中", Operator: user})
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AlertHandler) Resolve(c *gin.Context) {
	h.closeAlert(c, "resolve", "告警已关闭")
}

func (h *AlertHandler) FalsePositive(c *gin.Context) {
	h.closeAlert(c, "false_positive", "标记为误报并关闭")
}

func (h *AlertHandler) closeAlert(c *gin.Context, action, detail string) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	var b struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&b)
	user := middleware.Username(c)
	a.Status = store.AlertStatusResolved
	a.Handle = store.AlertHandleResolved
	a.ResolvedAt = time.Now().UTC().Format(time.RFC3339)
	if err := h.Store.SaveAlert(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	msg := detail
	if n := strings.TrimSpace(b.Note); n != "" {
		msg += "：" + n
	}
	_ = h.Store.AddAlertEvent(&store.AlertEvent{AlertID: a.ID, Action: action, Detail: msg, Operator: user})
	c.JSON(http.StatusOK, gin.H{"data": a})
}

// CreateTodo 从告警创建待办（source=alert）。
func (h *AlertHandler) CreateTodo(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	user := middleware.Username(c)
	desc := ""
	if a.Annotations != nil {
		desc = a.Annotations["summary"]
	}
	td := &store.Todo{
		Title: a.Title, Source: store.TodoSourceAlert, SourceRef: a.ID,
		Owner: user, PlanAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		Description: desc, Status: store.TodoStatusPending, CreatedBy: user,
		LatestProgress: "由告警 " + a.ID + " 转待办",
	}
	if err := h.Store.CreateTodo(td); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AddAlertEvent(&store.AlertEvent{
		AlertID: a.ID, Action: "todo", Detail: "已转待办 " + td.ID, Operator: user,
	})
	c.JSON(http.StatusOK, gin.H{"data": td})
}

func (h *AlertHandler) ListRules(c *gin.Context) {
	list, err := h.Store.ListAlertRulesInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AlertHandler) ListExprTemplates(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": store.ListAlertExprTemplates()})
}

func (h *AlertHandler) GetRule(c *gin.Context) {
	r, err := h.Store.GetAlertRuleInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if r == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "规则不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

type alertRuleBody struct {
	Name         string                   `json:"name"`
	Expr         string                   `json:"expr"`
	MetricExpr   string                   `json:"metric_expr"`
	Severity     string                   `json:"severity"`
	ForDuration  string                   `json:"for_duration"`
	Labels       map[string]string        `json:"labels"`
	Annotations  map[string]string        `json:"annotations"`
	Cluster      string                   `json:"cluster"`
	Scope        *store.AlertScope        `json:"scope"`
	Tiers        []store.AlertTier        `json:"tiers"`
	NotifyPolicy *store.AlertNotifyPolicy `json:"notify_policy"`
	Enabled      *bool                    `json:"enabled"`
}

func (h *AlertHandler) CreateRule(c *gin.Context) {
	var b alertRuleBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	r := &store.AlertRule{
		Name: b.Name, Expr: b.Expr, MetricExpr: b.MetricExpr, Severity: b.Severity, ForDuration: b.ForDuration,
		Labels: b.Labels, Annotations: b.Annotations, Cluster: b.Cluster, Tiers: b.Tiers,
		Enabled: true, CreatedBy: middleware.Username(c), Scope: store.DefaultAlertScope(),
		NotifyPolicy: store.DefaultAlertNotifyPolicy(),
	}
	if b.Scope != nil {
		r.Scope = *b.Scope
	}
	if b.NotifyPolicy != nil {
		r.NotifyPolicy = *b.NotifyPolicy
	}
	if b.Enabled != nil {
		r.Enabled = *b.Enabled
	}
	stampTenantID(c, func(tid string) { r.TenantID = tid })
	if err := h.Store.CreateAlertRule(r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *AlertHandler) UpdateRule(c *gin.Context) {
	r, err := h.Store.GetAlertRuleInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if r == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "规则不存在"})
		return
	}
	var b alertRuleBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	r.Name, r.Expr, r.MetricExpr, r.Severity = b.Name, b.Expr, b.MetricExpr, b.Severity
	r.ForDuration, r.Cluster = b.ForDuration, b.Cluster
	r.Tiers = b.Tiers
	if b.NotifyPolicy != nil {
		r.NotifyPolicy = *b.NotifyPolicy
	}
	if b.Scope != nil {
		r.Scope = *b.Scope
	}
	if b.Labels != nil {
		r.Labels = b.Labels
	}
	if b.Annotations != nil {
		r.Annotations = b.Annotations
	}
	if b.Enabled != nil {
		r.Enabled = *b.Enabled
	}
	if err := h.Store.SaveAlertRule(r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *AlertHandler) DeleteRule(c *gin.Context) {
	exist, err := h.Store.GetAlertRuleInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "规则不存在"})
		return
	}
	if err := h.Store.DeleteAlertRule(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": true})
}

func (h *AlertHandler) TestRule(c *gin.Context) {
	var b struct {
		Expr       string            `json:"expr"`
		MetricExpr string            `json:"metric_expr"`
		Tiers      []store.AlertTier `json:"tiers"`
		Scope      *store.AlertScope `json:"scope"`
	}
	_ = c.ShouldBindJSON(&b)

	tmpl := strings.TrimSpace(b.MetricExpr)
	if tmpl == "" {
		tmpl = strings.TrimSpace(b.Expr)
	}
	scope := store.DefaultAlertScope()
	var tiers []store.AlertTier
	if id := strings.TrimSpace(c.Param("id")); id != "" && id != "test" {
		r, err := h.Store.GetAlertRuleInTenant(id, effectiveListTenantID(h.Store, c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if r != nil {
			if tmpl == "" {
				if r.MetricExpr != "" {
					tmpl = r.MetricExpr
				} else {
					tmpl = r.Expr
				}
			}
			scope = r.Scope
			tiers = r.Tiers
		}
	}
	if b.Scope != nil {
		scope = *b.Scope
	}
	if len(b.Tiers) > 0 {
		tiers = b.Tiers
	}
	if tmpl == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无表达式可测试"})
		return
	}
	tmp := &store.AlertRule{Expr: tmpl, MetricExpr: tmpl, Scope: scope, Tiers: tiers}
	if err := h.Store.CompileAlertRuleScope(tmp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	expr := tmp.EvalExpr()
	if expr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "编译后表达式为空，请检查作用域"})
		return
	}
	cli, err := h.promClient()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if cli == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "未配置 Prometheus 地址（SQL 洞察设置 → awr_prometheus_base_url）"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	samples, err := cli.Query(ctx, expr, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	series := alert.SamplesToSeries(samples)
	items := make([]gin.H, 0, len(series))
	instances := make([]string, 0)
	seenInst := map[string]struct{}{}
	hitTiers := 0
	for _, s := range series {
		sev := ""
		if len(tiers) > 0 {
			if t := store.PickAlertTier(tiers, s.Value); t != nil {
				sev = t.Severity
				hitTiers++
			}
		}
		items = append(items, gin.H{"labels": s.Labels, "value": s.Value, "tier_severity": sev})
		if inst := s.Labels["instance"]; inst != "" {
			if _, ok := seenInst[inst]; !ok {
				seenInst[inst] = struct{}{}
				instances = append(instances, inst)
			}
		}
		if inst := s.Labels["d_b_instance_name"]; inst != "" {
			if _, ok := seenInst[inst]; !ok {
				seenInst[inst] = struct{}{}
				instances = append(instances, inst)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"count": len(items), "series": items, "instances": instances,
		"tier_hits": hitTiers, "compiled_expr": tmp.CompiledExpr, "matcher": tmp.Scope.Matcher,
	}})
}

func (h *AlertHandler) CompilePreview(c *gin.Context) {
	var b struct {
		Expr       string           `json:"expr"`
		MetricExpr string           `json:"metric_expr"`
		Scope      store.AlertScope `json:"scope"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	tmpl := strings.TrimSpace(b.MetricExpr)
	if tmpl == "" {
		tmpl = b.Expr
	}
	r := &store.AlertRule{Expr: tmpl, MetricExpr: tmpl, Scope: b.Scope}
	_ = h.Store.CompileAlertRuleScope(r)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"matcher":       r.Scope.Matcher,
		"compiled_expr": r.CompiledExpr,
		"compile_error": r.CompileError,
		"compiled_at":   r.CompiledAt,
		"scope":         r.Scope,
	}})
}

func (h *AlertHandler) promClient() (*promclient.Client, error) {
	base, _ := h.Store.GetGlobalArg("awr_prometheus_base_url")
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, nil
	}
	token, _ := h.Store.GetGlobalArg("awr_prometheus_auth_token")
	return promclient.New(promclient.Config{BaseURL: base, AuthToken: token}), nil
}

func (h *AlertHandler) ListSilences(c *gin.Context) {
	list, err := h.Store.ListAlertSilencesInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	tab := c.Query("tab")
	out := make([]store.AlertSilence, 0, len(list))
	for _, s := range list {
		n, _ := h.Store.CountSilenceAffectedInTenant(effectiveListTenantID(h.Store, c), s.Matchers)
		s.Affected = n
		if tab != "" && s.Status != tab {
			continue
		}
		out = append(out, s)
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *AlertHandler) CreateSilence(c *gin.Context) {
	var b struct {
		Name     string `json:"name"`
		Matchers string `json:"matchers"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	starts, ends := b.StartsAt, b.EndsAt
	// 兼容前端传本地时间字符串时尝试规范化
	if t, err := time.Parse(time.RFC3339, starts); err == nil {
		starts = t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339, ends); err == nil {
		ends = t.UTC().Format(time.RFC3339)
	}
	s := &store.AlertSilence{
		Name: b.Name, Matchers: b.Matchers, StartsAt: starts, EndsAt: ends,
		Reason: b.Reason, CreatedBy: middleware.Username(c), TenantID: middleware.TenantID(c),
	}
	if err := h.Store.CreateAlertSilence(s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s})
}

func (h *AlertHandler) CancelSilence(c *gin.Context) {
	if sl, err := h.Store.GetAlertSilenceInTenant(c.Param("id"), effectiveListTenantID(h.Store, c)); err != nil || sl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "静默不存在"})
		return
	}
	if err := h.Store.CancelAlertSilence(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": true})
}

// QuickSilence 为单条告警创建 2 小时静默。
func (h *AlertHandler) QuickSilence(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	now := time.Now().UTC()
	parts := []string{}
	for _, k := range []string{"service", "alertname", "cluster"} {
		if v := a.Labels[k]; v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 && a.Service != "" {
		parts = append(parts, "service="+a.Service)
	}
	if len(parts) == 0 {
		parts = append(parts, "fingerprint="+a.Fingerprint)
		a.Labels["fingerprint"] = a.Fingerprint
		_ = h.Store.SaveAlert(a)
	}
	s := &store.AlertSilence{
		Name: "静默 " + a.ID, Matchers: strings.Join(parts, ", "),
		StartsAt: now.Format(time.RFC3339), EndsAt: now.Add(2 * time.Hour).Format(time.RFC3339),
		Reason: "快捷静默 2 小时", CreatedBy: middleware.Username(c), TenantID: middleware.TenantID(c),
	}
	if err := h.Store.CreateAlertSilence(s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	a.Handle = store.AlertHandleSilenced
	_ = h.Store.SaveAlert(a)
	_ = h.Store.AddAlertEvent(&store.AlertEvent{
		AlertID: a.ID, Action: "silence", Detail: "快捷静默 2 小时", Operator: middleware.Username(c),
	})
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"alert": a, "silence": s}})
}
