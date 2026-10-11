package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CMDB 关系 / 标签 / 审计 / 同步任务表。

type CmdbRelation struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id,omitempty"`
	SrcType   string `json:"src_type"`
	SrcID     string `json:"src_id"`
	RelType   string `json:"rel_type"`
	DstType   string `json:"dst_type"`
	DstID     string `json:"dst_id"`
	Source    string `json:"source"` // manual | sync
	Remark    string `json:"remark,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CmdbTagDef struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Values      []string `json:"values"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
	CITypes     []string `json:"ci_types"` // 适用类型；空=全部
	Scope       string   `json:"scope"`    // general | monitor
	SortOrder   int      `json:"sort_order"`
	TenantID    string   `json:"tenant_id,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type CmdbTagBinding struct {
	ID        string `json:"id"`
	CIType    string `json:"ci_type"`
	CIID      string `json:"ci_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CmdbAuditLog struct {
	ID         string          `json:"id"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"` // create | update | delete | import | sync
	CIType     string          `json:"ci_type"`
	CIID       string          `json:"ci_id"`
	BeforeJSON json.RawMessage `json:"before_json,omitempty"`
	AfterJSON  json.RawMessage `json:"after_json,omitempty"`
	Source     string          `json:"source"` // api | import | sync
	CreatedAt  string          `json:"created_at"`
}

type CmdbSyncJob struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Adapter    string          `json:"adapter"` // prometheus_labels | cloud_tencent | http_cmdb
	Cron       string          `json:"cron,omitempty"`
	ConfigJSON json.RawMessage `json:"config_json,omitempty"`
	Enabled    bool            `json:"enabled"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

type CmdbSyncRun struct {
	ID         string `json:"id"`
	JobID      string `json:"job_id"`
	Status     string `json:"status"` // running | success | failed
	Added      int    `json:"added"`
	Updated    int    `json:"updated"`
	Skipped    int    `json:"skipped"`
	Candidates int    `json:"candidates"`
	LogText    string `json:"log_text,omitempty"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

