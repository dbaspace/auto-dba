package pgspace

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/store"
)

const jobAllKey = "__all__"

type jobCtl struct {
	id     string
	key    string
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	paused   bool
	resumeCh chan struct{}
}

func (c *jobCtl) pause() {
	c.mu.Lock()
	if !c.paused {
		c.paused = true
		if c.resumeCh == nil {
			c.resumeCh = make(chan struct{})
		}
	}
	c.mu.Unlock()
}

func (c *jobCtl) resume() {
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

func (c *jobCtl) waitIfPaused(ctx context.Context) error {
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

type jobManager struct {
	mu    sync.Mutex
	byID  map[string]*jobCtl
	byKey map[string]string
}

func newJobManager() *jobManager {
	return &jobManager{byID: make(map[string]*jobCtl), byKey: make(map[string]string)}
}

func (m *jobManager) get(id string) *jobCtl {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

func (m *jobManager) cleanup(id, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
	if cur, ok := m.byKey[key]; ok && cur == id {
		delete(m.byKey, key)
	}
}

type collectReporter struct {
	mu        sync.Mutex
	row       *store.SpaceCollectJob
	ctl       *jobCtl
	writeCtx  context.Context
	store     *store.DB
	lastFlush time.Time
}

func (r *collectReporter) flushLocked(force bool) {
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
	_ = r.store.UpdateSpaceCollectJobProgress(r.writeCtx, r.row)
}

func (r *collectReporter) instanceStart(ctx context.Context, idx, total int, host string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.row.State = store.SpaceJobRunning
	r.row.InstancesTotal = total
	r.row.CurrentInstance = host
	r.row.CurrentStage = "cluster"
	r.row.DbTotal = 0
	r.row.DbDone = 0
	r.row.CurrentDb = ""
	r.flushLocked(true)
	r.mu.Unlock()
}

func (r *collectReporter) instanceDone(ctx context.Context, done int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.row.InstancesDone = done
	r.row.CurrentStage = "done"
	r.row.DbDone = r.row.DbTotal
	r.flushLocked(true)
	r.mu.Unlock()
}

func (r *collectReporter) addRows(ctx context.Context, n int) {
	if r == nil || n == 0 {
		return
	}
	r.mu.Lock()
	r.row.RowsWritten += n
	r.flushLocked(false)
	r.mu.Unlock()
}

func (r *collectReporter) progressFn(ctx context.Context) progressFn {
	if r == nil {
		return nil
	}
	return func(stage string, dbDone, dbTotal int, currentDB string) error {
		if err := r.ctl.waitIfPaused(ctx); err != nil {
			return err
		}
		r.mu.Lock()
		r.row.State = store.SpaceJobRunning
		r.row.CurrentStage = stage
		r.row.DbTotal = dbTotal
		r.row.DbDone = dbDone
		r.row.CurrentDb = currentDB
		r.flushLocked(false)
		r.mu.Unlock()
		return ctx.Err()
	}
}

// StartCollectJob 启动异步采集。
func (s *Service) StartCollectJob(ctx context.Context, instanceID, host string, port int64, createdBy string) (*JobStartData, error) {
	instanceID = strings.TrimSpace(instanceID)
	host = strings.TrimSpace(host)

	scope := store.SpaceJobScopeAll
	key := jobAllKey
	var physicals []physicalInstance
	var jInstanceID, jHost string
	var jPort int64

	if instanceID != "" || (host != "" && port > 0) {
		scope = store.SpaceJobScopeInstance
		t, err := s.resolveTarget(instanceID, host, port)
		if err != nil {
			return nil, err
		}
		physicals = []physicalInstance{{target: t}}
		jInstanceID = t.inst.ID
		jHost = t.host
		jPort = t.port
		key = connectionKey(t.host, t.port)
	}

	m := s.jobs()
	m.mu.Lock()
	if existing, ok := m.byKey[key]; ok {
		m.mu.Unlock()
		state := store.SpaceJobRunning
		if row, e := s.Store.GetSpaceCollectJob(ctx, existing); e == nil && row != nil {
			state = row.State
		}
		return &JobStartData{JobID: existing, State: state, Reused: true}, nil
	}

	now := time.Now()
	job := &store.SpaceCollectJob{
		ID:         uuid.NewString(),
		Scope:      scope,
		InstanceID: jInstanceID,
		Host:       jHost,
		Port:       jPort,
		State:      store.SpaceJobPending,
		CreatedBy:  strings.TrimSpace(createdBy),
		StartedAt:  &now,
	}
	if err := s.Store.CreateSpaceCollectJob(ctx, job); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	ctl := &jobCtl{id: job.ID, key: key, ctx: jobCtx, cancel: cancel}
	m.byID[job.ID] = ctl
	m.byKey[key] = job.ID
	m.mu.Unlock()

	go s.runJob(ctl, job, physicals)
	return &JobStartData{JobID: job.ID, State: store.SpaceJobPending}, nil
}

func (s *Service) runJob(ctl *jobCtl, job *store.SpaceCollectJob, physicals []physicalInstance) {
	writeCtx := context.Background()
	m := s.jobs()
	defer m.cleanup(job.ID, ctl.key)
	defer ctl.cancel()

	if job.Scope == store.SpaceJobScopeAll {
		ph, err := s.enumeratePhysicalPostgresInstances()
		if err != nil {
			_ = s.Store.SetSpaceCollectJobState(writeCtx, job.ID, store.SpaceJobFailed, "enumerate instances failed: "+err.Error(), true)
			return
		}
		physicals = ph
	}

	job.State = store.SpaceJobRunning
	job.InstancesTotal = len(physicals)
	_ = s.Store.UpdateSpaceCollectJobProgress(writeCtx, job)

	reporter := &collectReporter{row: job, ctl: ctl, writeCtx: writeCtx, store: s.Store}
	_, _, rerr := s.runCollectTargets(ctl.ctx, physicals, reporter)

	finalState := store.SpaceJobSucceeded
	errMsg := ""
	switch {
	case ctl.ctx.Err() != nil:
		finalState = store.SpaceJobCanceled
	case rerr != nil:
		finalState = store.SpaceJobFailed
		errMsg = rerr.Error()
	}
	_ = s.Store.SetSpaceCollectJobState(writeCtx, job.ID, finalState, errMsg, true)
}

// GetJob 查询任务。
func (s *Service) GetJob(ctx context.Context, id string) (*store.SpaceCollectJob, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("job_id required")
	}
	return s.Store.GetSpaceCollectJob(ctx, id)
}

// ListJobs 近期任务。
func (s *Service) ListJobs(ctx context.Context, limit int) ([]*store.SpaceCollectJob, error) {
	return s.Store.ListRecentSpaceCollectJobs(ctx, limit)
}

// PauseJob 暂停。
func (s *Service) PauseJob(ctx context.Context, id string) (*JobControlData, error) {
	ctl := s.jobs().get(strings.TrimSpace(id))
	if ctl == nil {
		return nil, fmt.Errorf("job not running: %s", id)
	}
	ctl.pause()
	_ = s.Store.SetSpaceCollectJobState(ctx, ctl.id, store.SpaceJobPaused, "", false)
	return &JobControlData{JobID: ctl.id, State: store.SpaceJobPaused}, nil
}

// ResumeJob 继续。
func (s *Service) ResumeJob(ctx context.Context, id string) (*JobControlData, error) {
	ctl := s.jobs().get(strings.TrimSpace(id))
	if ctl == nil {
		return nil, fmt.Errorf("job not running: %s", id)
	}
	ctl.resume()
	_ = s.Store.SetSpaceCollectJobState(ctx, ctl.id, store.SpaceJobRunning, "", false)
	return &JobControlData{JobID: ctl.id, State: store.SpaceJobRunning}, nil
}

// CancelJob 终止。
func (s *Service) CancelJob(ctx context.Context, id string) (*JobControlData, error) {
	ctl := s.jobs().get(strings.TrimSpace(id))
	if ctl == nil {
		return nil, fmt.Errorf("job not running: %s", id)
	}
	ctl.resume()
	ctl.cancel()
	_ = s.Store.SetSpaceCollectJobState(ctx, ctl.id, store.SpaceJobCanceled, "", true)
	return &JobControlData{JobID: ctl.id, State: store.SpaceJobCanceled}, nil
}

// MarkJobsInterrupted 进程启动时标记中断。
func (s *Service) MarkJobsInterrupted(ctx context.Context) {
	_, _ = s.Store.MarkSpaceCollectJobsInterrupted(ctx)
}

var globalJobMgr = newJobManager()

func (s *Service) jobs() *jobManager {
	return globalJobMgr
}
