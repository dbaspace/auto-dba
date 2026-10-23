package handler

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/dns"
	"auto-dba/internal/notify"
	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

const (
	dnsAlertService   = "dns"
	dnsMonitorMarker  = "auto-dba:dns"
	defaultServfail   = 0.05
	dnsAlertKindDown  = "offline"
	dnsAlertKindSync  = "unsynced"
	dnsAlertKindSFail = "servfail"
	dnsAlertKindFwd   = "forward"
)

// DNSAlertCheck 一次巡检中某节点某类告警的判定结果。
type DNSAlertCheck struct {
	Kind   string
	IP     string
	Firing bool
	Title  string
	Detail string
}

// EvalNodeAlerts 按心跳判定节点离线与未同步。
func EvalNodeAlerts(nodes []dns.Node, now int64) []DNSAlertCheck {
	var out []DNSAlertCheck
	for _, n := range nodes {
		if strings.TrimSpace(n.IP) == "" {
			continue
		}
		label := n.IP
		if n.Name != "" && n.Name != n.IP {
			label = n.Name + "(" + n.IP + ")"
		}
		out = append(out, DNSAlertCheck{
			Kind: dnsAlertKindDown, IP: n.IP, Firing: !n.Online, Title: "DNS 节点离线 " + label,
			Detail: fmt.Sprintf("心跳已 %d 秒未更新", now-n.UpdatedTime),
		})
		out = append(out, DNSAlertCheck{
			Kind: dnsAlertKindSync, IP: n.IP, Firing: n.Online && !n.Synced, Title: "DNS 节点未同步 " + label,
			Detail: fmt.Sprintf("节点数据加载于 %s，平台最近变更于 %s", unixText(n.DataUpdatedTime), unixText(n.ChangeTime)),
		})
	}
	return out
}

func unixText(t int64) string {
	if t <= 0 {
		return "-"
	}
	return time.Unix(t, 0).Format("2006-01-02 15:04:05")
}

