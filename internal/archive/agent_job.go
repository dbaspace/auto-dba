package archive

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/sqlexec"
)

const archiveJobRetain = 24 * time.Hour

type archiveAgentJob struct {
	mu     sync.Mutex
	view   sqlexec.ArchiveView
	cancel context.CancelFunc
	ended  time.Time
}

// ArchiveAgent 在 sql-agent 进程里跑归档循环。任务只留在内存里。
type ArchiveAgent struct {
	mu   sync.Mutex
	jobs map[string]*archiveAgentJob
}

func NewArchiveAgent() *ArchiveAgent {
	return &ArchiveAgent{jobs: map[string]*archiveAgentJob{}}
}

func (a *ArchiveAgent) Submit(id string, req sqlexec.ArchiveRequest) sqlexec.ArchiveView {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	job := &archiveAgentJob{
		cancel: cancel,
		view:   sqlexec.ArchiveView{ID: id, Status: "running"},
	}
	a.mu.Lock()
	a.sweepLocked(time.Now())
	a.jobs[id] = job
	a.mu.Unlock()
	go a.run(ctx, job, req)
	return job.snapshot()
}

func (a *ArchiveAgent) Get(id string) (sqlexec.ArchiveView, bool) {
	a.mu.Lock()
	a.sweepLocked(time.Now())
	job := a.jobs[id]
	a.mu.Unlock()
	if job == nil {
		return sqlexec.ArchiveView{}, false
	}
	return job.snapshot(), true
}

func (a *ArchiveAgent) Stop(id string) (sqlexec.ArchiveView, bool) {
	a.mu.Lock()
	job := a.jobs[id]
	a.mu.Unlock()
	if job == nil {
		return sqlexec.ArchiveView{}, false
	}
	job.mu.Lock()
	cancel := job.cancel
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return job.snapshot(), true
}

func (a *ArchiveAgent) run(ctx context.Context, job *archiveAgentJob, req sqlexec.ArchiveRequest) {
	defer func() {
		job.mu.Lock()
		if job.view.Status == "running" {
			job.view.Status = "done"
		}
		if job.ended.IsZero() {
			job.ended = time.Now()
		}
		job.mu.Unlock()
	}()
	src, err := dbmig.Open(ctx, req.Source.DBType, req.Source.Host, req.Source.Port, req.Source.Username, req.Source.Password, req.Source.DBName)
	if err != nil {
		a.finishErr(job, ctx, err)
		return
	}
	defer src.Close()
	var dst *sql.DB
	if req.Target != nil {
		dst, err = dbmig.Open(ctx, req.Target.DBType, req.Target.Host, req.Target.Port, req.Target.Username, req.Target.Password, req.Target.DBName)
		if err != nil {
			a.finishErr(job, ctx, err)
			return
		}
		defer dst.Close()
	}
	err = RunOnDB(ctx, req.Strategy, src, dst, req.DBType, req.Table, req.Condition, req.PGDeleteMode, req.ScriptType, req.OSSBucket, req.TargetTable, req.CleanupAfter, func(p Progress) {
		a.apply(job, p)
	})
	if err != nil {
		a.finishErr(job, ctx, err)
		return
	}
	job.mu.Lock()
	job.view.Status = "done"
	job.ended = time.Now()
	job.mu.Unlock()
}

func (a *ArchiveAgent) apply(job *archiveAgentJob, p Progress) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if p.Rounds > 0 {
		job.view.Rounds = p.Rounds
	}
	if p.DeletedRows > 0 || p.Interim {
		job.view.DeletedRows = p.DeletedRows
	}
	if p.SQL != "" {
		job.view.SQLPreview = p.SQL
	}
	if p.Message != "" {
		job.view.Message = p.Message
	}
}

func (a *ArchiveAgent) finishErr(job *archiveAgentJob, ctx context.Context, err error) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.ended = time.Now()
	if ctx.Err() != nil {
		job.view.Status = "stopped"
		if job.view.Message == "" {
			job.view.Message = errStopped.Error()
		}
		return
	}
	job.view.Status = "failed"
	if err != nil {
		job.view.Error = err.Error()
	}
}

func (j *archiveAgentJob) snapshot() sqlexec.ArchiveView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.view
}

func (a *ArchiveAgent) sweepLocked(now time.Time) {
	for id, job := range a.jobs {
		job.mu.Lock()
		ended := job.ended
		status := job.view.Status
		job.mu.Unlock()
		if status != "running" && !ended.IsZero() && now.Sub(ended) > archiveJobRetain {
			delete(a.jobs, id)
		}
	}
}
