package dbmonitor

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

const selCacheTTL = 120 * time.Second

type resolvedSel struct {
	DB     string
	Cloud  string // qce selector（主机专用，不读 node_exporter）
	Source string // SourceQCE | SourceSelf | ""
	OK     bool
}

type selCacheEntry struct {
	sel resolvedSel
	exp time.Time
}

type selCall struct {
	wg  sync.WaitGroup
	sel resolvedSel
	err error
}

func (s *Service) getSelCache(id string) (resolvedSel, bool) {
	s.selMu.Lock()
	defer s.selMu.Unlock()
	if s.selCache == nil {
		return resolvedSel{}, false
	}
	e, ok := s.selCache[id]
	if !ok || time.Now().After(e.exp) {
		return resolvedSel{}, false
	}
	return e.sel, true
}

func (s *Service) putSelCache(id string, sel resolvedSel) {
	s.selMu.Lock()
	defer s.selMu.Unlock()
	if s.selCache == nil {
		s.selCache = map[string]selCacheEntry{}
	}
	s.selCache[id] = selCacheEntry{sel: sel, exp: time.Now().Add(selCacheTTL)}
}

// resolveSelectors：Grafana 变量式绑定；singleflight 合并并发；结果缓存 120s。
func (s *Service) resolveSelectors(ctx context.Context, cli *promclient.Client, instanceID string) (resolvedSel, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return resolvedSel{}, nil
	}
	if cached, ok := s.getSelCache(instanceID); ok {
		return cached, nil
	}

	s.selMu.Lock()
	if s.selInflight == nil {
		s.selInflight = map[string]*selCall{}
	}
	if c, ok := s.selInflight[instanceID]; ok {
		s.selMu.Unlock()
		c.wg.Wait()
		return c.sel, c.err
	}
	c := &selCall{}
	c.wg.Add(1)
	s.selInflight[instanceID] = c
	s.selMu.Unlock()

	sel, err := s.resolveSelectorsUncached(ctx, cli, instanceID)
	c.sel, c.err = sel, err
	c.wg.Done()

	s.selMu.Lock()
	delete(s.selInflight, instanceID)
	s.selMu.Unlock()

	if err == nil {
		s.putSelCache(instanceID, sel)
	}
	return sel, err
}

// ClusterSel 基于注册的集群 selector（不依赖实例表）。
type ClusterSel struct {
	IDC          string
	Domain       string
	FEInstances  []string
	BEInstances  []string
}

func (c ClusterSel) cacheKey() string {
	return "sr|" + strings.TrimSpace(c.IDC) + "|" + strings.TrimSpace(c.Domain) + "|" +
		strings.Join(c.FEInstances, ",") + "|" + strings.Join(c.BEInstances, ",")
}

// resolveClusterSelectors StarRocks 等：idc+domain 直拼，可选 instance 过滤。
func (s *Service) resolveClusterSelectors(ctx context.Context, cli *promclient.Client, cs ClusterSel) (resolvedSel, error) {
	idc := strings.TrimSpace(cs.IDC)
	domain := strings.TrimSpace(cs.Domain)
	if domain == "" {
		return resolvedSel{}, fmt.Errorf("请选择集群（domain）")
	}
	key := cs.cacheKey()
	if cached, ok := s.getSelCache(key); ok {
		return cached, nil
	}

	s.selMu.Lock()
	if s.selInflight == nil {
		s.selInflight = map[string]*selCall{}
	}
	if c, ok := s.selInflight[key]; ok {
		s.selMu.Unlock()
		c.wg.Wait()
		return c.sel, c.err
	}
	c := &selCall{}
	c.wg.Add(1)
	s.selInflight[key] = c
	s.selMu.Unlock()

	now := time.Now()
	cands := starrocksSelectorCandidates(idc, domain, domain, nil)
	sel, ok := s.probeFirst(ctx, cli, now, cands, "starrocks_fe_query_total", "up")
	if sel == "" && len(cands) > 0 {
		sel = cands[0]
	}
	sel = appendInstanceMatcher(sel, append(append([]string{}, cs.FEInstances...), cs.BEInstances...))
	out := resolvedSel{DB: sel, OK: ok || sel != "", Source: SourceSelf}
	c.sel, c.err = out, nil
	c.wg.Done()

	s.selMu.Lock()
	delete(s.selInflight, key)
	s.selMu.Unlock()
	s.putSelCache(key, out)
	return out, nil
}

