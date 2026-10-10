package dbmonitor

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

const (
	argPromBaseURL   = "awr_prometheus_base_url"
	argPromAuthToken = "awr_prometheus_auth_token"
	argPromPartial   = "awr_prometheus_partial_response"
	argPromDedup     = "awr_prometheus_dedup"
	argPromInstLabel = "awr_prom_instance_label"
	argPromHostLabel = "awr_prom_host_label"

	maxPanelsPerReq = 12
	minStepSeconds  = 15
	maxSeriesKeep   = 12
	// 按 instance 展开的面板（StarRocks BE 等）允许更多序列，配合前端图例滚动看全
	maxSeriesKeepInstance = 64
	// 多序列面板时压缩点数，降低大集群 JSON 体积
	maxPointsDenseSeries = 180
	// 进程内对 Prometheus 的并发上限，避免 overview∥rest / 多面板打满 Thanos
	promGlobalConcurrency = 8
)

// Service 数据库监控服务。
type Service struct {
	Store       *store.DB
	Secret      string
	selMu       sync.Mutex
	selCache    map[string]selCacheEntry
	selInflight map[string]*selCall
	promSem     chan struct{}
}

// New 创建服务。
func New(db *store.DB, secret string) *Service {
	return &Service{
		Store: db, Secret: secret,
		selCache:    map[string]selCacheEntry{},
		selInflight: map[string]*selCall{},
		promSem:     make(chan struct{}, promGlobalConcurrency),
	}
}

