package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
)

var errStopped = errors.New("任务已停止")

type Engine struct {
	Store  *store.DB
	Secret string
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	// instRun 记录本进程已占住的源库实例，避免两个任务同时删同一台库。
	instRun map[string]string
	// watching 是本进程正在轮询的执行记录，避免扫描再挂上一份。
	watching map[string]struct{}
}

func New(st *store.DB, secret string) *Engine {
	return &Engine{
		Store: st, Secret: secret,
		cancel: map[string]context.CancelFunc{}, instRun: map[string]string{}, watching: map[string]struct{}{},
	}
}

func (e *Engine) Start(taskID string) error {
	return e.start(taskID, true)
}

func (e *Engine) start(taskID string, bindIssue bool) error {
	t, err := e.Store.GetArchive(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	if !e.Store.ArchiveApproved(t) {
		return fmt.Errorf("归档任务须审批通过后才能执行")
	}
	if t.Status == store.StatusRunning {
		return fmt.Errorf("任务已在执行")
	}
	if t.Status == store.StatusPaused {
		return fmt.Errorf("任务已停用，请先恢复")
	}
	if _, err := e.stopRemoteForTask(context.Background(), taskID); err != nil {
		return err
	}
	if err := e.lockInstance(t.SourceID, t.ID); err != nil {
		return err
	}
	if err := e.Store.UpdateArchiveState(t.ID, store.StatusRunning, t.ApprovalStatus); err != nil {
		e.unlockInstance(t.SourceID, t.ID)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	e.mu.Lock()
	if old, ok := e.cancel[taskID]; ok {
		old()
	}
	e.cancel[taskID] = cancel
	e.mu.Unlock()
	if bindIssue {
		e.syncIssueStatus(t.IssueID, store.StatusRunning)
	}
	go func() {
		defer e.unlockInstance(t.SourceID, t.ID)
		e.run(ctx, taskID, bindIssue)
	}()
	return nil
}

func (e *Engine) lockInstance(instanceID, taskID string) error {
	instanceID = strings.TrimSpace(instanceID)
	taskID = strings.TrimSpace(taskID)
	if e == nil || instanceID == "" || taskID == "" {
		return nil
	}
	e.mu.Lock()
	if e.instRun == nil {
		e.instRun = map[string]string{}
	}
	if owner, ok := e.instRun[instanceID]; ok && owner != taskID {
		e.mu.Unlock()
		return fmt.Errorf("同一实例已有归档在执行")
	}
	e.instRun[instanceID] = taskID
	e.mu.Unlock()
	if e.Store != nil {
		if err := e.Store.TryLockArchiveInstance(instanceID, taskID); err != nil {
			e.mu.Lock()
			if e.instRun[instanceID] == taskID {
				delete(e.instRun, instanceID)
			}
			e.mu.Unlock()
			return err
		}
	}
	return nil
}

func (e *Engine) unlockInstance(instanceID, taskID string) {
	instanceID = strings.TrimSpace(instanceID)
	taskID = strings.TrimSpace(taskID)
	if e == nil || instanceID == "" {
		return
	}
	e.mu.Lock()
	if e.instRun[instanceID] == taskID {
		delete(e.instRun, instanceID)
	}
	e.mu.Unlock()
	if e.Store != nil {
		e.Store.UnlockArchiveInstance(instanceID, taskID)
	}
}

func (e *Engine) syncIssueStatus(issueID, status string) {
	if e == nil || e.Store == nil || strings.TrimSpace(issueID) == "" || strings.TrimSpace(status) == "" {
		return
	}
	_ = e.Store.SetIssueStatus(issueID, status)
}

func (e *Engine) Stop(taskID string) bool {
	e.mu.Lock()
	c, ok := e.cancel[taskID]
	if ok {
		c()
		delete(e.cancel, taskID)
	}
	e.mu.Unlock()
	stopped, err := e.stopRemoteForTask(context.Background(), taskID)
	if err != nil {
		log.Printf("archive stop remote %s: %v", taskID, err)
	}
	return ok || stopped
}

func (e *Engine) Pause(taskID string) error {
	e.Stop(taskID)
	return e.Store.UpdateArchiveStatus(taskID, store.StatusPaused)
}

func (e *Engine) run(ctx context.Context, taskID string, bindIssue bool) {
	t, err := e.Store.GetArchive(taskID)
	if err != nil || t == nil {
		return
	}
	finishIssue := func(status string) {
		if bindIssue {
			e.syncIssueStatus(t.IssueID, status)
		}
	}
	extra := store.ParseArchiveExtra(t)
	strategy := NormalizeStrategy(extra.Strategy)
	run := &store.ArchiveRun{
		TaskID: t.ID, Status: store.StatusRunning, Strategy: strategy,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := e.Store.CreateArchiveRun(run); err != nil {
		_ = e.Store.UpdateArchiveState(t.ID, store.StatusFailed, t.ApprovalStatus)
		finishIssue(store.StatusFailed)
		return
	}
	_ = e.Store.UpdateArchiveState(t.ID, store.StatusRunning, t.ApprovalStatus)

	resErr := e.execute(ctx, t, extra, strategy, run)
	e.finishArchive(t, run, resErr, bindIssue)
}

func (e *Engine) finishArchive(t *store.ArchiveTask, run *store.ArchiveRun, resErr error, bindIssue bool) {
	if run == nil || e == nil || e.Store == nil {
		return
	}
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	issueStatus := ""
	taskStatus := ""
	switch {
	case errors.Is(resErr, errStopped) || errors.Is(resErr, context.Canceled):
		run.Status = store.StatusCanceled
		run.Message = errStopped.Error()
		taskStatus = store.StatusCanceled
		issueStatus = store.StatusCanceled
	case resErr != nil:
		run.Status = store.StatusFailed
		run.Message = resErr.Error()
		taskStatus = store.StatusFailed
		issueStatus = store.StatusFailed
	default:
		run.Status = store.StatusExecuted
		if run.Message == "" || strings.HasPrefix(run.Message, "执行中：") {
			run.Message = fmt.Sprintf("完成，删除 %d 行 / %d 轮", run.DeletedRows, run.Rounds)
		}
		taskStatus = store.StatusExecuted
		if t != nil && store.ArchiveRunMode(t) == "scheduled" {
			taskStatus = "scheduled"
		}
		issueStatus = store.StatusExecuted
	}
	_ = e.Store.UpdateArchiveRun(run)
	if t != nil {
		_ = e.Store.UpdateArchiveState(t.ID, taskStatus, t.ApprovalStatus)
		if bindIssue {
			e.syncIssueStatus(t.IssueID, issueStatus)
		}
	}
}

func (e *Engine) execute(ctx context.Context, t *store.ArchiveTask, extra store.ArchiveExtra, strategy string, run *store.ArchiveRun) error {
	inst, err := e.Store.GetInstance(t.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	dbType := store.NormalizeDBType(inst.DBType)
	if extra.DBType != "" {
		dbType = store.NormalizeDBType(extra.DBType)
	}
	dec, err := e.decide(inst)
	if err != nil {
		return err
	}
	if dec.Mode == sqlexec.ModeMissing {
		return fmt.Errorf("环境 %s 未绑定执行器，不从本机直连", strings.TrimSpace(inst.IDC))
	}
	if dec.Mode == sqlexec.ModeRemote {
		return e.runRemote(ctx, inst, t, extra, strategy, dbType, run, dec.Executor)
	}
	src, err := dbmig.OpenInstance(ctx, inst, e.Secret, t.SourceDB)
	if err != nil {
		return fmt.Errorf("连接源端失败: %w", err)
	}
	defer src.Close()

	var dst *sql.DB
	dstTable := extra.TargetTable
	if strategy == "backup_target" {
		if extra.TargetInstID == "" {
			return fmt.Errorf("备份到目标实例须指定 target_instance_id")
		}
		dstInst, err := e.Store.GetInstance(extra.TargetInstID)
		if err != nil || dstInst == nil {
			return fmt.Errorf("目标实例不存在")
		}
		dstDB := extra.TargetDB
		if dstDB == "" {
			dstDB = t.SourceDB
		}
		dst, err = dbmig.OpenInstance(ctx, dstInst, e.Secret, dstDB)
		if err != nil {
			return fmt.Errorf("连接目标端失败: %w", err)
		}
		defer dst.Close()
		if dstTable == "" {
			dstTable = t.TableName
		}
	}
	return e.runLocal(ctx, src, dst, dbType, t, extra, strategy, dstTable, run)
}

func (e *Engine) runLocal(ctx context.Context, src, dst *sql.DB, dbType string, t *store.ArchiveTask, extra store.ArchiveExtra, strategy, dstTable string, run *store.ArchiveRun) error {
	return RunOnDB(ctx, strategy, src, dst, dbType, t.TableName, store.ArchiveConditionSQL(t), extra.PGDeleteMode, extra.ScriptType, extra.OSSBucket, dstTable, extra.CleanupAfter, func(p Progress) {
		applyProgress(run, p)
		_ = e.Store.UpdateArchiveRun(run)
	})
}

func applyProgress(run *store.ArchiveRun, p Progress) {
	if run == nil {
		return
	}
	if p.Rounds > 0 {
		run.Rounds = p.Rounds
	}
	if p.DeletedRows > 0 || p.Interim {
		run.DeletedRows = p.DeletedRows
	}
	if p.SQL != "" {
		run.SQLPreview = p.SQL
	}
	if p.Message != "" {
		run.Message = p.Message
	}
}

func execAffected(ctx context.Context, db *sql.DB, q string) (int64, error) {
	res, err := db.ExecContext(ctx, q)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func splitSQL(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p != "" && !strings.HasPrefix(p, "--") {
			out = append(out, p)
		}
	}
	return out
}

func tablePKs(ctx context.Context, db *sql.DB, dbType, table string) ([]string, error) {
	table = strings.TrimSpace(table)
	schema, name := "", table
	if i := strings.LastIndex(table, "."); i >= 0 {
		schema, name = table[:i], table[i+1:]
	}
	var (
		q    string
		args []any
	)
	if dbType == "postgres" {
		if schema == "" {
			schema = "public"
		}
		q = `SELECT a.attname FROM pg_index i
JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=ANY(i.indkey)
WHERE i.indrelid=($1||'.'||$2)::regclass AND i.indisprimary`
		args = []any{schema, name}
	} else {
		q = `SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_NAME=? AND CONSTRAINT_NAME='PRIMARY' ORDER BY ORDINAL_POSITION`
		args = []any{name}
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func hasCreateTime(ctx context.Context, db *sql.DB, dbType, table string) bool {
	name := table
	if i := strings.LastIndex(table, "."); i >= 0 {
		name = table[i+1:]
	}
	var n int
	if dbType == "postgres" {
		_ = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM information_schema.columns WHERE table_name=$1 AND column_name='create_time'`, name).Scan(&n)
	} else {
		_ = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM information_schema.columns WHERE table_name=? AND column_name='create_time'`, name).Scan(&n)
	}
	return n > 0
}

// StartScheduler 已废弃：由 jobreg archive.due_scanner 接管。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs。
func (e *Engine) StartScheduler(ctx context.Context) {
	_ = ctx
	log.Printf("DEPRECATED: archive.StartScheduler ignored; scheduling is owned by jobreg (archive.due_scanner)")
}

// ScanDue 扫描到期归档任务（供 jobreg 调用）。
func (e *Engine) ScanDue() {
	e.reattachRemote()
	e.scanDue()
	if e != nil {
		EvaluateAlerts(context.Background(), e.Store)
	}
}

func (e *Engine) scanDue() {
	list, err := e.Store.ListArchives()
	if err != nil {
		return
	}
	now := time.Now()
	for i := range list {
		t := &list[i]
		if !e.Store.ArchiveApproved(t) || store.ArchiveRunMode(t) != "scheduled" {
			continue
		}
		if t.Status == store.StatusRunning || t.Status == store.StatusPaused {
			continue
		}
		expr := strings.TrimSpace(t.ScheduleExpr)
		if expr == "" {
			expr = store.ParseArchiveExtra(t).ScheduleExpr
		}
		if !scheduleDue(expr, e.Store.LastArchiveRunAt(t.ID), now) {
			continue
		}
		_ = e.start(t.ID, false)
	}
}

func scheduleDue(expr, lastRun string, now time.Time) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if last := parseTime(lastRun); last != nil && now.Sub(*last) < 50*time.Second {
		return false
	}
	if strings.Count(expr, " ") >= 4 {
		return cronDue(expr, lastRun, now)
	}
	if t, err := time.Parse("15:04", expr); err == nil {
		if now.Hour() != t.Hour() || now.Minute() != t.Minute() {
			return false
		}
		if last := parseTime(lastRun); last != nil && last.Format("2006-01-02") == now.Format("2006-01-02") {
			return false
		}
		return true
	}
	return false
}

func cronDue(expr, lastRun string, now time.Time) bool {
	parts := strings.Fields(expr)
	if len(parts) < 5 {
		return false
	}
	if !cronField(parts[0], now.Minute()) || !cronField(parts[1], now.Hour()) {
		return false
	}
	if last := parseTime(lastRun); last != nil && now.Sub(*last) < time.Minute {
		return false
	}
	return true
}

func cronField(f string, v int) bool {
	f = strings.TrimSpace(f)
	if f == "*" || f == "*/1" {
		return true
	}
	if n, err := fmt.Sscanf(f, "%d", new(int)); err == nil && n == 1 {
		var x int
		_, _ = fmt.Sscanf(f, "%d", &x)
		return x == v
	}
	return false
}

func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}
