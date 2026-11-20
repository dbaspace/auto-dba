package slowreport

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/jobreg"
	"auto-dba/internal/store"
)

const sharedInstanceNeedDB = "共用实例需指定库名"

// Scope 是一条规则命中的云实例和库。
type Scope struct {
	CloudIDs    []string
	DBNames     []string
	EnvLabel    string
	TenantLabel string
	InstLabel   string
	DBLabel     string
	Err         string
	Instances   []store.Instance
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

// FireKey 返回上海时区内最近 6 小时里命中的 Cron 槽。已经发过则 ok 为 false。
func FireKey(spec, last string, now time.Time) (string, bool) {
	now = now.In(shanghai()).Truncate(time.Minute)
	for i := 0; i <= 6*60; i++ {
		t := now.Add(-time.Duration(i) * time.Minute)
		if jobreg.Cron5Matches(spec, t) {
			key := t.Format("2006-01-02T15:04")
			if key == strings.TrimSpace(last) {
				return "", false
			}
			return key, true
		}
	}
	return "", false
}

func Resolve(db *store.DB, rule store.SlowReport) Scope {
	picked, all, err := matchInstances(db, rule.EnvIDs, rule.TenantID, rule.InstanceIDs, true)
	if err != nil {
		return Scope{Err: err.Error()}
	}
	byCloud := map[string]int{}
	for _, it := range all {
		if id := strings.TrimSpace(it.CloudInstanceID); id != "" {
			byCloud[id]++
		}
	}
	dbs := compactLower(rule.DBNames)
	shared := false
	cloudSeen := map[string]struct{}{}
	var clouds []string
	var names []string
	for _, it := range picked {
		cid := strings.TrimSpace(it.CloudInstanceID)
		if byCloud[cid] > 1 {
			shared = true
		}
		if _, ok := cloudSeen[cid]; !ok {
			cloudSeen[cid] = struct{}{}
			clouds = append(clouds, cid)
		}
		names = append(names, it.Name)
	}
	if shared && len(dbs) == 0 {
		return Scope{Err: sharedInstanceNeedDB, EnvLabel: envLabel(db, rule.EnvIDs), TenantLabel: tenantLabel(db, rule.TenantID)}
	}
	dbLabel := "全部慢 SQL"
	if len(dbs) > 0 {
		dbLabel = strings.Join(dbs, ", ")
	} else if len(picked) == 0 {
		dbLabel = "—"
	}
	return Scope{
		CloudIDs:    clouds,
		DBNames:     dbs,
		EnvLabel:    envLabel(db, rule.EnvIDs),
		TenantLabel: tenantLabel(db, rule.TenantID),
		InstLabel:   strings.Join(names, ", "),
		DBLabel:     dbLabel,
		Instances:   picked,
	}
}

// OptionInstance 是周报表单里的一条可选实例。
type OptionInstance struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	EnvID      string   `json:"env_id"`
	EnvName    string   `json:"env_name"`
	TenantID   string   `json:"tenant_id"`
	TenantName string   `json:"tenant_name"`
	Databases  []string `json:"databases"`
}

