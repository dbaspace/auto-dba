package cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/store"
)

const (
	PresenceNotFound    = "NOT_FOUND"
	PresencePresent     = "PRESENT"
	PresenceUnreachable = "UNREACHABLE"
	PresenceUnknown     = "UNKNOWN"

	connectorListCacheTTL = 20 * time.Second
	connectorListTimeout  = 3 * time.Second
)

type connectorListEntry struct {
	names map[string]bool
	err   error
	at    time.Time
}

var (
	connectorListMu    sync.Mutex
	connectorListCache = map[string]connectorListEntry{}
)

// ConnectorPresence 任务全部 Connector 在 Connect 上的存在性。
type ConnectorPresence struct {
	State   string   `json:"state"`
	Base    string   `json:"connect_base_url"`
	Missing []string `json:"missing,omitempty"`
	Present []string `json:"present,omitempty"`
	Message string   `json:"message,omitempty"`
}

// ListConnectConnectors GET /connectors 列出 Connect 上全部 Connector 名称（按地址短时缓存）。
func ListConnectConnectors(ctx context.Context, base string) (map[string]bool, error) {
	base = NormalizeConnectBaseURL(base)
	if base == "" {
		return nil, fmt.Errorf("未解析到 Debezium Connect 地址")
	}
	connectorListMu.Lock()
	if e, ok := connectorListCache[base]; ok && time.Since(e.at) < connectorListCacheTTL {
		connectorListMu.Unlock()
		return e.names, e.err
	}
	connectorListMu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, connectorListTimeout)
	defer cancel()
	names, err := fetchConnectConnectors(cctx, base)
	connectorListMu.Lock()
	connectorListCache[base] = connectorListEntry{names: names, err: err, at: time.Now()}
	connectorListMu.Unlock()
	return names, err
}

// InvalidateConnectorList 创建/删除 Connector 后清掉该地址缓存。
func InvalidateConnectorList(base string) {
	base = NormalizeConnectBaseURL(base)
	connectorListMu.Lock()
	delete(connectorListCache, base)
	connectorListMu.Unlock()
}

func fetchConnectConnectors(ctx context.Context, base string) (map[string]bool, error) {
	raw, code, err := connectDo(ctx, base, http.MethodGet, "/connectors", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("GET %s/connectors -> %d %s", base, code, strings.TrimSpace(string(raw)))
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("解析 Connect /connectors 响应失败: %w", err)
	}
	out := make(map[string]bool, len(arr))
	for _, n := range arr {
		out[strings.TrimSpace(n)] = true
	}
	return out, nil
}

// TaskConnectorPresence 任务任一 Connector 在 Connect 上不存在即 NOT_FOUND；Connect 不可达为 UNREACHABLE。
func (o *Orchestrator) TaskConnectorPresence(ctx context.Context, t *store.CDCTask) ConnectorPresence {
	if o == nil || t == nil {
		return ConnectorPresence{State: PresenceUnknown}
	}
	return o.presenceForExtra(ctx, t, parseTaskExtra(t.Description))
}

func (o *Orchestrator) presenceForExtra(ctx context.Context, t *store.CDCTask, extra map[string]string) ConnectorPresence {
	names := o.taskConnectorNames(t, extra)
	base := o.ResolveConnectBase(extra)
	out := ConnectorPresence{Base: base}
	if len(names) == 0 {
		out.State = PresenceUnknown
		out.Message = "任务未登记 Connector 名称"
		return out
	}
	if base == "" {
		out.State = PresenceUnknown
		out.Message = "未解析到 Debezium Connect 地址"
		return out
	}
	existing, err := ListConnectConnectors(ctx, base)
	if err != nil {
		out.State = PresenceUnreachable
		out.Message = err.Error()
		return out
	}
	for _, n := range names {
		if existing[n] {
			out.Present = append(out.Present, n)
		} else {
			out.Missing = append(out.Missing, n)
		}
	}
	if len(out.Missing) > 0 {
		out.State = PresenceNotFound
	} else {
		out.State = PresencePresent
	}
	return out
}

