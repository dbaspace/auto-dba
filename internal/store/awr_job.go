package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AWR 生成任务状态。
const (
	AWRJobPending     = "pending"
	AWRJobRunning     = "running"
	AWRJobPaused      = "paused"
	AWRJobSucceeded   = "succeeded"
	AWRJobFailed      = "failed"
	AWRJobCanceled    = "canceled"
	AWRJobInterrupted = "interrupted"
)

// AWR 任务范围。
const (
	AWRJobScopeAll      = "all"
	AWRJobScopeInstance = "instance"
)

// AWRGenerateJob 报告生成任务行。
type AWRGenerateJob struct {
	ID              string     `json:"id"`
	Scope           string     `json:"scope"`
	ReportDate      string     `json:"report_date"`
	InstanceID      string     `json:"instance_id"`
	Host            string     `json:"host"`
	Port            int64      `json:"port"`
	State           string     `json:"state"`
	InstancesTotal  int        `json:"instances_total"`
	InstancesDone   int        `json:"instances_done"`
	CurrentInstance string     `json:"current_instance"`
	CurrentStage    string     `json:"current_stage"`
	ReportsWritten  int        `json:"reports_written"`
	FailedCount     int        `json:"failed_count"`
	Error           string     `json:"error"`
	CreatedBy       string     `json:"created_by"`
	StartedAt       *time.Time `json:"started_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

const awrJobSelectCols = `
SELECT id, scope, report_date, COALESCE(instance_id,''), COALESCE(host,''), COALESCE(port,0), state,
       instances_total, instances_done, COALESCE(current_instance,''), COALESCE(current_stage,''),
       reports_written, failed_count, COALESCE(error,''), COALESCE(created_by,''),
       started_at, updated_at, finished_at, created_at
FROM tbl_db_awr_generate_jobs`

// CreateAWRGenerateJob 插入任务。
func (d *DB) CreateAWRGenerateJob(ctx context.Context, j *AWRGenerateJob) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_db_awr_generate_jobs (
  id, scope, report_date, instance_id, host, port, state,
  instances_total, instances_done, current_instance, current_stage,
  reports_written, failed_count, error, created_by,
  started_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`),
		j.ID, j.Scope, j.ReportDate, j.InstanceID, j.Host, j.Port, j.State,
		j.InstancesTotal, j.InstancesDone, j.CurrentInstance, j.CurrentStage,
		j.ReportsWritten, j.FailedCount, j.Error, j.CreatedBy,
		j.StartedAt, j.UpdatedAt,
	)
	return err
}

// UpdateAWRGenerateJobProgress 更新进度。
func (d *DB) UpdateAWRGenerateJobProgress(ctx context.Context, j *AWRGenerateJob) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_db_awr_generate_jobs SET
  state=?, instances_total=?, instances_done=?, current_instance=?, current_stage=?,
  reports_written=?, failed_count=?, started_at=COALESCE(started_at,?), updated_at=NOW()
WHERE id=?`),
		j.State, j.InstancesTotal, j.InstancesDone, j.CurrentInstance, j.CurrentStage,
		j.ReportsWritten, j.FailedCount, j.StartedAt, j.ID,
	)
	return err
}

// SetAWRGenerateJobState 更新状态；finished=true 时置 finished_at。
func (d *DB) SetAWRGenerateJobState(ctx context.Context, id, state, errMsg string, finished bool) error {
	if finished {
		_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_db_awr_generate_jobs SET state=?, error=?, finished_at=NOW(), updated_at=NOW() WHERE id=?`),
			state, errMsg, id)
		return err
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_db_awr_generate_jobs SET state=?, error=?, updated_at=NOW() WHERE id=?`),
		state, errMsg, id)
	return err
}

// GetAWRGenerateJob 按 id 查询。
func (d *DB) GetAWRGenerateJob(ctx context.Context, id string) (*AWRGenerateJob, error) {
	row := d.db.QueryRowContext(ctx, d.rewrite(awrJobSelectCols+" WHERE id=?"), id)
	j, err := scanAWRGenerateJob(row.Scan)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job not found: %s", id)
	}
	return j, err
}

// ListRecentAWRGenerateJobs 近期任务。
func (d *DB) ListRecentAWRGenerateJobs(ctx context.Context, limit int) ([]*AWRGenerateJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx, d.rewrite(awrJobSelectCols+" ORDER BY created_at DESC LIMIT ?"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AWRGenerateJob
	for rows.Next() {
		j, e := scanAWRGenerateJob(rows.Scan)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkAWRGenerateJobsInterrupted 启动时把遗留活跃任务置 interrupted。
func (d *DB) MarkAWRGenerateJobsInterrupted(ctx context.Context) (int64, error) {
	res, err := d.db.ExecContext(ctx, `
UPDATE tbl_db_awr_generate_jobs
SET state='interrupted', finished_at=NOW(), updated_at=NOW()
WHERE state IN ('pending','running','paused')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanAWRGenerateJob(scan func(dest ...any) error) (*AWRGenerateJob, error) {
	var (
		j          AWRGenerateJob
		reportDate sql.NullTime
		startedAt  sql.NullTime
		updatedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	if err := scan(
		&j.ID, &j.Scope, &reportDate, &j.InstanceID, &j.Host, &j.Port, &j.State,
		&j.InstancesTotal, &j.InstancesDone, &j.CurrentInstance, &j.CurrentStage,
		&j.ReportsWritten, &j.FailedCount, &j.Error, &j.CreatedBy,
		&startedAt, &updatedAt, &finishedAt, &j.CreatedAt,
	); err != nil {
		return nil, err
	}
	if reportDate.Valid {
		j.ReportDate = reportDate.Time.Format("2006-01-02")
	} else if s := strings.TrimSpace(j.ReportDate); s != "" {
		j.ReportDate = s
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