// Options 按环境和业务线返回可选的 PostgreSQL 实例及其库名。
func Options(db *store.DB, envIDs []string, tenantID string) ([]OptionInstance, error) {
	if len(envKeys(nil, envIDs)) == 0 {
		return []OptionInstance{}, nil
	}
	picked, _, err := matchInstances(db, envIDs, tenantID, nil, false)
	if err != nil {
		return nil, err
	}
	envs, _ := db.ListEnvironments()
	tenants, _ := db.ListTenants()
	envName := map[string]string{}
	for _, e := range envs {
		name := e.Name
		if name == "" {
			name = e.Code
		}
		envName[e.ID] = name
		if e.Code != "" {
			envName[strings.ToLower(e.Code)] = name
		}
	}
	tenantName := map[string]string{}
	for _, t := range tenants {
		name := t.Name
		if name == "" {
			name = t.Code
		}
		tenantName[t.ID] = name
	}
	dbs, err := db.ListDatabases("", "")
	if err != nil {
		return nil, err
	}
	byInst := map[string][]string{}
	for _, d := range dbs {
		name := strings.TrimSpace(d.DBName)
		if name == "" {
			continue
		}
		byInst[d.InstanceID] = append(byInst[d.InstanceID], name)
	}
	var clouds []string
	for _, it := range picked {
		if id := strings.TrimSpace(it.CloudInstanceID); id != "" {
			clouds = append(clouds, id)
		}
	}
	byCloud, err := db.DistinctSlowLogDBNames(clouds)
	if err != nil {
		return nil, err
	}
	out := make([]OptionInstance, 0, len(picked))
	for _, it := range picked {
		names := map[string]struct{}{}
		var list []string
		add := func(n string) {
			n = strings.TrimSpace(n)
			if n == "" {
				return
			}
			if _, ok := names[strings.ToLower(n)]; ok {
				return
			}
			names[strings.ToLower(n)] = struct{}{}
			list = append(list, n)
		}
		for _, n := range byInst[it.ID] {
			add(n)
		}
		for _, n := range byCloud[strings.TrimSpace(it.CloudInstanceID)] {
			add(n)
		}
		sortStrings(list)
		env := envName[it.EnvID]
		if env == "" {
			env = envName[strings.ToLower(it.EnvID)]
		}
		out = append(out, OptionInstance{
			ID: it.ID, Name: it.Name, EnvID: it.EnvID, EnvName: env,
			TenantID: it.TenantID, TenantName: tenantName[it.TenantID], Databases: list,
		})
	}
	return out, nil
}

func matchInstances(db *store.DB, envIDs []string, tenantID string, instanceIDs []string, needCloud bool) (picked, all []store.Instance, err error) {
	listed, err := db.ListInstances("", "")
	if err != nil {
		return nil, nil, err
	}
	for _, it := range listed {
		if isPG(it.DBType) {
			all = append(all, it)
		}
	}
	envList, err := db.ListEnvironments()
	if err != nil {
		return nil, nil, err
	}
	wanted := envKeys(envList, envIDs)
	wantInst := map[string]struct{}{}
	for _, id := range instanceIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			wantInst[id] = struct{}{}
		}
	}
	tenantID = strings.TrimSpace(tenantID)
	for _, it := range all {
		if needCloud && strings.TrimSpace(it.CloudInstanceID) == "" {
			continue
		}
		if len(wanted) > 0 && !instanceInEnv(it, wanted) {
			continue
		}
		if tenantID != "" && it.TenantID != tenantID {
			continue
		}
		if len(wantInst) > 0 {
			if _, ok := wantInst[it.ID]; !ok {
				continue
			}
		}
		picked = append(picked, it)
	}
	return picked, all, nil
}

func envKeys(envs []store.Environment, selected []string) map[string]struct{} {
	sel := map[string]struct{}{}
	for _, id := range selected {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" {
			sel[id] = struct{}{}
		}
	}
	if len(sel) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, e := range envs {
		keys := []string{e.ID, e.Code, e.Name}
		hit := false
		for _, k := range keys {
			if _, ok := sel[strings.ToLower(strings.TrimSpace(k))]; ok {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, k := range keys {
			k = strings.ToLower(strings.TrimSpace(k))
			if k != "" {
				out[k] = struct{}{}
			}
		}
	}
	if len(out) == 0 {
		for id := range sel {
			out[id] = struct{}{}
		}
	}
	return out
}

func instanceInEnv(it store.Instance, wanted map[string]struct{}) bool {
	if _, ok := wanted[strings.ToLower(strings.TrimSpace(it.EnvID))]; ok {
		return true
	}
	return false
}

func sortStrings(in []string) {
	sort.Strings(in)
}

func isPG(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "postgres", "postgresql", "pg":
		return true
	default:
		return false
	}
}

