package handler

import (
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
