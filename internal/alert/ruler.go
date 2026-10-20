package alert

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/notify"
	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

const (
	argPromBaseURL   = "awr_prometheus_base_url"
	argPromAuthToken = "awr_prometheus_auth_token"
	argPromPartial   = "awr_prometheus_partial_response"
	argPromDedup     = "awr_prometheus_dedup"
	argRulerInterval = "alert_ruler_interval_sec"

	DefaultInterval       = 30 * time.Second
	DefaultCompileRefresh = 5 * time.Minute
	promEvalConcurrency   = 8
	ruleQueryTimeout      = 20 * time.Second
)

// Ruler 周期评估告警规则。
type Ruler struct {
	Store    *store.DB
	Secret   string
	Interval time.Duration
}

// Start 已废弃：由 jobreg alert.ruler / alert.rule_compile 接管。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs。
func Start(ctx context.Context, db *store.DB, interval time.Duration, secret string) {
	_ = ctx
	_ = db
	_ = interval
	_ = secret
	log.Printf("DEPRECATED: alert.Start ignored; scheduling is owned by jobreg (alert.ruler / alert.rule_compile)")
}

func (r *Ruler) promClient() (*promclient.Client, error) {
	base, _ := r.Store.GetGlobalArg(argPromBaseURL)
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, nil
	}
	token, _ := r.Store.GetGlobalArg(argPromAuthToken)
	partial, dedup := false, false
	if v, _ := r.Store.GetGlobalArg(argPromPartial); v != "" {
		partial = parseBool(v)
	}
	if v, _ := r.Store.GetGlobalArg(argPromDedup); v != "" {
		dedup = parseBool(v)
	}
	return promclient.New(promclient.Config{
		BaseURL: base, AuthToken: token, PartialResponse: partial, Dedup: dedup,
	}), nil
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

type ruleEvalResult struct {
	rule   store.AlertRule
	series []EvalSeries
	err    error
	health int
}