func compactLower(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func envLabel(db *store.DB, ids []string) string {
	if len(ids) == 0 {
		return "全部环境"
	}
	list, err := db.ListEnvironments()
	names := map[string]string{}
	if err == nil {
		for _, e := range list {
			names[e.ID] = e.Name
			if names[e.ID] == "" {
				names[e.ID] = e.Code
			}
		}
	}
	var out []string
	for _, id := range ids {
		if n := names[id]; n != "" {
			out = append(out, n)
		} else {
			out = append(out, id)
		}
	}
	return strings.Join(out, ", ")
}

func tenantLabel(db *store.DB, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "全部业务线"
	}
	list, err := db.ListTenants()
	if err == nil {
		for _, t := range list {
			if t.ID == id {
				if t.Name != "" {
					return t.Name
				}
				return t.Code
			}
		}
	}
	return id
}

// CPUFunc 查询窗口内 CPU 峰值百分数。
type CPUFunc func(ctx context.Context, names []string, from, to time.Time) (float64, bool)

func Build(ctx context.Context, db *store.DB, rule store.SlowReport, scope Scope, now time.Time, cpu CPUFunc) (Document, error) {
	loc := shanghai()
	end := now.In(loc)
	thisFrom := end.AddDate(0, 0, -7)
	lastFrom := thisFrom.AddDate(0, 0, -7)
	title := strings.TrimSpace(rule.Name)
	if title == "" {
		title = "慢 SQL 周报"
	}
	doc := Document{Title: title, Tenant: emptyDash(scope.TenantLabel), ThisFrom: thisFrom, ThisTo: end}
	for _, bucket := range envBuckets(db, rule, scope.Instances) {
		sec, err := buildSection(ctx, db, rule, bucket, scope.DBNames, thisFrom, end, lastFrom, cpu)
		if err != nil {
			return Document{}, err
		}
		doc.Envs = append(doc.Envs, sec)
	}
	if len(doc.Envs) == 0 {
		doc.Envs = []EnvSection{{Name: emptyDash(scope.EnvLabel), Empty: true}}
	}
	return doc, nil
}

type envBucket struct {
	Name     string
	CloudIDs []string
	Insts    []string
}

func envBuckets(db *store.DB, rule store.SlowReport, picked []store.Instance) []envBucket {
	list, _ := db.ListEnvironments()
	order := []string{}
	seen := map[string]int{}
	ensure := func(name string) int {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "未分环境"
		}
		if i, ok := seen[name]; ok {
			return i
		}
		seen[name] = len(order)
		order = append(order, name)
		return len(order) - 1
	}
	if len(rule.EnvIDs) == 0 {
		ensure("全部环境")
	} else {
		for _, id := range rule.EnvIDs {
			ensure(envDisplay(list, id))
		}
	}
	buckets := make([]envBucket, len(order))
	for i, name := range order {
		buckets[i].Name = name
	}
	cloudSeen := map[string]map[string]struct{}{}
	for _, it := range picked {
		name := envDisplay(list, it.EnvID)
		if len(rule.EnvIDs) == 0 {
			name = "全部环境"
		}
		i := ensure(name)
		if i >= len(buckets) {
			buckets = append(buckets, envBucket{Name: name})
		}
		buckets[i].Insts = append(buckets[i].Insts, it.Name)
		cid := strings.TrimSpace(it.CloudInstanceID)
		if cid == "" {
			continue
		}
		if cloudSeen[name] == nil {
			cloudSeen[name] = map[string]struct{}{}
		}
		if _, ok := cloudSeen[name][cid]; ok {
			continue
		}
		cloudSeen[name][cid] = struct{}{}
		buckets[i].CloudIDs = append(buckets[i].CloudIDs, cid)
	}
	return buckets
}

func envDisplay(envs []store.Environment, id string) string {
	id = strings.TrimSpace(id)
	for _, e := range envs {
		if strings.EqualFold(e.ID, id) || strings.EqualFold(e.Code, id) || strings.EqualFold(e.Name, id) {
			if strings.TrimSpace(e.Name) != "" {
				return e.Name
			}
			if strings.TrimSpace(e.Code) != "" {
				return e.Code
			}
			return e.ID
		}
	}
	if id == "" {
		return "未分环境"
	}
	return id
}

