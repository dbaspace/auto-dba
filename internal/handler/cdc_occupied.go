package handler

import (
	"context"
	"encoding/json"
	"strings"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

// cdcOccupancyReleased 对齐 Hub：驳回/取消/删除/软删的子任务不再占用源表。
func cdcOccupancyReleased(t *store.CDCTask) bool {
	if t == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(t.Status)) {
	case store.StatusRejected, store.StatusCanceled, store.StatusDeleted, "soft_deleted", "superseded":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(t.ApprovalStatus)) {
	case store.StatusRejected, store.StatusCanceled:
		return true
	}
	return false
}

func sourceTablesFromCDCDesc(desc string) []string {
	extra := cdc.ParseTaskExtra(desc)
	seen := map[string]struct{}{}
	var out []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		k := strings.ToLower(n)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	for _, t := range extraCSVTables(desc) {
		add(t)
	}
	for _, m := range cdc.ParseMappingsJSON(extra["mappings"]) {
		if strings.EqualFold(strings.TrimSpace(m.Status), "deleted") {
			continue
		}
		for _, s := range m.SourceTables {
			add(s)
		}
	}
	return out
}

// listOccupiedSourceTables 对齐 Hub ListSyncedSourceTables：
// 父 init 已同步表 + 仍有效（含 pending/error）的 add_table 子任务表。
func (h *CDCHandler) listOccupiedSourceTables(initTask *store.CDCTask) []string {
	if initTask == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		k := strings.ToLower(n)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	for _, t := range sourceTablesFromCDCDesc(initTask.Description) {
		add(t)
	}
	if h == nil || h.Store == nil {
		return out
	}
	children, err := h.Store.ListCDCByParentID(initTask.ID, "add_table")
	if err != nil {
		return out
	}
	for i := range children {
		ch := &children[i]
		if cdcOccupancyReleased(ch) {
			continue
		}
		for _, t := range sourceTablesFromCDCDesc(ch.Description) {
			add(t)
		}
	}
	return out
}

type cdcOccupancyEntry struct {
	Table      string `json:"table"`
	ByTaskID   string `json:"by_task_id,omitempty"`
	ByTaskName string `json:"by_task_name,omitempty"`
	Status     string `json:"status,omitempty"`
}

// cdcOccupancy 核对源库 publication 后的占用口径。
// Tables：仍占用的表；InFlight：进行中子任务占用；Stale：平台记录已加、publication 缺失（不占用，可重新加）。
type cdcOccupancy struct {
	Tables      []string
	InFlight    []cdcOccupancyEntry
	Stale       []cdcOccupancyEntry
	LiveChecked bool
}

// cdcChildInFlight 子任务尚未走完编排（待审批/审批中/编排中），其表始终占用，避免重复提交。
func cdcChildInFlight(t *store.CDCTask) bool {
	if strings.EqualFold(strings.TrimSpace(t.ApprovalStatus), store.StatusPending) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(t.Status)) {
	case "", store.StatusPending, store.StatusApproved, store.StatusScheduled, store.StatusPreChecking,
		store.StatusInit, store.StatusReinitializing:
		return true
	case store.StatusRunning:
		return cdc.ParseTaskExtra(t.Description)["pipeline_status"] != "running"
	}
	return false
}

func bareTableKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndex(n, "."); i >= 0 {
		n = n[i+1:]
	}
	return n
}

// computeCDCOccupancy liveOK=false 时与 listOccupiedSourceTables 一致。
func computeCDCOccupancy(initTask *store.CDCTask, children []store.CDCTask, live map[string]struct{}, liveOK bool) cdcOccupancy {
	out := cdcOccupancy{LiveChecked: liveOK}
	if initTask == nil {
		return out
	}
	seen := map[string]struct{}{}
	add := func(n string) {
		k := strings.ToLower(strings.TrimSpace(n))
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out.Tables = append(out.Tables, strings.TrimSpace(n))
	}
	inLive := func(n string) bool {
		if !liveOK {
			return true
		}
		_, ok := live[bareTableKey(n)]
		return ok
	}
	for _, t := range sourceTablesFromCDCDesc(initTask.Description) {
		if inLive(t) {
			add(t)
		} else {
			out.Stale = append(out.Stale, cdcOccupancyEntry{Table: t, ByTaskID: initTask.ID, ByTaskName: initTask.Name, Status: initTask.Status})
		}
	}
	for i := range children {
		ch := &children[i]
		if cdcOccupancyReleased(ch) {
			continue
		}
		inflight := cdcChildInFlight(ch)
		for _, t := range sourceTablesFromCDCDesc(ch.Description) {
			entry := cdcOccupancyEntry{Table: t, ByTaskID: ch.ID, ByTaskName: ch.Name, Status: ch.Status}
			switch {
			case inflight:
				add(t)
				out.InFlight = append(out.InFlight, entry)
			case inLive(t):
				add(t)
			default:
				out.Stale = append(out.Stale, entry)
			}
		}
	}
	// 同一张表若已被其它记录确认占用，不再算作 stale
	stale := out.Stale[:0]
	for _, e := range out.Stale {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(e.Table))]; !ok {
			stale = append(stale, e)
		}
	}
	out.Stale = stale
	return out
}

