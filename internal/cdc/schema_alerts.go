package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

const (
	SchemaAlertOK      = "OK"
	SchemaAlertWarn    = "WARN"
	SchemaAlertError   = "ERROR"
	SchemaAlertSkipped = "SKIPPED"
)

type SchemaAlertItem struct {
	TargetTable          string   `json:"target_table"`
	SourceTables         []string `json:"source_tables,omitempty"`
	MissingColumns       []string `json:"missing_columns"`
	MissingCount         int      `json:"missing_count"`
	ExtraColumnsOnTarget []string `json:"extra_columns_on_target,omitempty"`
	ExtraCount           int      `json:"extra_count"`
	HasDrift             bool     `json:"has_drift"`
	SubsetAligned        bool     `json:"subset_aligned"`
	Error                string   `json:"error,omitempty"`
	Message              string   `json:"message,omitempty"`
}

type SchemaAlertsData struct {
	InitTaskID      string            `json:"init_task_id"`
	CheckedAt       time.Time         `json:"checked_at"`
	State           string            `json:"state"`
	Message         string            `json:"message"`
	TotalTargets    int               `json:"total_targets"`
	DriftTableCount int               `json:"drift_table_count"`
	Items           []SchemaAlertItem `json:"items"`
}

type cachedSchemaAlerts struct {
	at   time.Time
	data *SchemaAlertsData
}

var (
	schemaDriftMu    sync.Mutex
	schemaDriftCache = map[string]*cachedSchemaAlerts{}
)

func SetSchemaDriftCache(initTaskID string, data *SchemaAlertsData) {
	if strings.TrimSpace(initTaskID) == "" || data == nil {
		return
	}
	schemaDriftMu.Lock()
	defer schemaDriftMu.Unlock()
	cp := *data
	cp.Items = append([]SchemaAlertItem(nil), data.Items...)
	schemaDriftCache[initTaskID] = &cachedSchemaAlerts{at: time.Now(), data: &cp}
}

func GetSchemaDriftCache(initTaskID string) *SchemaAlertsData {
	schemaDriftMu.Lock()
	defer schemaDriftMu.Unlock()
	c := schemaDriftCache[initTaskID]
	if c == nil || c.data == nil {
		return nil
	}
	cp := *c.data
	cp.Items = append([]SchemaAlertItem(nil), c.data.Items...)
	return &cp
}

func ListSchemaDriftCache() []*SchemaAlertsData {
	schemaDriftMu.Lock()
	defer schemaDriftMu.Unlock()
	out := make([]*SchemaAlertsData, 0, len(schemaDriftCache))
	for _, c := range schemaDriftCache {
		if c == nil || c.data == nil {
			continue
		}
		cp := *c.data
		cp.Items = append([]SchemaAlertItem(nil), c.data.Items...)
		out = append(out, &cp)
	}
	return out
}

func SummarizeSchemaAlertsState(items []SchemaAlertItem, taskErr error) (state, message string, driftCount int) {
	if taskErr != nil {
		return SchemaAlertError, taskErr.Error(), 0
	}
	for _, it := range items {
		if strings.TrimSpace(it.Error) != "" {
			return SchemaAlertError, "one or more targets failed to check", 0
		}
		if it.HasDrift || it.MissingCount > 0 {
			driftCount++
		}
	}
	if driftCount > 0 {
		return SchemaAlertWarn, fmt.Sprintf("%d 张目标表存在 PG 有而 StarRocks 无的列", driftCount), driftCount
	}
	return SchemaAlertOK, "源端与目标端表结构对齐", 0
}

