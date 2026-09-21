package archive

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

var errStopped = errors.New("任务已停止")

type Engine struct {
	Store  *store.DB
	Secret string
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func New(st *store.DB, secret string) *Engine {
	return &Engine{Store: st, Secret: secret, cancel: map[string]context.CancelFunc{}}
}

func (e *Engine) Start(taskID string) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	e.mu.Lock()
	if old, ok := e.cancel[taskID]; ok {
		old()
	}
	e.cancel[taskID] = cancel
	e.mu.Unlock()
	go e.run(ctx, taskID)
	return nil
}

func (e *Engine) Stop(taskID string) bool {
	e.mu.Lock()
	c, ok := e.cancel[taskID]
	if ok {
		c()
		delete(e.cancel, taskID)
	}
	e.mu.Unlock()
	return ok
}

func (e *Engine) Pause(taskID string) error {
	e.Stop(taskID)
	return e.Store.UpdateArchiveStatus(taskID, store.StatusPaused)
}

func (e *Engine) run(ctx context.Context, taskID string) {
	t, err := e.Store.GetArchive(taskID)
	if err != nil || t == nil {
		return
	}
	extra := store.ParseArchiveExtra(t)
	strategy := NormalizeStrategy(extra.Strategy)
	run := &store.ArchiveRun{
		TaskID: t.ID, Status: store.StatusRunning, Strategy: strategy,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := e.Store.CreateArchiveRun(run); err != nil {
		_ = e.Store.UpdateArchiveState(t.ID, store.StatusFailed, t.ApprovalStatus)
		return
	}
	_ = e.Store.UpdateArchiveState(t.ID, store.StatusRunning, t.ApprovalStatus)

	resErr := e.execute(ctx, t, extra, strategy, run)
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if errors.Is(resErr, errStopped) || errors.Is(resErr, context.Canceled) {
		run.Status = store.StatusCanceled
		run.Message = errStopped.Error()
		_ = e.Store.UpdateArchiveRun(run)
		_ = e.Store.UpdateArchiveState(t.ID, store.StatusCanceled, t.ApprovalStatus)
		return
	}
	if resErr != nil {
		run.Status = store.StatusFailed
		run.Message = resErr.Error()
		_ = e.Store.UpdateArchiveRun(run)
		_ = e.Store.UpdateArchiveState(t.ID, store.StatusFailed, t.ApprovalStatus)
		return
	}
	run.Status = store.StatusExecuted
	if run.Message == "" {
		run.Message = fmt.Sprintf("完成，删除 %d 行 / %d 轮", run.DeletedRows, run.Rounds)
	}
	_ = e.Store.UpdateArchiveRun(run)
	next := store.StatusExecuted
	if store.ArchiveRunMode(t) == "scheduled" {
		next = "scheduled"
	}
	_ = e.Store.UpdateArchiveState(t.ID, next, t.ApprovalStatus)
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
	src, err := dbmig.OpenInstance(ctx, inst, e.Secret, t.SourceDB)
	if err != nil {
		return fmt.Errorf("连接源端失败: %w", err)
	}
	defer src.Close()

	switch strategy {
	case "custom_script":
		return e.runScript(ctx, src, t, extra, run)
	case "backup_target":
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
		dst, err := dbmig.OpenInstance(ctx, dstInst, e.Secret, dstDB)
		if err != nil {
			return fmt.Errorf("连接目标端失败: %w", err)
		}
		defer dst.Close()
		dstTable := extra.TargetTable
		if dstTable == "" {
			dstTable = t.TableName
		}
		if err := e.copyThenDelete(ctx, src, dst, dbType, t, dstTable, extra, run); err != nil {
			return err
		}
		return nil
	case "archive_oss":
		if err := e.exportCSV(ctx, src, dbType, t, extra, run); err != nil {
			return err
		}
		if extra.CleanupAfter {
			return e.deleteLoop(ctx, src, dbType, t, extra, run)
		}
		return nil
	default:
		return e.deleteLoop(ctx, src, dbType, t, extra, run)
	}
}

func (e *Engine) deleteLoop(ctx context.Context, db *sql.DB, dbType string, t *store.ArchiveTask, extra store.ArchiveExtra, run *store.ArchiveRun) error {
	cond := store.ArchiveConditionSQL(t)
	if err := ValidateDeleteCondition(cond); err != nil {
		return err
	}
	pks, _ := tablePKs(ctx, db, dbType, t.TableName)
	var lastSQL string
	for run.Rounds < maxRounds {
		if err := ctx.Err(); err != nil {
			return errStopped
		}
		var (
			q   string
			err error
			n   int64
		)
		if dbType == "postgres" {
			mode, rerr := ResolvePGMode(extra.PGDeleteMode, hasCreateTime(ctx, db, dbType, t.TableName), 0)
			if rerr != nil {
				return rerr
			}
			if mode == "shard" && extra.PGDeleteMode == "shard" {
				q, err = BuildPGCTESQL(t.TableName, cond+" AND create_time < NOW()", pks)
			} else {
				q, err = BuildPGCTESQL(t.TableName, cond, pks)
			}
		} else {
			q, err = BuildMySQLDeleteSQL(t.TableName, cond)
		}
		if err != nil {
			return err
		}
		lastSQL = q
		n, err = execAffected(ctx, db, q)
		if err != nil {
			run.SQLPreview = lastSQL
			return err
		}
		run.Rounds++
		run.DeletedRows += n
		run.SQLPreview = lastSQL
		_ = e.Store.UpdateArchiveRun(run)
		if n == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errStopped
		case <-time.After(600 * time.Millisecond):
		}
	}
	return fmt.Errorf("超过最大轮次 %d", maxRounds)
}