func (d *DB) ensureCmdbTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_relations (
			id TEXT PRIMARY KEY,
			src_type TEXT NOT NULL,
			src_id TEXT NOT NULL,
			rel_type TEXT NOT NULL,
			dst_type TEXT NOT NULL,
			dst_id TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'manual',
			remark TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cmdb_rel_uniq ON tbl_cmdb_relations(src_type, src_id, rel_type, dst_type, dst_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_tag_defs (
			id TEXT PRIMARY KEY,
			key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL DEFAULT '',
			values_json TEXT NOT NULL DEFAULT '[]',
			description TEXT NOT NULL DEFAULT '',
			required INTEGER NOT NULL DEFAULT 0,
			ci_types_json TEXT NOT NULL DEFAULT '[]',
			scope TEXT NOT NULL DEFAULT 'general',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_tag_bindings (
			id TEXT PRIMARY KEY,
			ci_type TEXT NOT NULL,
			ci_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cmdb_bind_uniq ON tbl_cmdb_tag_bindings(ci_type, ci_id, key)`,
		`CREATE INDEX IF NOT EXISTS idx_cmdb_bind_ci ON tbl_cmdb_tag_bindings(ci_type, ci_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_audit_logs (
			id TEXT PRIMARY KEY,
			actor TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			ci_type TEXT NOT NULL,
			ci_id TEXT NOT NULL,
			before_json TEXT NOT NULL DEFAULT '',
			after_json TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'api',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cmdb_audit_ci ON tbl_cmdb_audit_logs(ci_type, ci_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cmdb_audit_time ON tbl_cmdb_audit_logs(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_sync_jobs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			adapter TEXT NOT NULL,
			cron TEXT NOT NULL DEFAULT '',
			config_json TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_sync_runs (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			status TEXT NOT NULL,
			added INTEGER NOT NULL DEFAULT 0,
			updated INTEGER NOT NULL DEFAULT 0,
			skipped INTEGER NOT NULL DEFAULT 0,
			candidates INTEGER NOT NULL DEFAULT 0,
			log_text TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cmdb_sync_runs_job ON tbl_cmdb_sync_runs(job_id, started_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	if err := d.ensureCmdbSnapshotTable(); err != nil {
		return err
	}
	return nil
}

func (d *DB) AppendCmdbAudit(actor, action, ciType, ciID, source string, before, after any) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var beforeB, afterB []byte
	if before != nil {
		beforeB, _ = json.Marshal(before)
	}
	if after != nil {
		afterB, _ = json.Marshal(after)
	}
	_, err := d.Exec(
		`INSERT INTO tbl_cmdb_audit_logs(id, actor, action, ci_type, ci_id, before_json, after_json, source, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), strings.TrimSpace(actor), strings.TrimSpace(action),
		strings.TrimSpace(ciType), strings.TrimSpace(ciID),
		string(beforeB), string(afterB), strings.TrimSpace(source), now,
	)
	return err
}

func (d *DB) ListCmdbAuditLogs(ciType, ciID, actor, q string, limit, offset int) ([]CmdbAuditLog, error) {
	return d.ListCmdbAuditLogsInTenant("", ciType, ciID, actor, q, limit, offset)
}

func (d *DB) ListCmdbAuditLogsInTenant(tenantID, ciType, ciID, actor, q string, limit, offset int) ([]CmdbAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"1=1"}
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		where = append(where, "COALESCE(tenant_id,'')=?")
		args = append(args, tid)
	}
	if t := strings.TrimSpace(ciType); t != "" {
		where = append(where, "ci_type=?")
		args = append(args, t)
	}
	if id := strings.TrimSpace(ciID); id != "" {
		where = append(where, "ci_id=?")
		args = append(args, id)
	}
	if a := strings.TrimSpace(actor); a != "" {
		where = append(where, "actor=?")
		args = append(args, a)
	}
	if qq := strings.TrimSpace(q); qq != "" {
		where = append(where, "(ci_id LIKE ? OR action LIKE ? OR before_json LIKE ? OR after_json LIKE ?)")
		like := "%" + qq + "%"
		args = append(args, like, like, like, like)
	}
	args = append(args, limit, offset)
	rows, err := d.Query(`SELECT id, actor, action, ci_type, ci_id, before_json, after_json, source, created_at
		FROM tbl_cmdb_audit_logs WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbAuditLog
	for rows.Next() {
		var r CmdbAuditLog
		var beforeS, afterS string
		if err := rows.Scan(&r.ID, &r.Actor, &r.Action, &r.CIType, &r.CIID, &beforeS, &afterS, &r.Source, &r.CreatedAt); err != nil {
			return nil, err
		}
		if beforeS != "" {
			r.BeforeJSON = json.RawMessage(beforeS)
		}
		if afterS != "" {
			r.AfterJSON = json.RawMessage(afterS)
		}
		out = append(out, r)
	}
	if out == nil {
		out = []CmdbAuditLog{}
	}
	return out, rows.Err()
}

func (d *DB) ListCmdbRelations(srcType, srcID, dstType, dstID string) ([]CmdbRelation, error) {
	return d.ListCmdbRelationsInTenant("", srcType, srcID, dstType, dstID)
}

func (d *DB) ListCmdbRelationsInTenant(tenantID, srcType, srcID, dstType, dstID string) ([]CmdbRelation, error) {
	where := []string{"1=1"}
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		where = append(where, "COALESCE(tenant_id,'')=?")
		args = append(args, tid)
	}
	if t := strings.TrimSpace(srcType); t != "" {
		where = append(where, "src_type=?")
		args = append(args, t)
	}
	if id := strings.TrimSpace(srcID); id != "" {
		where = append(where, "src_id=?")
		args = append(args, id)
	}
	if t := strings.TrimSpace(dstType); t != "" {
		where = append(where, "dst_type=?")
		args = append(args, t)
	}
	if id := strings.TrimSpace(dstID); id != "" {
		where = append(where, "dst_id=?")
		args = append(args, id)
	}
	rows, err := d.Query(`SELECT id, src_type, src_id, rel_type, dst_type, dst_id, source, remark, created_at, updated_at
		FROM tbl_cmdb_relations WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbRelation
	for rows.Next() {
		var r CmdbRelation
		if err := rows.Scan(&r.ID, &r.SrcType, &r.SrcID, &r.RelType, &r.DstType, &r.DstID, &r.Source, &r.Remark, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []CmdbRelation{}
	}
	return out, rows.Err()
}

func (d *DB) CreateCmdbRelation(r *CmdbRelation) error {
	if r == nil {
		return errors.New("nil relation")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Source == "" {
		r.Source = "manual"
	}
	r.CreatedAt, r.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_cmdb_relations(id, src_type, src_id, rel_type, dst_type, dst_id, source, remark, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.SrcType, r.SrcID, r.RelType, r.DstType, r.DstID, r.Source, r.Remark, r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if tid := strings.TrimSpace(r.TenantID); tid != "" {
		return d.AssignTenantID("tbl_cmdb_relations", r.ID, tid)
	}
	return nil
}

func (d *DB) DeleteCmdbRelation(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_cmdb_relations WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) DeleteCmdbRelationInTenant(id, tenantID string) error {
	ok, err := d.rowVisibleInTenant("tbl_cmdb_relations", id, tenantID)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}
	return d.DeleteCmdbRelation(id)
}

func (d *DB) ListCmdbTagDefs(scope string) ([]CmdbTagDef, error) {
	return d.ListCmdbTagDefsInTenant("", scope)
}

func (d *DB) ListCmdbTagDefsInTenant(tenantID, scope string) ([]CmdbTagDef, error) {
	q := `SELECT id, key, name, values_json, description, required, ci_types_json, scope, sort_order, created_at, updated_at FROM tbl_cmdb_tag_defs WHERE 1=1`
	args := []any{}
	if s := strings.TrimSpace(scope); s != "" {
		q += ` AND scope=?`
		args = append(args, s)
	}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY sort_order ASC, key ASC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbTagDef
	for rows.Next() {
		var r CmdbTagDef
		var vals, types string
		var req int
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &vals, &r.Description, &req, &types, &r.Scope, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Required = req == 1
		r.Values = decodeStringSlice(vals)
		r.CITypes = decodeStringSlice(types)
		out = append(out, r)
	}
	if out == nil {
		out = []CmdbTagDef{}
	}
	return out, rows.Err()
}

func (d *DB) GetCmdbTagDef(id string) (*CmdbTagDef, error) {
	row := d.QueryRow(`SELECT id, key, name, values_json, description, required, ci_types_json, scope, sort_order, created_at, updated_at
		FROM tbl_cmdb_tag_defs WHERE id=?`, strings.TrimSpace(id))
	var r CmdbTagDef
	var vals, types string
	var req int
	err := row.Scan(&r.ID, &r.Key, &r.Name, &vals, &r.Description, &req, &types, &r.Scope, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Required = req == 1
	r.Values = decodeStringSlice(vals)
	r.CITypes = decodeStringSlice(types)
	return &r, nil
}

func (d *DB) UpsertCmdbTagDef(r *CmdbTagDef) error {
	if r == nil {
		return errors.New("nil tag def")
	}
	r.Key = strings.TrimSpace(r.Key)
	if r.Key == "" {
		return errors.New("key 必填")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if r.Scope == "" {
		r.Scope = "general"
	}
	req := 0
	if r.Required {
		req = 1
	}
	tcl, targs := tenantClause(r.TenantID)
	if r.ID != "" {
		res, err := d.Exec(
			`UPDATE tbl_cmdb_tag_defs SET key=?, name=?, values_json=?, description=?, required=?, ci_types_json=?, scope=?, sort_order=?, updated_at=? WHERE id=?`+tcl,
			append([]any{r.Key, r.Name, encodeJSON(r.Values), r.Description, req, encodeJSON(r.CITypes), r.Scope, r.SortOrder, now, r.ID}, targs...)...,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			r.UpdatedAt = now
			return nil
		}
	}
	// 按 key 更新
	res, err := d.Exec(
		`UPDATE tbl_cmdb_tag_defs SET name=?, values_json=?, description=?, required=?, ci_types_json=?, scope=?, sort_order=?, updated_at=? WHERE key=?`+tcl,
		append([]any{r.Name, encodeJSON(r.Values), r.Description, req, encodeJSON(r.CITypes), r.Scope, r.SortOrder, now, r.Key}, targs...)...,
	)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			r.UpdatedAt = now
			return nil
		}
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	// tenant_id 必须在 INSERT 时写入：列默认是 default 租户，后置 Assign 会撞 (tenant_id,key) 唯一约束。
	tid := strings.TrimSpace(r.TenantID)
	if tid == "" {
		tid = DefaultTenantID
		r.TenantID = tid
	}
	_, err = d.Exec(
		`INSERT INTO tbl_cmdb_tag_defs(id, key, name, values_json, description, required, ci_types_json, scope, sort_order, created_at, updated_at, tenant_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Key, r.Name, encodeJSON(r.Values), r.Description, req, encodeJSON(r.CITypes), r.Scope, r.SortOrder, r.CreatedAt, r.UpdatedAt, tid,
	)
	return err
}

func (d *DB) DeleteCmdbTagDef(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_cmdb_tag_defs WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) ListCmdbTagBindings(ciType, ciID string) ([]CmdbTagBinding, error) {
	rows, err := d.Query(
		`SELECT id, ci_type, ci_id, key, value, created_at, updated_at FROM tbl_cmdb_tag_bindings WHERE ci_type=? AND ci_id=? ORDER BY key`,
		strings.TrimSpace(ciType), strings.TrimSpace(ciID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbTagBinding
	for rows.Next() {
		var r CmdbTagBinding
		if err := rows.Scan(&r.ID, &r.CIType, &r.CIID, &r.Key, &r.Value, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []CmdbTagBinding{}
	}
	return out, rows.Err()
}

func (d *DB) ReplaceCmdbTagBindings(ciType, ciID string, pairs map[string]string) error {
	ciType, ciID = strings.TrimSpace(ciType), strings.TrimSpace(ciID)
	if ciType == "" || ciID == "" {
		return errors.New("ci_type/ci_id 必填")
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_cmdb_tag_bindings WHERE ci_type=? AND ci_id=?`), ciType, ciID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for k, v := range pairs {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if _, err := tx.Exec(
			d.rewrite(`INSERT INTO tbl_cmdb_tag_bindings(id, ci_type, ci_id, key, value, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`),
			uuid.NewString(), ciType, ciID, k, v, now, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) ListCmdbSyncJobs() ([]CmdbSyncJob, error) {
	rows, err := d.Query(`SELECT id, name, adapter, cron, config_json, enabled, created_at, updated_at FROM tbl_cmdb_sync_jobs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbSyncJob
	for rows.Next() {
		var r CmdbSyncJob
		var en int
		var cfg string
		if err := rows.Scan(&r.ID, &r.Name, &r.Adapter, &r.Cron, &cfg, &en, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = en == 1
		if cfg != "" {
			r.ConfigJSON = json.RawMessage(cfg)
		}
		out = append(out, r)
	}
	if out == nil {
		out = []CmdbSyncJob{}
	}
	return out, rows.Err()
}

func (d *DB) GetCmdbSyncJob(id string) (*CmdbSyncJob, error) {
	row := d.QueryRow(`SELECT id, name, adapter, cron, config_json, enabled, created_at, updated_at FROM tbl_cmdb_sync_jobs WHERE id=?`, strings.TrimSpace(id))
	var r CmdbSyncJob
	var en int
	var cfg string
	err := row.Scan(&r.ID, &r.Name, &r.Adapter, &r.Cron, &cfg, &en, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = en == 1
	if cfg != "" {
		r.ConfigJSON = json.RawMessage(cfg)
	}
	return &r, nil
}

func (d *DB) UpsertCmdbSyncJob(j *CmdbSyncJob) error {
	if j == nil {
		return errors.New("nil job")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	en := 0
	if j.Enabled {
		en = 1
	}
	cfg := "{}"
	if len(j.ConfigJSON) > 0 {
		cfg = string(j.ConfigJSON)
	}
	if j.ID != "" {
		res, err := d.Exec(
			`UPDATE tbl_cmdb_sync_jobs SET name=?, adapter=?, cron=?, config_json=?, enabled=?, updated_at=? WHERE id=?`,
			j.Name, j.Adapter, j.Cron, cfg, en, now, j.ID,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			j.UpdatedAt = now
			return nil
		}
	}
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	j.CreatedAt, j.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_cmdb_sync_jobs(id, name, adapter, cron, config_json, enabled, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		j.ID, j.Name, j.Adapter, j.Cron, cfg, en, j.CreatedAt, j.UpdatedAt,
	)
	return err
}

func (d *DB) DeleteCmdbSyncJob(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_cmdb_sync_jobs WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) CreateCmdbSyncRun(r *CmdbSyncRun) error {
	if r == nil {
		return errors.New("nil run")
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.StartedAt == "" {
		r.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.Exec(
		`INSERT INTO tbl_cmdb_sync_runs(id, job_id, status, added, updated, skipped, candidates, log_text, started_at, finished_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.JobID, r.Status, r.Added, r.Updated, r.Skipped, r.Candidates, r.LogText, r.StartedAt, r.FinishedAt,
	)
	return err
}

func (d *DB) UpdateCmdbSyncRun(r *CmdbSyncRun) error {
	if r == nil {
		return errors.New("nil run")
	}
	_, err := d.Exec(
		`UPDATE tbl_cmdb_sync_runs SET status=?, added=?, updated=?, skipped=?, candidates=?, log_text=?, finished_at=? WHERE id=?`,
		r.Status, r.Added, r.Updated, r.Skipped, r.Candidates, r.LogText, r.FinishedAt, r.ID,
	)
	return err
}

func (d *DB) ListCmdbSyncRuns(jobID string, limit int) ([]CmdbSyncRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(
		`SELECT id, job_id, status, added, updated, skipped, candidates, log_text, started_at, finished_at
		 FROM tbl_cmdb_sync_runs WHERE (?='' OR job_id=?) ORDER BY started_at DESC LIMIT ?`,
		strings.TrimSpace(jobID), strings.TrimSpace(jobID), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbSyncRun
	for rows.Next() {
		var r CmdbSyncRun
		if err := rows.Scan(&r.ID, &r.JobID, &r.Status, &r.Added, &r.Updated, &r.Skipped, &r.Candidates, &r.LogText, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []CmdbSyncRun{}
	}
	return out, rows.Err()
}

// EnsureDefaultCmdbTagDefs 从监控标签字典种子同步一批通用标签定义。
func (d *DB) EnsureDefaultCmdbTagDefs() error {
	return d.EnsureDefaultCmdbTagDefsForTenant("")
}

// EnsureDefaultCmdbTagDefsForTenant 租户无任何标签定义时写入内置默认集。
func (d *DB) EnsureDefaultCmdbTagDefsForTenant(tenantID string) error {
	exist, err := d.ListCmdbTagDefsInTenant(tenantID, "")
	if err != nil {
		return err
	}
	if len(exist) > 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	defs := []CmdbTagDef{
		{ID: uuid.NewString(), Key: "idc", Name: "机房/环境", Values: []string{}, Scope: "general", CITypes: []string{"instance", "database", "host"}, SortOrder: 1, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.NewString(), Key: "domain", Name: "域名", Values: []string{}, Scope: "general", CITypes: []string{"instance", "monitor_group"}, SortOrder: 2, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.NewString(), Key: "app", Name: "应用", Values: []string{}, Scope: "monitor", CITypes: []string{"instance", "monitor_job", "monitor_group"}, SortOrder: 3, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.NewString(), Key: "env", Name: "环境标签", Values: []string{}, Scope: "monitor", SortOrder: 4, CreatedAt: now, UpdatedAt: now},
	}
	for i := range defs {
		defs[i].TenantID = tenantID
		if err := d.UpsertCmdbTagDef(&defs[i]); err != nil {
			return fmt.Errorf("seed tag %s: %w", defs[i].Key, err)
		}
	}
	return nil
}
