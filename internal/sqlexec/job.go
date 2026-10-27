package sqlexec

import (
	"context"
	"sync"
	"time"

	"auto-dba/internal/ticketexec"
)

const (
	JobRunning = "running"
	JobDone    = "done"
	JobStopped = "stopped"
)

// Target 是执行器要在当地连上的一个库。
type Target struct {
	InstanceID    string `json:"instance_id"`
	InstanceName  string `json:"instance_name"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	DBName        string `json:"db_name"`
	DBType        string `json:"db_type"`
	Env           string `json:"env"`
	LockTimeoutMS int64  `json:"lock_timeout_ms,omitempty"`
}

// JobRequest 一次下发。密码只在内存里用，执行器不落盘。
type JobRequest struct {
	IssueType string   `json:"issue_type"`
	SQL       string   `json:"sql"`
	Targets   []Target `json:"targets"`
}

// DBResult 一个库跑完后的结果。Records 是该库的全部语句，不含数据库密码。
type DBResult struct {
	InstanceID   string              `json:"instance_id"`
	InstanceName string              `json:"instance_name"`
	Host         string              `json:"host"`
	DB           string              `json:"db"`
	OK           bool                `json:"ok"`
	SuccessCount int                 `json:"success_count"`
	FailCount    int                 `json:"fail_count"`
	Error        string              `json:"error,omitempty"`
	Records      []ticketexec.Record `json:"records,omitempty"`
}

// JobView 是轮询看到的进度。
type JobView struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	Message         string     `json:"message,omitempty"`
	Done            int        `json:"done"`
	Total           int        `json:"total"`
	Failed          int        `json:"failed"`
	CurrentInstance string     `json:"current_instance,omitempty"`
	CurrentDB       string     `json:"current_db,omitempty"`
	StmtIndex       int        `json:"stmt_index"`
	StmtTotal       int        `json:"stmt_total"`
	SessionID       int64      `json:"session_id,omitempty"`
	Engine          string     `json:"engine,omitempty"`
	StartedAt       string     `json:"started_at,omitempty"`
	EndedAt         string     `json:"ended_at,omitempty"`
	Results         []DBResult `json:"results"`
	// Incremental 为真时 Results 只含 after 之后新完成的库。旧执行器没有该字段。
	Incremental bool `json:"incremental,omitempty"`
}

// jobRetain 终态任务在内存中保留的时间。过期后按 jobID 查询会返回不存在。
const jobRetain = 24 * time.Hour

type runningJob struct {
	mu        sync.Mutex
	view      JobView
	cancel    context.CancelFunc
	current   ticketexec.Target
	sessionID int64
	started   time.Time
	ended     time.Time
}

// snapshot 只带下标 >= after 的库。已经交给控制面的库不再重复带语句。
func (j *runningJob) snapshot(after int) JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.view
	if after < 0 {
		after = 0
	}
	if after > len(j.view.Results) {
		after = len(j.view.Results)
	}
	out.Results = append([]DBResult(nil), j.view.Results[after:]...)
	out.Done = len(j.view.Results)
	out.Incremental = true
	if !j.started.IsZero() {
		out.StartedAt = j.started.UTC().Format(time.RFC3339)
	}
	if !j.ended.IsZero() {
		out.EndedAt = j.ended.UTC().Format(time.RFC3339)
	}
	return out
}

// Runner 在执行器进程内串行跑完任务里的库。
type Runner struct {
	mu   sync.Mutex
	jobs map[string]*runningJob
}

func NewRunner() *Runner {
	return &Runner{jobs: map[string]*runningJob{}}
}

func (r *Runner) Submit(id string, req JobRequest) JobView {
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	job := &runningJob{
		cancel:  cancel,
		started: now,
		view: JobView{
			ID: id, Status: JobRunning, Total: len(req.Targets), Results: []DBResult{},
			StartedAt: now.UTC().Format(time.RFC3339),
		},
	}
	r.mu.Lock()
	r.sweepLocked(now)
	r.jobs[id] = job
	r.mu.Unlock()
	go r.run(ctx, job, req)
	return job.snapshot(0)
}

func (r *Runner) Get(id string, after int) (JobView, bool) {
	r.mu.Lock()
	r.sweepLocked(time.Now())
	job := r.jobs[id]
	r.mu.Unlock()
	if job == nil {
		return JobView{}, false
	}
	return job.snapshot(after), true
}

// sweepLocked 删掉已结束且超过保留期的任务。调用方须持有 r.mu。
func (r *Runner) sweepLocked(now time.Time) {
	for id, job := range r.jobs {
		job.mu.Lock()
		ended := job.ended
		status := job.view.Status
		job.mu.Unlock()
		if status != JobRunning && !ended.IsZero() && now.Sub(ended) > jobRetain {
			delete(r.jobs, id)
		}
	}
}

// Stop 取消任务，并在当地终止当前会话。终止失败会再试一次。
func (r *Runner) Stop(id string) (JobView, bool) {
	r.mu.Lock()
	job := r.jobs[id]
	r.mu.Unlock()
	if job == nil {
		return JobView{}, false
	}
	job.mu.Lock()
	target := job.current
	sid := job.sessionID
	cancel := job.cancel
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if sid > 0 {
		kctx, cancelKill := context.WithTimeout(context.Background(), 8*time.Second)
		err := ticketexec.KillSession(kctx, target, sid)
		cancelKill()
		if err != nil {
			kctx, cancelKill = context.WithTimeout(context.Background(), 8*time.Second)
			err = ticketexec.KillSession(kctx, target, sid)
			cancelKill()
		}
		if err != nil {
			job.mu.Lock()
			job.view.Message = "终止会话失败: " + err.Error()
			job.mu.Unlock()
		}
	}
	return job.snapshot(1 << 30), true
}

func (r *Runner) run(ctx context.Context, job *runningJob, req JobRequest) {
	defer func() {
		job.mu.Lock()
		if job.view.Status == JobRunning {
			job.view.Status = JobDone
		}
		if job.ended.IsZero() {
			job.ended = time.Now()
		}
		job.sessionID = 0
		job.mu.Unlock()
	}()
	for _, raw := range req.Targets {
		if ctx.Err() != nil {
			job.mu.Lock()
			job.view.Status = JobStopped
			job.mu.Unlock()
			return
		}
		t := ticketexec.Target{
			Host: raw.Host, Port: raw.Port, Username: raw.Username, Password: raw.Password,
			DBName: raw.DBName, DBType: raw.DBType, KeepAll: true,
			LockTimeout: time.Duration(raw.LockTimeoutMS) * time.Millisecond,
		}
		job.mu.Lock()
		job.current = t
		job.sessionID = 0
		job.view.CurrentInstance = raw.InstanceName
		job.view.CurrentDB = raw.DBName
		job.view.Engine = ticketexec.EngineOf(raw.DBType)
		job.view.StmtIndex = 0
		job.view.SessionID = 0
		job.mu.Unlock()
		t.OnProgress = func(stmtIndex, stmtTotal int, sessionID int64) {
			job.mu.Lock()
			job.sessionID = sessionID
			job.view.StmtIndex = stmtIndex
			job.view.StmtTotal = stmtTotal
			job.view.SessionID = sessionID
			job.view.CurrentDB = raw.DBName
			job.view.CurrentInstance = raw.InstanceName
			job.mu.Unlock()
		}
		res := ticketexec.Run(ctx, req.IssueType, req.SQL, t)
		if ctx.Err() != nil {
			job.mu.Lock()
			job.view.Status = JobStopped
			job.view.SessionID = 0
			job.mu.Unlock()
			return
		}
		item := DBResult{
			InstanceID: raw.InstanceID, InstanceName: raw.InstanceName, Host: raw.Host, DB: raw.DBName,
		}
		if res != nil {
			item.OK = res.Success
			item.SuccessCount = res.SuccessCount
			item.FailCount = res.FailCount
			item.Error = res.Message
			item.Records = statementRecords(raw, res.Records)
		}
		if !item.OK && item.Error == "" {
			item.Error = "执行失败"
		}
		job.mu.Lock()
		job.view.Results = append(job.view.Results, item)
		job.view.Done++
		if !item.OK {
			job.view.Failed++
			job.view.Message = item.Error
		}
		job.view.SessionID = 0
		job.sessionID = 0
		job.mu.Unlock()
	}
}

func statementRecords(raw Target, recs []ticketexec.Record) []ticketexec.Record {
	if len(recs) == 0 {
		return nil
	}
	out := make([]ticketexec.Record, len(recs))
	for i, rec := range recs {
		rec.InstanceID = raw.InstanceID
		rec.InstanceName = raw.InstanceName
		rec.Username = ""
		out[i] = rec
	}
	return out
}
