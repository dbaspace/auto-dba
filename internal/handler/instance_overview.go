package handler

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/instoverview"
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
}

type overviewJob struct {
	Status  string `json:"status"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Message string `json:"message,omitempty"`
}

type overviewRow struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	DBType      string               `json:"db_type"`
	Host        string               `json:"host"`
	Port        int                  `json:"port"`
	EnvID       string               `json:"env_id"`
	EnvName     string               `json:"env_name"`
	Collectable bool                 `json:"collectable"`
	Sample      *instoverview.Sample `json:"sample,omitempty"`
}

func (h *OverviewHandler) List(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	rows, err := h.rows(tid, c.Query("db_type"), c.Query("env"), c.Query("q"))
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
	rows, err := h.rows(tid, req.DBType, req.Env, req.Q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var targets []overviewRow
	for _, row := range rows {
		if row.Collectable {
			targets = append(targets, row)
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
	if len(targets) == 0 {
		job.Status = "done"
		job.Message = "当前筛选没有可采集的实例"
	}
	h.jobs[tid] = job
	h.mu.Unlock()
	if len(targets) > 0 {
		go h.runCollect(tid, targets)
	}
	c.JSON(http.StatusOK, gin.H{"collect": h.jobView(tid)})
}

func (h *OverviewHandler) runCollect(tid string, targets []overviewRow) {
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

func (h *OverviewHandler) rows(tid, dbType, env, q string) ([]overviewRow, error) {
	list, err := h.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		return nil, err
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
		if needle != "" && !strings.Contains(strings.ToLower(name+" "+host), needle) {
			continue
		}
		row := overviewRow{
			ID: inst.ID, Name: name, DBType: norm, Host: host, Port: inst.Port,
			EnvID: envID, Collectable: !instoverview.Unsupported(norm),
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
