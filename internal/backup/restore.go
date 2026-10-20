package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// LogicalRestorePayload 恢复工单 SQLContent JSON（逻辑 / 物理全量共用）。
type LogicalRestorePayload struct {
	SourceRunID      string   `json:"source_run_id"`
	TargetInstanceID string   `json:"target_instance_id"`
	TargetDB         string   `json:"target_db,omitempty"`
	Databases        []string `json:"databases,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	LocalPath        string   `json:"local_path,omitempty"`
	ObjectKey        string   `json:"object_key,omitempty"`
	Engine           string   `json:"engine,omitempty"`
	ConfirmProd      bool     `json:"confirm_prod,omitempty"`
	RestoreKind      string   `json:"restore_kind,omitempty"` // logical（默认）| physical | pitr
	DataDir          string   `json:"data_dir,omitempty"`     // 物理/PITR 目标数据目录（Agent 本机）
	ConfirmStop      bool     `json:"confirm_stop,omitempty"` // 物理/PITR 须确认已停库
	RecoverTo        string   `json:"recover_to,omitempty"`   // PITR 时间点 RFC3339
	WalOrBinlogDir   string   `json:"wal_or_binlog_dir,omitempty"` // Agent 本机 WAL 或 binlog 目录
}

// ParseLogicalRestorePayload 解析工单载荷。
func ParseLogicalRestorePayload(raw string) (*LogicalRestorePayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("恢复工单载荷为空")
	}
	var p LogicalRestorePayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("恢复工单载荷无效: %w", err)
	}
	p.SourceRunID = strings.TrimSpace(p.SourceRunID)
	p.TargetInstanceID = strings.TrimSpace(p.TargetInstanceID)
	p.TargetDB = strings.TrimSpace(p.TargetDB)
	p.AgentID = strings.TrimSpace(p.AgentID)
	p.LocalPath = strings.TrimSpace(p.LocalPath)
	p.ObjectKey = strings.TrimSpace(p.ObjectKey)
	p.Engine = strings.TrimSpace(p.Engine)
	p.RestoreKind = strings.ToLower(strings.TrimSpace(p.RestoreKind))
	p.DataDir = strings.TrimSpace(p.DataDir)
	p.RecoverTo = strings.TrimSpace(p.RecoverTo)
	p.WalOrBinlogDir = strings.TrimSpace(p.WalOrBinlogDir)
	if p.SourceRunID == "" {
		return nil, fmt.Errorf("缺少 source_run_id")
	}
	if p.TargetInstanceID == "" {
		return nil, fmt.Errorf("缺少 target_instance_id")
	}
	return &p, nil
}

// ExecuteLogicalRestore 按工单载荷执行恢复（逻辑或物理全量；同步，由工单后台调用）。
func (e *Engine) ExecuteLogicalRestore(ctx context.Context, payload *LogicalRestorePayload) (string, error) {
	if e == nil || e.Store == nil {
		return "", fmt.Errorf("备份引擎未初始化")
	}
	if payload == nil {
		return "", fmt.Errorf("恢复载荷为空")
	}
	run, err := e.Store.GetBackupRun(payload.SourceRunID)
	if err != nil || run == nil {
		return "", fmt.Errorf("备份记录不存在")
	}
	if !strings.EqualFold(run.Status, store.BackupRunSuccess) {
		return "", fmt.Errorf("仅成功的备份记录可恢复，当前状态=%s", run.Status)
	}
	method := strings.TrimSpace(run.Method)
	if method == "" {
		method = store.BackupMethodLogicalFull
	}
	kind := payload.RestoreKind
	if kind == "" {
		if store.IsPhysicalBackupMethod(method) {
			kind = "physical"
		} else {
			kind = "logical"
		}
	}
	switch kind {
	case "physical":
		return e.executePhysicalRestore(ctx, payload, run, method)
	case "pitr":
		return e.executePITRRestore(ctx, payload, run, method)
	}
	if store.IsPhysicalBackupMethod(method) {
		return "", fmt.Errorf("该备份为物理集，请使用物理恢复（restore_kind=physical）或 PITR（restore_kind=pitr）")
	}
	return e.executeLogicalRestore(ctx, payload, run)
}

func (e *Engine) resolveRestoreAgentAuth(payload *LogicalRestorePayload, run *store.BackupRun) (AgentAuth, *store.BackupPolicy, error) {
	policy, _ := e.Store.GetBackupPolicy(run.PolicyID)
	agentID := payload.AgentID
	if agentID == "" && policy != nil {
		agentID = strings.TrimSpace(policy.AgentID)
	}
	if agentID == "" {
		return AgentAuth{}, policy, fmt.Errorf("未指定 Backup Agent（请在策略或工单中绑定）")
	}
	agent, err := e.Store.GetBackupAgent(agentID)
	if err != nil || agent == nil {
		return AgentAuth{}, policy, fmt.Errorf("Backup Agent 不存在")
	}
	agentPass := ""
	if agent.PasswordEnc != "" {
		agentPass, err = secret.Decrypt(e.Secret, agent.PasswordEnc)
		if err != nil {
			return AgentAuth{}, policy, fmt.Errorf("解密 Agent 密码失败: %w", err)
		}
	}
	return AgentAuth{BaseURL: agent.BaseURL, Username: agent.Username, Password: agentPass}, policy, nil
}

func (e *Engine) executeLogicalRestore(ctx context.Context, payload *LogicalRestorePayload, run *store.BackupRun) (string, error) {
	auth, policy, err := e.resolveRestoreAgentAuth(payload, run)
	if err != nil {
		return "", err
	}
	inst, err := e.Store.GetInstance(payload.TargetInstanceID)
	if err != nil || inst == nil {
		return "", fmt.Errorf("目标实例不存在")
	}
	pass := ""
	if strings.TrimSpace(inst.Password) != "" {
		pass, err = secret.Decrypt(e.Secret, inst.Password)
		if err != nil {
			return "", fmt.Errorf("解密目标实例密码失败: %w", err)
		}
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	engine := payload.Engine
	if engine == "" {
		engine = run.Engine
	}
	if engine == "" {
		engine = store.NormalizeDBType(inst.DBType)
	}
	targetDB := payload.TargetDB
	if targetDB == "" && len(payload.Databases) > 0 {
		targetDB = payload.Databases[0]
	}
	localPath := payload.LocalPath
	if localPath == "" {
		localPath = run.LocalPath
	}
	objectKey := payload.ObjectKey
	if objectKey == "" {
		objectKey = run.ObjectKey
	}
	if localPath == "" && objectKey == "" {
		return "", fmt.Errorf("备份记录缺少 local_path / object_key，无法恢复")
	}

	req := LogicalRestoreRequest{
		Engine:    engine,
		Host:      host,
		Port:      inst.Port,
		Username:  inst.Username,
		Password:  pass,
		Database:  targetDB,
		Databases: payload.Databases,
		LocalPath: localPath,
		ObjectKey: objectKey,
	}
	if policy != nil && strings.TrimSpace(policy.StorageID) != "" {
		if st, _ := e.Store.GetBackupStorage(policy.StorageID); st != nil {
			req.Dest = LogicalDest{
				Kind:      st.Kind,
				LocalPath: st.LocalPath,
				Endpoint:  st.Endpoint,
				Bucket:    st.Bucket,
				Prefix:    st.Prefix,
				AccessKey: st.AccessKey,
			}
			if st.SecretKeyEnc != "" {
				if sk, derr := secret.Decrypt(e.Secret, st.SecretKeyEnc); derr == nil {
					req.Dest.SecretKey = sk
				}
			}
		}
	}
	runCtx, cancel := e.restoreCtx(ctx)
	defer cancel()
	resp, err := CallLogicalRestore(runCtx, auth, req)
	if err != nil {
		return "", err
	}
	msg := strings.TrimSpace(resp.Message)
	if msg == "" {
		msg = "逻辑恢复成功"
	}
	if resp.JobID != "" {
		msg = msg + " job_id=" + resp.JobID
	}
	return msg, nil
}

func (e *Engine) executePhysicalRestore(ctx context.Context, payload *LogicalRestorePayload, run *store.BackupRun, method string) (string, error) {
	if method != store.BackupMethodPhysicalFull && method != store.BackupMethodPhysicalIncr {
		return "", fmt.Errorf("物理恢复仅支持 physical_full / physical_incr（method=%s）", method)
	}
	if !payload.ConfirmStop {
		return "", fmt.Errorf("物理恢复须确认目标实例已停库（confirm_stop=true）")
	}
	dataDir := strings.TrimSpace(payload.DataDir)
	if dataDir == "" {
		return "", fmt.Errorf("物理恢复须指定 Agent 本机 data_dir（目标数据目录）")
	}
	auth, _, err := e.resolveRestoreAgentAuth(payload, run)
	if err != nil {
		return "", err
	}
	inst, err := e.Store.GetInstance(payload.TargetInstanceID)
	if err != nil || inst == nil {
		return "", fmt.Errorf("目标实例不存在")
	}
	engine := payload.Engine
	if engine == "" {
		engine = run.Engine
	}
	if engine == "" {
		engine = store.NormalizeDBType(inst.DBType)
	}
	chain, err := e.resolvePhysicalRestoreChain(run)
	if err != nil {
		return "", err
	}
	localPath := payload.LocalPath
	if localPath == "" {
		localPath = run.LocalPath
	}
	objectKey := payload.ObjectKey
	if objectKey == "" {
		objectKey = run.ObjectKey
	}
	if len(chain) == 0 && localPath == "" && objectKey == "" {
		return "", fmt.Errorf("备份记录缺少 local_path / object_key，无法恢复")
	}
	if len(chain) > 1 {
		eng := strings.ToLower(engine)
		if eng == "postgres" || eng == "postgresql" || eng == "pg" {
			return "", fmt.Errorf("PostgreSQL 物理增量链请改用 PITR（WAL）；当前仅 MySQL xtrabackup 增量链可自动拼接（链长度=%d）", len(chain))
		}
	}
	req := PhysicalRestoreRequest{
		Engine:      engine,
		LocalPath:   localPath,
		ObjectKey:   objectKey,
		DataDir:     dataDir,
		ConfirmStop: true,
		ChainDirs:   chain,
	}
	runCtx, cancel := e.restoreCtx(ctx)
	defer cancel()
	resp, err := CallPhysicalRestore(runCtx, auth, req)
	if err != nil {
		return "", err
	}
	msg := strings.TrimSpace(resp.Message)
	if msg == "" {
		if len(chain) > 1 {
			msg = fmt.Sprintf("物理增量链恢复成功（%d 段，请人工确认实例已用新数据目录拉起）", len(chain))
		} else {
			msg = "物理全量恢复成功（请人工确认实例已用新数据目录拉起）"
		}
	}
	if resp.JobID != "" {
		msg = msg + " job_id=" + resp.JobID
	}
	return msg, nil
}

// resolvePhysicalRestoreChain 沿 parent_run_id 回溯到 physical_full，返回 base→…→tip 的本机路径。
func (e *Engine) resolvePhysicalRestoreChain(tip *store.BackupRun) ([]string, error) {
	if tip == nil {
		return nil, fmt.Errorf("备份记录为空")
	}
	var runs []*store.BackupRun
	seen := map[string]bool{}
	cur := tip
	for i := 0; i < 64; i++ {
		if cur == nil {
			break
		}
		if seen[cur.ID] {
			return nil, fmt.Errorf("物理备份链存在环：%s", cur.ID)
		}
		seen[cur.ID] = true
		runs = append(runs, cur)
		pid := strings.TrimSpace(cur.ParentRunID)
		if pid == "" {
			// ExtraJSON 兼容
			if pid == "" && strings.TrimSpace(cur.ExtraJSON) != "" {
				var extra map[string]any
				if json.Unmarshal([]byte(cur.ExtraJSON), &extra) == nil {
					if v, ok := extra["parent_run_id"].(string); ok {
						pid = strings.TrimSpace(v)
					}
				}
			}
		}
		if pid == "" {
			break
		}
		parent, err := e.Store.GetBackupRun(pid)
		if err != nil || parent == nil {
			return nil, fmt.Errorf("物理备份链断裂：缺少 parent_run_id=%s", pid)
		}
		if !strings.EqualFold(parent.Status, store.BackupRunSuccess) {
			return nil, fmt.Errorf("物理备份链中 parent=%s 状态=%s，须为 success", parent.ID, parent.Status)
		}
		cur = parent
	}
	// tip→base 已收集，反转为 base→tip
	paths := make([]string, 0, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		p := strings.TrimSpace(runs[i].LocalPath)
		if p == "" {
			p = strings.TrimSpace(runs[i].ObjectKey)
		}
		if p == "" {
			return nil, fmt.Errorf("物理备份链节点 %s 缺少 local_path/object_key", runs[i].ID)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func (e *Engine) executePITRRestore(ctx context.Context, payload *LogicalRestorePayload, run *store.BackupRun, method string) (string, error) {
	if !store.IsPhysicalBackupMethod(method) {
		return "", fmt.Errorf("PITR 须基于物理备份集（physical_full/physical_incr），当前 method=%s", method)
	}
	if strings.TrimSpace(payload.RecoverTo) == "" {
		return "", fmt.Errorf("PITR 须指定 recover_to（RFC3339 时间点）")
	}
	if _, err := time.Parse(time.RFC3339, payload.RecoverTo); err != nil {
		if _, err2 := time.Parse(time.RFC3339Nano, payload.RecoverTo); err2 != nil {
			return "", fmt.Errorf("recover_to 须为 RFC3339: %w", err)
		}
	}
	if !payload.ConfirmStop {
		return "", fmt.Errorf("PITR 须确认目标已停库（confirm_stop=true）")
	}
	if strings.TrimSpace(payload.DataDir) == "" {
		return "", fmt.Errorf("PITR 须指定 data_dir")
	}
	if strings.TrimSpace(payload.WalOrBinlogDir) == "" {
		return "", fmt.Errorf("PITR 须指定 Agent 本机 wal_or_binlog_dir（PG 的 WAL 归档目录或 MySQL binlog 目录）")
	}
	auth, _, err := e.resolveRestoreAgentAuth(payload, run)
	if err != nil {
		return "", err
	}
	inst, err := e.Store.GetInstance(payload.TargetInstanceID)
	if err != nil || inst == nil {
		return "", fmt.Errorf("目标实例不存在")
	}
	engine := payload.Engine
	if engine == "" {
		engine = run.Engine
	}
	if engine == "" {
		engine = store.NormalizeDBType(inst.DBType)
	}
	localPath := payload.LocalPath
	if localPath == "" {
		localPath = run.LocalPath
	}
	objectKey := payload.ObjectKey
	if objectKey == "" {
		objectKey = run.ObjectKey
	}
	req := PITRRestoreRequest{
		Engine:         engine,
		LocalPath:      localPath,
		ObjectKey:      objectKey,
		DataDir:        payload.DataDir,
		ConfirmStop:    true,
		RecoverTo:      payload.RecoverTo,
		WalOrBinlogDir: payload.WalOrBinlogDir,
	}
	runCtx, cancel := e.restoreCtx(ctx)
	defer cancel()
	resp, err := CallPITRRestore(runCtx, auth, req)
	if err != nil {
		return "", err
	}
	msg := strings.TrimSpace(resp.Message)
	if msg == "" {
		msg = "PITR 准备完成（请按提示拉起实例并确认恢复到目标时间点）"
	}
	if resp.JobID != "" {
		msg = msg + " job_id=" + resp.JobID
	}
	return msg, nil
}

func (e *Engine) restoreCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := e.Cfg.DefaultTimeout
	if timeout <= 0 {
		timeout = 6 * time.Hour
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