// effectiveOccupancy 占用核对源库 publication；连不上或无 publication 时回退原口径。
func (h *CDCHandler) effectiveOccupancy(ctx context.Context, initTask *store.CDCTask) cdcOccupancy {
	if initTask == nil {
		return cdcOccupancy{}
	}
	if h == nil || h.Store == nil {
		return computeCDCOccupancy(initTask, nil, nil, false)
	}
	inst, _ := h.Store.GetInstance(initTask.SourceID)
	meta := h.loadSourceMetaLocal(ctx, inst, initTask)
	return h.occupancyFromMeta(initTask, meta)
}

func (h *CDCHandler) effectiveOccupiedSourceTables(ctx context.Context, initTask *store.CDCTask) []string {
	return h.effectiveOccupancy(ctx, initTask).Tables
}

// ctxCDCAddTablesPrefiltered 批量加表已按父任务算好待加表，prepareCDCCreate 不再重复核对占用。
const ctxCDCAddTablesPrefiltered = "cdc_add_tables_prefiltered"

type cdcParentTableState struct {
	Occupied []string
	// Existing 仅单库父任务可核对（分库模式只能连代表库，不据此跳过）
	Existing      []string
	ExistsChecked bool
}

// parentTableStates 并发计算各父任务的占用与源库现有表；parents 须已按租户解析。异地走 sql-agent。
func (h *CDCHandler) parentTableStates(ctx context.Context, parents []*store.CDCTask) []cdcParentTableState {
	out := make([]cdcParentTableState, len(parents))
	jobs := make([]cdcMetaJob, len(parents))
	for i, p := range parents {
		if p == nil || h == nil || h.Store == nil {
			continue
		}
		inst, _ := h.Store.GetInstance(p.SourceID)
		jobs[i] = cdcMetaJob{Init: p, Inst: inst}
	}
	metas := h.loadSourceMetaForParents(ctx, jobs)
	for i, p := range parents {
		if p == nil {
			continue
		}
		meta := metas[i]
		occ := h.occupancyFromMeta(p, meta)
		out[i].Occupied = occ.Tables
		if meta.Error != "" || cdc.ShardModeActive(cdc.ParseTaskExtra(p.Description)) {
			continue
		}
		out[i].Existing = meta.Tables
		out[i].ExistsChecked = true
	}
	return out
}

// splitAddTables 返回待加、已占用、该库不存在三组表。
func (st cdcParentTableState) splitAddTables(want []string) (kept, occupied, missing []string) {
	kept = subtractFold(want, st.Occupied)
	occupied = subtractFold(want, kept)
	if st.ExistsChecked {
		have := map[string]struct{}{}
		for _, n := range st.Existing {
			have[bareTableKey(n)] = struct{}{}
		}
		var next []string
		for _, t := range kept {
			if _, ok := have[bareTableKey(t)]; ok {
				next = append(next, t)
			} else {
				missing = append(missing, t)
			}
		}
		kept = next
	}
	return kept, occupied, missing
}

func rewriteCDCTaskTables(desc string, keep []string) string {
	text, extra := splitCDCDescMap(desc)
	if extra == nil {
		extra = map[string]any{}
	}
	keepSet := map[string]struct{}{}
	for _, t := range keep {
		keepSet[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	extra["tables"] = keep
	if raw, ok := extra["mappings"]; ok {
		var maps []cdc.TableMapping
		switch v := raw.(type) {
		case string:
			maps = cdc.ParseMappingsJSON(v)
		case []byte:
			maps = cdc.ParseMappingsJSON(string(v))
		default:
			b, _ := json.Marshal(v)
			maps = cdc.ParseMappingsJSON(string(b))
		}
		if len(maps) > 0 {
			var next []cdc.TableMapping
			for _, m := range maps {
				var src []string
				for _, s := range m.SourceTables {
					if _, ok := keepSet[strings.ToLower(strings.TrimSpace(s))]; ok {
						src = append(src, s)
					}
				}
				if len(src) == 0 {
					continue
				}
				m.SourceTables = src
				next = append(next, m)
			}
			extra["mappings"] = next
		}
	}
	return encodeCDCExtraJSON(text, extra)
}

// overlayParentPipelineExtra 用父任务链路身份覆盖子任务 description，
// 避免批量加表时表单里第一个父任务的 publication/slot/connector 串到其它分库子任务。
// 同时写入父任务生效源端（source_host / Debezium hostname），不把实例默认 host 写进子任务。
func overlayParentPipelineExtra(childDesc, parentDesc string) string {
	text, extra := splitCDCDescMap(childDesc)
	if extra == nil {
		extra = map[string]any{}
	}
	pe := cdc.ParseTaskExtra(parentDesc)
	for _, k := range []string{
		"publication_name", "slot_name", "connector_name", "topic_prefix",
		"connector_id", "connect_base_url", "env", "source_schema", "kafka_bootstrap_servers",
	} {
		if v := strings.TrimSpace(pe[k]); v != "" {
			extra[k] = v
		}
	}
	// nil 实例：只取父任务 source_host 或 debezium database.hostname，不回落到实例 host。
	if host, port := cdc.EffectiveSourceEndpoint(pe, nil); host != "" {
		extra["source_host"] = host
		if port > 0 {
			extra["source_port"] = port
		}
	}
	return encodeCDCExtraJSON(text, extra)
}
