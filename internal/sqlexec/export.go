package sqlexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/dqlexport"
	"auto-dba/internal/store"
)

const (
	ExportQueued    = "queued"
	ExportQuerying  = "querying"
	ExportEncoding  = "encoding"
	ExportDone      = "done"
	ExportFailed    = "failed"
	ExportStopped   = "stopped"
	exportJobRetain = 2 * time.Hour
	exportTimeout   = 10 * time.Minute
)

// ExportRequest 执行器侧 DQL 导出任务。
type ExportRequest struct {
	Format   string `json:"format"`
	SQL      string `json:"sql"`
	Limit    int    `json:"limit"`
	TicketNo string `json:"ticket_no"`
	Target   Target `json:"target"`
}

// ExportView 轮询视图。
type ExportView struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Message   string `json:"message,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	Format    string `json:"format,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

type exportJob struct {
	mu       sync.Mutex
	view     ExportView
	cancel   context.CancelFunc
	filePath string
	started  time.Time
	ended    time.Time
}

func (j *exportJob) snapshot() ExportView {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.view
	if !j.started.IsZero() {
		out.StartedAt = j.started.UTC().Format(time.RFC3339)
	}
	if !j.ended.IsZero() {
		out.EndedAt = j.ended.UTC().Format(time.RFC3339)
	}
	return out
}

func (j *exportJob) setPhase(status, phase, msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.view.Status = status
	j.view.Phase = phase
	if msg != "" {
		j.view.Message = msg
	}
}

// ExportRunner 在 sql-agent 进程内跑导出任务。
type ExportRunner struct {
	mu      sync.Mutex
	jobs    map[string]*exportJob
	workDir string
}

func NewExportRunner(workDir string) *ExportRunner {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		workDir = filepath.Join(os.TempDir(), "sql-agent-exports")
	}
	_ = os.MkdirAll(workDir, 0o755)
	r := &ExportRunner{jobs: map[string]*exportJob{}, workDir: workDir}
	go r.reapLoop()
	return r
}

func (r *ExportRunner) Submit(id string, req ExportRequest) ExportView {
	if strings.TrimSpace(id) == "" {
		id = uuid.NewString()
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "csv"
	}
	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	j := &exportJob{
		view: ExportView{
			ID:     id,
			Status: ExportQueued,
			Phase:  ExportQueued,
			Format: format,
		},
		cancel:  cancel,
		started: time.Now(),
	}
	r.mu.Lock()
	r.jobs[id] = j
	r.mu.Unlock()

	go r.run(ctx, j, req)
	return j.snapshot()
}

func (r *ExportRunner) Get(id string) (ExportView, bool) {
	r.mu.Lock()
	j, ok := r.jobs[id]
	r.mu.Unlock()
	if !ok {
		return ExportView{}, false
	}
	return j.snapshot(), true
}

func (r *ExportRunner) FilePath(id string) (string, ExportView, bool) {
	r.mu.Lock()
	j, ok := r.jobs[id]
	r.mu.Unlock()
	if !ok {
		return "", ExportView{}, false
	}
	view := j.snapshot()
	j.mu.Lock()
	path := j.filePath
	j.mu.Unlock()
	if view.Status != ExportDone || path == "" {
		return "", view, false
	}
	return path, view, true
}

func (r *ExportRunner) Stop(id string) (ExportView, bool) {
	r.mu.Lock()
	j, ok := r.jobs[id]
	r.mu.Unlock()
	if !ok {
		return ExportView{}, false
	}
	j.cancel()
	j.setPhase(ExportStopped, ExportStopped, "已取消")
	j.mu.Lock()
	j.ended = time.Now()
	j.mu.Unlock()
	return j.snapshot(), true
}

func (r *ExportRunner) run(ctx context.Context, j *exportJob, req ExportRequest) {
	defer func() {
		if rec := recover(); rec != nil {
			j.setPhase(ExportFailed, ExportFailed, fmt.Sprintf("导出异常: %v", rec))
			j.mu.Lock()
			j.ended = time.Now()
			j.mu.Unlock()
		}
	}()
	dir := filepath.Join(r.workDir, j.view.ID)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		j.setPhase(ExportFailed, ExportFailed, "创建目录失败: "+err.Error())
		j.mu.Lock()
		j.ended = time.Now()
		j.mu.Unlock()
		return
	}

	j.setPhase("running", ExportQuerying, "正在查询")
	t := req.Target
	inst := &store.Instance{
		ID: t.InstanceID, Name: t.InstanceName, Host: t.Host, Port: t.Port,
		Username: t.Username, DBType: t.DBType, IDC: t.Env,
	}
	limit := req.Limit
	if limit <= 0 {
		var err error
		limit, err = dbprobe.RequireExportSQLLimit(req.SQL)
		if err != nil {
			j.setPhase(ExportFailed, ExportFailed, err.Error())
			j.mu.Lock()
			j.ended = time.Now()
			j.mu.Unlock()
			return
		}
	}
	res, err := dbprobe.ExecExport(ctx, inst, t.Password, t.DBName, req.SQL, limit)
	if err != nil {
		msg := err.Error()
		if ctx.Err() != nil {
			msg = "导出超时或已取消: " + msg
		}
		j.setPhase(ExportFailed, ExportFailed, msg)
		_ = os.RemoveAll(dir)
		j.mu.Lock()
		j.ended = time.Now()
		j.mu.Unlock()
		return
	}
	if res != nil {
		res.AppliedLimit = limit
		res.Limit = dbprobe.MaxExportLimit
	}

	j.setPhase("running", ExportEncoding, "正在编码")
	table := dqlexport.TableNameFromSQL(req.SQL)
	body, ext, _ := dqlexport.Encode(res, req.Format, table, strings.ToLower(t.DBType))
	name := fmt.Sprintf("dql_%s_%s.%s", dqlexport.SanitizeFilePart(req.TicketNo), time.Now().Format("20060102_150405"), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		j.setPhase(ExportFailed, ExportFailed, "写文件失败: "+err.Error())
		_ = os.RemoveAll(dir)
		j.mu.Lock()
		j.ended = time.Now()
		j.mu.Unlock()
		return
	}
	j.mu.Lock()
	j.filePath = path
	j.view.Status = ExportDone
	j.view.Phase = ExportDone
	j.view.Filename = name
	j.view.Bytes = int64(len(body))
	j.view.Message = "导出完成"
	j.view.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if j.view.Format == "" {
		j.view.Format = "csv"
	}
	j.ended = time.Now()
	j.mu.Unlock()
}

func (r *ExportRunner) reapLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		r.reap()
	}
}

func (r *ExportRunner) reap() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, j := range r.jobs {
		j.mu.Lock()
		ended := j.ended
		path := j.filePath
		status := j.view.Status
		j.mu.Unlock()
		if ended.IsZero() {
			continue
		}
		if now.Sub(ended) < exportJobRetain {
			continue
		}
		if path != "" {
			_ = os.RemoveAll(filepath.Dir(path))
		}
		if status == ExportDone || status == ExportFailed || status == ExportStopped {
			delete(r.jobs, id)
		}
	}
}
