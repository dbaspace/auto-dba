package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CmdbCISnapshot CI 关键属性快照（非全量参数中心）。
type CmdbCISnapshot struct {
	ID        string          `json:"id"`
	CIType    string          `json:"ci_type"`
	CIID      string          `json:"ci_id"`
	Hash      string          `json:"hash"`
	Payload   json.RawMessage `json:"payload"`
	IsBaseline bool           `json:"is_baseline"`
	Actor     string          `json:"actor,omitempty"`
	CreatedAt string          `json:"created_at"`
}

// CmdbDriftItem 相对基线的漂移项。
type CmdbDriftItem struct {
	CIType     string          `json:"ci_type"`
	CIID       string          `json:"ci_id"`
	Name       string          `json:"name,omitempty"`
	Field      string          `json:"field"`
	Baseline   any             `json:"baseline"`
	Current    any             `json:"current"`
	BaselineAt string          `json:"baseline_at,omitempty"`
	BaselinePayload json.RawMessage `json:"-"`
}

func (d *DB) ensureCmdbSnapshotTable() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_cmdb_ci_snapshots (
			id TEXT PRIMARY KEY,
			ci_type TEXT NOT NULL,
			ci_id TEXT NOT NULL,
			hash TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '{}',
			is_baseline INTEGER NOT NULL DEFAULT 0,
			actor TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cmdb_snap_ci ON tbl_cmdb_ci_snapshots(ci_type, ci_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cmdb_snap_baseline ON tbl_cmdb_ci_snapshots(ci_type, ci_id, is_baseline)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func hashPayload(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SnapshotInstanceMeta 抓取实例关键属性（脱敏，不含密码）。
func SnapshotInstanceMeta(it *Instance) map[string]any {
	if it == nil {
		return nil
	}
	return map[string]any{
		"name": it.Name, "host": it.Host, "port": it.Port, "db_type": it.DBType,
		"domain_name": it.DomainName, "domains": it.Domains, "tags": it.Tags,
		"visibility": it.Visibility, "tenant_id": it.TenantID, "env_id": it.EnvID,
		"region_id": it.RegionID, "idc": it.IDC, "approvers": it.Approvers,
		"cloud_vendor": it.CloudVendor, "cloud_instance_id": it.CloudInstanceID,
	}
}

// SnapshotDatabaseMeta 抓取库关键属性。
func SnapshotDatabaseMeta(db *Database) map[string]any {
	if db == nil {
		return nil
	}
	return map[string]any{
		"db_name": db.DBName, "instance_id": db.InstanceID, "tenant_id": db.TenantID,
		"approvers": db.Approvers, "visibility": db.Visibility, "tags": db.Tags,
		"domains": db.Domains, "access_users": db.AccessUsers, "role_ids": db.RoleIDs,
	}
}

func (d *DB) AppendCmdbCISnapshot(ciType, ciID, actor string, payload any, asBaseline bool) (*CmdbCISnapshot, error) {
	ciType = strings.TrimSpace(ciType)
	ciID = strings.TrimSpace(ciID)
	if ciType == "" || ciID == "" {
		return nil, fmt.Errorf("ci_type/ci_id 必填")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snap := &CmdbCISnapshot{
		ID: uuid.NewString(), CIType: ciType, CIID: ciID,
		Hash: hashPayload(raw), Payload: raw, IsBaseline: asBaseline,
		Actor: actor, CreatedAt: now,
	}
	if asBaseline {
		_, _ = d.Exec(`UPDATE tbl_cmdb_ci_snapshots SET is_baseline=0 WHERE ci_type=? AND ci_id=? AND is_baseline=1`, ciType, ciID)
	}
	_, err = d.Exec(
		`INSERT INTO tbl_cmdb_ci_snapshots(id, ci_type, ci_id, hash, payload, is_baseline, actor, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		snap.ID, snap.CIType, snap.CIID, snap.Hash, string(raw), boolToInt(asBaseline), snap.Actor, snap.CreatedAt,
	)
	return snap, err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (d *DB) ListCmdbCISnapshots(ciType, ciID string, limit int) ([]CmdbCISnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := d.Query(
		`SELECT id, ci_type, ci_id, hash, payload, is_baseline, actor, created_at
		 FROM tbl_cmdb_ci_snapshots WHERE ci_type=? AND ci_id=? ORDER BY created_at DESC LIMIT ?`,
		strings.TrimSpace(ciType), strings.TrimSpace(ciID), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CmdbCISnapshot
	for rows.Next() {
		var s CmdbCISnapshot
		var payload string
		var base int
		if err := rows.Scan(&s.ID, &s.CIType, &s.CIID, &s.Hash, &payload, &base, &s.Actor, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Payload = json.RawMessage(payload)
		s.IsBaseline = base == 1
		out = append(out, s)
	}
	if out == nil {
		out = []CmdbCISnapshot{}
	}
	return out, rows.Err()
}

func (d *DB) GetCmdbCIBaseline(ciType, ciID string) (*CmdbCISnapshot, error) {
	row := d.QueryRow(
		`SELECT id, ci_type, ci_id, hash, payload, is_baseline, actor, created_at
		 FROM tbl_cmdb_ci_snapshots WHERE ci_type=? AND ci_id=? AND is_baseline=1
		 ORDER BY created_at DESC LIMIT 1`,
		strings.TrimSpace(ciType), strings.TrimSpace(ciID),
	)
	var s CmdbCISnapshot
	var payload string
	var base int
	err := row.Scan(&s.ID, &s.CIType, &s.CIID, &s.Hash, &payload, &base, &s.Actor, &s.CreatedAt)
	if err == sql.ErrNoRows {
		// fallback: latest snapshot
		row = d.QueryRow(
			`SELECT id, ci_type, ci_id, hash, payload, is_baseline, actor, created_at
			 FROM tbl_cmdb_ci_snapshots WHERE ci_type=? AND ci_id=? ORDER BY created_at ASC LIMIT 1`,
			strings.TrimSpace(ciType), strings.TrimSpace(ciID),
		)
		err = row.Scan(&s.ID, &s.CIType, &s.CIID, &s.Hash, &payload, &base, &s.Actor, &s.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}
	s.Payload = json.RawMessage(payload)
	s.IsBaseline = base == 1
	return &s, nil
}

// DiffCmdbPayload 对比两个 JSON object，返回变更字段。
func DiffCmdbPayload(baseline, current json.RawMessage) []CmdbDriftItem {
	var bMap, cMap map[string]any
	_ = json.Unmarshal(baseline, &bMap)
	_ = json.Unmarshal(current, &cMap)
	if bMap == nil {
		bMap = map[string]any{}
	}
	if cMap == nil {
		cMap = map[string]any{}
	}
	keys := map[string]bool{}
	for k := range bMap {
		keys[k] = true
	}
	for k := range cMap {
		keys[k] = true
	}
	var out []CmdbDriftItem
	for k := range keys {
		bv, cv := bMap[k], cMap[k]
		bj, _ := json.Marshal(bv)
		cj, _ := json.Marshal(cv)
		if string(bj) != string(cj) {
			out = append(out, CmdbDriftItem{Field: k, Baseline: bv, Current: cv})
		}
	}
	return out
}

// ListCmdbDrifts 扫描 instance/database 相对基线的漂移。
func (d *DB) ListCmdbDrifts(tenantID string, limit int) ([]CmdbDriftItem, error) {
	if limit <= 0 {
		limit = 200
	}
	var out []CmdbDriftItem
	insts, err := d.ListInstancesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	for _, it := range insts {
		base, err := d.GetCmdbCIBaseline("instance", it.ID)
		if err != nil || base == nil {
			continue
		}
		cur, _ := json.Marshal(SnapshotInstanceMeta(&it))
		diffs := DiffCmdbPayload(base.Payload, cur)
		for _, di := range diffs {
			di.CIType, di.CIID, di.Name = "instance", it.ID, it.Name
			di.BaselineAt = base.CreatedAt
			out = append(out, di)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	dbs, err := d.ListDatabasesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	for _, db := range dbs {
		base, err := d.GetCmdbCIBaseline("database", db.ID)
		if err != nil || base == nil {
			continue
		}
		cur, _ := json.Marshal(SnapshotDatabaseMeta(&db))
		diffs := DiffCmdbPayload(base.Payload, cur)
		for _, di := range diffs {
			di.CIType, di.CIID, di.Name = "database", db.ID, db.DBName
			di.BaselineAt = base.CreatedAt
			out = append(out, di)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	if out == nil {
		out = []CmdbDriftItem{}
	}
	return out, nil
}

// MaybeSnapshotInstance 写路径：内容变化时追加快照。
func (d *DB) MaybeSnapshotInstance(it *Instance, actor string) {
	if d == nil || it == nil {
		return
	}
	payload := SnapshotInstanceMeta(it)
	raw, _ := json.Marshal(payload)
	h := hashPayload(raw)
	var lastHash string
	_ = d.QueryRow(
		`SELECT hash FROM tbl_cmdb_ci_snapshots WHERE ci_type=? AND ci_id=? ORDER BY created_at DESC LIMIT 1`,
		"instance", it.ID,
	).Scan(&lastHash)
	if lastHash == h {
		return
	}
	asBase := lastHash == ""
	_, _ = d.AppendCmdbCISnapshot("instance", it.ID, actor, payload, asBase)
}

// MaybeSnapshotDatabase 写路径：内容变化时追加快照。
func (d *DB) MaybeSnapshotDatabase(dbRow *Database, actor string) {
	if d == nil || dbRow == nil {
		return
	}
	payload := SnapshotDatabaseMeta(dbRow)
	raw, _ := json.Marshal(payload)
	h := hashPayload(raw)
	var lastHash string
	_ = d.QueryRow(
		`SELECT hash FROM tbl_cmdb_ci_snapshots WHERE ci_type=? AND ci_id=? ORDER BY created_at DESC LIMIT 1`,
		"database", dbRow.ID,
	).Scan(&lastHash)
	if lastHash == h {
		return
	}
	asBase := lastHash == ""
	_, _ = d.AppendCmdbCISnapshot("database", dbRow.ID, actor, payload, asBase)
}