// RunOnce 执行一轮评估（并行查 Prom，每规则仍单次 Query）。
func (r *Ruler) RunOnce(ctx context.Context) {
	cli, err := r.promClient()
	if err != nil || cli == nil {
		return
	}
	rules, err := r.Store.ListEnabledAlertRules()
	if err != nil {
		log.Printf("alert-ruler: list rules: %v", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	now := time.Now()
	silences, err := r.Store.ListActiveAlertSilences(now)
	if err != nil {
		log.Printf("alert-ruler: list silences: %v", err)
		silences = nil
	}

	sem := make(chan struct{}, promEvalConcurrency)
	var wg sync.WaitGroup
	results := make([]ruleEvalResult, len(rules))
	for i := range rules {
		i := i
		rule := rules[i]
		expr := rule.EvalExpr()
		if expr == "" {
			results[i] = ruleEvalResult{rule: rule, err: nil, health: rule.Health}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			qctx, cancel := context.WithTimeout(ctx, ruleQueryTimeout)
			samples, qerr := cli.Query(qctx, expr, now)
			cancel()
			res := ruleEvalResult{rule: rule, health: rule.Health}
			if qerr != nil {
				res.err = qerr
				if res.health > 10 {
					res.health -= 5
				}
			} else {
				res.series = SamplesToSeries(samples)
				if res.health < 100 {
					res.health = min(100, res.health+1)
				}
			}
			results[i] = res
		}()
	}
	wg.Wait()

	// 读取登记资源失败时不过滤，保持原有评估行为
	var registry *registryIndex
	if insts, ierr := r.Store.ListInstances("", ""); ierr != nil {
		log.Printf("alert-ruler: list instances: %v", ierr)
	} else {
		registry = newRegistryIndex(insts)
	}

	seen := map[string]bool{}
	dropped := map[string]bool{}
	for _, res := range results {
		rule := res.rule
		if res.err != nil {
			log.Printf("alert-ruler: rule %s query: %v", rule.ID, res.err)
			if res.health != rule.Health {
				_ = r.Store.PatchAlertRuleStats(rule.ID, res.health, rule.Triggered7d)
			}
			continue
		}
		if rule.EvalExpr() == "" {
			continue
		}
		series := res.series
		if len(series) > store.MaxAlertSeriesPerEval {
			log.Printf("alert-ruler: rule %s series truncated %d→%d", rule.ID, len(series), store.MaxAlertSeriesPerEval)
			series = series[:store.MaxAlertSeriesPerEval]
		}
		for _, ser := range series {
			labels := MergeLabels(rule.Labels, ser.Labels)
			if rule.Cluster != "" && labels["cluster"] == "" {
				labels["cluster"] = rule.Cluster
			}
			sev := rule.Severity
			forDur, _ := store.ParseAlertDuration(rule.ForDuration)
			thr := ThresholdHint(rule.Annotations)
			if rule.HasTiers() {
				tier := store.PickAlertTier(rule.Tiers, ser.Value)
				if tier == nil {
					continue
				}
				sev = tier.Severity
				forDur, _ = store.ParseAlertDuration(rule.TierForDuration(tier))
				thr = store.FormatTierThreshold(tier)
			}
			fp := store.AlertFingerprint(rule.ID, labels)
			if registry != nil {
				svc := rule.Labels["service"]
				if svc == "" {
					svc = labels["service"]
				}
				if svc == "" {
					svc = labels["job"]
				}
				inst, ok := registry.match(labels, svc)
				if !ok {
					dropped[fp] = true
					continue
				}
				labels = registeredLabels(labels, inst)
			}
			seen[fp] = true
			if err := r.upsertFiring(rule, labels, fp, ser.Value, sev, thr, forDur, now, silences); err != nil {
				log.Printf("alert-ruler: upsert %s: %v", fp, err)
			}
		}
		if res.health != rule.Health {
			_ = r.Store.PatchAlertRuleStats(rule.ID, res.health, rule.Triggered7d)
			rule.Health = res.health
		}
	}
	r.resolveMissing(seen, dropped, now)
}

func (r *Ruler) upsertFiring(rule store.AlertRule, labels map[string]string, fp string, value float64, severity, thr string, forDur time.Duration, now time.Time, silences []store.AlertSilence) error {
	exist, err := r.Store.GetOpenAlertByFingerprint(fp)
	if err != nil {
		return err
	}
	svc := labels["service"]
	if svc == "" {
		svc = labels["job"]
	}
	cluster := labels["cluster"]
	if cluster == "" {
		cluster = rule.Cluster
	}
	cur := FormatValue(value)
	title := TitleFrom(rule, labels)
	ann := rule.Annotations
	if thr != "" {
		ann = cloneAnn(ann)
		ann["threshold"] = thr
	}

	if exist == nil {
		a := &store.Alert{
			TenantID:     rule.TenantID,
			Fingerprint:  fp,
			RuleID:       rule.ID,
			Severity:     severity,
			Title:        title,
			Service:      svc,
			Cluster:      cluster,
			Labels:       labels,
			Annotations:  ann,
			Status:       store.AlertStatusPending,
			Handle:       store.AlertHandleUnclaimed,
			CurValue:     cur,
			Threshold:    thr,
			PendingSince: now.UTC().Format(time.RFC3339),
			StartedAt:    now.UTC().Format(time.RFC3339),
		}
		if ShouldFire(a.PendingSince, forDur, now) {
			a.Status = store.AlertStatusFiring
			ApplySilence(a, silences)
			if err := r.Store.UpsertAlert(a); err != nil {
				return err
			}
			_ = r.Store.AddAlertEvent(&store.AlertEvent{
				AlertID: a.ID, Action: "trigger", Detail: "告警触发：" + title, Operator: "系统",
			})
			_ = r.Store.PatchAlertRuleStats(rule.ID, rule.Health, rule.Triggered7d+1)
			r.maybeNotify(&rule, a, true, now)
			return r.Store.SaveAlert(a)
		}
		return r.Store.UpsertAlert(a)
	}

	exist.CurValue = cur
	exist.Threshold = thr
	exist.Title = title
	exist.Labels = labels
	exist.Annotations = ann
	exist.Service = svc
	exist.Cluster = cluster
	exist.Severity = severity

	justFired := false
	if exist.Status == store.AlertStatusPending {
		if ShouldFire(exist.PendingSince, forDur, now) {
			exist.Status = store.AlertStatusFiring
			justFired = true
			_ = r.Store.AddAlertEvent(&store.AlertEvent{
				AlertID: exist.ID, Action: "trigger", Detail: "持续满足条件，告警 firing：" + title, Operator: "系统",
			})
			_ = r.Store.PatchAlertRuleStats(rule.ID, rule.Health, rule.Triggered7d+1)
		}
	}
	_ = ApplySilence(exist, silences)
	if exist.Status == store.AlertStatusFiring {
		r.maybeNotify(&rule, exist, justFired, now)
	}
	return r.Store.SaveAlert(exist)
}

func (r *Ruler) maybeNotify(rule *store.AlertRule, a *store.Alert, justFired bool, now time.Time) {
	if rule == nil || a == nil {
		return
	}
	_ = r.Store.LoadFingerprintNotifyIntoAlert(a)
	policy := store.ResolveAlertNotifyPolicy(rule.NotifyPolicy)
	d := store.DecideAlertNotify(a, policy, now, justFired)
	if !d.Should {
		return
	}
	// 抑制：源告警 firing 时目标不外发
	sources, _ := r.Store.ListAlerts(store.AlertFilter{Tab: "active"})
	inhibits, _ := r.Store.ListEnabledAlertInhibitions()
	if store.AlertInhibitedBy(a, sources, inhibits) {
		_ = r.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "inhibit", Detail: "命中抑制规则，跳过外发", Operator: "系统",
		})
		return
	}
	opts := r.buildDispatchOpts(a, sources)
	if err := r.Store.ApplyFingerprintNotifyMark(a, policy, now); err != nil {
		store.ApplyAlertNotifyMark(a, policy, now)
	}
	if opts.GroupSize > 1 {
		a.AggregateCount = opts.GroupSize
	}
	detail := "通知外发：" + d.Reason
	if len(opts.Channels) > 0 {
		detail += " [" + strings.Join(opts.Channels, ",") + "]"
	}
	_ = r.Store.AddAlertEvent(&store.AlertEvent{
		AlertID: a.ID, Action: "notify", Detail: detail, Operator: "系统",
	})
	cp := cloneAlert(a)
	ruleCp := *rule
	ctxOpts := buildAlertNotifyContext(r.Store, cp, &ruleCp)
	go notify.AlertFired(r.Store, r.Secret, cp, &ruleCp, d.Reason, opts, ctxOpts)
}

