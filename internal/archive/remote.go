package archive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
)

func (e *Engine) decide(inst *store.Instance) (sqlexec.Decision, error) {
	if e == nil || e.Store == nil || inst == nil {
		return sqlexec.Decision{}, fmt.Errorf("归档任务无效")
	}
	raw, err := e.Store.GetGlobalArg(store.ArgTicketLocalIDC)
	if err != nil {
		return sqlexec.Decision{}, err
	}
	rows, err := e.Store.ListSQLExecutors()
	if err != nil {
		return sqlexec.Decision{}, err
	}
	execs := make([]sqlexec.Executor, 0, len(rows))
	for _, row := range rows {
		pass, decErr := plainSecret(e.Secret, row.PasswordEnc)
		if decErr != nil {
			return sqlexec.Decision{}, fmt.Errorf("执行器口令无法解密")
		}
		execs = append(execs, sqlexec.Executor{
			ID: row.ID, Name: row.Name, BaseURL: row.BaseURL,
			Username: row.Username, Password: pass, EnvCodes: row.EnvCodes, Enabled: row.Enabled,
		})
	}
	return sqlexec.Decide(inst.IDC, splitEnvCodes(raw), execs), nil
}

func (e *Engine) executorByID(id string) (*sqlexec.Executor, error) {
	if strings.TrimSpace(id) == "" || e == nil || e.Store == nil {
		return nil, nil
	}
	row, err := e.Store.GetSQLExecutor(id)
	if err != nil || row == nil {
		return nil, err
	}
	pass, err := plainSecret(e.Secret, row.PasswordEnc)
	if err != nil {
		return nil, fmt.Errorf("执行器口令无法解密")
	}
	return &sqlexec.Executor{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseURL,
		Username: row.Username, Password: pass, EnvCodes: row.EnvCodes, Enabled: row.Enabled,
	}, nil
}

func (e *Engine) runRemote(ctx context.Context, inst *store.Instance, t *store.ArchiveTask, extra store.ArchiveExtra, strategy, dbType string, run *store.ArchiveRun, exec *sqlexec.Executor) error {
	if exec == nil {
		return fmt.Errorf("环境 %s 未绑定执行器，不从本机直连", strings.TrimSpace(inst.IDC))
	}
	req, err := e.archiveRequest(inst, t, extra, strategy, dbType)
	if err != nil {
		return err
	}
	jobID, err := sqlexec.SubmitArchive(ctx, exec.BaseURL, exec.Username, exec.Password, req)
	if err != nil {
		return err
	}
	run.ExecutorID = exec.ID
	run.ExecutorJobID = jobID
	if err := e.Store.UpdateArchiveRun(run); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = sqlexec.StopArchive(stopCtx, exec.BaseURL, exec.Username, exec.Password, jobID)
		cancel()
		return err
	}
	if !e.markWatch(run.ID) {
		return e.pollArchive(ctx, exec, run)
	}
	defer e.unwatch(run.ID)
	return e.pollArchive(ctx, exec, run)
}

func (e *Engine) archiveRequest(inst *store.Instance, t *store.ArchiveTask, extra store.ArchiveExtra, strategy, dbType string) (sqlexec.ArchiveRequest, error) {
	src, err := dialDB(e.Secret, inst, t.SourceDB, dbType)
	if err != nil {
		return sqlexec.ArchiveRequest{}, fmt.Errorf("源端连接信息无效: %w", err)
	}
	req := sqlexec.ArchiveRequest{
		Strategy: strategy, DBType: dbType, Table: t.TableName,
		Condition: store.ArchiveConditionSQL(t), PGDeleteMode: extra.PGDeleteMode,
		ScriptType: extra.ScriptType, OSSBucket: extra.OSSBucket,
		TargetTable: extra.TargetTable, CleanupAfter: extra.CleanupAfter, Source: src,
	}
	if strategy != "backup_target" {
		return req, nil
	}
	if extra.TargetInstID == "" {
		return req, fmt.Errorf("备份到目标实例须指定 target_instance_id")
	}
	dstInst, err := e.Store.GetInstance(extra.TargetInstID)
	if err != nil || dstInst == nil {
		return req, fmt.Errorf("目标实例不存在")
	}
	dstDB := extra.TargetDB
	if dstDB == "" {
		dstDB = t.SourceDB
	}
	dstType := dbType
	if extra.DBType == "" && dstInst.DBType != "" {
		dstType = store.NormalizeDBType(dstInst.DBType)
	}
	dst, err := dialDB(e.Secret, dstInst, dstDB, dstType)
	if err != nil {
		return req, fmt.Errorf("目标端连接信息无效: %w", err)
	}
	req.Target = &dst
	if req.TargetTable == "" {
		req.TargetTable = t.TableName
	}
	return req, nil
}