func (s *Service) acquireProm(ctx context.Context) error {
	if s == nil || s.promSem == nil {
		return nil
	}
	select {
	case s.promSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseProm() {
	if s == nil || s.promSem == nil {
		return
	}
	select {
	case <-s.promSem:
	default:
	}
}

// PointDTO 时序点。
type PointDTO struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// SeriesDTO 一条序列。
type SeriesDTO struct {
	Name    string            `json:"name"`
	Labels  map[string]string `json:"labels,omitempty"`
	Points  []PointDTO        `json:"points"`
	Max     *float64          `json:"max,omitempty"`
	Current *float64          `json:"current,omitempty"`
}

// PanelResult 面板结果。
type PanelResult struct {
	Key     string      `json:"key"`
	Title   string      `json:"title"`
	Unit    string      `json:"unit"`
	Section string      `json:"section"`
	Kind    string      `json:"kind"`
	Span    string      `json:"span,omitempty"`
	Value   *float64    `json:"value,omitempty"`
	Text    string      `json:"text,omitempty"`
	Series  []SeriesDTO `json:"series,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// InstanceItem 可监控实例。
type InstanceItem struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	DBType     string   `json:"db_type"`
	IDC        string   `json:"idc,omitempty"`
	RegionID   string   `json:"region_id,omitempty"`
	RegionCode string   `json:"region_code,omitempty"`
	RegionName string   `json:"region_name,omitempty"`
	EnvID      string   `json:"env_id,omitempty"`
	DomainName string   `json:"domain_name,omitempty"`
	Domains    []string `json:"domains,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

func (s *Service) promClient() (*promclient.Client, error) {
	base, _ := s.Store.GetGlobalArg(argPromBaseURL)
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, fmt.Errorf("未配置 Prometheus 地址（SQL 洞察设置 → awr_prometheus_base_url）")
	}
	token, _ := s.Store.GetGlobalArg(argPromAuthToken)
	// 仅当显式配置为 true 时才带 Thanos 参数，避免标准 Prometheus 400
	partial := false
	if v, _ := s.Store.GetGlobalArg(argPromPartial); v != "" {
		partial = parseBool(v, false)
	}
	dedup := false
	if v, _ := s.Store.GetGlobalArg(argPromDedup); v != "" {
		dedup = parseBool(v, false)
	}
	cli := promclient.New(promclient.Config{
		BaseURL: base, AuthToken: token,
		PartialResponse: partial, Dedup: dedup,
	})
	if cli == nil {
		return nil, fmt.Errorf("Prometheus 地址无效")
	}
	return cli, nil
}

func (s *Service) resolveEngine(instanceID, hint string) string {
	if id := strings.TrimSpace(instanceID); id != "" {
		if inst, err := s.Store.GetInstance(id); err == nil && inst != nil {
			if e := NormalizeEngine(inst.DBType); e != "" {
				return e
			}
		}
	}
	if e := NormalizeEngine(hint); e != "" {
		return e
	}
	return "postgres"
}

func parseBool(v string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	case "false", "0", "off", "no":
		return false
	default:
		return def
	}
}

func (s *Service) labelKeys() (dbLabel, hostLabel string) {
	dbLabel = "id"
	hostLabel = "instance"
	if v, _ := s.Store.GetGlobalArg(argPromInstLabel); strings.TrimSpace(v) != "" {
		dbLabel = strings.TrimSpace(v)
	}
	if v, _ := s.Store.GetGlobalArg(argPromHostLabel); strings.TrimSpace(v) != "" {
		hostLabel = strings.TrimSpace(v)
	}
	return dbLabel, hostLabel
}

// ListInstances 按引擎返回可监控实例。
func (s *Service) ListInstances(dbType string) ([]InstanceItem, error) {
	return s.ListInstancesInTenant("", dbType)
}

// ListInstancesInTenant 按租户+引擎返回可监控实例。
func (s *Service) ListInstancesInTenant(tenantID, dbType string) ([]InstanceItem, error) {
	engine := NormalizeEngine(dbType)
	if engine == "" {
		engine = "postgres"
	}
	list, err := s.Store.ListInstancesInTenant(tenantID, engine, "")
	if err != nil {
		return nil, err
	}
	// mysql 不再合并 tidb（tidb 有独立看板）；其它引擎兼容别名
	if engine == "postgres" {
		for _, alias := range []string{"postgresql", "pg"} {
			extra, _ := s.Store.ListInstancesInTenant(tenantID, alias, "")
			list = append(list, extra...)
		}
	}
	if engine == "mongodb" {
		extra, _ := s.Store.ListInstancesInTenant(tenantID, "mongo", "")
		list = append(list, extra...)
	}
	if engine == "elasticsearch" {
		for _, alias := range []string{"es", "elastic"} {
			extra, _ := s.Store.ListInstancesInTenant(tenantID, alias, "")
			list = append(list, extra...)
		}
	}
	if engine == "sqlserver" {
		extra, _ := s.Store.ListInstancesInTenant(tenantID, "mssql", "")
		list = append(list, extra...)
	}
	if engine == "cockroachdb" {
		for _, alias := range []string{"cockroach", "crdb"} {
			extra, _ := s.Store.ListInstancesInTenant(tenantID, alias, "")
			list = append(list, extra...)
		}
	}
	if engine == "oceanbase" {
		extra, _ := s.Store.ListInstancesInTenant(tenantID, "ob", "")
		list = append(list, extra...)
	}
	if engine == "host" {
		for _, alias := range []string{"node", "server"} {
			extra, _ := s.Store.ListInstancesInTenant(tenantID, alias, "")
			list = append(list, extra...)
		}
	}
	regions, _ := s.Store.ListRegions()
	regionByID := map[string]store.Region{}
	for _, r := range regions {
		regionByID[r.ID] = r
	}
	seen := map[string]bool{}
	out := make([]InstanceItem, 0)
	for _, it := range list {
		if seen[it.ID] {
			continue
		}
		t := NormalizeEngine(it.DBType)
		if t != engine {
			continue
		}
		seen[it.ID] = true
		item := InstanceItem{
			ID: it.ID, Name: it.Name, Host: it.Host, Port: it.Port, DBType: it.DBType,
			IDC: it.IDC, RegionID: it.RegionID, EnvID: it.EnvID,
			DomainName: it.DomainName, Domains: it.Domains, Tags: it.Tags,
		}
		if r, ok := regionByID[strings.TrimSpace(it.RegionID)]; ok {
			item.RegionCode = r.Code
			item.RegionName = r.Name
		}
		item.IDC = effectiveInstanceIDC(item)
		out = append(out, item)
	}
	return out, nil
}

// effectiveInstanceIDC 优先标签 idc=，其次实例 idc 字段，再回退区域 code/name（对齐 Grafana 变量）。
func effectiveInstanceIDC(it InstanceItem) string {
	for _, raw := range it.Tags {
		t := strings.TrimSpace(raw)
		if i := strings.IndexByte(t, '='); i > 0 {
			if strings.EqualFold(strings.TrimSpace(t[:i]), "idc") {
				if v := strings.TrimSpace(t[i+1:]); v != "" {
					return v
				}
			}
		}
	}
	if v := strings.TrimSpace(it.IDC); v != "" {
		return v
	}
	if v := strings.TrimSpace(it.RegionCode); v != "" {
		return v
	}
	return strings.TrimSpace(it.RegionName)
}


// Overview 瞬时 KPI（默认仅 Overview 段，不含 Settings）。
func (s *Service) Overview(ctx context.Context, instanceID string, includeSettings bool, engineHint string) ([]PanelResult, error) {
	cli, err := s.promClient()
	if err != nil {
		return nil, err
	}
	engine := s.resolveEngine(instanceID, engineHint)
	sel, err := s.resolveSelectors(ctx, cli, instanceID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	secs := []string{"overview"}
	if includeSettings {
		secs = append(secs, "settings")
	}
	var defs []PanelDef
	for _, sec := range secs {
		for _, p := range panelsBySection(engine, sec) {
			if p.Kind == KindStat {
				defs = append(defs, p)
			}
		}
	}
	return s.runStats(ctx, cli, defs, sel, now), nil
}

func (s *Service) runStats(ctx context.Context, cli *promclient.Client, defs []PanelDef, sel resolvedSel, ts time.Time) []PanelResult {
	out := make([]PanelResult, len(defs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, p := range defs {
		i, p := i, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := s.queryStat(ctx, cli, p, sel.DB, sel.Source, ts)
			if !sel.OK && r.Value == nil && r.Error == "" && (r.Text == "" || r.Text == "-") {
				r.Error = "未匹配到 Prometheus 指标，请检查实例标签是否与采集标签一致（如 domain/db/idc）"
			}
			out[i] = r
		}()
	}
	wg.Wait()
	return out
}

// QueryOpts 编排查询参数。
type QueryOpts struct {
	InstanceID  string
	Engine      string
	IDC         string
	Domain      string
	FEInstances []string
	BEInstances []string
	Sections    []string
	Start       time.Time
	End         time.Time
	Step        time.Duration
}

func defaultQuerySections(engine string) []string {
	if NormalizeEngine(engine) == "starrocks" {
		return []string{"overview", "cluster_overview", "query_statistic", "fe_jvm", "be"}
	}
	return []string{"overview", "db_stats", "sessions"}
}

// QueryAll 一次 resolve，并行拉 KPI + 时序。
func (s *Service) QueryAll(ctx context.Context, opts QueryOpts) ([]PanelResult, error) {
	cli, err := s.promClient()
	if err != nil {
		return nil, err
	}
	engine := NormalizeEngine(opts.Engine)
	if engine == "" {
		engine = s.resolveEngine(opts.InstanceID, opts.Engine)
	}

	var sel resolvedSel
	if NormalizeEngine(engine) == "starrocks" && strings.TrimSpace(opts.Domain) != "" {
		sel, err = s.resolveClusterSelectors(ctx, cli, ClusterSel{
			IDC: opts.IDC, Domain: opts.Domain,
			FEInstances: opts.FEInstances, BEInstances: opts.BEInstances,
		})
	} else if EnvQueryEngine(engine) && (strings.TrimSpace(opts.IDC) != "" || strings.TrimSpace(opts.Domain) != "") && strings.TrimSpace(opts.InstanceID) == "" {
		sel, err = s.resolveEnvSelectors(ctx, cli, engine, opts.IDC, opts.Domain)
	} else {
		sel, err = s.resolveSelectors(ctx, cli, opts.InstanceID)
	}
	if err != nil {
		return nil, err
	}

	start, end, step := opts.Start, opts.End, opts.Step
	if end.Before(start) || end.Equal(start) {
		end = start.Add(time.Hour)
	}
	step = adaptiveStep(start, end, step)
	if NormalizeEngine(engine) == "starrocks" {
		// 大集群多 BE 序列：3h 窗抬到 60s，压点数与 Prom 负载
		span := end.Sub(start)
		if span >= 3*time.Hour && step < 60*time.Second {
			step = 60 * time.Second
		}
	}

	sections := opts.Sections
	if len(sections) == 0 {
		sections = defaultQuerySections(engine)
	}
	want := map[string]bool{}
	for _, sec := range sections {
		want[strings.TrimSpace(sec)] = true
	}

	var defs []PanelDef
	for _, sec := range sectionsFor(engine) {
		if !want[sec.Name] {
			continue
		}
		for _, p := range sec.Panels {
			defs = append(defs, p)
		}
	}
	if len(defs) > maxPanelsPerReq*4 {
		defs = defs[:maxPanelsPerReq*4]
	}

	var preferInst *store.Instance
	var idx *instanceDisplayIndex
	if strings.TrimSpace(opts.InstanceID) != "" {
		preferInst, _ = s.Store.GetInstance(opts.InstanceID)
	}
	if preferInst == nil && NormalizeEngine(engine) != "starrocks" && !EnvQueryEngine(engine) {
		needIdx := false
		for _, p := range defs {
			if p.Kind == KindTimeseries && !p.isHost() {
				needIdx = true
				break
			}
		}
		if needIdx {
			idx = s.buildInstanceDisplayIndex()
		}
	}

	panelConc := 6
	switch NormalizeEngine(engine) {
	case "starrocks":
		panelConc = 5
	case "redis":
		panelConc = 4
	}
	out := make([]PanelResult, len(defs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, panelConc)
	sharedPin := ""
	eng := NormalizeEngine(engine)
	if (eng == "postgres" || eng == "redis") && strings.TrimSpace(sel.DB) != "" {
		for _, p := range defs {
			if p.Kind == KindTimeseries && !p.isHost() && needsSharedLabelPin(p) {
				sharedPin = s.resolveSharedLabelPin(ctx, cli, eng, sel.DB, end)
				break
			}
		}
	}
	for i, p := range defs {
		i, p := i, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			use := sel.DB
			src := sel.Source
			if p.isHost() {
				use = strings.TrimSpace(sel.Cloud)
				src = SourceQCE
				if use == "" {
					out[i] = PanelResult{
						Key: p.Key, Title: p.Title, Unit: p.Unit, Section: p.Section, Kind: p.Kind, Span: p.Span,
						Error: "未匹配云监控标签，请确认 IDC 与实例名（d_b_instance_name）",
					}
					return
				}
			}
			if p.Kind == KindStat {
				r := s.queryStat(ctx, cli, p, use, src, end)
				if !sel.OK && r.Value == nil && r.Error == "" && (r.Text == "" || r.Text == "-") {
					r.Error = "未匹配到 Prometheus 指标，请检查标签是否与采集一致（如 domain/idc）"
				}
				out[i] = r
				return
			}
			pin := ""
			if needsSharedLabelPin(p) {
				pin = sharedPin
			}
			r := s.queryRange(ctx, cli, p, use, src, start, end, step, preferInst, idx, pin)
			if len(r.Series) == 0 && r.Error == "" {
				if !sel.OK {
					r.Error = "未匹配到时序数据（selector=" + use + "）"
				} else if p.isHost() {
					r.Error = "主机指标无数据（selector=" + use + "）"
				}
			}
			out[i] = r
		}()
	}
	wg.Wait()
	return out, nil
}

// QueryPanels 拉取时序/指定面板。
func (s *Service) QueryPanels(ctx context.Context, instanceID, section string, keys []string, start, end time.Time, step time.Duration, engineHint string) ([]PanelResult, error) {
	cli, err := s.promClient()
	if err != nil {
		return nil, err
	}
	engine := s.resolveEngine(instanceID, engineHint)
	sel, err := s.resolveSelectors(ctx, cli, instanceID)
	if err != nil {
		return nil, err
	}
	if end.Before(start) || end.Equal(start) {
		end = start.Add(time.Hour)
	}
	step = adaptiveStep(start, end, step)

	var defs []PanelDef
	if len(keys) > 0 {
		for _, k := range keys {
			if p := findPanel(engine, k); p != nil {
				defs = append(defs, *p)
			}
		}
	} else if strings.TrimSpace(section) == "settings" {
		for _, p := range panelsBySection(engine, "settings") {
			if p.Kind == KindStat {
				defs = append(defs, p)
			}
		}
		return s.runStats(ctx, cli, defs, sel, end), nil
	} else {
		for _, p := range panelsBySection(engine, section) {
			if p.Kind == KindTimeseries {
				defs = append(defs, p)
			}
		}
	}
	if len(defs) > maxPanelsPerReq {
		defs = defs[:maxPanelsPerReq]
	}

	requireInst := strings.TrimSpace(instanceID) == ""

	var preferInst *store.Instance
	if !requireInst {
		preferInst, _ = s.Store.GetInstance(instanceID)
	}
	var idx *instanceDisplayIndex
	// 已选定实例时用 prefer 做图例，跳过全表 ListInstances
	if preferInst == nil {
		needIdx := false
		for _, p := range defs {
			if !p.isHost() {
				needIdx = true
				break
			}
		}
		if needIdx {
			idx = s.buildInstanceDisplayIndex()
		}
	}

	out := make([]PanelResult, len(defs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	sharedPin := ""
	eng := NormalizeEngine(engine)
	if (eng == "postgres" || eng == "redis") && strings.TrimSpace(sel.DB) != "" {
		for _, p := range defs {
			if p.Kind == KindTimeseries && !p.isHost() && needsSharedLabelPin(p) {
				sharedPin = s.resolveSharedLabelPin(ctx, cli, eng, sel.DB, end)
				break
			}
		}
	}
	for i, p := range defs {
		i, p := i, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			use := sel.DB
			src := sel.Source
			if p.isHost() {
				if requireInst {
					out[i] = PanelResult{
						Key: p.Key, Title: p.Title, Unit: p.Unit, Section: p.Section, Kind: p.Kind, Span: p.Span,
						Error: "请选择具体实例以查看主机指标",
					}
					return
				}
				use = strings.TrimSpace(sel.Cloud)
				src = SourceQCE
				if use == "" {
					out[i] = PanelResult{
						Key: p.Key, Title: p.Title, Unit: p.Unit, Section: p.Section, Kind: p.Kind, Span: p.Span,
						Error: "未匹配云监控标签，请确认 IDC 与实例名（d_b_instance_name）",
					}
					return
				}
			}
			if p.Kind == KindStat {
				r := s.queryStat(ctx, cli, p, use, src, end)
				if !sel.OK && r.Value == nil && r.Error == "" && (r.Text == "" || r.Text == "-") {
					r.Error = "未匹配到 Prometheus 指标，请检查实例标签是否与采集标签一致（如 domain/db/idc）"
				}
				out[i] = r
				return
			}
			pin := ""
			if needsSharedLabelPin(p) {
				pin = sharedPin
			}
			r := s.queryRange(ctx, cli, p, use, src, start, end, step, preferInst, idx, pin)
			if len(r.Series) == 0 && r.Error == "" {
				if !sel.OK {
					r.Error = "未匹配到时序数据（selector=" + use + "）"
				} else if p.isHost() {
					r.Error = "主机指标无数据（source=" + src + ", selector=" + use + "）"
				}
			}
			out[i] = r
		}()
	}
	wg.Wait()
	return out, nil
}

func (s *Service) queryStat(ctx context.Context, cli *promclient.Client, p PanelDef, sel, source string, ts time.Time) PanelResult {
	res := PanelResult{
		Key: p.Key, Title: p.Title, Unit: p.Unit, Section: p.Section, Kind: p.Kind, Span: p.Span,
	}
	queries := []string{p.promQLFor(source)}
	if source != SourceQCE {
		queries = append(queries, statFallbacks(p.Key)...)
	}
	var lastErr error
	for _, tmpl := range queries {
		if strings.TrimSpace(tmpl) == "" {
			continue
		}
		promql := renderPromQL(tmpl, sel)
		if err := s.acquireProm(ctx); err != nil {
			lastErr = err
			continue
		}
		samples, err := cli.Query(ctx, promql, ts)
		s.releaseProm()
		if err != nil {
			lastErr = err
			continue
		}
		if len(samples) == 0 {
			continue
		}
		if p.Key == "pg_version" {
			if txt := versionFromLabels(samples[0].Metric); txt != "" {
				res.Text = txt
				return res
			}
		}
		v, ok := promclient.InstantValue(samples[0])
		if !ok {
			if txt := versionFromLabels(samples[0].Metric); txt != "" {
				res.Text = txt
				return res
			}
			continue
		}
		res.Value = &v
		res.Text = formatStat(p.Unit, v)
		return res
	}
	if lastErr != nil {
		res.Error = lastErr.Error()
	}
	res.Text = "-"
	return res
}

func versionFromLabels(m map[string]string) string {
	for _, k := range []string{"short_version", "version", "server_version", "pg_version"} {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func rangeFallbacks(key string) []string {
	switch key {
	case "idle_sessions":
		return []string{
			`sum by (datname) (pg_stat_activity_count{__SEL__,state="idle"})`,
		}
	default:
		return nil
	}
}

func statFallbacks(key string) []string {
	switch key {
	case "pg_version":
		return []string{
			`max(pg_static{__SEL__})`,
			`max(pg_version{__SEL__})`,
			`pg_static{__SEL__}`,
			`pg_version_info{__SEL__}`,
		}
	case "pg_start_time":
		return []string{
			`max(pg_postmaster_start_time{__SEL__})`,
		}
	case "max_connections":
		return []string{
			`max(pg_settings_max_connections{__SEL__})`,
			`max(pg_settings{name="max_connections",__SEL__})`,
		}
	case "shared_buffers":
		return []string{
			`max(pg_settings_shared_buffers{__SEL__})`,
		}
	case "effective_cache":
		return []string{
			`max(pg_settings_effective_cache_size{__SEL__})`,
		}
	case "maintenance_work_mem":
		return []string{`max(pg_settings_maintenance_work_mem{__SEL__})`}
	case "work_mem":
		return []string{`max(pg_settings_work_mem{__SEL__})`}
	case "max_wal_size":
		return []string{`max(pg_settings_max_wal_size{__SEL__})`}
	default:
		return nil
	}
}

func (s *Service) queryRange(ctx context.Context, cli *promclient.Client, p PanelDef, sel, source string, start, end time.Time, step time.Duration, prefer *store.Instance, idx *instanceDisplayIndex, sharedPin string) PanelResult {
	res := PanelResult{
		Key: p.Key, Title: p.Title, Unit: p.Unit, Section: p.Section, Kind: p.Kind, Span: p.Span,
	}
	queries := []string{p.promQLFor(source)}
	if source != SourceQCE {
		queries = append(queries, rangeFallbacks(p.Key)...)
	}
	var lastErr error
	var samples []promclient.Sample
	for _, tmpl := range queries {
		if strings.TrimSpace(tmpl) == "" {
			continue
		}
		promql := renderPromQL(tmpl, sel)
		got, err := s.queryRangeMaybePinTopK(ctx, cli, promql, p.LegendKeys, start, end, step, sharedPin)
		if err != nil {
			lastErr = err
			continue
		}
		if len(got) == 0 {
			continue
		}
		samples = got
		lastErr = nil
		break
	}
	if lastErr != nil && len(samples) == 0 {
		res.Error = lastErr.Error()
		return res
	}
	keep := seriesKeepLimit(p)
	maxPts := 0
	if keep > maxSeriesKeep {
		maxPts = maxPointsDenseSeries
	}
	series := make([]SeriesDTO, 0, len(samples))
	for _, sm := range samples {
		pts := promclient.SampleToPoints(sm)
		if len(pts) == 0 {
			continue
		}
		name := seriesName(sm.Metric, p.LegendKeys, prefer, idx)
		// 压掉极端尖刺后再算 max/current，并下发裁剪后的点
		pts = softClipPoints(pts)
		dtoPts := make([]PointDTO, 0, len(pts))
		for _, pt := range pts {
			dtoPts = append(dtoPts, PointDTO{T: pt.T, V: pt.V})
		}
		dtoPts = downsamplePoints(dtoPts, maxPts)
		max, cur, ok := promclient.SeriesStats(pts)
		item := SeriesDTO{Name: name, Labels: sm.Metric, Points: dtoPts}
		if ok {
			mv, cv := max, cur
			item.Max = &mv
			item.Current = &cv
		}
		series = append(series, item)
	}
	series = trimSeries(series, keep)
	res.Series = series
	return res
}

// needsSharedLabelPin 判断面板是否可用批次共享的 datname/db pin。
func needsSharedLabelPin(p PanelDef) bool {
	ql := strings.TrimSpace(p.PromQL)
	if ql == "" || !strings.HasPrefix(strings.ToLower(ql), "topk(") {
		return false
	}
	k := pinLabelKey(p.LegendKeys)
	return k == "datname" || k == "db" || k == "database"
}

// resolveSharedLabelPin 一次 instant 钉死 top 标签，供同批 topk range 复用。
func (s *Service) resolveSharedLabelPin(ctx context.Context, cli *promclient.Client, engine, dbSel string, end time.Time) string {
	dbSel = strings.TrimSpace(dbSel)
	if dbSel == "" {
		return ""
	}
	var candidates []string
	switch NormalizeEngine(engine) {
	case "redis":
		candidates = []string{
			fmt.Sprintf(`topk(12, sum by (db) (redis_db_keys{%s}))`, dbSel),
		}
	default:
		candidates = []string{
			fmt.Sprintf(`topk(12, sum by (datname) (pg_stat_activity_count{%s,datname!~"template.*|postgres"}))`, dbSel),
			fmt.Sprintf(`topk(12, sum by (datname) (rate(pg_stat_database_xact_commit{%s,datname!~"template.*|postgres"}[5m])))`, dbSel),
		}
	}
	label := "datname"
	if NormalizeEngine(engine) == "redis" {
		label = "db"
	}
	for _, q := range candidates {
		if err := s.acquireProm(ctx); err != nil {
			return ""
		}
		inst, err := cli.Query(ctx, q, end)
		s.releaseProm()
		if err != nil || len(inst) == 0 {
			continue
		}
		if m := matcherFromSamples(inst, label, 12); m != "" {
			return m
		}
	}
	return ""
}

// queryRangeMaybePinTopK：topk 在 query_range 上会跨时间戳并集膨胀；
// 优先用批次共享 pin；否则先 instant topk 钉死标签，再对 INNER 做 range。
func (s *Service) queryRangeMaybePinTopK(
	ctx context.Context, cli *promclient.Client, promql string, legendKeys []string,
	start, end time.Time, step time.Duration, sharedPin string,
) (samples []promclient.Sample, err error) {
	k, inner, isTopK := splitOuterTopK(promql)
	rangeQL := promql
	if isTopK {
		pinKey := pinLabelKey(legendKeys)
		if sharedPin != "" && (pinKey == "datname" || pinKey == "db" || pinKey == "database") {
			rangeQL = injectLabelMatcher(inner, sharedPin)
		} else {
			if err := s.acquireProm(ctx); err != nil {
				return nil, err
			}
			inst, qerr := cli.Query(ctx, promql, end)
			s.releaseProm()
			if qerr == nil {
				if m := matcherFromSamples(inst, pinKey, k); m != "" {
					rangeQL = injectLabelMatcher(inner, m)
				}
			}
		}
	}
	if err := s.acquireProm(ctx); err != nil {
		return nil, err
	}
	got, qerr := cli.QueryRange(ctx, rangeQL, start, end, step)
	s.releaseProm()
	if qerr != nil {
		return nil, qerr
	}
	return got, nil
}

// downsamplePoints 均匀抽稀时序点，保留首尾。
func downsamplePoints(pts []PointDTO, maxPts int) []PointDTO {
	if maxPts <= 0 || len(pts) <= maxPts {
		return pts
	}
	if maxPts < 2 {
		return pts[:1]
	}
	out := make([]PointDTO, 0, maxPts)
	last := len(pts) - 1
	for i := 0; i < maxPts; i++ {
		idx := i * last / (maxPts - 1)
		out = append(out, pts[idx])
	}
	return out
}

// seriesKeepLimit：topk 面板已在 PromQL 限制；按 instance 展开的（如 SR BE）放宽以便图例滚动看全。
func seriesKeepLimit(p PanelDef) int {
	ql := strings.TrimSpace(p.PromQL)
	if strings.HasPrefix(strings.ToLower(ql), "topk(") {
		return maxSeriesKeep
	}
	for _, k := range p.LegendKeys {
		switch strings.TrimSpace(k) {
		case "instance", "path_hash", "mountpoint", "device":
			return maxSeriesKeepInstance
		}
	}
	return maxSeriesKeep
}

func trimSeries(series []SeriesDTO, keep int) []SeriesDTO {
	if keep <= 0 || len(series) <= keep {
		return series
	}
	sort.SliceStable(series, func(i, j int) bool {
		ai, aj := 0.0, 0.0
		if series[i].Current != nil {
			ai = *series[i].Current
		} else if series[i].Max != nil {
			ai = *series[i].Max
		}
		if series[j].Current != nil {
			aj = *series[j].Current
		} else if series[j].Max != nil {
			aj = *series[j].Max
		}
		return ai > aj
	})
	return series[:keep]
}

// softClipPoints 用 P98*8 裁剪 rate 尖刺，避免单点把图例 max / Y 轴拉飞。
func softClipPoints(pts []promclient.Point) []promclient.Point {
	if len(pts) < 8 {
		return pts
	}
	vals := make([]float64, 0, len(pts))
	for _, p := range pts {
		if p.V >= 0 && !isBadFloat(p.V) {
			vals = append(vals, p.V)
		}
	}
	if len(vals) < 8 {
		return pts
	}
	sort.Float64s(vals)
	p98 := vals[int(float64(len(vals)-1)*0.98)]
	if p98 <= 0 {
		return pts
	}
	capV := p98 * 8
	out := make([]promclient.Point, 0, len(pts))
	for _, p := range pts {
		if isBadFloat(p.V) || p.V < 0 {
			continue
		}
		if p.V > capV {
			p.V = capV
		}
		out = append(out, p)
	}
	return out
}

func isBadFloat(v float64) bool {
	return v != v || v > 1e18 // NaN or absurd
}

func seriesName(metric map[string]string, keys []string, prefer *store.Instance, idx *instanceDisplayIndex) string {
	if len(keys) == 0 {
		if v := metric["datname"]; v != "" {
			return v
		}
		if v := metric["instance"]; v != "" {
			return resolveInstanceLegend(v, prefer, idx)
		}
		return "series"
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.TrimSpace(metric[k])
		if v == "" {
			continue
		}
		if k == "instance" {
			v = resolveInstanceLegend(v, prefer, idx)
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "series"
	}
	return strings.Join(parts, "; ")
}

// instanceDisplayIndex 将主机 IP 映射到平台登记实例，用于图例展示。
type instanceDisplayIndex struct {
	byIP map[string]*store.Instance
}

func (s *Service) buildInstanceDisplayIndex() *instanceDisplayIndex {
	idx := &instanceDisplayIndex{byIP: map[string]*store.Instance{}}
	if s == nil || s.Store == nil {
		return idx
	}
	list, err := s.Store.ListInstances("", "")
	if err != nil {
		return idx
	}
	for i := range list {
		it := &list[i]
		for _, h := range []string{it.Host, it.DomainName} {
			ip := hostIP(h)
			if ip == "" {
				continue
			}
			if _, ok := idx.byIP[ip]; !ok {
				idx.byIP[ip] = it
			}
		}
		for _, d := range it.Domains {
			ip := hostIP(d)
			if ip == "" {
				continue
			}
			if _, ok := idx.byIP[ip]; !ok {
				idx.byIP[ip] = it
			}
		}
	}
	return idx
}

func instanceLegendLabel(it *store.Instance) string {
	if it == nil {
		return ""
	}
	// 优先 db= 标签值（实例标签），其次其它非 idc 标签，再名称，最后实例地址
	var extra []string
	for _, t := range it.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if i := strings.IndexByte(t, '='); i > 0 {
			k := strings.TrimSpace(t[:i])
			v := strings.TrimSpace(t[i+1:])
			if strings.EqualFold(k, "db") && v != "" {
				return v
			}
			if strings.EqualFold(k, "idc") {
				continue
			}
			if v != "" {
				extra = append(extra, v)
			}
		} else {
			extra = append(extra, t)
		}
	}
	if len(extra) > 0 {
		return extra[0]
	}
	if name := strings.TrimSpace(it.Name); name != "" {
		return name
	}
	host := strings.TrimSpace(it.Host)
	if host == "" {
		host = strings.TrimSpace(it.DomainName)
	}
	if host == "" && len(it.Domains) > 0 {
		host = strings.TrimSpace(it.Domains[0])
	}
	if host == "" {
		return ""
	}
	if it.Port > 0 && !strings.Contains(host, ":") {
		return fmt.Sprintf("%s:%d", host, it.Port)
	}
	return host
}

// resolveInstanceLegend 将 Prometheus exporter 采集地址转为平台实例标签名；无标签则实例地址。
func resolveInstanceLegend(exporterAddr string, prefer *store.Instance, idx *instanceDisplayIndex) string {
	exporterAddr = strings.TrimSpace(exporterAddr)
	ip := hostIP(exporterAddr)

	if prefer != nil {
		prefIP := hostIP(prefer.Host)
		if prefIP == "" {
			prefIP = hostIP(prefer.DomainName)
		}
		// 已选实例的主机面板：exporter IP 通常与实例同机；匹配不上也优先用当前实例展示名
		if prefIP == "" || ip == "" || prefIP == ip || strings.HasPrefix(ip, prefIP) || strings.HasPrefix(prefIP, ip) {
			if label := instanceLegendLabel(prefer); label != "" {
				return label
			}
		}
	}
	if idx != nil && ip != "" {
		if it := idx.byIP[ip]; it != nil {
			if label := instanceLegendLabel(it); label != "" {
				return label
			}
		}
	}
	// 未登记：去掉 exporter 端口，仅保留地址；再不行回退原文
	if ip != "" {
		return ip
	}
	return exporterAddr
}

func formatStat(unit string, v float64) string {
	switch unit {
	case "unix":
		if v <= 0 {
			return "N/A"
		}
		return time.Unix(int64(v), 0).Local().Format("2006-01-02 15:04:05")
	case "bytes":
		return formatBytes(v)
	case "rows":
		return formatRows(v)
	case "ratio":
		return fmt.Sprintf("%.2f%%", v*100)
	case "percent":
		return fmt.Sprintf("%.2f%%", v)
	case "seconds":
		sec := int64(v)
		if sec < 60 {
			return fmt.Sprintf("%ds", sec)
		}
		if sec < 3600 {
			return fmt.Sprintf("%dm%ds", sec/60, sec%60)
		}
		if sec < 86400 {
			return fmt.Sprintf("%dh%dm", sec/3600, (sec%3600)/60)
		}
		return fmt.Sprintf("%dd%dh", sec/86400, (sec%86400)/3600)
	case "text":
		// 版本号常以整数形式上报，尽量保留小数语义
		if v > 1000 && v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		s := strconv.FormatFloat(v, 'f', -1, 64)
		return s
	default:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return fmt.Sprintf("%.2f", v)
	}
}

func formatBytes(n float64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	u := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for n >= 1024 && i < len(u)-1 {
		n /= 1024
		i++
	}
	var body string
	if i == 0 {
		body = fmt.Sprintf("%.0f", n)
	} else {
		body = fmt.Sprintf("%.2f", n)
	}
	if strings.HasPrefix(body, ".") {
		body = "0" + body
	}
	return sign + body + " " + u[i]
}

func formatRows(n float64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1e12:
		return fmt.Sprintf("%.2f T", n/1e12)
	case abs >= 1e9:
		return fmt.Sprintf("%.2f B", n/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("%.2f M", n/1e6)
	case abs >= 1e3:
		return fmt.Sprintf("%.2f K", n/1e3)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}