func (r *Ruler) buildDispatchOpts(a *store.Alert, active []store.Alert) *notify.AlertDispatchOpts {
	routes, _ := r.Store.ListEnabledAlertRoutes()
	hit := store.MatchAlertRoutes(a.Labels, routes)
	opts := &notify.AlertDispatchOpts{}
	if len(hit) > 0 {
		opts.Channels = store.MergeRouteChannels(hit)
		// 取第一条命中路由的 group_by
		gk := store.AlertGroupKey(a.Labels, hit[0].GroupBy)
		opts.GroupKey = gk
		if gk != "" {
			n := 0
			for _, x := range active {
				if x.Status != store.AlertStatusFiring {
					continue
				}
				if store.AlertGroupKey(x.Labels, hit[0].GroupBy) == gk {
					n++
				}
			}
			if n > 0 {
				opts.GroupSize = n
			}
		}
	}
	if oncall, err := r.Store.CurrentAlertOncallUsernames(time.Now()); err == nil && len(oncall) > 0 {
		opts.ToUsers = oncall
	}
	return opts
}

// resolveMissing 关闭本轮未命中的告警；dropped 为资源未登记或类型不符的，静默关闭不外发恢复通知。
func (r *Ruler) resolveMissing(seen, dropped map[string]bool, now time.Time) {
	list, err := r.Store.ListAlerts(store.AlertFilter{Tab: "active"})
	if err != nil {
		return
	}
	all, err := r.Store.ListAlerts(store.AlertFilter{Tab: "all"})
	if err != nil {
		all = list
	}
	for _, a := range all {
		if a.Status == store.AlertStatusResolved {
			continue
		}
		if seen[a.Fingerprint] {
			continue
		}
		if dropped[a.Fingerprint] {
			a.Status = store.AlertStatusResolved
			a.Handle = store.AlertHandleResolved
			a.ResolvedAt = now.UTC().Format(time.RFC3339)
			a.ResolveNotified = true
			if err := r.Store.SaveAlert(&a); err == nil {
				_ = r.Store.AddAlertEvent(&store.AlertEvent{
					AlertID: a.ID, Action: "resolve", Detail: "资源未在平台登记或类型与规则不符，关闭告警（不外发）", Operator: "系统",
				})
			}
			continue
		}
		alreadyResolveNotified := a.ResolveNotified
		a.Status = store.AlertStatusResolved
		a.Handle = store.AlertHandleResolved
		a.ResolvedAt = now.UTC().Format(time.RFC3339)
		// 最终恢复必发一条，且同一生命周期至多一次（不受 trigger 频控限制）
		sendResolve := !alreadyResolveNotified
		if sendResolve {
			a.ResolveNotified = true
		}
		if err := r.Store.SaveAlert(&a); err != nil {
			continue
		}
		_ = r.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "resolve", Detail: "指标回落，告警自动恢复", Operator: "系统",
		})
		if !sendResolve {
			continue
		}
		cp := a
		opts := r.buildDispatchOpts(&cp, list)
		var rule *store.AlertRule
		ruleName := ""
		if cp.RuleID != "" {
			rule, _ = r.Store.GetAlertRule(cp.RuleID)
			if rule != nil {
				ruleName = rule.Name
			}
		}
		ctxOpts := buildAlertNotifyContext(r.Store, &cp, rule)
		_ = r.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "notify", Detail: "通知外发：resolve（告警已恢复）", Operator: "系统",
		})
		go notify.AlertResolved(r.Store, r.Secret, &cp, ruleName, opts, ctxOpts)
	}
}

func cloneAnn(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAlert(a *store.Alert) *store.Alert {
	if a == nil {
		return nil
	}
	cp := *a
	cp.Labels = cloneAnn(a.Labels)
	cp.Annotations = cloneAnn(a.Annotations)
	return &cp
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
