package handler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

type remoteHandle struct {
	BaseURL  string
	Username string
	Password string
	JobID    string
}

type remoteSet struct {
	mu    sync.Mutex
	items []remoteHandle
}

var ticketRemoteJobs sync.Map

func trackRemoteJob(issueID string, h remoteHandle) {
	v, _ := ticketRemoteJobs.LoadOrStore(issueID, &remoteSet{})
	set := v.(*remoteSet)
	set.mu.Lock()
	set.items = append(set.items, h)
	set.mu.Unlock()
}

func clearRemoteJobs(issueID string) {
	ticketRemoteJobs.Delete(issueID)
}

func (h *TicketHandler) stopRemoteJobs(ctx context.Context, issueID string) error {
	v, ok := ticketRemoteJobs.Load(issueID)
	if !ok {
		return nil
	}
	set := v.(*remoteSet)
	set.mu.Lock()
	items := append([]remoteHandle(nil), set.items...)
	set.mu.Unlock()
	var last error
	for _, item := range items {
		err := sqlexec.StopJob(ctx, item.BaseURL, item.Username, item.Password, item.JobID)
		if err != nil {
			err = sqlexec.StopJob(ctx, item.BaseURL, item.Username, item.Password, item.JobID)
		}
		if err != nil {
			last = err
		}
	}
	return last
}

func (h *TicketHandler) executionCatalog() (local []string, execs []sqlexec.Executor, err error) {
	if h == nil || h.Store == nil {
		return nil, nil, nil
	}
	raw, err := h.Store.GetGlobalArg(store.ArgTicketLocalIDC)
	if err != nil {
		return nil, nil, err
	}
	local = splitCodes(raw)
	rows, err := h.Store.ListSQLExecutors()
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		pass, decErr := secret.Decrypt(h.Secret, row.PasswordEnc)
		if decErr != nil {
			pass = ""
		}
		execs = append(execs, sqlexec.Executor{
			ID: row.ID, Name: row.Name, BaseURL: row.BaseURL,
			Username: row.Username, Password: pass, EnvCodes: row.EnvCodes, Enabled: row.Enabled,
		})
	}
	return local, execs, nil
}

type ddlLive struct {
	mu                sync.Mutex
	last              time.Time
	prog              *store.ExecProgress
	merged            *ticketexec.Result
	doneN, failN, okN int
	it                *store.Issue
}

func (h *TicketHandler) runDDLLanes(ctx context.Context, it *store.Issue, jobs []ddlExecJob, stmtN int, live *ddlLive) error {
	localCodes, execs, err := h.executionCatalog()
	if err != nil {
		return err
	}
	lanes := map[string][]ddlExecJob{}
	order := []string{}
	laneExec := map[string]*sqlexec.Executor{}
	for _, job := range jobs {
		if job.skip || job.failMsg != "" {
			continue
		}
		idc := ""
		if job.inst != nil {
			idc = job.inst.IDC
		}
		dec := sqlexec.Decide(idc, localCodes, execs)
		if dec.Mode == sqlexec.ModeMissing {
			name := job.id
			if job.inst != nil {
				name = job.inst.Name
			}
			h.liveFail(live, ticketexec.Record{
				InstanceID: job.id, InstanceName: name, Host: job.host, DB: job.dbName,
				ErrorMessage: fmt.Sprintf("环境 %s 未绑定执行器，不从本机直连", idc),
			}, 0, 1)
			continue
		}
		key := "local"
		if dec.Mode == sqlexec.ModeRemote {
			key = dec.Executor.ID
			if _, ok := laneExec[key]; !ok {
				cp := *dec.Executor
				laneExec[key] = &cp
			}
		}
		if _, ok := lanes[key]; !ok {
			order = append(order, key)
		}
		lanes[key] = append(lanes[key], job)
	}
	var wg sync.WaitGroup
	var stopErr error
	var once sync.Once
	for _, key := range order {
		key := key
		group := lanes[key]
		wg.Add(1)
		go func() {
			defer wg.Done()
			var runErr error
			if key == "local" {
				runErr = h.runLocalDDLLane(ctx, it, group, stmtN, live)
			} else {
				runErr = h.runRemoteDDLLane(ctx, it, laneExec[key], group, live)
			}
			if runErr != nil {
				once.Do(func() { stopErr = runErr })
			}
		}()
	}
	wg.Wait()
	clearRemoteJobs(it.ID)
	return stopErr
}

