package awr

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/store"
)

const awrJobAllKey = "__all__"

type awrJobCtl struct {
	id     string
	key    string
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	paused   bool
	resumeCh chan struct{}
}

func (c *awrJobCtl) pause() {
	c.mu.Lock()
	if !c.paused {
		c.paused = true
		if c.resumeCh == nil {
			c.resumeCh = make(chan struct{})
		}
	}
	c.mu.Unlock()
}

func (c *awrJobCtl) resume() {
	c.mu.Lock()
	if c.paused {
		c.paused = false
		if c.resumeCh != nil {
			close(c.resumeCh)
			c.resumeCh = nil
		}
	}
	c.mu.Unlock()
}

func (c *awrJobCtl) waitIfPaused(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		if !c.paused {
			c.mu.Unlock()
			return ctx.Err()
		}
		ch := c.resumeCh
		if ch == nil {
			ch = make(chan struct{})
			c.resumeCh = ch
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

type awrJobManager struct {
	mu    sync.Mutex
	byID  map[string]*awrJobCtl
	byKey map[string]string
}

var awrJobMgr = &awrJobManager{
	byID:  make(map[string]*awrJobCtl),
	byKey: make(map[string]string),
}

func (m *awrJobManager) get(id string) *awrJobCtl {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

func (m *awrJobManager) cleanup(id, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
	if cur, ok := m.byKey[key]; ok && cur == id {
		delete(m.byKey, key)
	}
}

type awrGenerateReporter struct {
	mu        sync.Mutex
	row       *store.AWRGenerateJob
	ctl       *awrJobCtl
	writeCtx  context.Context
	store     *store.DB
	lastFlush time.Time
}

func (r *awrGenerateReporter) flushLocked(force bool) {
	if r == nil {
		return
	}
	if r.ctl != nil && r.ctl.ctx.Err() != nil {
		return
	}
	if !force && time.Since(r.lastFlush) < time.Second {
		return
	}
	r.lastFlush = time.Now()
	if err := r.store.UpdateAWRGenerateJobProgress(r.writeCtx, r.row); err != nil {
		log.Printf("awr job: update progress failed job_id=%s: %v", r.row.ID, err)
	}
}

func (r *awrGenerateReporter) setTotal(ctx context.Context, total int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.row.State = store.AWRJobRunning
	r.row.InstancesTotal = total
	r.row.CurrentStage = "generate"
	r.flushLocked(true)
	r.mu.Unlock()
}

func (r *awrGenerateReporter) checkpoint(ctx context.Context) error {
	if r == nil {
		return ctx.Err()
	}
	if err := r.ctl.waitIfPaused(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *awrGenerateReporter) instanceStart(ctx context.Context, hostPort string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.row.State = store.AWRJobRunning
	r.row.CurrentStage = "generate"
	r.row.CurrentInstance = hostPort
	r.flushLocked(false)
	r.mu.Unlock()
}

func (r *awrGenerateReporter) instanceDone(ctx context.Context, done, written, failed int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.row.InstancesDone = done
	r.row.ReportsWritten = written
	r.row.FailedCount = failed
	r.flushLocked(false)
	r.mu.Unlock()
}

func awrJobKey(day time.Time, scope, instanceID, host string, port int64) string {
	prefix := day.Format("2006-01-02") + "|"
	if scope == store.AWRJobScopeAll {
		return prefix + awrJobAllKey
	}
	if instanceID != "" {
		return prefix + "id:" + instanceID
	}
	return prefix + connectionKey(host, port)
}

func (s *Service) awrJobRegister(ctx context.Context, day time.Time, instanceID, host string, port int64, createdBy string) (*awrJobCtl, *store.AWRGenerateJob, bool, error) {
	instanceID = strings.TrimSpace(instanceID)
	host = strings.TrimSpace(host)

	scope := store.AWRJobScopeAll
	if instanceID != "" || (host != "" && port > 0) {
		scope = store.AWRJobScopeInstance
	}
	key := awrJobKey(day, scope, instanceID, host, port)

	m := awrJobMgr
	m.mu.Lock()
	if existing, ok := m.byKey[key]; ok {
		m.mu.Unlock()
		return nil, &store.AWRGenerateJob{ID: existing}, true, nil
	}

	now := time.Now()
	job := &store.AWRGenerateJob{
		ID:           uuid.NewString(),
		Scope:        scope,
		ReportDate:   day.Format("2006-01-02"),
		InstanceID:   instanceID,
		Host:         host,
		Port:         port,
		State:        store.AWRJobPending,
		CurrentStage: "enumerate",
		CreatedBy:    strings.TrimSpace(createdBy),
		StartedAt:    &now,
	}
	if err := s.Store.CreateAWRGenerateJob(ctx, job); err != nil {
		m.mu.Unlock()
		return nil, nil, false, err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	ctl := &awrJobCtl{id: job.ID, key: key, ctx: jobCtx, cancel: cancel}
	m.byID[job.ID] = ctl
	m.byKey[key] = job.ID
	m.mu.Unlock()
	return ctl, job, false, nil
}

// StartGenerateJob 异步启动生成。
func (s *Service) StartGenerateJob(ctx context.Context, req *GenerateReq, createdBy string) (*JobStartData, error) {
	if req == nil {
		req = &GenerateReq{}
	}
	day := awrGenerateDay(req.Date)

	ctl, job, reused, err := s.awrJobRegister(ctx, day, req.InstanceID, req.Host, req.Port, createdBy)
	if err != nil {
		return nil, err
	}
	if reused {
		state := store.AWRJobRunning
		if row, e := s.Store.GetAWRGenerateJob(ctx, job.ID); e == nil && row != nil {
			state = row.State
		}
		return &JobStartData{JobID: job.ID, State: state, Reused: true}, nil
	}

	go s.runGenerateJob(ctl, job, day)
	return &JobStartData{JobID: job.ID, State: store.AWRJobPending}, nil
}

func (s *Service) runGenerateJob(ctl *awrJobCtl, job *store.AWRGenerateJob, day time.Time) {
	writeCtx := context.Background()
	defer awrJobMgr.cleanup(job.ID, ctl.key)
	defer ctl.cancel()

	finalState, errMsg := s.executeGenerateJob(ctl, job, day, writeCtx)
	if err := s.Store.SetAWRGenerateJobState(writeCtx, job.ID, finalState, errMsg, true); err != nil {
		log.Printf("awr job: set final state failed job_id=%s: %v", job.ID, err)
	}
	log.Printf("awr job finished job_id=%s state=%s written=%d failed=%d",
		job.ID, finalState, job.ReportsWritten, job.FailedCount)
}

func (s *Service) executeGenerateJob(ctl *awrJobCtl, job *store.AWRGenerateJob, day time.Time, writeCtx context.Context) (string, string) {
	job.State = store.AWRJobRunning
	_ = s.Store.UpdateAWRGenerateJobProgress(writeCtx, job)

	reporter := &awrGenerateReporter{row: job, ctl: ctl, writeCtx: writeCtx, store: s.Store}
	_, _, _, rerr := s.runGenerateWithReporter(ctl.ctx, day, job.InstanceID, job.Host, job.Port, reporter)

	switch {
	case ctl.ctx.Err() != nil:
		return store.AWRJobCanceled, ""
	case rerr != nil:
		return store.AWRJobFailed, rerr.Error()
	default:
		job.CurrentStage = "done"
		return store.AWRJobSucceeded, ""
	}
}

// RunGenerateJobSync 同步执行（供每日 cron）。
func (s *Service) RunGenerateJobSync(ctx context.Context, day time.Time, createdBy string) (processed, written, failed int, err error) {
	ctl, job, reused, err := s.awrJobRegister(ctx, day, "", "", 0, createdBy)
	if err != nil {
		return 0, 0, 0, err
	}
	if reused {
		return 0, 0, 0, nil
	}
	writeCtx := context.Background()
	defer awrJobMgr.cleanup(job.ID, ctl.key)
	defer ctl.cancel()

	job.State = store.AWRJobRunning
	_ = s.Store.UpdateAWRGenerateJobProgress(writeCtx, job)
	reporter := &awrGenerateReporter{row: job, ctl: ctl, writeCtx: writeCtx, store: s.Store}
	processed, written, failed, err = s.runGenerateWithReporter(ctl.ctx, day, "", "", 0, reporter)

	finalState := store.AWRJobSucceeded
	errMsg := ""
	if err != nil {
		finalState = store.AWRJobFailed
		errMsg = err.Error()
	} else {
		job.CurrentStage = "done"
	}
	_ = s.Store.SetAWRGenerateJobState(writeCtx, job.ID, finalState, errMsg, true)
	return processed, written, failed, err
}

// GetGenerateJob 查询任务。
func (s *Service) GetGenerateJob(ctx context.Context, id string) (*GenerateJobItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("job_id required")
	}
	row, err := s.Store.GetAWRGenerateJob(ctx, id)
	if err != nil {
		return nil, err
	}
	return jobRowToItem(row), nil
}

// ListGenerateJobs 近期任务。
func (s *Service) ListGenerateJobs(ctx context.Context, limit int) ([]*GenerateJobItem, error) {
	rows, err := s.Store.ListRecentAWRGenerateJobs(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*GenerateJobItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, jobRowToItem(r))
	}
	return out, nil
}

// PauseGenerateJob 暂停。
func (s *Service) PauseGenerateJob(ctx context.Context, id string) (*JobControlData, error) {
	ctl := awrJobMgr.get(strings.TrimSpace(id))
	if ctl == nil {
		return nil, fmt.Errorf("job not running: %s", id)
	}
	ctl.pause()
	_ = s.Store.SetAWRGenerateJobState(ctx, ctl.id, store.AWRJobPaused, "", false)
	return &JobControlData{JobID: ctl.id, State: store.AWRJobPaused}, nil
}

// ResumeGenerateJob 继续。
func (s *Service) ResumeGenerateJob(ctx context.Context, id string) (*JobControlData, error) {
	ctl := awrJobMgr.get(strings.TrimSpace(id))
	if ctl == nil {
		return nil, fmt.Errorf("job not running: %s", id)
	}
	ctl.resume()
	_ = s.Store.SetAWRGenerateJobState(ctx, ctl.id, store.AWRJobRunning, "", false)
	return &JobControlData{JobID: ctl.id, State: store.AWRJobRunning}, nil
}

// CancelGenerateJob 终止。
func (s *Service) CancelGenerateJob(ctx context.Context, id string) (*JobControlData, error) {
	ctl := awrJobMgr.get(strings.TrimSpace(id))
	if ctl == nil {
		return nil, fmt.Errorf("job not running: %s", id)
	}
	ctl.resume()
	ctl.cancel()
	_ = s.Store.SetAWRGenerateJobState(ctx, ctl.id, store.AWRJobCanceled, "", true)
	return &JobControlData{JobID: ctl.id, State: store.AWRJobCanceled}, nil
}

// MarkJobsInterrupted 进程启动时标记中断。
func (s *Service) MarkJobsInterrupted(ctx context.Context) {
	n, err := s.Store.MarkAWRGenerateJobsInterrupted(ctx)
	if err != nil {
		log.Printf("awr job: mark interrupted failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("awr job: marked interrupted on startup count=%d", n)
	}
}

func jobRowToItem(r *store.AWRGenerateJob) *GenerateJobItem {
	if r == nil {
		return nil
	}
	return &GenerateJobItem{
		ID: r.ID, Scope: r.Scope, ReportDate: r.ReportDate,
		InstanceID: r.InstanceID, Host: r.Host, Port: r.Port, State: r.State,
		InstancesTotal: r.InstancesTotal, InstancesDone: r.InstancesDone,
		CurrentInstance: r.CurrentInstance, CurrentStage: r.CurrentStage,
		ReportsWritten: r.ReportsWritten, FailedCount: r.FailedCount,
		Error: r.Error, CreatedBy: r.CreatedBy,
		StartedAt: r.StartedAt, UpdatedAt: r.UpdatedAt, FinishedAt: r.FinishedAt, CreatedAt: r.CreatedAt,
	}
}