func buildSection(ctx context.Context, db *store.DB, rule store.SlowReport, bucket envBucket, dbNames []string, thisFrom, end, lastFrom time.Time, cpu CPUFunc) (EnvSection, error) {
	thisWin, err := db.SlowReportSummary(bucket.CloudIDs, dbNames, thisFrom, end)
	if err != nil {
		return EnvSection{}, err
	}
	lastWin, err := db.SlowReportSummary(bucket.CloudIDs, dbNames, lastFrom, thisFrom)
	if err != nil {
		return EnvSection{}, err
	}
	ranks, err := db.SlowReportRanks(bucket.CloudIDs, dbNames, thisFrom, end, rule.TopN)
	if err != nil {
		return EnvSection{}, err
	}
	cpuThis, cpuLast, cpuDelta := "—", "—", "—"
	if cpu != nil && len(bucket.Insts) > 0 {
		if v, ok := cpu(ctx, bucket.Insts, thisFrom, end); ok {
			cpuThis = fmt.Sprintf("%.0f%%", v)
			if p, ok2 := cpu(ctx, bucket.Insts, lastFrom, thisFrom); ok2 {
				cpuLast = fmt.Sprintf("%.0f%%", p)
				cpuDelta = ChangePoints(v, p)
			}
		}
	}
	newN, goneN := 0, 0
	for fp := range thisWin.Fingerprints {
		if _, ok := lastWin.Fingerprints[fp]; !ok {
			newN++
		}
	}
	for fp := range lastWin.Fingerprints {
		if _, ok := thisWin.Fingerprints[fp]; !ok {
			goneN++
		}
	}
	sec := EnvSection{
		Name:      bucket.Name,
		Instances: strings.Join(bucket.Insts, ", "),
		Empty:     thisWin.Count == 0,
		Compare: []CompareRow{
			{"慢查询总次数", fmt.Sprintf("%d", thisWin.Count), fmt.Sprintf("%d", lastWin.Count), ChangePercent(float64(thisWin.Count), float64(lastWin.Count)), "持续下降为佳"},
			{"日均慢查询数", fmt.Sprintf("%.0f", float64(thisWin.Count)/7), fmt.Sprintf("%.0f", float64(lastWin.Count)/7), ChangePercent(float64(thisWin.Count)/7, float64(lastWin.Count)/7), "—"},
			{"慢查询占总请求比", "—", "—", "—", "慢日志没有总请求数，无法计算"},
			{"数据库 CPU 峰值", cpuThis, cpuLast, cpuDelta, "小于 80% 为安全"},
			{"慢查询 P99 延迟", p99Text(thisWin), p99Text(lastWin), p99Delta(thisWin, lastWin), "目标小于 1 秒"},
			{"新增慢 SQL 模板数", fmt.Sprintf("%d", newN), "—", "—", "越少越好"},
			{"本周未再出现的慢 SQL 模板数", fmt.Sprintf("%d", goneN), "—", "—", "只表示本周没有再命中，不是已确认的优化完成"},
		},
	}
	for i, r := range ranks {
		sql := strings.TrimSpace(r.SQL)
		if sql == "" {
			sql = r.Fingerprint
		}
		dbName := strings.TrimSpace(r.DBName)
		if dbName == "" {
			dbName = "—"
		}
		sec.Ranks = append(sec.Ranks, RankRow{
			Rank: i + 1, SQL: sql, DB: dbName, Calls: r.Calls,
			Avg: FormatDuration(r.AvgMS), P99: FormatDuration(r.P99MS),
			Rows: RowsFromRaw(r.Raw), Tables: TablesFromSQL(r.SQL),
		})
	}
	return sec, nil
}

func p99Text(w store.SlowReportWindow) string {
	if w.Count == 0 {
		return "—"
	}
	return FormatDuration(w.P99MS)
}

func p99Delta(this, last store.SlowReportWindow) string {
	if this.Count == 0 || last.Count == 0 {
		return "—"
	}
	return ChangePercent(this.P99MS, last.P99MS)
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
