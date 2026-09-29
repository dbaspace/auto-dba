package cmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// CandidateCI 同步候选（默认不自动入库）。
type CandidateCI struct {
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Host       string         `json:"host,omitempty"`
	Port       int            `json:"port,omitempty"`
	Domain     string         `json:"domain,omitempty"`
	DBType     string         `json:"db_type,omitempty"`
	IDC        string         `json:"idc,omitempty"`
	Attrs      map[string]any `json:"attrs,omitempty"`
	ExternalID string         `json:"external_id,omitempty"`
	MatchedID  string         `json:"matched_instance_id,omitempty"`
}

// SyncAdapter 可插拔同步。
type SyncAdapter interface {
	Name() string
	Pull(ctx context.Context, cfg json.RawMessage) ([]CandidateCI, error)
}

var adapters = map[string]SyncAdapter{}

func RegisterAdapter(a SyncAdapter) {
	if a != nil {
		adapters[a.Name()] = a
	}
}

func init() {
	RegisterAdapter(&prometheusLabelsAdapter{})
	RegisterAdapter(&httpCmdbAdapter{})
	RegisterAdapter(&cloudTencentAdapter{})
}

type prometheusLabelsAdapter struct{}

func (a *prometheusLabelsAdapter) Name() string { return "prometheus_labels" }

func (a *prometheusLabelsAdapter) Pull(ctx context.Context, cfg json.RawMessage) ([]CandidateCI, error) {
	// cfg 可选；实际拉取走 Service 注入的 store —— 通过包装
	_ = ctx
	_ = cfg
	return nil, fmt.Errorf("use Service.PullAdapter")
}

type httpCmdbAdapter struct{}

func (a *httpCmdbAdapter) Name() string { return "http_cmdb" }

