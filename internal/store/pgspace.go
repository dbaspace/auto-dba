package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SpaceRow 表空间/存储分析快照行（tablespace/database/table/index/sequence 复用）。
type SpaceRow struct {
	ID             int64     `json:"id"`
	CollectedAt    time.Time `json:"collected_at"`
	InstanceID     string    `json:"instance_id"`
	InstanceName   string    `json:"instance_name"`
	Host           string    `json:"host"`
	Port           int64     `json:"port"`
	ObjectType     string    `json:"object_type"`
	DbName         string    `json:"db_name"`
	SchemaName     string    `json:"schema_name"`
	ObjectName     string    `json:"object_name"`
	TablespaceName string    `json:"tablespace_name"`
	ShardCount     int64     `json:"shard_count"`
	SizeBytes      *int64    `json:"size_bytes"`
	TableBytes     *int64    `json:"table_bytes"`
	IndexBytes     *int64    `json:"index_bytes"`
	ToastBytes     *int64    `json:"toast_bytes"`
	NLiveTup       *int64    `json:"n_live_tup"`
	NDeadTup       *int64    `json:"n_dead_tup"`
	DeadRatio      *float64  `json:"dead_ratio"`
	BloatBytes     *int64    `json:"bloat_bytes"`
	BloatRatio     *float64  `json:"bloat_ratio"`
	IdxScan        *int64    `json:"idx_scan"`
	IsUnused       *bool     `json:"is_unused"`

	SeqScan           *int64     `json:"seq_scan,omitempty"`
	TblIdxScan        *int64     `json:"tbl_idx_scan,omitempty"`
	HeapBlksRead      *int64     `json:"heap_blks_read,omitempty"`
	HeapBlksHit       *int64     `json:"heap_blks_hit,omitempty"`
	HeapCacheHitRatio *float64   `json:"heap_cache_hit_ratio,omitempty"`
	NTupIns           *int64     `json:"n_tup_ins,omitempty"`
	NTupUpd           *int64     `json:"n_tup_upd,omitempty"`
	NTupDel           *int64     `json:"n_tup_del,omitempty"`
	LastVacuum        *time.Time `json:"last_vacuum,omitempty"`
	LastAutovacuum    *time.Time `json:"last_autovacuum,omitempty"`
	LastAnalyze       *time.Time `json:"last_analyze,omitempty"`
	LastAutoanalyze   *time.Time `json:"last_autoanalyze,omitempty"`
	RelFrozenXIDAge   *int64     `json:"relfrozenxid_age,omitempty"`
	DatFrozenXIDAge   *int64     `json:"datfrozenxid_age,omitempty"`

	SeqLastValue *int64   `json:"seq_last_value,omitempty"`
	SeqMaxValue  *int64   `json:"seq_max_value,omitempty"`
	SeqMinValue  *int64   `json:"seq_min_value,omitempty"`
	SeqUsedRatio *float64 `json:"seq_used_ratio,omitempty"`
	SeqRemaining *int64   `json:"seq_remaining,omitempty"`
	SeqCycle     *bool    `json:"seq_cycle,omitempty"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// SpaceListFilter 快照查询过滤。
type SpaceListFilter struct {
	InstanceID  string
	Host        string
	Port        int64
	ObjectType  string
	DbName      string
	OnlyLatest  bool
	CollectedAt *time.Time
	SkipTotal   bool
	Offset      int
	Limit       int
}

// SpaceTrendFilter 单对象趋势过滤。
type SpaceTrendFilter struct {
	InstanceID string
	Host       string
	Port       int64
	ObjectType string
	DbName     string
	ObjectName string
	StartTime  *time.Time
	EndTime    *time.Time
}

// SpaceTrendPoint 趋势点。
type SpaceTrendPoint struct {
	CollectedAt time.Time `json:"collected_at"`
	SizeBytes   *int64    `json:"size_bytes"`
}

// SpaceAdviceLatest 每实例最新建议预制。
type SpaceAdviceLatest struct {
	InstanceID        string
	InstanceName      string
	Host              string
	Port              int64
	CollectedAt       time.Time
	Severity          string
	AdviceCount       int
	ReclaimableBytes  int64
	XIDRiskCount      int
	SequenceRiskCount int
	VacuumRiskCount   int
	AccessRiskCount   int
	CacheRiskCount    int
	HotTableCount     int
	ReportJSON        []byte
	UpdatedAt         time.Time
}

const spaceSelectCols = `
id, collected_at, COALESCE(instance_id,''), COALESCE(instance_name,''), host, port,
object_type, COALESCE(db_name,''), COALESCE(schema_name,''), object_name, COALESCE(tablespace_name,''), shard_count,
size_bytes, table_bytes, index_bytes, toast_bytes,
n_live_tup, n_dead_tup, dead_ratio, bloat_bytes, bloat_ratio,
idx_scan, is_unused,
seq_scan, tbl_idx_scan, heap_blks_read, heap_blks_hit, heap_cache_hit_ratio,
n_tup_ins, n_tup_upd, n_tup_del,
last_vacuum, last_autovacuum, last_analyze, last_autoanalyze,
relfrozenxid_age, datfrozenxid_age,
seq_last_value, seq_max_value, seq_min_value, seq_used_ratio, seq_remaining, seq_cycle,
created_at`

const spaceInsertSQL = `
INSERT INTO tbl_pg_space_snapshots (
  collected_at, instance_id, instance_name, host, port,
  object_type, db_name, schema_name, object_name, tablespace_name, shard_count,
  size_bytes, table_bytes, index_bytes, toast_bytes,
  n_live_tup, n_dead_tup, dead_ratio, bloat_bytes, bloat_ratio,
  idx_scan, is_unused,
  seq_scan, tbl_idx_scan, heap_blks_read, heap_blks_hit, heap_cache_hit_ratio,
  n_tup_ins, n_tup_upd, n_tup_del,
  last_vacuum, last_autovacuum, last_analyze, last_autoanalyze,
  relfrozenxid_age, datfrozenxid_age,
  seq_last_value, seq_max_value, seq_min_value, seq_used_ratio, seq_remaining, seq_cycle
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

// BatchInsertSpaceSnapshots 批量写入快照。
func (d *DB) BatchInsertSpaceSnapshots(ctx context.Context, rows []*SpaceRow) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, d.rewrite(spaceInsertSQL))
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, r := range rows {
		sc := r.ShardCount
		if sc <= 0 {
			sc = 1
		}
		_, err = stmt.ExecContext(ctx,
			r.CollectedAt, r.InstanceID, r.InstanceName, r.Host, r.Port,
			r.ObjectType, r.DbName, r.SchemaName, r.ObjectName, r.TablespaceName, sc,
			nullInt64Arg(r.SizeBytes), nullInt64Arg(r.TableBytes), nullInt64Arg(r.IndexBytes), nullInt64Arg(r.ToastBytes),
			nullInt64Arg(r.NLiveTup), nullInt64Arg(r.NDeadTup), nullFloat64Arg(r.DeadRatio), nullInt64Arg(r.BloatBytes), nullFloat64Arg(r.BloatRatio),
			nullInt64Arg(r.IdxScan), nullBoolArg(r.IsUnused),
			nullInt64Arg(r.SeqScan), nullInt64Arg(r.TblIdxScan), nullInt64Arg(r.HeapBlksRead), nullInt64Arg(r.HeapBlksHit), nullFloat64Arg(r.HeapCacheHitRatio),
			nullInt64Arg(r.NTupIns), nullInt64Arg(r.NTupUpd), nullInt64Arg(r.NTupDel),
			nullTimeArg(r.LastVacuum), nullTimeArg(r.LastAutovacuum), nullTimeArg(r.LastAnalyze), nullTimeArg(r.LastAutoanalyze),
			nullInt64Arg(r.RelFrozenXIDAge), nullInt64Arg(r.DatFrozenXIDAge),
			nullInt64Arg(r.SeqLastValue), nullInt64Arg(r.SeqMaxValue), nullInt64Arg(r.SeqMinValue), nullFloat64Arg(r.SeqUsedRatio), nullInt64Arg(r.SeqRemaining), nullBoolArg(r.SeqCycle),
		)
		if err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// LatestSpaceTime 返回某实例最近一批采集时间。
func (d *DB) LatestSpaceTime(ctx context.Context, instanceID, host string, port int64) (time.Time, bool, error) {
	conds, args := spaceInstanceConds(instanceID, host, port)
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var t sql.NullTime
	err := d.db.QueryRowContext(ctx, d.rewrite("SELECT MAX(collected_at) FROM tbl_pg_space_snapshots "+where), args...).Scan(&t)
	if err != nil {
		return time.Time{}, false, err
	}
	if !t.Valid {
		return time.Time{}, false, nil
	}
	return t.Time, true, nil
}

// ListSpaceSnapshots 分页查询快照。
func (d *DB) ListSpaceSnapshots(ctx context.Context, f SpaceListFilter) ([]*SpaceRow, int, error) {
	var conds []string
	var args []any
	if strings.TrimSpace(f.InstanceID) != "" {
		conds = append(conds, "instance_id=?")
		args = append(args, f.InstanceID)
	}
	if strings.TrimSpace(f.Host) != "" {
		conds = append(conds, "host=?")
		args = append(args, f.Host)
	}
	if f.Port > 0 {
		conds = append(conds, "port=?")
		args = append(args, f.Port)
	}
	if strings.TrimSpace(f.ObjectType) != "" {
		conds = append(conds, "object_type=?")
		args = append(args, f.ObjectType)
	}
	if strings.TrimSpace(f.DbName) != "" {
		conds = append(conds, "db_name=?")
		args = append(args, f.DbName)
	}
	if f.CollectedAt != nil {
		conds = append(conds, "collected_at=?")
		args = append(args, *f.CollectedAt)
	} else if f.OnlyLatest {
		latest, ok, err := d.LatestSpaceTime(ctx, f.InstanceID, f.Host, f.Port)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			return []*SpaceRow{}, 0, nil
		}
		conds = append(conds, "collected_at=?")
		args = append(args, latest)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	total := -1
	if !f.SkipTotal {
		if err := d.db.QueryRowContext(ctx, d.rewrite("SELECT COUNT(*) FROM tbl_pg_space_snapshots "+where), args...).Scan(&total); err != nil {
			return nil, 0, err
		}
		if total == 0 {
			return []*SpaceRow{}, 0, nil
		}
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q := fmt.Sprintf(`SELECT %s FROM tbl_pg_space_snapshots %s
ORDER BY size_bytes DESC NULLS LAST, id DESC
LIMIT ? OFFSET ?`, spaceSelectCols, where)
	args = append(args, limit, f.Offset)

	rows, err := d.db.QueryContext(ctx, d.rewrite(q), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*SpaceRow
	for rows.Next() {
		r, err := scanSpaceRow(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if f.SkipTotal {
		total = len(list)
	}
	return list, total, nil
}

// SpaceTrend 单对象大小趋势。
func (d *DB) SpaceTrend(ctx context.Context, f SpaceTrendFilter) ([]*SpaceTrendPoint, error) {
	conds := []string{"object_type=?", "object_name=?"}
	args := []any{f.ObjectType, f.ObjectName}
	if strings.TrimSpace(f.InstanceID) != "" {
		conds = append(conds, "instance_id=?")
		args = append(args, f.InstanceID)
	}
	if strings.TrimSpace(f.Host) != "" {
		conds = append(conds, "host=?")
		args = append(args, f.Host)
	}
	if f.Port > 0 {
		conds = append(conds, "port=?")
		args = append(args, f.Port)
	}
	if strings.TrimSpace(f.DbName) != "" {
		conds = append(conds, "db_name=?")
		args = append(args, f.DbName)
	}
	if f.StartTime != nil {
		conds = append(conds, "collected_at>=?")
		args = append(args, *f.StartTime)
	}
	if f.EndTime != nil {
		conds = append(conds, "collected_at<=?")
		args = append(args, *f.EndTime)
	}
	where := "WHERE " + strings.Join(conds, " AND ")
	q := "SELECT collected_at, size_bytes FROM tbl_pg_space_snapshots " + where + " ORDER BY collected_at ASC"

	rows, err := d.db.QueryContext(ctx, d.rewrite(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*SpaceTrendPoint{}
	for rows.Next() {
		var p SpaceTrendPoint
		var sz sql.NullInt64
		if err := rows.Scan(&p.CollectedAt, &sz); err != nil {
			return nil, err
		}
		if sz.Valid {
			v := sz.Int64
			p.SizeBytes = &v
		}
		list = append(list, &p)
	}
	return list, rows.Err()
}

// DeleteSpaceSnapshotsBefore 按保留期清理。
func (d *DB) DeleteSpaceSnapshotsBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := d.db.ExecContext(ctx, d.rewrite("DELETE FROM tbl_pg_space_snapshots WHERE collected_at < ?"), before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertSpaceAdviceLatest 按 host+port 覆盖写入最新建议。
func (d *DB) UpsertSpaceAdviceLatest(ctx context.Context, r *SpaceAdviceLatest) error {
	if r == nil {
		return fmt.Errorf("nil advice row")
	}
	if len(r.ReportJSON) == 0 {
		r.ReportJSON = []byte("{}")
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_pg_space_advice_latest (
  instance_id, instance_name, host, port, collected_at, severity,
  advice_count, reclaimable_bytes,
  xid_risk_count, sequence_risk_count, vacuum_risk_count,
  access_risk_count, cache_risk_count, hot_table_count,
  report, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb, NOW())
ON CONFLICT (host, port) DO UPDATE SET
  instance_id = EXCLUDED.instance_id,
  instance_name = EXCLUDED.instance_name,
  collected_at = EXCLUDED.collected_at,
  severity = EXCLUDED.severity,
  advice_count = EXCLUDED.advice_count,
  reclaimable_bytes = EXCLUDED.reclaimable_bytes,
  xid_risk_count = EXCLUDED.xid_risk_count,
  sequence_risk_count = EXCLUDED.sequence_risk_count,
  vacuum_risk_count = EXCLUDED.vacuum_risk_count,
  access_risk_count = EXCLUDED.access_risk_count,
  cache_risk_count = EXCLUDED.cache_risk_count,
  hot_table_count = EXCLUDED.hot_table_count,
  report = EXCLUDED.report,
  updated_at = NOW()
`),
		r.InstanceID, r.InstanceName, r.Host, r.Port, r.CollectedAt, r.Severity,
		r.AdviceCount, r.ReclaimableBytes,
		r.XIDRiskCount, r.SequenceRiskCount, r.VacuumRiskCount,
		r.AccessRiskCount, r.CacheRiskCount, r.HotTableCount,
		string(r.ReportJSON),
	)
	return err
}

// GetSpaceAdviceLatest 按 instance_id 或 host+port 取最新建议。
func (d *DB) GetSpaceAdviceLatest(ctx context.Context, instanceID, host string, port int64) (*SpaceAdviceLatest, bool, error) {
	var (
		q    string
		args []any
	)
	if id := strings.TrimSpace(instanceID); id != "" {
		q = `SELECT instance_id, COALESCE(instance_name,''), host, port, collected_at, severity,
			advice_count, reclaimable_bytes,
			xid_risk_count, sequence_risk_count, vacuum_risk_count,
			access_risk_count, cache_risk_count, hot_table_count,
			report, updated_at
			FROM tbl_pg_space_advice_latest WHERE instance_id=?
			ORDER BY updated_at DESC LIMIT 1`
		args = []any{id}
	} else if strings.TrimSpace(host) != "" && port > 0 {
		q = `SELECT instance_id, COALESCE(instance_name,''), host, port, collected_at, severity,
			advice_count, reclaimable_bytes,
			xid_risk_count, sequence_risk_count, vacuum_risk_count,
			access_risk_count, cache_risk_count, hot_table_count,
			report, updated_at
			FROM tbl_pg_space_advice_latest WHERE host=? AND port=? LIMIT 1`
		args = []any{host, port}
	} else {
		return nil, false, fmt.Errorf("instance_id or host+port required")
	}

	var r SpaceAdviceLatest
	var report []byte
	err := d.db.QueryRowContext(ctx, d.rewrite(q), args...).Scan(
		&r.InstanceID, &r.InstanceName, &r.Host, &r.Port, &r.CollectedAt, &r.Severity,
		&r.AdviceCount, &r.ReclaimableBytes,
		&r.XIDRiskCount, &r.SequenceRiskCount, &r.VacuumRiskCount,
		&r.AccessRiskCount, &r.CacheRiskCount, &r.HotTableCount,
		&report, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	r.ReportJSON = report
	return &r, true, nil
}

// ListSpaceAdviceLatest 列出全部实例最新建议。
func (d *DB) ListSpaceAdviceLatest(ctx context.Context) ([]*SpaceAdviceLatest, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT instance_id, COALESCE(instance_name,''), host, port, collected_at, severity,
  advice_count, reclaimable_bytes,
  xid_risk_count, sequence_risk_count, vacuum_risk_count,
  access_risk_count, cache_risk_count, hot_table_count,
  report, updated_at
FROM tbl_pg_space_advice_latest
ORDER BY
  CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
  advice_count DESC, host, port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*SpaceAdviceLatest
	for rows.Next() {
		var r SpaceAdviceLatest
		var report []byte
		if err := rows.Scan(
			&r.InstanceID, &r.InstanceName, &r.Host, &r.Port, &r.CollectedAt, &r.Severity,
			&r.AdviceCount, &r.ReclaimableBytes,
			&r.XIDRiskCount, &r.SequenceRiskCount, &r.VacuumRiskCount,
			&r.AccessRiskCount, &r.CacheRiskCount, &r.HotTableCount,
			&report, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		r.ReportJSON = report
		list = append(list, &r)
	}
	return list, rows.Err()
}

func spaceInstanceConds(instanceID, host string, port int64) ([]string, []any) {
	var conds []string
	var args []any
	if strings.TrimSpace(instanceID) != "" {
		conds = append(conds, "instance_id=?")
		args = append(args, instanceID)
	}
	if strings.TrimSpace(host) != "" {
		conds = append(conds, "host=?")
		args = append(args, host)
	}
	if port > 0 {
		conds = append(conds, "port=?")
		args = append(args, port)
	}
	return conds, args
}

func scanSpaceRow(rows *sql.Rows) (*SpaceRow, error) {
	var r SpaceRow
	var (
		sizeBytes, tableBytes, indexBytes, toastBytes  sql.NullInt64
		nLiveTup, nDeadTup, bloatBytes, idxScan        sql.NullInt64
		deadRatio, bloatRatio                          sql.NullFloat64
		isUnused                                       sql.NullBool
		seqScan, tblIdxScan, heapBlksRead, heapBlksHit sql.NullInt64
		heapCacheHitRatio                              sql.NullFloat64
		nTupIns, nTupUpd, nTupDel                      sql.NullInt64
		lastVacuum, lastAutovacuum                     sql.NullTime
		lastAnalyze, lastAutoanalyze                   sql.NullTime
		relFrozen, datFrozen                           sql.NullInt64
		seqLast, seqMax, seqMin, seqRem                sql.NullInt64
		seqUsed                                        sql.NullFloat64
		seqCycle                                       sql.NullBool
		createdAt                                      sql.NullTime
	)
	if err := rows.Scan(
		&r.ID, &r.CollectedAt, &r.InstanceID, &r.InstanceName, &r.Host, &r.Port,
		&r.ObjectType, &r.DbName, &r.SchemaName, &r.ObjectName, &r.TablespaceName, &r.ShardCount,
		&sizeBytes, &tableBytes, &indexBytes, &toastBytes,
		&nLiveTup, &nDeadTup, &deadRatio, &bloatBytes, &bloatRatio,
		&idxScan, &isUnused,
		&seqScan, &tblIdxScan, &heapBlksRead, &heapBlksHit, &heapCacheHitRatio,
		&nTupIns, &nTupUpd, &nTupDel,
		&lastVacuum, &lastAutovacuum, &lastAnalyze, &lastAutoanalyze,
		&relFrozen, &datFrozen,
		&seqLast, &seqMax, &seqMin, &seqUsed, &seqRem, &seqCycle,
		&createdAt,
	); err != nil {
		return nil, err
	}
	r.SizeBytes = int64PtrFromNull(sizeBytes)
	r.TableBytes = int64PtrFromNull(tableBytes)
	r.IndexBytes = int64PtrFromNull(indexBytes)
	r.ToastBytes = int64PtrFromNull(toastBytes)
	r.NLiveTup = int64PtrFromNull(nLiveTup)
	r.NDeadTup = int64PtrFromNull(nDeadTup)
	r.DeadRatio = float64PtrFromNull(deadRatio)
	r.BloatBytes = int64PtrFromNull(bloatBytes)
	r.BloatRatio = float64PtrFromNull(bloatRatio)
	r.IdxScan = int64PtrFromNull(idxScan)
	if isUnused.Valid {
		v := isUnused.Bool
		r.IsUnused = &v
	}
	r.SeqScan = int64PtrFromNull(seqScan)
	r.TblIdxScan = int64PtrFromNull(tblIdxScan)
	r.HeapBlksRead = int64PtrFromNull(heapBlksRead)
	r.HeapBlksHit = int64PtrFromNull(heapBlksHit)
	r.HeapCacheHitRatio = float64PtrFromNull(heapCacheHitRatio)
	r.NTupIns = int64PtrFromNull(nTupIns)
	r.NTupUpd = int64PtrFromNull(nTupUpd)
	r.NTupDel = int64PtrFromNull(nTupDel)
	r.LastVacuum = timePtrFromNull(lastVacuum)
	r.LastAutovacuum = timePtrFromNull(lastAutovacuum)
	r.LastAnalyze = timePtrFromNull(lastAnalyze)
	r.LastAutoanalyze = timePtrFromNull(lastAutoanalyze)
	r.RelFrozenXIDAge = int64PtrFromNull(relFrozen)
	r.DatFrozenXIDAge = int64PtrFromNull(datFrozen)
	r.SeqLastValue = int64PtrFromNull(seqLast)
	r.SeqMaxValue = int64PtrFromNull(seqMax)
	r.SeqMinValue = int64PtrFromNull(seqMin)
	r.SeqUsedRatio = float64PtrFromNull(seqUsed)
	r.SeqRemaining = int64PtrFromNull(seqRem)
	if seqCycle.Valid {
		v := seqCycle.Bool
		r.SeqCycle = &v
	}
	if createdAt.Valid {
		t := createdAt.Time
		r.CreatedAt = &t
	}
	return &r, nil
}

func nullInt64Arg(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullFloat64Arg(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullBoolArg(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullTimeArg(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}

func int64PtrFromNull(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

func float64PtrFromNull(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}

func timePtrFromNull(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}