func (e *Engine) pollArchive(ctx context.Context, exec *sqlexec.Executor, run *store.ArchiveRun) error {
	miss := 0
	stop := func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = sqlexec.StopArchive(stopCtx, exec.BaseURL, exec.Username, exec.Password, run.ExecutorJobID)
		cancel()
	}
	for {
		if ctx.Err() != nil {
			stop()
			return errStopped
		}
		view, err := sqlexec.GetArchive(ctx, exec.BaseURL, exec.Username, exec.Password, run.ExecutorJobID)
		if err != nil {
			if ctx.Err() != nil {
				stop()
				return errStopped
			}
			miss++
			if errors.Is(err, sqlexec.ErrJobNotFound) || miss >= 5 {
				stop()
				if errors.Is(err, sqlexec.ErrJobNotFound) {
					return fmt.Errorf("执行器上的任务已经丢失")
				}
				return fmt.Errorf("读取执行器进度失败: %w", err)
			}
			select {
			case <-ctx.Done():
				stop()
				return errStopped
			case <-time.After(2 * time.Second):
			}
			continue
		}
		miss = 0
		run.Rounds = view.Rounds
		run.DeletedRows = view.DeletedRows
		if view.SQLPreview != "" {
			run.SQLPreview = view.SQLPreview
		}
		if view.Message != "" {
			run.Message = view.Message
		}
		_ = e.Store.UpdateArchiveRun(run)
		switch view.Status {
		case "done":
			if strings.TrimSpace(view.Error) != "" {
				return errors.New(view.Error)
			}
			return nil
		case "failed":
			if strings.TrimSpace(view.Error) != "" {
				return errors.New(view.Error)
			}
			return fmt.Errorf("执行器任务失败")
		case "stopped":
			return errStopped
		}
		select {
		case <-ctx.Done():
			stop()
			return errStopped
		case <-time.After(time.Second):
		}
	}
}

func (e *Engine) reattachRemote() {
	if e == nil || e.Store == nil {
		return
	}
	list, err := e.Store.ListRunningRemoteArchiveRuns(50)
	if err != nil {
		return
	}
	for i := range list {
		run := list[i]
		if !e.markWatch(run.ID) {
			continue
		}
		go e.finishRemote(run)
	}
}

func (e *Engine) finishRemote(run store.ArchiveRun) {
	defer e.unwatch(run.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	e.mu.Lock()
	if old, ok := e.cancel[run.TaskID]; ok {
		old()
	}
	e.cancel[run.TaskID] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.cancel[run.TaskID] != nil {
			delete(e.cancel, run.TaskID)
		}
		e.mu.Unlock()
		cancel()
	}()
	t, _ := e.Store.GetArchive(run.TaskID)
	exec, err := e.executorByID(run.ExecutorID)
	if err != nil || exec == nil {
		e.finishDetached(t, &run, fmt.Errorf("执行器上的任务已经丢失"))
		return
	}
	e.finishDetached(t, &run, e.pollArchive(ctx, exec, &run))
}

func (e *Engine) finishDetached(t *store.ArchiveTask, run *store.ArchiveRun, resErr error) {
	bind := false
	if t != nil && strings.TrimSpace(t.IssueID) != "" {
		if it, err := e.Store.GetIssue(t.IssueID); err == nil && it != nil && it.Status == store.StatusRunning {
			bind = true
		}
	}
	e.finishArchive(t, run, resErr, bind)
}

func (e *Engine) stopRemoteForTask(ctx context.Context, taskID string) (bool, error) {
	if e == nil || e.Store == nil || strings.TrimSpace(taskID) == "" {
		return false, nil
	}
	list, err := e.Store.ListArchiveRuns(taskID, 20)
	if err != nil {
		return false, err
	}
	stopped := false
	var last error
	for _, r := range list {
		if strings.TrimSpace(r.ExecutorJobID) == "" || r.Status == store.StatusExecuted {
			continue
		}
		exec, err := e.executorByID(r.ExecutorID)
		if err != nil || exec == nil {
			continue
		}
		if err := sqlexec.StopArchive(ctx, exec.BaseURL, exec.Username, exec.Password, r.ExecutorJobID); err != nil {
			last = err
			continue
		}
		stopped = true
	}
	if last != nil {
		return stopped, fmt.Errorf("停止上次执行器任务失败: %w", last)
	}
	return stopped, nil
}

func (e *Engine) markWatch(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.watching == nil {
		e.watching = map[string]struct{}{}
	}
	if _, ok := e.watching[id]; ok {
		return false
	}
	e.watching[id] = struct{}{}
	return true
}

func (e *Engine) unwatch(id string) {
	e.mu.Lock()
	delete(e.watching, id)
	e.mu.Unlock()
}

func dialDB(secretKey string, inst *store.Instance, dbName, dbType string) (sqlexec.ArchiveDB, error) {
	if inst == nil {
		return sqlexec.ArchiveDB{}, fmt.Errorf("实例不存在")
	}
	pass, err := plainSecret(secretKey, inst.Password)
	if err != nil {
		return sqlexec.ArchiveDB{}, fmt.Errorf("解密实例密码失败")
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" || inst.Port <= 0 {
		return sqlexec.ArchiveDB{}, fmt.Errorf("未配置连接主机或端口")
	}
	if strings.TrimSpace(inst.Username) == "" {
		return sqlexec.ArchiveDB{}, fmt.Errorf("未配置执行账号")
	}
	if strings.TrimSpace(dbType) == "" {
		dbType = store.NormalizeDBType(inst.DBType)
	}
	return sqlexec.ArchiveDB{
		Host: host, Port: inst.Port, Username: inst.Username, Password: pass,
		DBName: dbName, DBType: dbType,
	}, nil
}

func plainSecret(key, enc string) (string, error) {
	if strings.TrimSpace(enc) == "" {
		return "", nil
	}
	return secret.Decrypt(key, enc)
}

func splitEnvCodes(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\n' || r == '\t'
	})
	seen := map[string]struct{}{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}