type httpCmdbConfig struct {
	BaseURL string            `json:"base_url"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	// JSONPath-ish: items array field
	ItemsField string `json:"items_field"`
}

func (a *httpCmdbAdapter) Pull(ctx context.Context, cfgJSON json.RawMessage) ([]CandidateCI, error) {
	var cfg httpCmdbConfig
	if len(cfgJSON) > 0 {
		_ = json.Unmarshal(cfgJSON, &cfg)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("http_cmdb: base_url 必填")
	}
	path := cfg.Path
	if path == "" {
		path = "/api/cis"
	}
	url := strings.TrimRight(cfg.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http_cmdb: status %d: %s", resp.StatusCode, string(body))
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	items := extractItems(raw, cfg.ItemsField)
	var out []CandidateCI
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		c := CandidateCI{
			Type: TypeInstance,
			Name: strVal(m, "name", "hostname", "host"),
			Host: strVal(m, "host", "ip", "address"),
			Domain: strVal(m, "domain", "domain_name"),
			DBType: strVal(m, "db_type", "engine", "type"),
			IDC: strVal(m, "idc", "env", "environment"),
			ExternalID: strVal(m, "id", "ci_id", "external_id"),
			Attrs: m,
		}
		if p, ok := m["port"].(float64); ok {
			c.Port = int(p)
		}
		out = append(out, c)
	}
	return out, nil
}

func extractItems(raw any, field string) []any {
	if field == "" {
		field = "data"
	}
	switch v := raw.(type) {
	case []any:
		return v
	case map[string]any:
		if arr, ok := v[field].([]any); ok {
			return arr
		}
		if arr, ok := v["items"].([]any); ok {
			return arr
		}
		if arr, ok := v["cis"].([]any); ok {
			return arr
		}
	}
	return nil
}

func strVal(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			}
		}
	}
	return ""
}

type cloudTencentAdapter struct{}

func (a *cloudTencentAdapter) Name() string { return "cloud_tencent" }

func (a *cloudTencentAdapter) Pull(ctx context.Context, cfg json.RawMessage) ([]CandidateCI, error) {
	_ = ctx
	_ = cfg
	// 校验型：由 Service.PullCloudTencent 用本地 cloud_* 字段产出「待校验」候选
	return nil, fmt.Errorf("use Service.PullCloudTencent")
}

// RunSyncJob 执行同步任务，默认只产出候选写入 run log，不自动建实例。
func (s *Service) RunSyncJob(ctx context.Context, jobID, actor string) (*store.CmdbSyncRun, []CandidateCI, error) {
	job, err := s.Store.GetCmdbSyncJob(jobID)
	if err != nil || job == nil {
		return nil, nil, fmt.Errorf("同步任务不存在")
	}
	run := &store.CmdbSyncRun{
		JobID: job.ID, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Store.CreateCmdbSyncRun(run); err != nil {
		return nil, nil, err
	}
	cands, err := s.pullByAdapter(ctx, job.Adapter, job.ConfigJSON)
	if err != nil {
		run.Status = "failed"
		run.LogText = err.Error()
		run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = s.Store.UpdateCmdbSyncRun(run)
		return run, nil, err
	}
	// 匹配本地
	insts, _ := s.Store.ListInstances("", "")
	byHP := map[string]string{}
	for _, it := range insts {
		byHP[fmt.Sprintf("%s:%d", it.Host, it.Port)] = it.ID
		if it.CloudInstanceID != "" {
			byHP["cloud:"+it.CloudInstanceID] = it.ID
		}
	}
	matched, unmatched := 0, 0
	for i := range cands {
		if cands[i].Host != "" && cands[i].Port > 0 {
			if id := byHP[fmt.Sprintf("%s:%d", cands[i].Host, cands[i].Port)]; id != "" {
				cands[i].MatchedID = id
				matched++
				continue
			}
		}
		if cands[i].ExternalID != "" {
			if id := byHP["cloud:"+cands[i].ExternalID]; id != "" {
				cands[i].MatchedID = id
				matched++
				continue
			}
		}
		unmatched++
	}
	run.Status = "success"
	run.Candidates = len(cands)
	run.Skipped = matched
	run.Added = unmatched
	b, _ := json.Marshal(cands)
	run.LogText = string(b)
	if len(run.LogText) > 200000 {
		run.LogText = run.LogText[:200000] + "...(truncated)"
	}
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.Store.UpdateCmdbSyncRun(run)
	_ = s.Store.AppendCmdbAudit(actor, "sync", "sync_job", job.ID, "sync", nil, map[string]any{
		"candidates": len(cands), "matched": matched, "unmatched": unmatched,
	})
	return run, cands, nil
}

func (s *Service) pullByAdapter(ctx context.Context, name string, cfg json.RawMessage) ([]CandidateCI, error) {
	switch strings.TrimSpace(name) {
	case "prometheus_labels":
		mons, err := s.DiscoverFromMonitor()
		if err != nil {
			return nil, err
		}
		out := make([]CandidateCI, 0, len(mons))
		for _, m := range mons {
			out = append(out, CandidateCI{
				Type: TypeInstance, Name: m.Endpoint, Host: m.Host, Port: m.Port,
				Domain: m.Domain, IDC: m.IDC, MatchedID: m.MatchedID,
				Attrs: map[string]any{"job": m.JobName, "labels": m.Labels},
			})
		}
		return out, nil
	case "cloud_tencent":
		return s.pullCloudTencent()
	case "http_cmdb":
		a := &httpCmdbAdapter{}
		return a.Pull(ctx, cfg)
	default:
		return nil, fmt.Errorf("未知适配器: %s", name)
	}
}

func (s *Service) pullCloudTencent() ([]CandidateCI, error) {
	list, err := s.Store.ListInstances("", "")
	if err != nil {
		return nil, err
	}
	var out []CandidateCI
	for _, it := range list {
		if !strings.EqualFold(it.CloudVendor, "tencent") && !strings.EqualFold(it.CloudVendor, "qcloud") {
			continue
		}
		status := "ok"
		if strings.TrimSpace(it.CloudInstanceID) == "" {
			status = "missing_cloud_instance_id"
		}
		out = append(out, CandidateCI{
			Type: TypeInstance, Name: it.Name, Host: it.Host, Port: it.Port,
			Domain: it.DomainName, DBType: it.DBType, IDC: it.IDC,
			ExternalID: it.CloudInstanceID, MatchedID: it.ID,
			Attrs: map[string]any{
				"cloud_vendor": it.CloudVendor, "cloud_region": it.CloudRegion,
				"validate_status": status,
			},
		})
	}
	if out == nil {
		out = []CandidateCI{}
	}
	return out, nil
}