func appendInstanceMatcher(sel string, instances []string) string {
	var uniq []string
	seen := map[string]struct{}{}
	for _, ep := range instances {
		ep = strings.TrimSpace(ep)
		if ep == "" {
			continue
		}
		if _, ok := seen[ep]; ok {
			continue
		}
		seen[ep] = struct{}{}
		uniq = append(uniq, regexpEscape(ep))
	}
	if len(uniq) == 0 {
		return sel
	}
	inst := `instance=~"` + strings.Join(uniq, "|") + `"`
	if strings.TrimSpace(sel) == "" {
		return inst
	}
	return sel + "," + inst
}

func (s *Service) resolveSelectorsUncached(ctx context.Context, cli *promclient.Client, instanceID string) (resolvedSel, error) {
	inst, err := s.Store.GetInstance(instanceID)
	if err != nil {
		return resolvedSel{}, err
	}
	if inst == nil {
		return resolvedSel{}, fmt.Errorf("实例不存在")
	}

	engine := NormalizeEngine(inst.DBType)
	idc, dbName := grafanaVarsFromInstance(inst)
	domain := domainFromInstance(inst)
	now := time.Now()
	out := resolvedSel{}

	switch engine {
	case "redis":
		sel, ok := s.resolveRedisExporterSelector(ctx, cli, now, idc, domain)
		out.DB, out.Cloud = sel, sel
		out.Source = SourceSelf
		out.OK = ok || sel != ""
		return out, nil
	case "host":
		sel, ok := s.resolveNodeExporterSelector(ctx, cli, now, idc, domain)
		out.DB, out.Cloud = sel, sel
		out.Source = SourceSelf
		out.OK = ok || sel != ""
		return out, nil
	case "starrocks":
		cands := starrocksSelectorCandidates(idc, domain, dbName, inst)
		sel, ok := s.probeFirst(ctx, cli, now, cands, "starrocks_fe_query_total", "up")
		out.DB = sel
		out.OK = ok || sel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "clickhouse":
		cands := clickhouseSelectorCandidates(idc, domain, inst)
		sel, ok := s.probeFirst(ctx, cli, now, cands, "ClickHouseAsyncMetrics_Uptime", "ClickHouseProfileEvents_Query")
		out.DB = sel
		out.OK = ok || sel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "kafka":
		cands := kafkaSelectorCandidates(inst, domain)
		sel, ok := s.probeFirst(ctx, cli, now, cands, "kafka_brokers", "kafka_topic_partitions")
		out.DB = sel
		out.OK = ok || sel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "mongodb":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "mongodb_up", "mongodb_ss_connections", "mongodb_connections")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "oracle":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "oracledb_up", "oracledb_activity_execute_count")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "elasticsearch":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "elasticsearch_cluster_health_number_of_nodes", "elasticsearch_nodejs_heap_size_used_bytes")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "sqlserver":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "mssql_up", "sql_up", "mssql_connections")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "tidb":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "tidb_server_connections", "tidb_executor_statement_total")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "cockroachdb":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "sql_conns", "sys_uptime", "sql_select_count")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "cassandra":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "org_apache_cassandra_metrics_client_connectedNativeClients", "cassandra_storage_load", "jvm_memory_bytes_used")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "oceanbase":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "ob_active_session_num", "ob_sysstat")
		out.DB = dbSel
		out.OK = dbOK || dbSel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "debezium", "dolphinscheduler":
		sel, ok := s.resolveEnvExporterSelector(ctx, cli, now, engine, idc, domain)
		out.DB = sel
		out.OK = ok || sel != ""
		if out.OK {
			out.Source = SourceSelf
		}
		return out, nil
	case "mysql":
		dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "mysql_up", "mysql_global_status_uptime")
		out.DB = dbSel
		out.OK = dbOK
		if dbOK {
			out.Source = SourceSelf
		}
		return out, nil
	}

	// postgres（默认）：主机 qce + 库指标
	cloudCands := cloudSelectorCandidates(dbName, idc)
	maxCloud := 2
	if len(cloudCands) < maxCloud {
		maxCloud = len(cloudCands)
	}
	for i := 0; i < maxCloud; i++ {
		cloudSel := cloudCands[i]
		q := `qce_postgres_cpu_max{` + cloudSel + `}`
		if samples, err := cli.Query(ctx, q, now); err == nil && len(samples) > 0 {
			out.Cloud = cloudSel
			out.Source = SourceQCE
			out.OK = true
			break
		}
	}
	if out.Cloud == "" && len(cloudCands) > 0 {
		out.Cloud = cloudCands[0]
		out.Source = SourceQCE
	}

	dbSel, dbOK := s.probeDBSelectorEngine(ctx, cli, inst, now, "pg_up", "pg_stat_database_numbackends")
	out.DB = dbSel
	if dbOK {
		out.OK = true
	}
	if out.Source == "" && dbOK {
		out.Source = SourceSelf
	}
	return out, nil
}

