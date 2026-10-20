package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Job settings / runs / fire ledger / leases — 平台定时任务中心持久化。

type JobSetting struct {
	JobKey       string `json:"job_key"`
	Enabled      bool   `json:"enabled"`
	ScheduleSpec string `json:"schedule_spec"`
	UpdatedBy    string `json:"updated_by,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	Version      int64  `json:"version"`
}

type JobRun struct {
	ID            string `json:"id"`
	JobKey        string `json:"job_key"`
	Trigger       string `json:"trigger"`
	Status        string `json:"status"`
	FireKey       string `json:"fire_key,omitempty"`
	OwnerID       string `json:"owner_id,omitempty"`
	OwnerHost     string `json:"owner_host,omitempty"`
	FencingToken  int64  `json:"fencing_token,omitempty"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
}

type JobLease struct {
	JobKey       string `json:"job_key"`
	OwnerID      string `json:"owner_id"`
	OwnerHost    string `json:"owner_host"`
	OwnerPID     int    `json:"owner_pid"`
	FencingToken int64  `json:"fencing_token"`
	ExpiresAt    string `json:"expires_at"`
	HeartbeatAt  string `json:"heartbeat_at"`
	RunID        string `json:"run_id,omitempty"`
	Held         bool   `json:"held"` // expires_at > now
}

