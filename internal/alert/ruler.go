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

// Start 在后台循环评估与作用域编译刷新；ctx 取消后退出。
func Start(ctx context.Context, db *store.DB, interval time.Duration, secret string) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if v, _ := db.GetGlobalArg(argRulerInterval); strings.TrimSpace(v) != "" {
		if sec, err := time.ParseDuration(strings.TrimSpace(v) + "s"); err == nil && sec >= 10*time.Second {
			interval = sec
		}
	}
	r := &Ruler{Store: db, Secret: secret, Interval: interval}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		r.RunOnce(ctx)
		t := time.NewTicker(r.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.RunOnce(ctx)
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
		if n, err := db.RefreshAllAlertRuleCompiled(); err != nil {
			log.Printf("alert-ruler: compile refresh: %v", err)
		} else if n > 0 {
			log.Printf("alert-ruler: compiled %d rules", n)
		}
		t := time.NewTicker(DefaultCompileRefresh)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := db.RefreshAllAlertRuleCompiled(); err != nil {
					log.Printf("alert-ruler: compile refresh: %v", err)
				} else {
					log.Printf("alert-ruler: compiled %d rules", n)
				}
			}
		}
	}()
	log.Printf("alert-ruler: started interval=%s compile_refresh=%s", interval, DefaultCompileRefresh)
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

	seen := map[string]bool{}
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
	r.resolveMissing(seen, now)
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
	d := store.DecideAlertNotify(a, rule.NotifyPolicy, now, justFired)
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
	store.ApplyAlertNotifyMark(a, rule.NotifyPolicy, now)
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

func (r *Ruler) resolveMissing(seen map[string]bool, now time.Time) {
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
		a.Status = store.AlertStatusResolved
		a.Handle = store.AlertHandleResolved
		a.ResolvedAt = now.UTC().Format(time.RFC3339)
		if err := r.Store.SaveAlert(&a); err != nil {
			continue
		}
		_ = r.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "resolve", Detail: "指标回落，告警自动恢复", Operator: "系统",
		})
		cp := a
		opts := r.buildDispatchOpts(&cp, list)
		var rule *store.AlertRule
		if cp.RuleID != "" {
			rule, _ = r.Store.GetAlertRule(cp.RuleID)
		}
		ctxOpts := buildAlertNotifyContext(r.Store, &cp, rule)
		go notify.AlertResolved(r.Store, r.Secret, &cp, "", opts, ctxOpts)
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