func ScanSchemaAlerts(ctx context.Context, st *store.DB, secret string, init *store.CDCTask) (*SchemaAlertsData, error) {
	if init == nil {
		return nil, fmt.Errorf("init task nil")
	}
	now := time.Now().UTC()
	out := &SchemaAlertsData{InitTaskID: init.ID, CheckedAt: now, Items: []SchemaAlertItem{}}
	if init.TaskType != "init" && strings.TrimSpace(init.ParentID) != "" {
		out.State = SchemaAlertSkipped
		out.Message = "仅对初始化任务巡检"
		return out, nil
	}
	if init.Status != store.StatusRunning && init.Status != store.StatusPaused {
		out.State = SchemaAlertSkipped
		out.Message = fmt.Sprintf("任务状态=%s，仅巡检 running/paused", init.Status)
		return out, nil
	}
	extra := parseTaskExtra(init.Description)
	if extra["auto_create_table"] == "false" {
		out.State = SchemaAlertSkipped
		out.Message = "手动模式（auto_create_table=false），跳过表结构差异巡检"
		return out, nil
	}
	maps := ParseMappingsJSON(extra["mappings"])
	targets := schemaAlertTargets(maps, extra["tables"])
	out.TotalTargets = len(targets)
	if len(targets) == 0 {
		out.State = SchemaAlertOK
		out.Message = "no table mappings"
		return out, nil
	}
	srcInst, _ := st.GetInstance(init.SourceID)
	if srcInst == nil {
		out.State = SchemaAlertError
		out.Message = "源端实例不存在"
		return out, nil
	}
	pgDB, err := dbmig.OpenInstance(ctx, srcInst, secret, init.SourceDB)
	if err != nil {
		out.State = SchemaAlertError
		out.Message = err.Error()
		return out, nil
	}
	defer pgDB.Close()
	schema := defaultStr(extra["source_schema"], "public")
	var srQuery func(target string) ([]string, error)
	if strings.TrimSpace(init.TargetID) != "" {
		if tgt, _ := st.GetInstance(init.TargetID); tgt != nil {
			if srDB, serr := dbmig.OpenInstance(ctx, tgt, secret, init.TargetDB); serr == nil {
				defer srDB.Close()
				srQuery = func(target string) ([]string, error) {
					return ListSRColumns(ctx, srDB, init.TargetDB, target)
				}
			} else {
				out.State = SchemaAlertError
				out.Message = serr.Error()
				return out, nil
			}
		}
	}
	if srQuery == nil {
		out.State = SchemaAlertError
		out.Message = "目标端未配置"
		return out, nil
	}
	items := make([]SchemaAlertItem, 0, len(targets))
	for _, it := range targets {
		item := SchemaAlertItem{TargetTable: it.target, SourceTables: it.sources}
		pgCols, perr := mergePGColumns(ctx, pgDB, schema, it.sources)
		if perr != nil {
			item.Error = perr.Error()
			items = append(items, item)
			continue
		}
		srNames, serr := srQuery(it.target)
		if serr != nil {
			item.Error = serr.Error()
			items = append(items, item)
			continue
		}
		missing := DiffPGMinusSR(pgCols, srNames)
		for _, c := range missing {
			item.MissingColumns = append(item.MissingColumns, c.Name)
		}
		item.MissingCount = len(item.MissingColumns)
		pgSet := map[string]struct{}{}
		for _, c := range pgCols {
			pgSet[strings.ToLower(c.Name)] = struct{}{}
		}
		for _, n := range srNames {
			if _, ok := pgSet[strings.ToLower(n)]; !ok {
				item.ExtraColumnsOnTarget = append(item.ExtraColumnsOnTarget, n)
			}
		}
		item.ExtraCount = len(item.ExtraColumnsOnTarget)
		item.HasDrift = item.MissingCount > 0
		item.SubsetAligned = !item.HasDrift
		if item.HasDrift {
			item.Message = fmt.Sprintf("StarRocks 缺少 %d 列", item.MissingCount)
		} else {
			item.Message = "源端列已存在于目标端"
		}
		items = append(items, item)
	}
	out.Items = items
	out.State, out.Message, out.DriftTableCount = SummarizeSchemaAlertsState(items, nil)
	return out, nil
}

type schemaAlertTarget struct {
	target  string
	sources []string
}

func schemaAlertTargets(maps []TableMapping, tablesCSV string) []schemaAlertTarget {
	var out []schemaAlertTarget
	for _, m := range maps {
		tgt := strings.TrimSpace(m.TargetTable)
		if tgt == "" {
			continue
		}
		src := cleanStringSlice(m.SourceTables)
		if len(src) == 0 {
			src = []string{tgt}
		}
		out = append(out, schemaAlertTarget{target: tgt, sources: src})
	}
	if len(out) > 0 {
		return out
	}
	for _, t := range splitCSV(tablesCSV) {
		out = append(out, schemaAlertTarget{target: t, sources: []string{t}})
	}
	return out
}

func mergePGColumns(ctx context.Context, db *sql.DB, schema string, tables []string) ([]ColumnInfo, error) {
	byName := map[string]ColumnInfo{}
	var order []string
	for _, t := range tables {
		cols, err := ListPGColumns(ctx, db, schema, t)
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			key := strings.ToLower(c.Name)
			if _, ok := byName[key]; ok {
				continue
			}
			byName[key] = c
			order = append(order, key)
		}
	}
	out := make([]ColumnInfo, 0, len(order))
	for _, k := range order {
		out = append(out, byName[k])
	}
	return out, nil
}