func (d *DB) ensureJobRegTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_job_audits (
			id TEXT PRIMARY KEY,
			job_key TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_job_audits_created ON tbl_job_audits(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_job_settings (
			job_key TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 1,
			schedule_spec TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			version BIGINT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_job_fire_ledger (
			job_key TEXT NOT NULL,
			fire_key TEXT NOT NULL,
			fencing_token BIGINT NOT NULL DEFAULT 0,
			run_id TEXT NOT NULL DEFAULT '',
			triggered_at TEXT NOT NULL,
			PRIMARY KEY (job_key, fire_key)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_job_runs (
			id TEXT PRIMARY KEY,
			job_key TEXT NOT NULL,
			trigger TEXT NOT NULL DEFAULT 'cron',
			status TEXT NOT NULL,
			fire_key TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL DEFAULT '',
			owner_host TEXT NOT NULL DEFAULT '',
			fencing_token BIGINT NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			ended_at TEXT NOT NULL DEFAULT '',
			duration_ms BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_job_runs_job_started ON tbl_job_runs(job_key, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_job_leases (
			job_key TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			owner_host TEXT NOT NULL DEFAULT '',
			owner_pid INTEGER NOT NULL DEFAULT 0,
			fencing_token BIGINT NOT NULL DEFAULT 1,
			expires_at TIMESTAMPTZ NOT NULL,
			heartbeat_at TIMESTAMPTZ NOT NULL,
			run_id TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return fmt.Errorf("job_reg ddl: %w", err)
		}
	}
	return nil
}

func (d *DB) GetJobSetting(jobKey string) (*JobSetting, error) {
	jobKey = strings.TrimSpace(jobKey)
	var s JobSetting
	var en int
	err := d.QueryRow(`
		SELECT job_key, enabled, schedule_spec, updated_by, updated_at, version
		FROM tbl_job_settings WHERE job_key=?`, jobKey).Scan(
		&s.JobKey, &en, &s.ScheduleSpec, &s.UpdatedBy, &s.UpdatedAt, &s.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Enabled = en != 0
	return &s, nil
}

func (d *DB) UpsertJobSetting(jobKey string, enabled bool, scheduleSpec, updatedBy string) (*JobSetting, error) {
	jobKey = strings.TrimSpace(jobKey)
	scheduleSpec = strings.TrimSpace(scheduleSpec)
	updatedBy = strings.TrimSpace(updatedBy)
	now := time.Now().UTC().Format(time.RFC3339)
	en := 0
	if enabled {
		en = 1
	}
	_, err := d.Exec(`
		INSERT INTO tbl_job_settings(job_key, enabled, schedule_spec, updated_by, updated_at, version)
		VALUES(?,?,?,?,?,1)
		ON CONFLICT(job_key) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			schedule_spec=EXCLUDED.schedule_spec,
			updated_by=EXCLUDED.updated_by,
			updated_at=EXCLUDED.updated_at,
			version=tbl_job_settings.version+1`,
		jobKey, en, scheduleSpec, updatedBy, now,
	)
	if err != nil {
		return nil, err
	}
	return d.GetJobSetting(jobKey)
}

func (d *DB) EnsureJobSetting(jobKey, defaultSpec string, enabled bool) error {
	cur, err := d.GetJobSetting(jobKey)
	if err != nil {
		return err
	}
	if cur != nil {
		return nil
	}
	_, err = d.UpsertJobSetting(jobKey, enabled, defaultSpec, "system")
	return err
}

func (d *DB) InsertJobFireLedger(jobKey, fireKey string, fencingToken int64, runID string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(`
		INSERT INTO tbl_job_fire_ledger(job_key, fire_key, fencing_token, run_id, triggered_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(job_key, fire_key) DO NOTHING`,
		jobKey, fireKey, fencingToken, runID, now,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (d *DB) HasJobFireLedger(jobKey, fireKey string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_job_fire_ledger WHERE job_key=? AND fire_key=?`, jobKey, fireKey).Scan(&n)
	return n > 0, err
}

func (d *DB) InsertJobRun(r *JobRun) error {
	if r == nil {
		return fmt.Errorf("nil run")
	}
	_, err := d.Exec(`
		INSERT INTO tbl_job_runs(id, job_key, trigger, status, fire_key, owner_id, owner_host, fencing_token, error, started_at, ended_at, duration_ms)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.JobKey, r.Trigger, r.Status, r.FireKey, r.OwnerID, r.OwnerHost, r.FencingToken,
		r.Error, r.StartedAt, r.EndedAt, r.DurationMs,
	)
	return err
}

func (d *DB) FinishJobRun(id, status, errMsg string, durationMs int64) error {
	ended := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`
		UPDATE tbl_job_runs SET status=?, error=?, ended_at=?, duration_ms=? WHERE id=?`,
		status, errMsg, ended, durationMs, id,
	)
	return err
}

func (d *DB) ListJobRuns(jobKey string, limit int) ([]JobRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.Query(`
		SELECT id, job_key, trigger, status, fire_key, owner_id, owner_host, fencing_token, error, started_at, ended_at, duration_ms
		FROM tbl_job_runs WHERE job_key=? ORDER BY started_at DESC LIMIT ?`, jobKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRun
	for rows.Next() {
		var r JobRun
		if err := rows.Scan(&r.ID, &r.JobKey, &r.Trigger, &r.Status, &r.FireKey, &r.OwnerID, &r.OwnerHost,
			&r.FencingToken, &r.Error, &r.StartedAt, &r.EndedAt, &r.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) LastJobRun(jobKey string) (*JobRun, error) {
	list, err := d.ListJobRuns(jobKey, 1)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (d *DB) CountJobRunsSince(jobKey, sinceRFC3339, status string) (int, error) {
	q := `SELECT COUNT(1) FROM tbl_job_runs WHERE job_key=? AND started_at>=?`
	args := []any{jobKey, sinceRFC3339}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (d *DB) RecoverStuckJobRuns(maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		maxAge = 2 * time.Minute
	}
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	// 仅当对应 lease 已过期或不存在时，才标记 interrupted
	res, err := d.Exec(`
		UPDATE tbl_job_runs r
		SET status='interrupted',
		    error=CASE WHEN error='' THEN 'lease expired / process lost' ELSE error END,
		    ended_at=?
		WHERE r.status='running' AND r.started_at<?
		  AND NOT EXISTS (
		    SELECT 1 FROM tbl_job_leases l
		    WHERE l.job_key=r.job_key AND l.expires_at > NOW() AND l.owner_id=r.owner_id
		  )`,
		time.Now().UTC().Format(time.RFC3339), cutoff,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AcquireJobLease 抢占或续自己的租约。成功返回 fencing_token；失败返回 ok=false。
func (d *DB) AcquireJobLease(jobKey, ownerID, ownerHost string, ownerPID int, ttlSec int) (token int64, ok bool, err error) {
	if ttlSec <= 0 {
		ttlSec = 30
	}
	err = d.QueryRow(`
		INSERT INTO tbl_job_leases(job_key, owner_id, owner_host, owner_pid, fencing_token, expires_at, heartbeat_at, run_id)
		VALUES(?,?,?,?,1, NOW() + make_interval(secs => ?::int), NOW(), '')
		ON CONFLICT(job_key) DO UPDATE SET
			owner_id = EXCLUDED.owner_id,
			owner_host = EXCLUDED.owner_host,
			owner_pid = EXCLUDED.owner_pid,
			fencing_token = CASE
				WHEN tbl_job_leases.owner_id = EXCLUDED.owner_id THEN tbl_job_leases.fencing_token
				ELSE tbl_job_leases.fencing_token + 1
			END,
			expires_at = NOW() + make_interval(secs => ?::int),
			heartbeat_at = NOW(),
			run_id = CASE
				WHEN tbl_job_leases.owner_id = EXCLUDED.owner_id THEN tbl_job_leases.run_id
				ELSE ''
			END
		WHERE tbl_job_leases.expires_at < NOW()
		   OR tbl_job_leases.owner_id = EXCLUDED.owner_id
		RETURNING fencing_token`,
		jobKey, ownerID, ownerHost, ownerPID, ttlSec, ttlSec,
	).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return token, true, nil
}

func (d *DB) HeartbeatJobLease(jobKey, ownerID string, fencingToken int64, ttlSec int) (bool, error) {
	if ttlSec <= 0 {
		ttlSec = 30
	}
	res, err := d.Exec(`
		UPDATE tbl_job_leases
		SET expires_at = NOW() + make_interval(secs => ?::int),
		    heartbeat_at = NOW()
		WHERE job_key=? AND owner_id=? AND fencing_token=?`,
		ttlSec, jobKey, ownerID, fencingToken,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (d *DB) ReleaseJobLease(jobKey, ownerID string, fencingToken int64) error {
	_, err := d.Exec(`
		UPDATE tbl_job_leases
		SET expires_at = NOW(),
		    heartbeat_at = NOW(),
		    run_id = ''
		WHERE job_key=? AND owner_id=? AND fencing_token=?`,
		jobKey, ownerID, fencingToken,
	)
	return err
}

func (d *DB) BindJobLeaseRun(jobKey, ownerID string, fencingToken int64, runID string) error {
	_, err := d.Exec(`
		UPDATE tbl_job_leases SET run_id=?
		WHERE job_key=? AND owner_id=? AND fencing_token=?`,
		runID, jobKey, ownerID, fencingToken,
	)
	return err
}

func (d *DB) GetJobLease(jobKey string) (*JobLease, error) {
	var l JobLease
	var held bool
	err := d.QueryRow(`
		SELECT job_key, owner_id, owner_host, owner_pid, fencing_token,
		       expires_at::text, heartbeat_at::text, run_id,
		       (expires_at > NOW()) AS held
		FROM tbl_job_leases WHERE job_key=?`, jobKey).Scan(
		&l.JobKey, &l.OwnerID, &l.OwnerHost, &l.OwnerPID, &l.FencingToken,
		&l.ExpiresAt, &l.HeartbeatAt, &l.RunID, &held,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.Held = held
	return &l, nil
}

func (d *DB) ListActiveJobLeaseOwners(within time.Duration) ([]JobLease, error) {
	if within <= 0 {
		within = 2 * time.Minute
	}
	sec := int(within.Seconds())
	rows, err := d.Query(`
		SELECT DISTINCT ON (owner_id) job_key, owner_id, owner_host, owner_pid, fencing_token,
		       expires_at::text, heartbeat_at::text, run_id, (expires_at > NOW())
		FROM tbl_job_leases
		WHERE heartbeat_at > NOW() - make_interval(secs => ?::int)
		ORDER BY owner_id, heartbeat_at DESC`, sec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobLease
	for rows.Next() {
		var l JobLease
		if err := rows.Scan(&l.JobKey, &l.OwnerID, &l.OwnerHost, &l.OwnerPID, &l.FencingToken,
			&l.ExpiresAt, &l.HeartbeatAt, &l.RunID, &l.Held); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) PurgeOldJobFireLedger(before time.Time) (int64, error) {
	res, err := d.Exec(`DELETE FROM tbl_job_fire_ledger WHERE triggered_at < ?`, before.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type JobAudit struct {
	ID        string `json:"id"`
	JobKey    string `json:"job_key"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (d *DB) AppendJobAudit(jobKey, action, actor, detail string) error {
	id := fmt.Sprintf("ja-%d", time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`
		INSERT INTO tbl_job_audits(id, job_key, action, actor, detail, created_at)
		VALUES(?,?,?,?,?,?)`,
		id, strings.TrimSpace(jobKey), strings.TrimSpace(action), strings.TrimSpace(actor), strings.TrimSpace(detail), now,
	)
	return err
}

func (d *DB) ListJobAudits(jobKey string, limit int) ([]JobAudit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, job_key, action, actor, detail, created_at FROM tbl_job_audits`
	args := []any{}
	if strings.TrimSpace(jobKey) != "" {
		q += ` WHERE job_key=?`
		args = append(args, strings.TrimSpace(jobKey))
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobAudit
	for rows.Next() {
		var a JobAudit
		if err := rows.Scan(&a.ID, &a.JobKey, &a.Action, &a.Actor, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReleaseJobLeasesByOwner 将本进程持有的未过期租约立即过期（优雅退出）。
func (d *DB) ReleaseJobLeasesByOwner(ownerID string) (int64, error) {
	res, err := d.Exec(`
		UPDATE tbl_job_leases
		SET expires_at = NOW(), heartbeat_at = NOW(), run_id = ''
		WHERE owner_id=? AND expires_at > NOW()`, ownerID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListDueScheduledIssues 已通过且 scheduled_at 已到期、仍待执行的工单。
func (d *DB) ListDueScheduledIssues(now time.Time, limit int) ([]Issue, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.Query(`
		SELECT id, ticket_no, issue_type, instance_id, COALESCE(domain,''), database_id, db_name, sql_content, description,
		       status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, execute_result, created_at, updated_at
		FROM tbl_issues
		WHERE status=? AND TRIM(scheduled_at)<>''
		ORDER BY updated_at ASC
		LIMIT 500`,
		StatusApproved,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var it Issue
		var exec sql.NullString
		if err := rows.Scan(
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.Domain, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &exec,
			&it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if exec.Valid {
			it.ExecuteResult = exec.String
		}
		it.Type = it.IssueType
		if strings.TrimSpace(it.ScheduledAt) == "" {
			continue
		}
		// 未到期则跳过；到期（或解析失败按 DueOrEmpty）纳入
		if !ScheduledAtDueOrEmpty(it.ScheduledAt, now) {
			continue
		}
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}
