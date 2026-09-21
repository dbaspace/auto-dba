package store

import (
	"database/sql"
	"strings"
	"time"
)

func (d *DB) InsertPGActivitySnapshots(rows []PGActivitySnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	const q = `INSERT INTO tbl_pg_activity_snapshots(
		id, collected_at, instance_id, instance_name, host, port, pid,
		p_user, p_host, p_db, command, xact_duration, state, info,
		query_normalized, query_hash, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	for i := range rows {
		r := rows[i]
		var pid any
		var xact any
		if r.PID != nil {
			pid = *r.PID
		}
		if r.XactDuration != nil {
			xact = *r.XactDuration
		}
		if _, err := d.Exec(q,
			r.ID, r.CollectedAt, r.InstanceID, r.InstanceName, r.Host, r.Port, pid,
			r.PUser, r.PHost, r.PDb, r.Command, xact, r.State, r.Info,
			r.QueryNormalized, r.QueryHash, r.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) DeletePGActivitySnapshotsBefore(cutoff time.Time) (int64, error) {
	res, err := d.Exec(`DELETE FROM tbl_pg_activity_snapshots WHERE collected_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func normalizeFilterTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return s
}

func appendTimeFilters(q string, args []any, start, end string) (string, []any) {
	if v := normalizeFilterTime(start); v != "" {
		q += ` AND collected_at >= ?`
		args = append(args, v)
	}
	if v := normalizeFilterTime(end); v != "" {
		q += ` AND collected_at <= ?`
		args = append(args, v)
	}
	return q, args
}

func (d *DB) ListPGActivitySnapshots(f PGActivitySnapshotFilter) ([]PGActivitySnapshot, int, error) {
	where := `WHERE 1=1`
	var args []any
	if strings.TrimSpace(f.InstanceID) != "" {
		where += ` AND instance_id=?`
		args = append(args, strings.TrimSpace(f.InstanceID))
	}
	where, args = appendTimeFilters(where, args, f.StartTime, f.EndTime)

	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_pg_activity_snapshots `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, collected_at, instance_id, instance_name, host, port, pid,
		p_user, p_host, p_db, command, xact_duration, state, info,
		query_normalized, query_hash, created_at
		FROM tbl_pg_activity_snapshots ` + where + ` ORDER BY collected_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []PGActivitySnapshot
	for rows.Next() {
		var (
			it   PGActivitySnapshot
			pid  sql.NullInt64
			xact sql.NullFloat64
		)
		if err := rows.Scan(
			&it.ID, &it.CollectedAt, &it.InstanceID, &it.InstanceName, &it.Host, &it.Port, &pid,
			&it.PUser, &it.PHost, &it.PDb, &it.Command, &xact, &it.State, &it.Info,
			&it.QueryNormalized, &it.QueryHash, &it.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if pid.Valid {
			v := pid.Int64
			it.PID = &v
		}
		if xact.Valid {
			v := xact.Float64
			it.XactDuration = &v
		}
		out = append(out, it)
	}
	if out == nil {
		out = []PGActivitySnapshot{}
	}
	return out, total, rows.Err()
}

func statsOrderBy(order string) string {
	switch strings.ToLower(strings.TrimSpace(order)) {
	case "avg_duration":
		return `AVG(xact_duration) DESC NULLS LAST`
	case "max_duration":
		return `MAX(xact_duration) DESC NULLS LAST`
	case "last_seen":
		return `MAX(collected_at) DESC`
	default:
		return `COUNT(*) DESC`
	}
}

func (d *DB) ListPGActivityStats(f PGActivityStatFilter) ([]PGActivityStat, int, error) {
	where := `WHERE query_hash <> ''`
	var args []any
	if strings.TrimSpace(f.InstanceID) != "" {
		where += ` AND instance_id=?`
		args = append(args, strings.TrimSpace(f.InstanceID))
	}
	where, args = appendTimeFilters(where, args, f.StartTime, f.EndTime)
	if strings.TrimSpace(f.PDb) != "" {
		where += ` AND p_db=?`
		args = append(args, strings.TrimSpace(f.PDb))
	}
	if strings.TrimSpace(f.PUser) != "" {
		where += ` AND p_user=?`
		args = append(args, strings.TrimSpace(f.PUser))
	}

	var total int
	countQ := `SELECT COUNT(1) FROM (
		SELECT query_hash FROM tbl_pg_activity_snapshots ` + where + ` GROUP BY query_hash, query_normalized
	) t`
	if err := d.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `SELECT query_hash, query_normalized, MAX(info) AS sample_info, COUNT(*) AS cnt,
		COUNT(DISTINCT instance_id) AS distinct_instances, COUNT(DISTINCT p_db) AS distinct_dbs,
		AVG(xact_duration) AS avg_xact_duration, MAX(xact_duration) AS max_xact_duration,
		MIN(collected_at) AS first_seen, MAX(collected_at) AS last_seen
		FROM tbl_pg_activity_snapshots ` + where + `
		GROUP BY query_hash, query_normalized
		ORDER BY ` + statsOrderBy(f.OrderBy) + `
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []PGActivityStat
	for rows.Next() {
		var (
			it   PGActivityStat
			avg  sql.NullFloat64
			maxv sql.NullFloat64
		)
		if err := rows.Scan(
			&it.QueryHash, &it.QueryNormalized, &it.SampleInfo, &it.Count,
			&it.DistinctInstances, &it.DistinctDbs, &avg, &maxv, &it.FirstSeen, &it.LastSeen,
		); err != nil {
			return nil, 0, err
		}
		if avg.Valid {
			v := avg.Float64
			it.AvgXactDuration = &v
		}
		if maxv.Valid {
			v := maxv.Float64
			it.MaxXactDuration = &v
		}
		out = append(out, it)
	}
	if out == nil {
		out = []PGActivityStat{}
	}
	return out, total, rows.Err()
}