func domainFromInstance(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	for _, raw := range inst.Tags {
		k, v := splitTagKV(raw)
		if strings.EqualFold(k, "domain") && v != "" {
			return v
		}
	}
	if d := strings.TrimSpace(inst.DomainName); d != "" {
		return d
	}
	for _, d := range inst.Domains {
		if strings.TrimSpace(d) != "" {
			return strings.TrimSpace(d)
		}
	}
	return strings.TrimSpace(inst.Host)
}

func redisSelectorCandidates(idc, domain string) []string {
	out := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	// Grafana 59j7eSRWk2：先用 idc+domain 查 redis_up，再落到 instance=
	if idc != "" && domain != "" {
		add(fmt.Sprintf(`idc=%q,domain=%q`, idc, domain))
	}
	if domain != "" {
		add(fmt.Sprintf(`domain=%q`, domain))
	}
	if idc != "" {
		add(fmt.Sprintf(`idc=%q`, idc))
	}
	return out
}

// resolveEnvSelectors 按 idc/domain 直拼 selector（Debezium / 海豚等无实例表场景）。
func (s *Service) resolveEnvSelectors(ctx context.Context, cli *promclient.Client, engine, idc, domain string) (resolvedSel, error) {
	engine = NormalizeEngine(engine)
	idc, domain = strings.TrimSpace(idc), strings.TrimSpace(domain)
	if idc == "" && domain == "" {
		return resolvedSel{}, fmt.Errorf("请选择环境（idc）")
	}
	key := "env|" + engine + "|" + idc + "|" + domain
	if cached, ok := s.getSelCache(key); ok {
		return cached, nil
	}
	now := time.Now()
	sel, ok := s.resolveEnvExporterSelector(ctx, cli, now, engine, idc, domain)
	out := resolvedSel{DB: sel, Source: SourceSelf, OK: ok || sel != ""}
	s.putSelCache(key, out)
	return out, nil
}