// RunHealth 定时任务 dns.health：节点告警、TTL 到期恢复、监控目标同步。
func (h *DNSHandler) RunHealth(ctx context.Context) error {
	st, err := h.db()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	var errs []string
	if nodes, err := st.ListNodes(ctx, now); err != nil {
		errs = append(errs, "读取节点: "+err.Error())
	} else {
		checks := EvalNodeAlerts(nodes, now)
		checks = append(checks, h.servfailChecks(ctx)...)
		checks = append(checks, h.forwardFailChecks(ctx)...)
		for _, ck := range checks {
			h.applyAlert(ck)
		}
	}
	if n, err := st.RestoreDueTTL(ctx, now); err != nil {
		errs = append(errs, "恢复 TTL: "+err.Error())
	} else if n > 0 {
		log.Printf("dns.health: restored %d ttl holds", n)
	}
	if err := h.syncMonitorJob(); err != nil {
		errs = append(errs, "同步监控目标: "+err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return nil
}

func (h *DNSHandler) ratioLimit(key string, def float64) float64 {
	if h.Store == nil {
		return def
	}
	v, _ := h.Store.GetGlobalArg(key)
	if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > 0 {
		return f
	}
	return def
}

// ratioChecks 把按 instance 聚合的占比结果转成告警判定。instance 形如 ip:port 时指纹只用 IP。
func ratioChecks(samples []promclient.Sample, kind string, limit float64, title, what string) []DNSAlertCheck {
	var out []DNSAlertCheck
	for _, s := range samples {
		inst := s.Metric["instance"]
		ip := inst
		if hh, _, err := net.SplitHostPort(inst); err == nil {
			ip = hh
		}
		if strings.TrimSpace(ip) == "" || len(s.Value) != 2 {
			continue
		}
		v, ok := promclient.ToFloat(s.Value[1])
		if !ok {
			continue
		}
		out = append(out, DNSAlertCheck{
			Kind: kind, IP: ip, Firing: v > limit, Title: title + " " + ip,
			Detail: fmt.Sprintf("近 5 分钟%s %.2f%%，阈值 %.2f%%", what, v*100, limit*100),
		})
	}
	return out
}

func (h *DNSHandler) queryRatio(ctx context.Context, q, kind, argKey string, def float64, title, what string) []DNSAlertCheck {
	cli := h.promClient()
	if cli == nil {
		return nil
	}
	samples, err := cli.Query(ctx, q, time.Now())
	if err != nil {
		log.Printf("dns.health: %s query: %v", kind, err)
		return nil
	}
	return ratioChecks(samples, kind, h.ratioLimit(argKey, def), title, what)
}

// servfailChecks SERVFAIL 占比超过阈值（全局参数 dns_servfail_ratio，缺省 0.05）。未配置 Prometheus 时跳过。
func (h *DNSHandler) servfailChecks(ctx context.Context) []DNSAlertCheck {
	sel := h.metricsSelector()
	q := `sum by (instance) (rate(coredns_dns_responses_total{` + sel + `,rcode="SERVFAIL"}[5m])) / sum by (instance) (rate(coredns_dns_responses_total{` + sel + `}[5m]))`
	return h.queryRatio(ctx, q, dnsAlertKindSFail, dnsServfailRatioArg, defaultServfail, "DNS SERVFAIL 偏高", "SERVFAIL 占比")
}

// forwardFailChecks 上游转发失败率（SERVFAIL 或 REFUSED）超过阈值。全局参数 dns_forward_fail_ratio，缺省 0.05。
func (h *DNSHandler) forwardFailChecks(ctx context.Context) []DNSAlertCheck {
	sel := h.metricsSelector()
	q := `sum by (instance) (rate(coredns_forward_responses_total{` + sel + `,rcode=~"SERVFAIL|REFUSED"}[5m])) / sum by (instance) (rate(coredns_forward_responses_total{` + sel + `}[5m]))`
	return h.queryRatio(ctx, q, dnsAlertKindFwd, dnsForwardFailRatioArg, defaultServfail, "DNS 上游转发失败偏高", "上游转发失败率")
}

// applyAlert 触发或恢复。指纹 dns:<kind>:<ip>；同一指纹未恢复前只更新不重复通知。
func (h *DNSHandler) applyAlert(ck DNSAlertCheck) {
	if h.Store == nil {
		return
	}
	fp := "dns:" + ck.Kind + ":" + ck.IP
	exist, err := h.Store.GetOpenAlertByFingerprint(fp)
	if err != nil {
		log.Printf("dns.health: load alert %s: %v", fp, err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if ck.Firing {
		if exist != nil {
			exist.CurValue = ck.Detail
			if exist.Annotations == nil {
				exist.Annotations = map[string]string{}
			}
			exist.Annotations["summary"] = ck.Detail
			_ = h.Store.SaveAlert(exist)
			return
		}
		sev := store.AlertSeverityP2
		if ck.Kind == dnsAlertKindDown {
			sev = store.AlertSeverityP1
		}
		a := &store.Alert{
			TenantID: store.DefaultTenantID, Fingerprint: fp, Severity: sev, Title: ck.Title, Service: dnsAlertService, Cluster: ck.IP,
			Status: store.AlertStatusFiring, Handle: store.AlertHandleUnclaimed, CurValue: ck.Detail,
			Labels:      map[string]string{"service": dnsAlertService, "kind": ck.Kind, "ip": ck.IP},
			Annotations: map[string]string{"summary": ck.Detail},
			StartedAt:   now, AggregateCount: 1,
		}
		if err := h.Store.UpsertAlert(a); err != nil {
			log.Printf("dns.health: save alert %s: %v", fp, err)
			return
		}
		_ = h.Store.AddAlertEvent(&store.AlertEvent{AlertID: a.ID, Action: "trigger", Detail: ck.Title + "：" + ck.Detail, Operator: "系统"})
		go notify.AlertFired(h.Store, h.Secret, a, nil, "trigger", nil, nil)
		return
	}
	if exist == nil {
		return
	}
	exist.Status = store.AlertStatusResolved
	exist.ResolvedAt = now
	if err := h.Store.SaveAlert(exist); err != nil {
		log.Printf("dns.health: resolve alert %s: %v", fp, err)
		return
	}
	_ = h.Store.AddAlertEvent(&store.AlertEvent{AlertID: exist.ID, Action: "resolve", Detail: "DNS 巡检恢复正常，告警自动恢复", Operator: "系统"})
	cp := *exist
	go notify.AlertResolved(h.Store, h.Secret, &cp, "DNS 节点巡检", nil, nil)
}

// syncMonitorJob 把已部署节点的指标地址写入监控目标任务 auto-dba-coredns。
func (h *DNSHandler) syncMonitorJob() error {
	if h.Engine == nil || h.Store == nil {
		return nil
	}
	nodes, err := h.Engine.ListDNSNodes()
	if err != nil {
		return err
	}
	var eps []string
	seen := map[string]bool{}
	for _, n := range nodes {
		port := n.MetricsPort
		if port <= 0 {
			port = dns.DefaultMetricsPort
		}
		ep := net.JoinHostPort(n.Host, strconv.Itoa(port))
		if strings.TrimSpace(n.Host) == "" || seen[ep] {
			continue
		}
		seen[ep] = true
		eps = append(eps, ep)
	}
	sort.Strings(eps)
	jobs, err := h.Store.ListMonitorJobs(dnsMonitorJobName)
	if err != nil {
		return err
	}
	var job *store.MonitorJob
	for i := range jobs {
		if jobs[i].Name == dnsMonitorJobName {
			job, err = h.Store.GetMonitorJob(jobs[i].ID)
			if err != nil {
				return err
			}
			break
		}
	}
	joined := strings.Join(eps, ",")
	if job == nil {
		if len(eps) == 0 {
			return nil
		}
		job = &store.MonitorJob{
			Name: dnsMonitorJobName, Labels: map[string]string{"app": "coredns"}, Description: dnsMonitorMarker,
			TargetGroups: []store.MonitorTargetGroup{{Endpoints: joined, Labels: map[string]string{"app": "coredns"}, Description: dnsMonitorMarker}},
		}
		return h.Store.CreateMonitorJob(job)
	}
	if !strings.Contains(job.Description, dnsMonitorMarker) {
		return nil
	}
	if len(eps) == 0 || (len(job.TargetGroups) == 1 && job.TargetGroups[0].Endpoints == joined) {
		return nil
	}
	job.TargetGroups = []store.MonitorTargetGroup{{Endpoints: joined, Labels: map[string]string{"app": "coredns"}, Description: dnsMonitorMarker}}
	return h.Store.UpdateMonitorJob(job)
}
