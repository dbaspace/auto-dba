package handler

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cloud"
	"auto-dba/internal/instoverview"
	"auto-dba/internal/middleware"
	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

const (
	overviewWorkers = 8
	overviewTimeout = 3 * time.Second
)

type OverviewHandler struct {
	Store  *store.DB
	Secret string

	mu    sync.Mutex
	jobs  map[string]*overviewJob
	cache map[string]map[string]instoverview.Sample
	// hostDisk 按租户缓存 node_exporter 数据盘和根分区用量，键是主机 IP。
	hostDisk   map[string]map[string]instoverview.HostDisk
	hostDiskAt map[string]map[string]string
	// trendJobs 是后台磁盘分析，同一租户同时只保留一条。
	trendJobs map[string]*trendJob
	// pgTrendJobs 是 PostgreSQL 智能分析，与主机磁盘分析分开，互不占用对方的进行中任务。
	pgTrendJobs map[string]*trendJob
}

type overviewJob struct {
	Status  string `json:"status"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Message string `json:"message,omitempty"`
}

type overviewRow struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	DBType          string                 `json:"db_type"`
	Host            string                 `json:"host"`
	Port            int                    `json:"port"`
	EnvID           string                 `json:"env_id"`
	EnvName         string                 `json:"env_name"`
	Collectable     bool                   `json:"collectable"`
	Domain          string                 `json:"domain,omitempty"`
	App             string                 `json:"app,omitempty"`
	CloudInstanceID string                 `json:"-"`
	IDC             string                 `json:"-"`
	Sample          *instoverview.Sample   `json:"sample,omitempty"`
	HostInfo        *instoverview.HostInfo `json:"host_info,omitempty"`
}

func (h *OverviewHandler) List(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	rows, err := h.rows(tid, middleware.UserID(c), c.Query("db_type"), c.Query("env"), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "collect": h.jobView(tid)})
}

func (h *OverviewHandler) Collect(c *gin.Context) {
	var req struct {
		DBType string `json:"db_type"`
		Env    string `json:"env"`
		Q      string `json:"q"`
	}
	_ = c.ShouldBindJSON(&req)
	tid := effectiveListTenantID(h.Store, c)
	rows, err := h.listRows(tid, middleware.UserID(c), req.DBType, req.Env, req.Q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var targets, hosts []overviewRow
	for _, row := range rows {
		if row.Collectable {
			targets = append(targets, row)
		} else if store.IsHostInstanceType(row.DBType) {
			hosts = append(hosts, row)
		}
	}
	h.mu.Lock()
	if h.jobs == nil {
		h.jobs = map[string]*overviewJob{}
	}
	if job := h.jobs[tid]; job != nil && job.Status == "running" {
		view := *job
		h.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"collect": view})
		return
	}
	job := &overviewJob{Status: "running", Total: len(targets)}
	if len(hosts) > 0 {
		job.Total++
	}
	if job.Total == 0 {
		job.Status = "done"
		job.Message = "当前筛选没有可采集的实例"
	}
	h.jobs[tid] = job
	h.mu.Unlock()
	if job.Total > 0 {
		go h.runCollect(tid, targets, hosts)
	}
	c.JSON(http.StatusOK, gin.H{"collect": h.jobView(tid)})
}

func (h *OverviewHandler) runCollect(tid string, targets, hosts []overviewRow) {
	sem := make(chan struct{}, overviewWorkers)
	var wg sync.WaitGroup
	for _, row := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			sample := h.probeOne(id)
			h.mu.Lock()
			if h.cache == nil {
				h.cache = map[string]map[string]instoverview.Sample{}
			}
			if h.cache[tid] == nil {
				h.cache[tid] = map[string]instoverview.Sample{}
			}
			h.cache[tid][id] = sample
			if job := h.jobs[tid]; job != nil {
				job.Done++
			}
			h.mu.Unlock()
		}(row.ID)
	}
	wg.Wait()
	h.attachStorageRates(tid, targets)
	h.attachStarRocksCapacity(tid, targets)
	h.attachRedisCPU(tid, targets)
	if len(hosts) > 0 {
		h.collectHostDisk(tid, hosts)
	}
	h.mu.Lock()
	if job := h.jobs[tid]; job != nil {
		job.Status = "done"
		job.Done = job.Total
	}
	h.mu.Unlock()
}

func (h *OverviewHandler) probeOne(id string) instoverview.Sample {
	ctx, cancel := context.WithTimeout(context.Background(), overviewTimeout)
	defer cancel()
	inst, err := h.Store.GetInstance(id)
	if err != nil || inst == nil {
		return instoverview.Sample{ErrorKind: "connect", Error: "连不上", CollectedAt: time.Now().Format(time.RFC3339)}
	}
	if instoverview.Unsupported(inst.DBType) {
		return instoverview.SkipSample()
	}
	return instoverview.Probe(ctx, inst, h.Secret)
}

func (h *OverviewHandler) jobView(tid string) overviewJob {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobs == nil || h.jobs[tid] == nil {
		return overviewJob{Status: "idle"}
	}
	return *h.jobs[tid]
}

func (h *OverviewHandler) rows(tid, userID, dbType, env, q string) ([]overviewRow, error) {
	out, err := h.listRows(tid, userID, dbType, env, q)
	if err != nil {
		return nil, err
	}
	h.attachPostgresCloudDisk(out)
	h.attachHostInfo(tid, out)
	return out, nil
}

// listRows 只列筛选后的实例和缓存样本，不读云清单。
func (h *OverviewHandler) listRows(tid, userID, dbType, env, q string) ([]overviewRow, error) {
	list, err := h.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tid) != "" {
		ids, err := grantedVisibleInstanceIDs(h.Store, tid, userID)
		if err != nil {
			return nil, err
		}
		list, err = appendInstancesByIDs(h.Store, list, ids, "", "")
		if err != nil {
			return nil, err
		}
	}
	envs, err := h.Store.ListEnvironments()
	if err != nil {
		return nil, err
	}
	envName := map[string]string{}
	for _, e := range envs {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = e.Code
		}
		envName[e.ID] = name
	}
	wantType := strings.ToLower(strings.TrimSpace(dbType))
	wantEnv := strings.TrimSpace(env)
	needle := strings.ToLower(strings.TrimSpace(q))
	h.mu.Lock()
	cached := map[string]instoverview.Sample{}
	if h.cache != nil && h.cache[tid] != nil {
		for id, s := range h.cache[tid] {
			cached[id] = s
		}
	}
	h.mu.Unlock()
	out := make([]overviewRow, 0, len(list))
	for _, inst := range list {
		norm := store.NormalizeDBType(inst.DBType)
		if !matchOverviewType(wantType, norm) {
			continue
		}
		envID := strings.TrimSpace(inst.EnvID)
		if !matchOverviewEnv(wantEnv, envID) {
			continue
		}
		name := strings.TrimSpace(inst.Name)
		host := strings.TrimSpace(inst.Host)
		domain, app := "", ""
		if store.IsHostInstanceType(norm) {
			domain = instoverview.HostDisplayDomain(inst.Tags, inst.Domains, inst.DomainName)
			app = instoverview.HostAppName(inst.Tags)
		} else {
			domain = strings.TrimSpace(inst.DomainName)
		}
		if needle != "" && !strings.Contains(strings.ToLower(name+" "+host+" "+domain+" "+app), needle) {
			continue
		}
		row := overviewRow{
			ID: inst.ID, Name: name, DBType: norm, Host: host, Port: inst.Port,
			EnvID: envID, Collectable: !instoverview.Unsupported(norm), Domain: domain, App: app,
			CloudInstanceID: strings.TrimSpace(inst.CloudInstanceID),
			IDC:             strings.TrimSpace(inst.IDC),
		}
		if envID == "" {
			row.EnvName = "未设置环境"
		} else if n := envName[envID]; n != "" {
			row.EnvName = n
		} else {
			row.EnvName = envID
		}
		if s, ok := cached[inst.ID]; ok {
			cp := s
			row.Sample = &cp
		}
		out = append(out, row)
	}
	return out, nil
}

// attachHostInfo 给主机行附上云清单规格和缓存的数据盘用量。云清单不看租户，和 PG 云盘一致。
func (h *OverviewHandler) attachHostInfo(tid string, rows []overviewRow) {
	if h == nil || h.Store == nil {
		return
	}
	var keys []instoverview.HostCloudKey
	for _, row := range rows {
		if store.IsHostInstanceType(row.DBType) {
			keys = append(keys, instoverview.HostCloudKey{ID: row.ID, CloudID: row.CloudInstanceID, Host: row.Host})
		}
	}
	if len(keys) == 0 {
		return
	}
	var matched map[string]store.CloudInstance
	if list, err := h.Store.ListCloudResourcesForPage("", cloud.EngineHost, ""); err != nil {
		log.Printf("instance overview cloud hosts: %v", err)
	} else {
		matched = instoverview.MatchCloudHosts(keys, list)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	usage := h.hostDisk[tid]
	at := h.hostDiskAt[tid]
	for i := range rows {
		if !store.IsHostInstanceType(rows[i].DBType) {
			continue
		}
		info := instoverview.HostInfo{}
		if it, ok := matched[rows[i].ID]; ok {
			info = instoverview.CloudHostInfo(it)
		}
		ip := store.HostAddrKey(rows[i].Host)
		if d, ok := usage[ip]; ok {
			if u := d.Data; u.Size > 0 {
				info.DataUsedKnown = true
				info.DataUsedRate = u.Rate()
				info.DataUsedBytes = u.Used
				info.DataSizeBytes = u.Size
			}
			if u := d.System; u.Size > 0 {
				info.SysUsedKnown = true
				info.SysUsedRate = u.Rate()
				info.SysUsedBytes = u.Used
				info.SysSizeBytes = u.Size
			}
			info.CollectedAt = at[ip]
		}
		rows[i].HostInfo = &info
	}
}

// collectHostDisk 只查这些主机的文件系统，按 IP 归并成数据盘和根分区用量，覆盖这些 IP 的缓存。
func (h *OverviewHandler) collectHostDisk(tid string, hosts []overviewRow) {
	if h.Store == nil {
		return
	}
	raw := make([]string, 0, len(hosts))
	for _, row := range hosts {
		raw = append(raw, row.Host)
	}
	addrs := instoverview.HostAddrs(raw)
	if len(addrs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := h.promClient()
	if err != nil || cli == nil {
		return
	}
	now := time.Now()
	fs, err := queryHostFS(ctx, cli, addrs, now)
	if err != nil {
		log.Printf("instance overview host fs: %v", err)
		return
	}
	usage := instoverview.HostDiskUsage(fs)
	at := now.Format(time.RFC3339)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hostDisk == nil {
		h.hostDisk = map[string]map[string]instoverview.HostDisk{}
		h.hostDiskAt = map[string]map[string]string{}
	}
	if h.hostDisk[tid] == nil {
		h.hostDisk[tid] = map[string]instoverview.HostDisk{}
		h.hostDiskAt[tid] = map[string]string{}
	}
	for _, ip := range addrs {
		if u, ok := usage[ip]; ok {
			h.hostDisk[tid][ip] = u
			h.hostDiskAt[tid][ip] = at
		} else {
			delete(h.hostDisk[tid], ip)
			delete(h.hostDiskAt[tid], ip)
		}
	}
}

// queryHostFS 取这些主机在 ts 时刻的 node_filesystem 容量和可用，按 instance、挂载点、设备配对。
func queryHostFS(ctx context.Context, cli *promclient.Client, addrs []string, ts time.Time) ([]instoverview.FSSample, error) {
	sizeExpr, availExpr := instoverview.HostFSExprs(addrs)
	if sizeExpr == "" {
		return nil, nil
	}
	var sizes, avails []promclient.Sample
	var sizeErr, availErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sizes, sizeErr = cli.Query(ctx, sizeExpr, ts)
	}()
	go func() {
		defer wg.Done()
		avails, availErr = cli.Query(ctx, availExpr, ts)
	}()
	wg.Wait()
	if sizeErr != nil {
		return nil, sizeErr
	}
	if availErr != nil {
		return nil, availErr
	}
	key := func(m map[string]string) string {
		return m["instance"] + "\n" + m["mountpoint"] + "\n" + m["device"]
	}
	avail := make(map[string]float64, len(avails))
	for _, sm := range avails {
		if v, ok := promclient.InstantValue(sm); ok {
			avail[key(sm.Metric)] = v
		}
	}
	fs := make([]instoverview.FSSample, 0, len(sizes))
	for _, sm := range sizes {
		v, ok := promclient.InstantValue(sm)
		if !ok {
			continue
		}
		a, ok := avail[key(sm.Metric)]
		if !ok {
			continue
		}
		fs = append(fs, instoverview.FSSample{
			Instance: sm.Metric["instance"], Mountpoint: sm.Metric["mountpoint"], Device: sm.Metric["device"],
			Size: v, Avail: a,
		})
	}
	return fs, nil
}

// attachPostgresCloudDisk 按云实例 ID 或平台实例 ID 取购买磁盘，不看云清单租户。只改返回副本。
func (h *OverviewHandler) attachPostgresCloudDisk(rows []overviewRow) {
	if h == nil || h.Store == nil || len(rows) == 0 {
		return
	}
	var keys []platformDiskKey
	var instIDs, cloudIDs []string
	for _, row := range rows {
		if row.DBType != "postgres" {
			continue
		}
		keys = append(keys, platformDiskKey{ID: row.ID, CloudID: row.CloudInstanceID})
		instIDs = append(instIDs, row.ID)
		if row.CloudInstanceID != "" {
			cloudIDs = append(cloudIDs, row.CloudInstanceID)
		}
	}
	if len(keys) == 0 {
		return
	}
	list, err := h.Store.ListPostgresCloudDisks(instIDs, cloudIDs)
	if err != nil {
		log.Printf("instance overview cloud disk: %v", err)
		return
	}
	disks := assignCloudDisk(keys, list)
	if len(disks) == 0 {
		return
	}
	for i := range rows {
		b, ok := disks[rows[i].ID]
		if !ok || b <= 0 {
			continue
		}
		if rows[i].Sample == nil {
			rows[i].Sample = &instoverview.Sample{}
		}
		applyCloudCapacity(rows[i].Sample, b)
	}
}

type platformDiskKey struct {
	ID      string
	CloudID string
}

// assignCloudDisk 优先用平台实例上的云实例 ID，否则用云行指向的平台实例 ID。同一键取同步时间最新的容量。
func assignCloudDisk(keys []platformDiskKey, rows []store.CloudInstance) map[string]int64 {
	type pick struct {
		sync  string
		bytes int64
	}
	byInst := map[string]pick{}
	byCloud := map[string]pick{}
	for _, it := range rows {
		if it.DiskGB <= 0 {
			continue
		}
		b := int64(it.DiskGB) * 1024 * 1024 * 1024
		if id := strings.TrimSpace(it.InstanceID); id != "" {
			cur, ok := byInst[id]
			if !ok || it.SyncedAt > cur.sync {
				byInst[id] = pick{sync: it.SyncedAt, bytes: b}
			}
		}
		if cid := strings.ToLower(strings.TrimSpace(it.CloudID)); cid != "" {
			cur, ok := byCloud[cid]
			if !ok || it.SyncedAt > cur.sync {
				byCloud[cid] = pick{sync: it.SyncedAt, bytes: b}
			}
		}
	}
	out := map[string]int64{}
	for _, k := range keys {
		if cid := strings.ToLower(strings.TrimSpace(k.CloudID)); cid != "" {
			if p, ok := byCloud[cid]; ok {
				out[k.ID] = p.bytes
				continue
			}
		}
		if p, ok := byInst[k.ID]; ok {
			out[k.ID] = p.bytes
		}
	}
	return out
}

func applyCloudCapacity(s *instoverview.Sample, bytes int64) {
	if s == nil || bytes <= 0 {
		return
	}
	s.DiskBytes = bytes
	s.DiskKnown = true
	if s.DataKnown {
		s.StorageRate = float64(s.DataBytes) / float64(bytes) * 100
		s.StorageRateKnown = true
	}
}

func (h *OverviewHandler) attachStorageRates(tid string, targets []overviewRow) {
	var ids []string
	for _, row := range targets {
		if store.NormalizeDBType(row.DBType) == "postgres" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 || h.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cli, err := h.promClient()
	if err != nil || cli == nil {
		return
	}
	samples, err := cli.Query(ctx, `max by (idc, d_b_instance_name) (qce_postgres_storagerate_max)`, time.Now())
	if err != nil {
		log.Printf("instance overview storage rate: %v", err)
		return
	}
	var hits []instoverview.RateHit
	for _, sm := range samples {
		v, ok := promclient.InstantValue(sm)
		if !ok {
			continue
		}
		name := strings.TrimSpace(sm.Metric["d_b_instance_name"])
		if name == "" {
			continue
		}
		hits = append(hits, instoverview.RateHit{Name: name, IDC: sm.Metric["idc"], Rate: v})
	}
	regions := map[string]store.Region{}
	if list, err := h.Store.ListRegions(); err == nil {
		for _, r := range list {
			regions[r.ID] = r
		}
	}
	envByID := map[string]store.Environment{}
	if list, err := h.Store.ListEnvironments(); err == nil {
		for _, e := range list {
			envByID[e.ID] = e
		}
	}
	type matched struct {
		id   string
		rate float64
	}
	var found []matched
	for _, id := range ids {
		inst, err := h.Store.GetInstance(id)
		if err != nil || inst == nil {
			continue
		}
		reg := regions[strings.TrimSpace(inst.RegionID)]
		env := envByID[strings.TrimSpace(inst.EnvID)]
		idcs := cloudIDCs(inst, reg.Code, reg.Name, env.Code, env.Name)
		rate, ok := instoverview.PickStorageRate(inst.Name, idcs, hits)
		if !ok {
			rate, ok = instoverview.PickStorageRate(cloudDBName(inst), idcs, hits)
		}
		if !ok {
			continue
		}
		found = append(found, matched{id: id, rate: rate})
	}
	if len(found) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil || h.cache[tid] == nil {
		return
	}
	for _, m := range found {
		s, ok := h.cache[tid][m.id]
		if !ok {
			continue
		}
		s.StorageRate = m.rate
		s.StorageRateKnown = true
		h.cache[tid][m.id] = s
	}
}

const redisCPURangeExpr = `sum by (instance) (rate(redis_cpu_user_seconds_total[5m]) + rate(redis_cpu_sys_seconds_total[5m]))`

func (h *OverviewHandler) attachRedisCPU(tid string, targets []overviewRow) {
	var ids []string
	for _, row := range targets {
		if store.NormalizeDBType(row.DBType) == "redis" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 || h.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cli, err := h.promClient()
	if err != nil || cli == nil {
		return
	}
	end := time.Now()
	start := end.Add(-5 * time.Minute)
	samples, err := cli.QueryRange(ctx, redisCPURangeExpr, start, end, 15*time.Second)
	if err != nil {
		log.Printf("instance overview redis cpu: %v", err)
		return
	}
	var hits []instoverview.CPUSeriesHit
	for _, sm := range samples {
		instLabel := strings.TrimSpace(sm.Metric["instance"])
		if instLabel == "" {
			continue
		}
		raw := promclient.SampleToPoints(sm)
		if len(raw) == 0 {
			continue
		}
		pts := make([]instoverview.CpuPoint, 0, len(raw))
		for _, p := range raw {
			pts = append(pts, instoverview.CpuPoint{T: p.T, V: p.V * 100})
		}
		hits = append(hits, instoverview.CPUSeriesHit{Instance: instLabel, Points: pts})
	}
	if len(hits) == 0 {
		return
	}
	type matched struct {
		id     string
		points []instoverview.CpuPoint
	}
	var found []matched
	for _, id := range ids {
		inst, err := h.Store.GetInstance(id)
		if err != nil || inst == nil {
			continue
		}
		domains := append([]string{}, inst.Domains...)
		if dn := strings.TrimSpace(inst.DomainName); dn != "" {
			domains = append(domains, dn)
		}
		pts, ok := instoverview.PickRedisCPU(inst.Host, inst.Port, domains, hits)
		if !ok {
			continue
		}
		found = append(found, matched{id: id, points: pts})
	}
	if len(found) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil || h.cache[tid] == nil {
		return
	}
	for _, m := range found {
		s, ok := h.cache[tid][m.id]
		if !ok {
			continue
		}
		s.CpuSeries = m.points
		s.CpuKnown = true
		if n := len(m.points); n > 0 {
			s.CpuRate = m.points[n-1].V
		}
		h.cache[tid][m.id] = s
	}
}

const (
	srDataUsedExpr  = `sum by (idc, domain, cname) (starrocks_be_disks_data_used_capacity)`
	srDiskTotalExpr = `sum by (idc, domain, cname) (starrocks_be_disks_total_capacity)`
)

// attachStarRocksCapacity 用与 Grafana Used/Total Capacity 相同的指标覆盖返回缓存。匹配不到时保留 SHOW BACKENDS。
func (h *OverviewHandler) attachStarRocksCapacity(tid string, targets []overviewRow) {
	var ids []string
	for _, row := range targets {
		if store.NormalizeDBType(row.DBType) == "starrocks" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 || h.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cli, err := h.promClient()
	if err != nil || cli == nil {
		return
	}
	now := time.Now()
	dataSamples, err := cli.Query(ctx, srDataUsedExpr, now)
	if err != nil {
		log.Printf("instance overview starrocks data capacity: %v", err)
		return
	}
	diskSamples, err := cli.Query(ctx, srDiskTotalExpr, now)
	if err != nil {
		log.Printf("instance overview starrocks disk capacity: %v", err)
		return
	}
	dataHits := capacityHits(dataSamples)
	diskHits := capacityHits(diskSamples)
	definedIDC, allowCName := starRocksLabelAliases(h.Store)
	envByID := map[string]store.Environment{}
	if list, err := h.Store.ListEnvironments(); err == nil {
		for _, e := range list {
			envByID[e.ID] = e
		}
	}
	type matched struct {
		id   string
		data float64
		disk float64
	}
	var found []matched
	for _, id := range ids {
		inst, err := h.Store.GetInstance(id)
		if err != nil || inst == nil {
			continue
		}
		env := envByID[strings.TrimSpace(inst.EnvID)]
		idcs := instoverview.StarRocksIDCAliases(env.Code, inst.IDC, inst.Tags, definedIDC)
		hosts := instoverview.StarRocksHostCandidates(inst)
		data, okData := instoverview.SumCapacity(idcs, hosts, allowCName, dataHits)
		disk, okDisk := instoverview.SumCapacity(idcs, hosts, allowCName, diskHits)
		if !okData || !okDisk {
			continue
		}
		found = append(found, matched{id: id, data: data, disk: disk})
	}
	if len(found) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil || h.cache[tid] == nil {
		return
	}
	for _, m := range found {
		s, ok := h.cache[tid][m.id]
		if !ok {
			continue
		}
		instoverview.ApplyPromCapacity(&s, m.data, m.disk)
		h.cache[tid][m.id] = s
	}
}

func starRocksLabelAliases(db *store.DB) (idcValues []string, allowCName bool) {
	if db == nil {
		return nil, false
	}
	tags, err := db.ListMonitorLabelTags()
	if err != nil {
		log.Printf("instance overview label tags: %v", err)
		return nil, false
	}
	for _, t := range tags {
		switch strings.ToLower(strings.TrimSpace(t.Key)) {
		case "idc":
			idcValues = append(idcValues, t.Values...)
		case "cname":
			allowCName = true
		}
	}
	return idcValues, allowCName
}

func capacityHits(samples []promclient.Sample) []instoverview.CapacityHit {
	out := make([]instoverview.CapacityHit, 0, len(samples))
	for _, sm := range samples {
		v, ok := promclient.InstantValue(sm)
		if !ok {
			continue
		}
		out = append(out, instoverview.CapacityHit{
			IDC:    sm.Metric["idc"],
			Domain: sm.Metric["domain"],
			CName:  sm.Metric["cname"],
			Bytes:  v,
		})
	}
	return out
}

func (h *OverviewHandler) promClient() (*promclient.Client, error) {
	base, _ := h.Store.GetGlobalArg("awr_prometheus_base_url")
	token, _ := h.Store.GetGlobalArg("awr_prometheus_auth_token")
	partial, _ := h.Store.GetGlobalArg("awr_prometheus_partial_response")
	dedup, _ := h.Store.GetGlobalArg("awr_prometheus_dedup")
	return promclient.New(promclient.Config{
		BaseURL:         base,
		AuthToken:       token,
		PartialResponse: strings.EqualFold(strings.TrimSpace(partial), "true"),
		Dedup:           strings.EqualFold(strings.TrimSpace(dedup), "true"),
		Timeout:         8 * time.Second,
	}), nil
}

func cloudDBName(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	for _, raw := range inst.Tags {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "db") && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(inst.Name)
}

func cloudIDCs(inst *store.Instance, regionCode, regionName, envCode, envName string) []string {
	if inst == nil {
		return nil
	}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, e := range out {
			if strings.EqualFold(e, v) {
				return
			}
		}
		out = append(out, v)
	}
	for _, raw := range inst.Tags {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "idc") {
			add(v)
		}
	}
	add(inst.IDC)
	add(regionCode)
	add(regionName)
	add(envCode)
	add(envName)
	return out
}

func matchOverviewType(want, norm string) bool {
	switch want {
	case "", "all":
		return true
	case "other":
		return norm != "postgres" && norm != "redis"
	default:
		return norm == store.NormalizeDBType(want)
	}
}

func matchOverviewEnv(want, envID string) bool {
	switch want {
	case "", "all":
		return true
	case "unset":
		return envID == ""
	default:
		return envID == want
	}
}