func envSelectorCandidates(idc, domain string) []string {
	out := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	if idc != "" && domain != "" {
		add(fmt.Sprintf(`idc=%q,domain=%q`, idc, domain))
		add(fmt.Sprintf(`idc=%q,name=%q`, idc, domain))
		add(fmt.Sprintf(`idc=%q,application=%q`, idc, domain))
		add(fmt.Sprintf(`domain=%q`, domain))
		add(fmt.Sprintf(`name=%q`, domain))
		add(fmt.Sprintf(`application=%q`, domain))
	}
	if idc != "" {
		add(fmt.Sprintf(`idc=%q`, idc))
	}
	if domain != "" {
		add(fmt.Sprintf(`domain=%q`, domain))
		add(fmt.Sprintf(`application=%q`, domain))
	}
	return out
}

func (s *Service) resolveEnvExporterSelector(ctx context.Context, cli *promclient.Client, now time.Time, engine, idc, domain string) (string, bool) {
	cands := envSelectorCandidates(idc, domain)
	metrics := []string{"up"}
	switch NormalizeEngine(engine) {
	case "debezium":
		metrics = []string{"debezium_metrics_Connected", "debezium_metrics_TotalNumberOfEventsSeen", "kafka_connect_connector_task_status"}
	case "dolphinscheduler":
		metrics = []string{"ds_master_quartz_job_executed_total", "ds_master_overload_count_total", "jvm_memory_used_bytes"}
	}
	sel, ok := s.probeFirst(ctx, cli, now, cands, metrics...)
	if ok {
		return sel, true
	}
	if len(cands) > 0 {
		return cands[0], false
	}
	return "", false
}

// resolveRedisExporterSelector 对齐 Grafana 变量链：
// label_values(redis_up{idc,domain}, instance) → selector 使用 instance="ip:port"。
func (s *Service) resolveRedisExporterSelector(ctx context.Context, cli *promclient.Client, now time.Time, idc, domain string) (string, bool) {
	cands := redisSelectorCandidates(idc, domain)
	for _, c := range cands {
		samples, err := cli.Query(ctx, "redis_up{"+c+"}", now)
		if err != nil || len(samples) == 0 {
			continue
		}
		for _, sm := range samples {
			if inst := strings.TrimSpace(sm.Metric["instance"]); inst != "" {
				return fmt.Sprintf(`instance=%q`, inst), true
			}
		}
		// 有数据但无 instance 标签时，直接用 idc/domain 过滤
		return c, true
	}
	if len(cands) > 0 {
		return cands[0], false
	}
	return "", false
}

// resolveNodeExporterSelector 对齐 Node Exporter Full（1860）：
// label_values(node_uname_info{idc,domain}, instance) → instance="ip:9100"。
func (s *Service) resolveNodeExporterSelector(ctx context.Context, cli *promclient.Client, now time.Time, idc, domain string) (string, bool) {
	cands := redisSelectorCandidates(idc, domain) // 同 idc+domain 链
	for _, c := range cands {
		for _, metric := range []string{"node_uname_info", "node_cpu_seconds_total", "up"} {
			samples, err := cli.Query(ctx, metric+"{"+c+"}", now)
			if err != nil || len(samples) == 0 {
				continue
			}
			for _, sm := range samples {
				if inst := strings.TrimSpace(sm.Metric["instance"]); inst != "" {
					return fmt.Sprintf(`instance=%q`, inst), true
				}
			}
			return c, true
		}
	}
	if len(cands) > 0 {
		return cands[0], false
	}
	return "", false
}

func starrocksSelectorCandidates(idc, domain, dbName string, inst *store.Instance) []string {
	out := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	if idc != "" && domain != "" {
		add(fmt.Sprintf(`idc=%q,domain=%q`, idc, domain))
	}
	if domain != "" {
		add(fmt.Sprintf(`domain=%q`, domain))
	}
	if idc != "" && dbName != "" {
		add(fmt.Sprintf(`idc=%q,domain=%q`, idc, dbName))
	}
	if inst != nil && strings.TrimSpace(inst.Host) != "" {
		add(fmt.Sprintf(`domain=%q`, strings.TrimSpace(inst.Host)))
	}
	return out
}

