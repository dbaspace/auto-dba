package srcluster

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// 与「配置中心 → 监控目标注册」现有 StarRocks 任务保持一致的约定。
const (
	monitorAppFE    = "fe-StarRocks"
	monitorAppBE    = "be-StarRocks"
	defaultFEHTTP   = 8030
	defaultBEHTTP   = 8040
	monitorHintPage = "「配置中心 → 监控目标注册」"
)

// MonitorLink 集群对应的监控任务摘要。
type MonitorLink struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Collector string   `json:"collector"`
	FECount   int      `json:"fe_count"`
	BECount   int      `json:"be_count"`
	Missing   []string `json:"missing,omitempty"`
}

func monitorMarker(clusterID string) string { return srClusterTagPrefix + clusterID }

func feMonitorEndpoint(cl *store.SrCluster, n store.SrNode) string {
	port := n.HTTPPort
	if port <= 0 && cl != nil {
		port = cl.FeHTTPPort
	}
	if port <= 0 {
		port = defaultFEHTTP
	}
	return net.JoinHostPort(strings.TrimSpace(n.Host), strconv.Itoa(port))
}

func beMonitorEndpoint(n store.SrNode) string {
	port := n.HTTPPort
	if port <= 0 {
		port = defaultBEHTTP
	}
	return net.JoinHostPort(strings.TrimSpace(n.Host), strconv.Itoa(port))
}

// monitorEndpoints 按组件列出节点的 HTTP 指标地址（host:port）。
func monitorEndpoints(cl *store.SrCluster, nodes []store.SrNode) (fe, be []string) {
	for _, n := range nodes {
		if strings.TrimSpace(n.Host) == "" {
			continue
		}
		switch n.Component {
		case "fe":
			fe = appendUniqueEP(fe, feMonitorEndpoint(cl, n))
		case "be":
			be = appendUniqueEP(be, beMonitorEndpoint(n))
		}
	}
	return fe, be
}

func appendUniqueEP(list []string, ep string) []string {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), ep) {
			return list
		}
	}
	return append(list, ep)
}

func epPort(ep string) string {
	_, p, err := net.SplitHostPort(strings.TrimSpace(ep))
	if err != nil {
		return ""
	}
	return p
}

// classifyMonitorGroup 判定目标组是 FE 还是 BE：端口一致且等于 FE/BE 端口时以端口为准，否则看 app 标签。
func classifyMonitorGroup(g store.MonitorTargetGroup, fePort, bePort string) string {
	eps := splitEndpoints(g.Endpoints)
	if len(eps) > 0 {
		p := epPort(eps[0])
		same := p != ""
		for _, ep := range eps[1:] {
			if epPort(ep) != p {
				same = false
				break
			}
		}
		if same && p != fePort && p == bePort {
			return "be"
		}
		if same && p == fePort && p != bePort {
			return "fe"
		}
	}
	app := strings.ToLower(strings.TrimSpace(g.Labels["app"]))
	switch {
	case strings.HasPrefix(app, "fe"):
		return "fe"
	case strings.HasPrefix(app, "be"):
		return "be"
	}
	return ""
}

