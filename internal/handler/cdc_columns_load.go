package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
)

const cdcColumnCacheTTL = 2 * time.Minute

type cdcColumnCacheKey struct {
	inst, db, schema, table string
}

type cdcColumnCacheEntry struct {
	cols []cdc.ColumnInfo
	at   time.Time
}

var cdcColumnCache = struct {
	mu sync.Mutex
	m  map[cdcColumnCacheKey]cdcColumnCacheEntry
}{m: map[cdcColumnCacheKey]cdcColumnCacheEntry{}}

func cdcColumnCacheGet(inst, dbName, schema, table string) ([]cdc.ColumnInfo, bool) {
	key := cdcColumnCacheKey{inst: inst, db: strings.ToLower(dbName), schema: schema, table: table}
	cdcColumnCache.mu.Lock()
	defer cdcColumnCache.mu.Unlock()
	ent, ok := cdcColumnCache.m[key]
	if !ok || time.Since(ent.at) > cdcColumnCacheTTL {
		delete(cdcColumnCache.m, key)
		return nil, false
	}
	return cloneCDCColumns(ent.cols), true
}

func cdcColumnCachePut(inst, dbName, schema, table string, cols []cdc.ColumnInfo) {
	key := cdcColumnCacheKey{inst: inst, db: strings.ToLower(dbName), schema: schema, table: table}
	cdcColumnCache.mu.Lock()
	defer cdcColumnCache.mu.Unlock()
	if len(cdcColumnCache.m) > 4000 {
		cdcColumnCache.m = map[cdcColumnCacheKey]cdcColumnCacheEntry{}
	}
	cdcColumnCache.m[key] = cdcColumnCacheEntry{cols: cloneCDCColumns(cols), at: time.Now()}
}

func cloneCDCColumns(in []cdc.ColumnInfo) []cdc.ColumnInfo {
	if len(in) == 0 {
		return []cdc.ColumnInfo{}
	}
	out := make([]cdc.ColumnInfo, len(in))
	copy(out, in)
	return out
}

// loadCDCColumns 先读 2 分钟缓存；未命中时异地优先走 sql-agent /v1/sql/meta 的 column_tables。
// 旧执行器不回 columns_ready 时回退平台直连。
func (h *CDCHandler) loadCDCColumns(ctx context.Context, inst *store.Instance, dbName, schema string, tables []string) (map[string][]cdc.ColumnInfo, error) {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "public"
	}
	dbName = strings.TrimSpace(dbName)
	out := map[string][]cdc.ColumnInfo{}
	var miss []string
	instID := ""
	if inst != nil {
		instID = inst.ID
	}
	seen := map[string]struct{}{}
	for _, raw := range tables {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		if cols, ok := cdcColumnCacheGet(instID, dbName, schema, raw); ok {
			out[raw] = cols
			continue
		}
		miss = append(miss, raw)
	}
	if len(miss) == 0 {
		return out, nil
	}
	loaded, err := h.fetchCDCColumns(ctx, inst, dbName, schema, miss)
	if err != nil {
		return nil, err
	}
	for _, name := range miss {
		cols := loaded[name]
		if cols == nil {
			cols = []cdc.ColumnInfo{}
		}
		cdcColumnCachePut(instID, dbName, schema, name, cols)
		out[name] = cols
	}
	return out, nil
}

func (h *CDCHandler) fetchCDCColumns(ctx context.Context, inst *store.Instance, dbName, schema string, tables []string) (map[string][]cdc.ColumnInfo, error) {
	if inst != nil {
		localCodes, execs, _ := h.cdcExecutionCatalog()
		dec := sqlexec.Decide(inst.IDC, localCodes, execs)
		if dec.Mode == sqlexec.ModeRemote && dec.Executor != nil {
			if got, ok, err := h.fetchCDCColumnsRemote(ctx, dec.Executor, inst, dbName, schema, tables); ok {
				return got, err
			}
		}
	}
	return h.fetchCDCColumnsDirect(ctx, inst, dbName, schema, tables)
}

func (h *CDCHandler) fetchCDCColumnsRemote(ctx context.Context, exec *sqlexec.Executor, inst *store.Instance, dbName, schema string, tables []string) (map[string][]cdc.ColumnInfo, bool, error) {
	if exec == nil || inst == nil || h == nil {
		return nil, false, nil
	}
	plain, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		return nil, false, nil
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	req := sqlexec.MetaRequest{
		Target: sqlexec.Target{
			InstanceID: inst.ID, InstanceName: inst.Name, Host: host, Port: inst.Port,
			Username: inst.Username, Password: plain, DBType: inst.DBType, Env: inst.IDC,
		},
		Databases: []sqlexec.MetaDB{{
			DBName: dbName, Schema: schema, ColumnTables: tables,
		}},
	}
	resp, err := sqlexec.FetchMeta(ctx, exec.BaseURL, exec.Username, exec.Password, req)
	if err != nil {
		return nil, false, nil
	}
	if len(resp.Results) == 0 || !resp.Results[0].ColumnsReady {
		return nil, false, nil
	}
	r := resp.Results[0]
	if r.Error != "" {
		return nil, true, fmt.Errorf("%s", r.Error)
	}
	out := map[string][]cdc.ColumnInfo{}
	for _, item := range r.ColumnTables {
		cols := make([]cdc.ColumnInfo, 0, len(item.Columns))
		for _, c := range item.Columns {
			nullable := c.IsNullable
			cols = append(cols, cdc.ColumnInfo{
				Name: c.Name, DataType: c.DataType, IsNullable: nullable, IsPrimaryKey: c.IsPrimaryKey, Comment: c.Comment,
			})
		}
		out[item.Table] = cols
	}
	for _, name := range tables {
		if _, ok := out[name]; !ok {
			out[name] = []cdc.ColumnInfo{}
		}
	}
	return out, true, nil
}

func (h *CDCHandler) fetchCDCColumnsDirect(ctx context.Context, inst *store.Instance, dbName, schema string, tables []string) (map[string][]cdc.ColumnInfo, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	sqlDB, err := dbmig.OpenInstance(ctx, inst, h.Secret, dbName)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	return cdc.ListPGColumnsMany(ctx, sqlDB, schema, tables)
}