func clickhouseSelectorCandidates(idc, domain string, inst *store.Instance) []string {
	out := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	host := ""
	if inst != nil {
		host = strings.TrimSpace(inst.Host)
	}
	if idc != "" && domain != "" {
		add(fmt.Sprintf(`idc=%q,instance=~%q`, idc, domain))
		add(fmt.Sprintf(`idc=%q,domain=%q`, idc, domain))
	}
	if domain != "" {
		add(fmt.Sprintf(`domain=%q`, domain))
		add(fmt.Sprintf(`instance=~%q`, domain))
	}
	if idc != "" && host != "" {
		add(fmt.Sprintf(`idc=%q,instance=~%q`, idc, host))
	}
	if host != "" {
		add(fmt.Sprintf(`instance=~%q`, host))
	}
	return out
}

func kafkaSelectorCandidates(inst *store.Instance, domain string) []string {
	out := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	if domain != "" {
		add(fmt.Sprintf(`instance=%q`, domain))
		add(fmt.Sprintf(`instance=~%q`, domain))
	}
	if inst != nil {
		host := strings.TrimSpace(inst.Host)
		if host != "" {
			add(fmt.Sprintf(`instance=%q`, host))
			if inst.Port > 0 {
				add(fmt.Sprintf(`instance=%q`, fmt.Sprintf("%s:%d", host, inst.Port)))
			}
		}
		if n := strings.TrimSpace(inst.Name); n != "" {
			add(fmt.Sprintf(`instance=%q`, n))
			add(fmt.Sprintf(`job=%q`, n))
		}
	}
	return out
}

func (s *Service) probeFirst(ctx context.Context, cli *promclient.Client, now time.Time, cands []string, metrics ...string) (string, bool) {
	limit := 4
	if len(cands) < limit {
		limit = len(cands)
	}
	for i := 0; i < limit; i++ {
		c := cands[i]
		for _, m := range metrics {
			q := m + "{" + c + "}"
			samples, err := cli.Query(ctx, q, now)
			if err == nil && len(samples) > 0 {
				return c, true
			}
		}
	}
	if len(cands) > 0 {
		return cands[0], false
	}
	return "", false
}