func (h *TicketHandler) runLocalDDLLane(ctx context.Context, it *store.Issue, jobs []ddlExecJob, stmtN int, live *ddlLive) error {
	for _, job := range jobs {
		if errors.Is(ctx.Err(), context.Canceled) || h.issueStopped(it.ID) {
			return errExecStopped
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		h.liveTouch(live, job, "", "", 0, stmtN, 0)
		t, err := h.execTarget(job.inst, job.dbName, job.domain)
		if err != nil {
			name := ""
			if job.inst != nil {
				name = job.inst.Name
			}
			h.liveFail(live, ticketexec.Record{
				InstanceID: job.id, InstanceName: name, Host: job.host, DB: job.dbName, ErrorMessage: err.Error(),
			}, 0, 1)
			continue
		}
		t.Light = true
		inst := job.inst
		dbName := job.dbName
		t.OnProgress = func(stmtIndex, stmtTotal int, sessionID int64) {
			h.liveTouch(live, job, "", "", stmtIndex, stmtTotal, sessionID)
		}
		res := ticketexec.Run(ctx, it.IssueType, it.SQLContent, t)
		if ctx.Err() != nil || h.issueStopped(it.ID) {
			if errors.Is(ctx.Err(), context.Canceled) || h.issueStopped(it.ID) {
				return errExecStopped
			}
			return ctx.Err()
		}
		rec := ticketexec.Record{InstanceID: job.id, Host: t.Host, DB: dbName}
		successSt, failSt := 0, 0
		ok := res != nil && res.Success
		if inst != nil {
			rec.InstanceName = inst.Name
		}
		if res != nil {
			successSt = res.SuccessCount
			failSt = res.FailCount
			rec.ErrorMessage = res.Message
		}
		if !ok && rec.ErrorMessage == "" {
			rec.ErrorMessage = "执行失败"
		}
		h.liveRecord(live, rec, ok, successSt, failSt)
	}
	return nil
}

func (h *TicketHandler) runRemoteDDLLane(ctx context.Context, it *store.Issue, exec *sqlexec.Executor, jobs []ddlExecJob, live *ddlLive) error {
	if exec == nil {
		return nil
	}
	var targets []sqlexec.Target
	var kept []ddlExecJob
	for _, job := range jobs {
		if errors.Is(ctx.Err(), context.Canceled) || h.issueStopped(it.ID) {
			return errExecStopped
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		t, err := h.execTarget(job.inst, job.dbName, job.domain)
		name := job.id
		env := ""
		if job.inst != nil {
			name = job.inst.Name
			env = job.inst.IDC
		}
		if err != nil {
			h.liveFail(live, ticketexec.Record{
				InstanceID: job.id, InstanceName: name, Host: job.host, DB: job.dbName, ErrorMessage: err.Error(),
			}, 0, 1)
			continue
		}
		targets = append(targets, sqlexec.Target{
			InstanceID: job.id, InstanceName: name, Host: t.Host, Port: t.Port,
			Username: t.Username, Password: t.Password, DBName: job.dbName, DBType: t.DBType, Env: env,
		})
		kept = append(kept, job)
	}
	if len(targets) == 0 {
		return nil
	}
	jobID, err := sqlexec.Submit(ctx, exec.BaseURL, exec.Username, exec.Password, sqlexec.JobRequest{
		IssueType: it.IssueType, SQL: it.SQLContent, Targets: targets,
	})
	if err != nil {
		for _, job := range kept {
			name := job.id
			if job.inst != nil {
				name = job.inst.Name
			}
			h.liveFail(live, ticketexec.Record{
				InstanceID: job.id, InstanceName: name, Host: job.host, DB: job.dbName,
				ErrorMessage: "下发执行器失败: " + err.Error(),
			}, 0, 1)
		}
		return nil
	}
	trackRemoteJob(it.ID, remoteHandle{BaseURL: exec.BaseURL, Username: exec.Username, Password: exec.Password, JobID: jobID})
	applied := 0
	miss := 0
	for {
		if errors.Is(ctx.Err(), context.Canceled) || h.issueStopped(it.ID) {
			stopCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_ = sqlexec.StopJob(stopCtx, exec.BaseURL, exec.Username, exec.Password, jobID)
			cancel()
			return errExecStopped
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		view, err := sqlexec.Get(ctx, exec.BaseURL, exec.Username, exec.Password, jobID)
		if err != nil {
			miss++
			if miss >= 5 {
				for _, job := range kept[applied:] {
					name := job.id
					if job.inst != nil {
						name = job.inst.Name
					}
					h.liveFail(live, ticketexec.Record{
						InstanceID: job.id, InstanceName: name, Host: job.host, DB: job.dbName,
						ErrorMessage: "读取执行器进度失败: " + err.Error(),
					}, 0, 1)
				}
				return nil
			}
		} else {
			miss = 0
			if applied < len(view.Results) {
				for _, item := range view.Results[applied:] {
					h.liveRecord(live, ticketexec.Record{
						InstanceID: item.InstanceID, InstanceName: item.InstanceName, Host: item.Host, DB: item.DB,
						ErrorMessage: item.Error,
					}, item.OK, item.SuccessCount, item.FailCount)
				}
				applied = len(view.Results)
			}
			env := ""
			if view.CurrentDB != "" {
				for _, job := range kept {
					if job.dbName == view.CurrentDB && (view.CurrentInstance == "" || job.inst == nil || job.inst.Name == view.CurrentInstance) {
						if job.inst != nil {
							env = job.inst.IDC
						}
						break
					}
				}
			}
			h.liveRemoteProgress(live, exec.Name, env, view)
			if view.Status == sqlexec.JobStopped {
				return errExecStopped
			}
			if view.Status != sqlexec.JobRunning {
				return nil
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (h *TicketHandler) liveFail(live *ddlLive, rec ticketexec.Record, successN, failN int) {
	h.liveRecord(live, rec, false, successN, failN)
}

func (h *TicketHandler) liveRecord(live *ddlLive, rec ticketexec.Record, ok bool, successN, failN int) {
	live.mu.Lock()
	appendCompactDB(live.merged, rec, ok, successN, failN)
	live.doneN++
	if ok {
		live.okN++
	} else {
		live.failN++
		if rec.ErrorMessage != "" {
			live.merged.Message = rec.ErrorMessage
		}
	}
	live.prog.Done = live.doneN
	live.prog.Failed = live.failN
	live.prog.SessionID = 0
	live.prog.StmtIndex = 0
	cp := *live.prog
	h.saveProgress(live.it.ID, &cp)
	_ = h.checkpointExec(live.it, live.merged)
	live.mu.Unlock()
}

func (h *TicketHandler) liveTouch(live *ddlLive, job ddlExecJob, executor, env string, stmtIndex, stmtTotal int, sessionID int64) {
	now := time.Now()
	live.mu.Lock()
	live.prog.CurrentInstance = ""
	live.prog.CurrentDB = job.dbName
	live.prog.InstanceID = job.id
	live.prog.Host = job.host
	live.prog.StmtIndex = stmtIndex
	live.prog.StmtTotal = stmtTotal
	live.prog.SessionID = sessionID
	live.prog.Executor = executor
	live.prog.Env = env
	if job.inst != nil {
		live.prog.CurrentInstance = job.inst.Name
		live.prog.DBType = job.inst.DBType
		live.prog.Engine = ticketexec.EngineOf(job.inst.DBType)
		if env == "" {
			live.prog.Env = job.inst.IDC
		}
	}
	write := stmtIndex <= 1 || now.Sub(live.last) >= 2*time.Second
	if write {
		live.last = now
	}
	cp := *live.prog
	live.mu.Unlock()
	if write {
		h.saveProgress(live.it.ID, &cp)
	}
}

func (h *TicketHandler) liveRemoteProgress(live *ddlLive, executor, env string, view sqlexec.JobView) {
	now := time.Now()
	live.mu.Lock()
	live.prog.Executor = executor
	if env != "" {
		live.prog.Env = env
	}
	live.prog.CurrentInstance = view.CurrentInstance
	live.prog.CurrentDB = view.CurrentDB
	live.prog.StmtIndex = view.StmtIndex
	live.prog.StmtTotal = view.StmtTotal
	live.prog.SessionID = view.SessionID
	live.prog.Engine = view.Engine
	live.prog.Done = live.doneN
	live.prog.Failed = live.failN
	write := view.StmtIndex <= 1 || now.Sub(live.last) >= 2*time.Second
	if write {
		live.last = now
	}
	cp := *live.prog
	live.mu.Unlock()
	if write {
		h.saveProgress(live.it.ID, &cp)
	}
}

// executeRemoteDML 把单库 DML 整段交给绑定了该环境的执行器。
func (h *TicketHandler) executeRemoteDML(ctx context.Context, it *store.Issue, inst *store.Instance, t ticketexec.Target, exec *sqlexec.Executor) error {
	target := sqlexec.Target{
		InstanceID: inst.ID, InstanceName: inst.Name, Host: t.Host, Port: t.Port,
		Username: t.Username, Password: t.Password, DBName: it.DBName, DBType: inst.DBType, Env: inst.IDC,
	}
	jobID, err := sqlexec.Submit(ctx, exec.BaseURL, exec.Username, exec.Password, sqlexec.JobRequest{
		IssueType: it.IssueType, SQL: it.SQLContent, Targets: []sqlexec.Target{target},
	})
	if err != nil {
		return h.persistExec(it, ticketexec.FailBeforeExec("下发执行器失败: "+err.Error(), ticketexec.Record{
			InstanceID: inst.ID, InstanceName: inst.Name, Host: t.Host, DB: it.DBName,
		}))
	}
	trackRemoteJob(it.ID, remoteHandle{BaseURL: exec.BaseURL, Username: exec.Username, Password: exec.Password, JobID: jobID})
	defer clearRemoteJobs(it.ID)
	prog := &store.ExecProgress{
		Total: 1, Phase: "running", CurrentInstance: inst.Name, CurrentDB: it.DBName,
		InstanceID: inst.ID, Host: t.Host, DBType: inst.DBType, Engine: ticketexec.EngineOf(inst.DBType),
		Env: inst.IDC, Executor: exec.Name,
	}
	h.saveProgress(it.ID, prog)
	for {
		if errors.Is(ctx.Err(), context.Canceled) || h.issueStopped(it.ID) {
			stopCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_ = sqlexec.StopJob(stopCtx, exec.BaseURL, exec.Username, exec.Password, jobID)
			cancel()
			return errExecStopped
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		view, err := sqlexec.Get(ctx, exec.BaseURL, exec.Username, exec.Password, jobID)
		if err == nil {
			prog.StmtIndex = view.StmtIndex
			prog.StmtTotal = view.StmtTotal
			prog.SessionID = view.SessionID
			prog.Done = view.Done
			prog.Failed = view.Failed
			h.saveProgress(it.ID, prog)
			if view.Status == sqlexec.JobStopped {
				return errExecStopped
			}
			if view.Status != sqlexec.JobRunning {
				res := &ticketexec.Result{Success: view.Failed == 0 && view.Done > 0, Message: view.Message}
				if len(view.Results) > 0 {
					item := view.Results[len(view.Results)-1]
					res.Success = item.OK
					res.SuccessCount = item.SuccessCount
					res.FailCount = item.FailCount
					res.Message = item.Error
					appendCompactDB(res, ticketexec.Record{
						InstanceID: inst.ID, InstanceName: inst.Name, Host: t.Host, DB: it.DBName, ErrorMessage: item.Error,
					}, item.OK, item.SuccessCount, item.FailCount)
				}
				return h.persistExec(it, res)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}