// ReinitEligibleStatus init 主任务、非审批中、状态 running / paused / failed。
func ReinitEligibleStatus(t *store.CDCTask) (bool, string) {
	if t == nil {
		return false, "任务不存在"
	}
	if strings.TrimSpace(t.TaskType) != "init" || strings.TrimSpace(t.ParentID) != "" {
		return false, "仅 init 主任务支持重新初始化"
	}
	if t.ApprovalStatus == store.StatusPending || t.Status == store.StatusPending {
		return false, "任务审批中，不能重新初始化"
	}
	extra := parseTaskExtra(t.Description)
	if extra["deleted"] == "true" || extra["pipeline_status"] == "deleted" || t.Status == store.StatusDeleted {
		return false, "任务已删除"
	}
	switch t.Status {
	case store.StatusRunning, store.StatusPaused, store.StatusFailed:
		return true, ""
	case store.StatusReinitializing:
		return false, "任务正在重新初始化"
	default:
		return false, "仅 running / paused / failed 状态可重新初始化，当前状态=" + t.Status
	}
}

// CanReinit 状态可重新初始化，且 Debezium Connector 在 Connect 上为 NOT_FOUND。
func CanReinit(t *store.CDCTask, p ConnectorPresence) bool {
	ok, _ := ReinitEligibleStatus(t)
	return ok && p.State == PresenceNotFound
}

// TaskConnectorNames 任务全部 Connector 名（分库每库一个）。
func (o *Orchestrator) TaskConnectorNames(t *store.CDCTask) []string {
	if t == nil {
		return nil
	}
	return o.taskConnectorNames(t, parseTaskExtra(t.Description))
}

// ReinitGate 执行前校验：状态允许，且 Connector 为 NOT_FOUND（换绑时按新服务探测）；error_step=reinit 的重试不校验存在性。
func (o *Orchestrator) ReinitGate(ctx context.Context, t *store.CDCTask, connectorID string) error {
	if ok, msg := ReinitEligibleStatus(t); !ok {
		return fmt.Errorf("%s", msg)
	}
	extra := parseTaskExtra(t.Description)
	if extra["error_step"] == "reinit" {
		return nil
	}
	c, err := o.validateRebind(extra, connectorID)
	if err != nil {
		return err
	}
	if c != nil {
		applyRebind(extra, c)
	}
	p := o.presenceForExtra(ctx, t, extra)
	switch p.State {
	case PresenceNotFound:
		return nil
	case PresencePresent:
		return fmt.Errorf("Connector 在 Debezium Connect 上仍存在，无需重新初始化")
	case PresenceUnreachable:
		return fmt.Errorf("Debezium Connect 不可达（%s），无法确认 Connector 缺失", p.Message)
	default:
		return fmt.Errorf("无法确认 Connector 缺失：%s", p.Message)
	}
}

// AnnotateReinit 为列表中的 init 主任务填充 can_reinit / connector_missing：按 Connect 地址分组，每组一次 GET /connectors 并发探测。
func (o *Orchestrator) AnnotateReinit(ctx context.Context, list []store.CDCTask) {
	type ref struct {
		idx   int
		names []string
	}
	groups := map[string][]ref{}
	for i := range list {
		t := &list[i]
		if ok, _ := ReinitEligibleStatus(t); !ok {
			continue
		}
		extra := parseTaskExtra(t.Description)
		base := o.ResolveConnectBase(extra)
		names := o.taskConnectorNames(t, extra)
		if base == "" || len(names) == 0 {
			continue
		}
		groups[base] = append(groups[base], ref{idx: i, names: names})
	}
	if len(groups) == 0 {
		return
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[string]map[string]bool{}
	for base := range groups {
		wg.Add(1)
		go func(base string) {
			defer wg.Done()
			names, err := ListConnectConnectors(ctx, base)
			if err != nil {
				return
			}
			mu.Lock()
			results[base] = names
			mu.Unlock()
		}(base)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	mu.Lock()
	defer mu.Unlock()
	for base, refs := range groups {
		existing, ok := results[base]
		if !ok {
			continue
		}
		for _, r := range refs {
			for _, n := range r.names {
				if !existing[n] {
					list[r.idx].ConnectorMissing = true
					list[r.idx].CanReinit = true
					break
				}
			}
		}
	}
}