// probeDBSelectorEngine 通用库 selector 探测。
func (s *Service) probeDBSelectorEngine(ctx context.Context, cli *promclient.Client, inst *store.Instance, now time.Time, metrics ...string) (sel string, ok bool) {
	candidates := buildDBCandidates(inst, s)
	limit := 3
	if len(candidates) < limit {
		limit = len(candidates)
	}
	for i := 0; i < limit; i++ {
		c := candidates[i]
		for _, m := range metrics {
			samples, err := cli.Query(ctx, m+"{"+c+"}", now)
			if err == nil && len(samples) > 0 {
				return c, true
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0], false
	}
	return "", false
}

func grafanaVarsFromInstance(inst *store.Instance) (idc, dbName string) {
	if inst == nil {
		return "", ""
	}
	item := InstanceItem{
		Name: inst.Name, Host: inst.Host, IDC: inst.IDC, Tags: inst.Tags,
		RegionCode: "", RegionName: "",
	}
	idc = effectiveInstanceIDC(item)
	for _, raw := range inst.Tags {
		k, v := splitTagKV(raw)
		if strings.EqualFold(k, "db") && v != "" {
			dbName = v
			break
		}
	}
	if dbName == "" {
		dbName = strings.TrimSpace(inst.Name)
	}
	return idc, dbName
}

func cloudSelectorCandidates(dbName, idc string) []string {
	dbName = strings.TrimSpace(dbName)
	idc = strings.TrimSpace(idc)
	if dbName == "" {
		return nil
	}
	out := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	if idc != "" {
		add(fmt.Sprintf(`d_b_instance_name=~%q,idc=~%q`, dbName, idc))
		add(fmt.Sprintf(`d_b_instance_name=~%q,env=~%q`, dbName, idc))
	}
	add(fmt.Sprintf(`d_b_instance_name=~%q`, dbName))
	return out
}

// probeDBSelector 最多校验 3 个候选 × 仅 pg_up（Grafana 变量直查风格）。
func (s *Service) probeDBSelector(ctx context.Context, cli *promclient.Client, inst *store.Instance, now time.Time) (sel string, ok bool) {
	return s.probeDBSelectorEngine(ctx, cli, inst, now, "pg_up", "pg_stat_database_numbackends")
}

func buildDBCandidates(inst *store.Instance, s *Service) []string {
	_, hostLabel := s.labelKeys()
	host := strings.TrimSpace(inst.Host)
	port := inst.Port
	candidates := []string{}
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range candidates {
			if c == sel {
				return
			}
		}
		candidates = append(candidates, sel)
	}
	// Grafana $instance 优先：domain / host:port
	for _, d := range append([]string{inst.DomainName}, inst.Domains...) {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		add(fmt.Sprintf(`instance=%q`, d))
		add(fmt.Sprintf(`domain=%q`, d))
		if port > 0 {
			add(fmt.Sprintf(`instance=%q`, fmt.Sprintf("%s:%d", d, port)))
		}
	}
	if host != "" {
		add(fmt.Sprintf(`instance=%q`, fmt.Sprintf("%s:%d", host, port)))
		add(fmt.Sprintf(`%s=%q`, hostLabel, fmt.Sprintf("%s:%d", host, port)))
		add(fmt.Sprintf(`instance=%q`, host))
	}
	for _, raw := range inst.Tags {
		k, v := splitTagKV(raw)
		if k == "" || v == "" {
			continue
		}
		if strings.EqualFold(k, "domain") {
			add(fmt.Sprintf(`instance=%q`, v))
			add(fmt.Sprintf(`domain=%q`, v))
		}
		if strings.EqualFold(k, "db") {
			add(fmt.Sprintf(`job=%q`, v))
			add(fmt.Sprintf(`datname=%q`, v))
		}
	}
	if name := strings.TrimSpace(inst.Name); name != "" {
		add(fmt.Sprintf(`job=%q`, name))
		add(fmt.Sprintf(`db=%q`, name))
	}
	return candidates
}

func splitTagKV(raw string) (key, val string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	i := strings.IndexByte(raw, '=')
	if i <= 0 {
		return "", ""
	}
	return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
}

func hostIP(host string) string {
	h := strings.TrimSpace(host)
	if i := strings.LastIndex(h, ":"); i > 0 {
		if _, err := strconv.Atoi(h[i+1:]); err == nil {
			h = h[:i]
		}
	}
	return h
}

func regexpEscape(s string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(s), `\`, `\\`)
}

const maxRangePoints = 720 // 对齐 Grafana 量级，避免长窗点数爆炸

// adaptiveStep 对齐 Grafana Interval≈15s / Step=auto：档位下限 + span/maxPoints。
func adaptiveStep(start, end time.Time, requested time.Duration) time.Duration {
	span := end.Sub(start)
	if span <= 0 {
		span = time.Hour
	}
	minStep := time.Duration(minStepSeconds) * time.Second
	var floor time.Duration
	switch {
	case span >= 30*24*time.Hour:
		floor = 30 * time.Minute
	case span >= 7*24*time.Hour:
		floor = 10 * time.Minute
	case span >= 24*time.Hour:
		floor = 5 * time.Minute
	case span >= 12*time.Hour:
		floor = time.Minute
	case span >= 3*time.Hour:
		floor = 30 * time.Second
	default:
		floor = minStep
	}
	byPoints := span / maxRangePoints
	if byPoints < minStep {
		byPoints = minStep
	}
	auto := floor
	if byPoints > auto {
		auto = byPoints
	}
	if requested < minStep {
		requested = minStep
	}
	if auto > requested {
		return auto
	}
	return requested
}
