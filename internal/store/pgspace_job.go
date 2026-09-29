package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// 表空间采集任务状态。
const (
	SpaceJobPending     = "pending"
	SpaceJobRunning     = "running"
	SpaceJobPaused      = "paused"
	SpaceJobSucceeded   = "succeeded"
	SpaceJobFailed      = "failed"
	SpaceJobCanceled    = "canceled"
	SpaceJobInterrupted = "interrupted"
)

// 任务范围。
const (
	SpaceJobScopeAll      = "all"
	SpaceJobScopeInstance = "instance"
)

// SpaceCollectJob 采集任务行。
type SpaceCollectJob struct {
	ID              string     `json:"id"`
	Scope           string     `json:"scope"`
	InstanceID      string     `json:"instance_id"`
	Host            string     `json:"host"`
	Port            int64      `json:"port"`
	State           string     `json:"state"`
	InstancesTotal  int        `json:"instances_total"`
	InstancesDone   int        `json:"instances_done"`
	CurrentInstance string     `json:"current_instance"`
	CurrentStage    string     `json:"current_stage"`
	DbTotal         int        `json:"db_total"`
	DbDone          int        `json:"db_done"`
	CurrentDb       string     `json:"current_db"`
	RowsWritten     int        `json:"rows_written"`
	Error           string     `json:"error"`
	CreatedBy       string     `json:"created_by"`
	StartedAt       *time.Time `json:"started_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

const spaceJobSelectCols = `
SELECT id, scope, COALESCE(instance_id,''), COALESCE(host,''), COALESCE(port,0), state,
       instances_total, instances_done, COALESCE(current_instance,''), COALESCE(current_stage,''),
       db_total, db_done, COALESCE(current_db,''), rows_written, COALESCE(error,''), COALESCE(created_by,''),
       started_at, updated_at, finished_at, created_at
FROM tbl_pg_space_collect_jobs`

// CreateSpaceCollectJob 插入采集任务。
func (d *DB) CreateSpaceCollectJob(ctx context.Context, j *SpaceCollectJob) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_pg_space_collect_jobs (
  id, scope, instance_id, host, port, state,
  instances_total, instances_done, current_instance, current_stage,
  db_total, db_done, current_db, rows_written, error, created_by,
  started_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`),
		j.ID, j.Scope, j.InstanceID, j.Host, j.Port, j.State,
		j.InstancesTotal, j.InstancesDone, j.CurrentInstance, j.CurrentStage,
		j.DbTotal, j.DbDone, j.CurrentDb, j.RowsWritten, j.Error, j.CreatedBy,
		j.StartedAt, j.UpdatedAt,
	)
	return err
}

// UpdateSpaceCollectJobProgress 更新进度。
func (d *DB) UpdateSpaceCollectJobProgress(ctx context.Context, j *SpaceCollectJob) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_pg_space_collect_jobs SET
  state=?, instances_total=?, instances_done=?, current_instance=?, current_stage=?,
  db_total=?, db_done=?, current_db=?, rows_written=?, started_at=COALESCE(started_at,?), updated_at=NOW()
WHERE id=?`),
		j.State, j.InstancesTotal, j.InstancesDone, j.CurrentInstance, j.CurrentStage,
		j.DbTotal, j.DbDone, j.CurrentDb, j.RowsWritten, j.StartedAt, j.ID,
	)
	return err
}

// SetSpaceCollectJobState 更新状态；finished=true 时置 finished_at。
func (d *DB) SetSpaceCollectJobState(ctx context.Context, id, state, errMsg string, finished bool) error {
	if finished {
		_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_pg_space_collect_jobs SET state=?, error=?, finished_at=NOW(), updated_at=NOW() WHERE id=?`),
			state, errMsg, id)
		return err
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_pg_space_collect_jobs SET state=?, error=?, updated_at=NOW() WHERE id=?`),
		state, errMsg, id)
	return err
}

// GetSpaceCollectJob 按 id 查询。
func (d *DB) GetSpaceCollectJob(ctx context.Context, id string) (*SpaceCollectJob, error) {
	row := d.db.QueryRowContext(ctx, d.rewrite(spaceJobSelectCols+" WHERE id=?"), id)
	j, err := scanSpaceCollectJob(row.Scan)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	return j, err
}

// ListRecentSpaceCollectJobs 近期任务列表。
func (d *DB) ListRecentSpaceCollectJobs(ctx context.Context, limit int) ([]*SpaceCollectJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx, d.rewrite(spaceJobSelectCols+" ORDER BY created_at DESC LIMIT ?"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SpaceCollectJob
	for rows.Next() {
		j, e := scanSpaceCollectJob(rows.Scan)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkSpaceCollectJobsInterrupted 启动时把遗留活跃任务置 interrupted。
func (d *DB) MarkSpaceCollectJobsInterrupted(ctx context.Context) (int64, error) {
	res, err := d.db.ExecContext(ctx, `
UPDATE tbl_pg_space_collect_jobs
SET state='interrupted', finished_at=NOW(), updated_at=NOW()
WHERE state IN ('pending','running','paused')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanSpaceCollectJob(scan func(dest ...any) error) (*SpaceCollectJob, error) {
	var (
		j          SpaceCollectJob
		startedAt  sql.NullTime
		updatedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	if err := scan(
		&j.ID, &j.Scope, &j.InstanceID, &j.Host, &j.Port, &j.State,
		&j.InstancesTotal, &j.InstancesDone, &j.CurrentInstance, &j.CurrentStage,
		&j.DbTotal, &j.DbDone, &j.CurrentDb, &j.RowsWritten, &j.Error, &j.CreatedBy,
		&startedAt, &updatedAt, &finishedAt, &j.CreatedAt,
	); err != nil {
		return nil, err
	}
	j.InstanceID = strings.TrimSpace(j.InstanceID)
	if startedAt.Valid {
		j.StartedAt = &startedAt.Time
	}
	if updatedAt.Valid {
		j.UpdatedAt = &updatedAt.Time
	}
	if finishedAt.Valid {
		j.FinishedAt = &finishedAt.Time
	}
	return &j, nil
}