func splitEndpoints(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// monitorPorts 集群 FE/BE 指标端口（用于识别目标组）。
func monitorPorts(cl *store.SrCluster) (fePort, bePort string) {
	fp := defaultFEHTTP
	if cl != nil && cl.FeHTTPPort > 0 {
		fp = cl.FeHTTPPort
	}
	return strconv.Itoa(fp), strconv.Itoa(defaultBEHTTP)
}

// mergeMonitorJob 把集群 FE/BE 目标与标签合并进监控任务：只补缺失，不覆盖人工内容；removed 中的地址从目标组移除。
// labels 中的 version 以平台为准（不同则更新），其余 key 仅在缺失时填写。
func mergeMonitorJob(job *store.MonitorJob, cl *store.SrCluster, fe, be, removed []string, labels map[string]string) bool {
	changed := false
	fePort, bePort := monitorPorts(cl)
	isRemoved := func(ep string) bool {
		for _, r := range removed {
			if strings.EqualFold(strings.TrimSpace(r), strings.TrimSpace(ep)) {
				return true
			}
		}
		return false
	}
	feIdx, beIdx := -1, -1
	for i := range job.TargetGroups {
		g := &job.TargetGroups[i]
		if g.Labels == nil {
			g.Labels = map[string]string{}
		}
		if len(removed) > 0 {
			var keep []string
			eps := splitEndpoints(g.Endpoints)
			for _, ep := range eps {
				if !isRemoved(ep) {
					keep = append(keep, ep)
				}
			}
			if len(keep) != len(eps) {
				g.Endpoints = strings.Join(keep, ",")
				changed = true
			}
		}
		if cur, ok := g.Labels["version"]; ok && labels["version"] != "" && strings.TrimSpace(cur) != labels["version"] {
			g.Labels["version"] = labels["version"]
			changed = true
		}
		kind := classifyMonitorGroup(*g, fePort, bePort)
		if kind == "fe" && feIdx < 0 {
			feIdx = i
		}
		if kind == "be" && beIdx < 0 {
			beIdx = i
		}
	}
	fill := func(idx int, want []string, app string) {
		if idx < 0 {
			if len(want) == 0 {
				return
			}
			job.TargetGroups = append(job.TargetGroups, store.MonitorTargetGroup{
				Endpoints: strings.Join(want, ","),
				Labels:    map[string]string{"app": app},
			})
			changed = true
			return
		}
		g := &job.TargetGroups[idx]
		eps := splitEndpoints(g.Endpoints)
		n := len(eps)
		for _, ep := range want {
			eps = appendUniqueEP(eps, ep)
		}
		if len(eps) != n {
			g.Endpoints = strings.Join(eps, ",")
			changed = true
		}
		if strings.TrimSpace(g.Labels["app"]) == "" {
			g.Labels["app"] = app
			changed = true
		}
	}
	fill(feIdx, fe, monitorAppFE)
	fill(beIdx, be, monitorAppBE)

	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	for k, v := range labels {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		cur := strings.TrimSpace(job.Labels[k])
		if cur == "" || (k == "version" && cur != v) {
			job.Labels[k] = v
			changed = true
		}
	}
	if job.Collector == "" && labels["idc"] != "" {
		job.Collector = labels["idc"]
		changed = true
	}
	if cl != nil && cl.ID != "" && !strings.Contains(job.Description, monitorMarker(cl.ID)) {
		job.Description = strings.TrimSpace(strings.TrimSpace(job.Description) + " " + monitorMarker(cl.ID))
		changed = true
	}
	return changed
}

// monitorMissing 任务中推导不出、需要手动补充的项。
func monitorMissing(job *store.MonitorJob) []string {
	var miss []string
	if strings.TrimSpace(job.Collector) == "" {
		miss = append(miss, "采集点")
	}
	for _, k := range []string{"domain", "idc"} {
		has := strings.TrimSpace(job.Labels[k]) != ""
		for _, g := range job.TargetGroups {
			has = has || strings.TrimSpace(g.Labels[k]) != ""
		}
		if !has {
			miss = append(miss, k)
		}
	}
	return miss
}

func monitorSummary(job *store.MonitorJob, cl *store.SrCluster) *MonitorLink {
	fePort, bePort := monitorPorts(cl)
	out := &MonitorLink{ID: job.ID, Name: job.Name, Collector: job.Collector, Missing: monitorMissing(job)}
	for _, g := range job.TargetGroups {
		n := len(splitEndpoints(g.Endpoints))
		switch classifyMonitorGroup(g, fePort, bePort) {
		case "fe":
			out.FECount += n
		case "be":
			out.BECount += n
		}
	}
	return out
}

// findClusterMonitorJob 依次按描述标记 sr-cluster=<id>、任务名=集群名、starrocks 任务的目标主机属于本集群查找。
func (e *Engine) findClusterMonitorJob(cl *store.SrCluster, nodes []store.SrNode) (*store.MonitorJob, error) {
	jobs, err := e.Store.ListMonitorJobs("")
	if err != nil {
		return nil, err
	}
	pick := func(id string) (*store.MonitorJob, error) { return e.Store.GetMonitorJob(id) }
	marker := monitorMarker(cl.ID)
	for _, j := range jobs {
		if cl.ID != "" && strings.Contains(j.Description, marker) {
			return pick(j.ID)
		}
	}
	for _, j := range jobs {
		if strings.EqualFold(strings.TrimSpace(j.Name), strings.TrimSpace(cl.Name)) {
			return pick(j.ID)
		}
	}
	for _, j := range jobs {
		if !strings.Contains(strings.ToLower(j.Name), "starrocks") {
			continue
		}
		full, err := pick(j.ID)
		if err != nil || full == nil {
			continue
		}
		for _, g := range full.TargetGroups {
			for _, ep := range splitEndpoints(g.Endpoints) {
				for _, n := range nodes {
					if hostsEqual(ep, n.Host) {
						return full, nil
					}
				}
			}
		}
	}
	return nil, nil
}

// monitorLabelsFor 平台能推导的任务标签：idc、version，以及关联实例的域名（非 IP 时）。
func (e *Engine) monitorLabelsFor(cl *store.SrCluster, nodes []store.SrNode) map[string]string {
	env, _ := e.envForCluster(cl)
	labels := map[string]string{
		"idc":     envCode(env, cl),
		"version": strings.TrimSpace(cl.Version),
	}
	if it, _ := e.findLinkedInstance(cl, nodes); it != nil {
		for _, d := range append([]string{it.DomainName}, it.Domains...) {
			d = strings.TrimSpace(d)
			if d != "" && net.ParseIP(d) == nil {
				labels["domain"] = d
				break
			}
		}
	}
	return labels
}

func readyNodes(nodes []store.SrNode, removed []store.SrNode) []store.SrNode {
	skip := map[string]bool{}
	for _, r := range removed {
		skip[r.ID] = true
	}
	var out []store.SrNode
	for _, n := range nodes {
		if n.Status == store.SrNodeReady && !skip[n.ID] {
			out = append(out, n)
		}
	}
	return out
}

// SyncClusterMonitor 同步集群到监控目标注册：不存在则新建任务；存在则补齐 ready 节点的 FE/BE 目标与标签；removed 节点的目标移除。
func (e *Engine) SyncClusterMonitor(cl *store.SrCluster, nodes, removed []store.SrNode) (*MonitorLink, bool, []string, error) {
	if e == nil || e.Store == nil || cl == nil || cl.ID == "" {
		return nil, false, nil, fmt.Errorf("集群无效")
	}
	fe, be := monitorEndpoints(cl, readyNodes(nodes, removed))
	rmFE, rmBE := monitorEndpoints(cl, removed)
	labels := e.monitorLabelsFor(cl, nodes)

	job, err := e.findClusterMonitorJob(cl, nodes)
	if err != nil {
		return nil, false, nil, err
	}
	created := false
	if job == nil {
		if len(fe) == 0 && len(be) == 0 {
			return nil, false, nil, nil
		}
		job = &store.MonitorJob{Name: cl.Name, Collector: labels["idc"], Labels: map[string]string{}}
		mergeMonitorJob(job, cl, fe, be, nil, labels)
		if err := e.Store.CreateMonitorJob(job); err != nil {
			return nil, false, nil, err
		}
		created = true
	} else if mergeMonitorJob(job, cl, fe, be, append(rmFE, rmBE...), labels) {
		if err := e.Store.UpdateMonitorJob(job); err != nil {
			return nil, false, nil, err
		}
	}
	sum := monitorSummary(job, cl)
	var warnings []string
	if len(sum.Missing) > 0 {
		warnings = append(warnings, fmt.Sprintf("监控任务 %s 缺少 %s，请到%s手动补充", job.Name, strings.Join(sum.Missing, "、"), monitorHintPage))
	}
	return sum, created, warnings, nil
}

// ClusterMonitor 供接口展示的监控任务摘要（只读）。
func (e *Engine) ClusterMonitor(cl *store.SrCluster, nodes []store.SrNode) *MonitorLink {
	if e == nil || e.Store == nil || cl == nil {
		return nil
	}
	job, err := e.findClusterMonitorJob(cl, nodes)
	if err != nil || job == nil {
		return nil
	}
	return monitorSummary(job, cl)
}