func (e *Engine) copyThenDelete(ctx context.Context, src, dst *sql.DB, dbType string, t *store.ArchiveTask, dstTable string, extra store.ArchiveExtra, run *store.ArchiveRun) error {
	cond := store.ArchiveConditionSQL(t)
	if err := ValidateDeleteCondition(cond); err != nil {
		return err
	}
	sel := fmt.Sprintf("SELECT * FROM %s WHERE %s", qualifyTable(dbType, t.TableName), normalizeCondition(cond))
	rows, err := src.QueryContext(ctx, sel)
	if err != nil {
		return fmt.Errorf("读取源表失败: %w", err)
	}
	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return err
	}
	qCols := make([]string, len(cols))
	ph := make([]string, len(cols))
	for i, c := range cols {
		qCols[i] = quoteIdent(dbType, c)
		ph[i] = "?"
		if dbType == "postgres" {
			ph[i] = fmt.Sprintf("$%d", i+1)
		}
	}
	ins := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qualifyTable(dbType, dstTable), strings.Join(qCols, ","), strings.Join(ph, ","))
	copied := int64(0)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			rows.Close()
			return errStopped
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			return err
		}
		if _, err := dst.ExecContext(ctx, ins, raw...); err != nil {
			rows.Close()
			return fmt.Errorf("写入目标表失败: %w", err)
		}
		copied++
	}
	rows.Close()
	run.Message = fmt.Sprintf("已备份 %d 行到 %s", copied, dstTable)
	run.SQLPreview = sel
	return e.deleteLoop(ctx, src, dbType, t, extra, run)
}

func (e *Engine) exportCSV(ctx context.Context, src *sql.DB, dbType string, t *store.ArchiveTask, extra store.ArchiveExtra, run *store.ArchiveRun) error {
	cond := store.ArchiveConditionSQL(t)
	if err := ValidateDeleteCondition(cond); err != nil {
		return err
	}
	sel := fmt.Sprintf("SELECT * FROM %s WHERE %s", qualifyTable(dbType, t.TableName), normalizeCondition(cond))
	rows, err := src.QueryContext(ctx, sel)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	dir := filepath.Join(os.TempDir(), "auto-dba-archive")
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("%s_%s.csv", strings.ReplaceAll(t.TableName, ".", "_"), time.Now().UTC().Format("20060102T150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write(cols)
	n := int64(0)
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			f.Close()
			return err
		}
		rec := make([]string, len(cols))
		for i, v := range raw {
			rec[i] = fmt.Sprint(v)
		}
		_ = w.Write(rec)
		n++
	}
	w.Flush()
	f.Close()
	run.DeletedRows = 0
	run.Message = fmt.Sprintf("已导出 %d 行到 %s", n, path)
	if extra.OSSBucket != "" {
		run.Message += "（OSS bucket=" + extra.OSSBucket + "，请在系统设置中配置上传）"
	}
	run.SQLPreview = sel
	return nil
}

func (e *Engine) runScript(ctx context.Context, db *sql.DB, t *store.ArchiveTask, extra store.ArchiveExtra, run *store.ArchiveRun) error {
	body := store.ArchiveConditionSQL(t)
	kind := strings.ToLower(strings.TrimSpace(extra.ScriptType))
	if kind == "" || kind == "sql" {
		var total int64
		for _, stmt := range splitSQL(body) {
			if err := ctx.Err(); err != nil {
				return errStopped
			}
			n, err := execAffected(ctx, db, stmt)
			if err != nil {
				run.SQLPreview = stmt
				return err
			}
			total += n
			run.Rounds++
		}
		run.DeletedRows = total
		run.SQLPreview = body
		return nil
	}
	return fmt.Errorf("脚本类型 %s 需在执行节点配置解释器，当前仅支持 SQL", extra.ScriptType)
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

func (e *Engine) StartScheduler(ctx context.Context) {
	if e == nil {
		return
	}
	tick := time.NewTicker(60 * time.Second)
	go func() {
		defer tick.Stop()
		e.scanDue()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				e.scanDue()
			}
		}
	}()
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
		_ = e.Start(t.ID)
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
