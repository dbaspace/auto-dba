package dbdeploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type Config struct {
	DryRun          bool
	AgentArtifact   string
	AgentRemoteDir  string
	AgentListenPort int
	SSHUser            string
	SSHIdentityFile    string
	SSHInsecureHostKey bool
}

// Engine 通用 DB 集群部署编排（对齐 StarRocks 变更单模式）。
type Engine struct {
	Store  *store.DB
	Secret string
	Cfg    Config
	Agent  *AgentClient
	Svc    *Service
}

func New(st *store.DB, secretKey string, cfg Config) *Engine {
	return &Engine{
		Store:  st,
		Secret: secretKey,
		Cfg:    cfg,
		Agent:  NewAgentClient(),
		Svc:    NewService(st, secretKey),
	}
}

func (e *Engine) agentAuthWithPassword(agentID string) (AgentAuth, error) {
	var id, name, base, user, enc, host, remark, cb, ub, ca, ua string
	err := e.Store.QueryRow(`SELECT id, name, base_url, username, password_enc, host_instance_id, remark, created_by, updated_by, created_at, updated_at FROM tbl_db_deploy_agents WHERE id=?`, strings.TrimSpace(agentID)).
		Scan(&id, &name, &base, &user, &enc, &host, &remark, &cb, &ub, &ca, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentAuth{}, fmt.Errorf("DB Agent %s 不存在（可能已删除），请在该主机重新下发 Agent", strings.TrimSpace(agentID))
	}
	if err != nil {
		return AgentAuth{}, fmt.Errorf("读取 DB Agent 失败: %w", err)
	}
	pass := ""
	if enc != "" {
		pass, err = secret.Decrypt(e.Secret, enc)
		if err != nil {
			return AgentAuth{}, fmt.Errorf("解密 Agent 密码失败")
		}
	}
	return AgentAuth{BaseURL: base, Username: user, Password: pass}, nil
}

// StartDeploy 创建 cluster_deploy 变更并异步执行。
func (e *Engine) StartDeploy(clusterID, operator string) (string, error) {
	cl, err := e.Store.GetDbDeployCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	if cl.DeployKind == store.DbDeployKindCloud || cl.Topology == store.DbDeployTopoCloudEndpoint {
		return "", fmt.Errorf("云托管实例请使用「登记已有」，不走装机部署")
	}
	if err := AssertAllowDeploy(cl.Engine); err != nil {
		return "", err
	}
	if cl.Status == store.DbDeployStatusReady {
		return "", fmt.Errorf("集群已为 ready，无需再部署；如需装机请新建集群")
	}
	if strings.TrimSpace(cl.ReleaseID) == "" {
		return "", fmt.Errorf("请先绑定版本目录 release_id")
	}
	nodes, err := e.Store.ListDbDeployNodes(cl.ID)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("请先添加节点")
	}
	if err := e.validateTopology(cl, nodes); err != nil {
		return "", err
	}
	for _, n := range nodes {
		if strings.TrimSpace(n.AgentID) == "" {
			return "", fmt.Errorf("节点 %s 未绑定 agent_id", n.Host)
		}
	}
	if _, _, _, err := e.Svc.PresignRelease(cl.Engine, cl.ReleaseID, time.Hour); err != nil {
		return "", fmt.Errorf("制品签发失败: %w", err)
	}
	if list, err := e.Store.ListDbDeployChanges(cl.ID, 20); err == nil {
		for _, ch := range list {
			if ch.Status == store.DbChangeRunning || ch.Status == store.DbChangePending {
				return "", fmt.Errorf("已有进行中的变更 %s", ch.ID)
			}
		}
	}
	if cl.HaEnabled {
		if err := ValidateDeployHA(e.Store, cl, nodes); err != nil {
			return "", fmt.Errorf("HA 门禁未通过: %w", err)
		}
	}

	ch := &store.DbDeployChange{
		ClusterID: cl.ID,
		Type:      store.DbChangeClusterDeploy,
		Status:    store.DbChangePending,
		CreatedBy: operator,
	}
	if err := e.Store.CreateDbDeployChange(ch); err != nil {
		return "", err
	}
	steps := e.buildDeploySteps(cl, nodes)
	for i := range steps {
		steps[i].ChangeID = ch.ID
		if err := e.Store.CreateDbDeployChangeStep(&steps[i]); err != nil {
			return "", err
		}
	}
	cl.Status = store.DbDeployStatusDeploying
	cl.UpdatedBy = operator
	_ = e.Store.UpsertDbDeployCluster(cl)

	go func() {
		_ = e.runChange(context.Background(), ch.ID)
	}()
	return ch.ID, nil
}

func (e *Engine) validateTopology(cl *store.DbDeployCluster, nodes []store.DbDeployNode) error {
	if err := validateComponentTopology(cl, nodes); err != nil {
		return err
	}
	switch cl.Topology {
	case store.DbDeployTopoMasterReplica:
		hasPrimary, hasReplica := false, false
		for _, n := range nodes {
			if n.Role == "primary" || n.Role == "single" {
				hasPrimary = true
			}
			if n.Role == "replica" {
				hasReplica = true
			}
		}
		if !hasPrimary {
			return fmt.Errorf("主从拓扑需要 primary 节点")
		}
		if !hasReplica {
			return fmt.Errorf("主从拓扑至少需要一个 replica 节点")
		}
	case store.DbDeployTopoSentinel:
		hasSentinel, hasRedis := false, false
		for _, n := range nodes {
			if n.Role == "sentinel" {
				hasSentinel = true
			}
			if n.Role == "primary" || n.Role == "replica" || n.Role == "single" {
				hasRedis = true
			}
		}
		if !hasRedis {
			return fmt.Errorf("Sentinel 拓扑需要至少一个 Redis 数据节点")
		}
		if !hasSentinel {
			return fmt.Errorf("Sentinel 拓扑需要 sentinel 节点")
		}
	case store.DbDeployTopoCodis:
		need := map[string]bool{"codis_dashboard": false, "codis_proxy": false, "codis_server": false}
		for _, n := range nodes {
			if _, ok := need[n.Role]; ok {
				need[n.Role] = true
			}
		}
		for role, ok := range need {
			if !ok {
				return fmt.Errorf("Codis 拓扑缺少角色 %s", role)
			}
		}
	}
	return nil
}

func (e *Engine) buildDeploySteps(cl *store.DbDeployCluster, nodes []store.DbDeployNode) []store.DbDeployChangeStep {
	var steps []store.DbDeployChangeStep
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		steps = append(steps, store.DbDeployChangeStep{Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	add("agents_health", "探活全部 Db Agent", "")
	ordered := orderNodesForDeploy(cl, nodes)
	for _, n := range ordered {
		add("precheck_"+n.ID, "预检 "+n.Host+" ("+n.Role+")", n.ID)
		add("install_"+n.ID, "安装制品 "+n.Host, n.ID)
		add("ensure_fs_"+n.ID, "准备目录 "+n.Host, n.ID)
		add("put_conf_"+n.ID, "写入配置 "+n.Host, n.ID)
		add("systemd_"+n.ID, "安装 systemd "+n.Host, n.ID)
		add("start_"+n.ID, "启动服务 "+n.Host, n.ID)
	}
	if cl.Topology == store.DbDeployTopoMasterReplica || cl.Topology == store.DbDeployTopoSentinel {
		add("repl_setup", "配置复制关系", "")
		add("repl_verify", "复制闭环验收", "")
	}
	if cl.Topology == store.DbDeployTopoCodis {
		add("codis_bootstrap", "Codis 组件联通检查", "")
	}
	if isComponentChainDeploy(cl) {
		add("component_health", "组件健康验收", "")
	}
	if cl.HaEnabled {
		appendHASteps(add, cl.HaMode)
	}
	add("finalize", "完成", "")
	return steps
}

func (e *Engine) runChange(ctx context.Context, changeID string) error {
	_ = e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangeRunning, "开始执行")
	ch, err := e.Store.GetDbDeployChange(changeID)
	if err != nil || ch == nil {
		return fmt.Errorf("变更不存在")
	}
	cl, err := e.Store.GetDbDeployCluster(ch.ClusterID)
	if err != nil || cl == nil {
		_ = e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangeFailed, "集群不存在")
		return fmt.Errorf("集群不存在")
	}
	nodes, _ := e.Store.ListDbDeployNodes(cl.ID)
	nodeByID := map[string]*store.DbDeployNode{}
	for i := range nodes {
		nodeByID[nodes[i].ID] = &nodes[i]
	}
	steps, err := e.Store.ListDbDeployChangeSteps(changeID)
	if err != nil {
		return err
	}
	for i := range steps {
		step := &steps[i]
		if step.Status == store.DbStepSuccess || step.Status == store.DbStepSkipped {
			continue
		}
		step.Status = store.DbStepRunning
		step.StartedAt = time.Now().UTC().Format(time.RFC3339)
		_ = e.Store.UpdateDbDeployChangeStep(step)
		if err := e.execStep(ctx, cl, nodeByID, nodes, step); err != nil {
			step.Status = store.DbStepFailed
			step.LogText = err.Error()
			step.EndedAt = time.Now().UTC().Format(time.RFC3339)
			_ = e.Store.UpdateDbDeployChangeStep(step)
			_ = e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangeFailed, "步骤失败: "+step.StepKey+": "+err.Error())
			if ch.Type == store.DbChangeClusterDeploy {
				if cl.HaEnabled && haStepFailedAfterInstall(step.StepKey) {
					cl.Status = store.DbDeployStatusHaPending
					_ = e.Store.UpsertDbDeployCluster(cl)
					_ = e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangeFailed,
						"装机已完成，HA 接入失败，集群保持 ha_pending，可发起 ha_init 重试")
				} else {
					cl.Status = store.DbDeployStatusFailed
					_ = e.Store.UpsertDbDeployCluster(cl)
				}
			}
			if ch.Type == store.DbChangeHaInit {
				cl.Status = store.DbDeployStatusHaPending
				_ = e.Store.UpsertDbDeployCluster(cl)
			}
			return err
		}
		step.Status = store.DbStepSuccess
		step.EndedAt = time.Now().UTC().Format(time.RFC3339)
		_ = e.Store.UpdateDbDeployChangeStep(step)
	}
	_ = e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangeSuccess, "全部步骤完成")
	if ch.Type == store.DbChangeClusterDeploy || ch.Type == store.DbChangeHaInit {
		cl, _ = e.Store.GetDbDeployCluster(ch.ClusterID)
		if cl == nil {
			return nil
		}
		if cl.HaEnabled {
			if strings.TrimSpace(cl.HaClusterID) != "" {
				cl.Status = store.DbDeployStatusReady
			} else {
				cl.Status = store.DbDeployStatusHaPending
			}
		} else if ch.Type == store.DbChangeClusterDeploy {
			cl.Status = store.DbDeployStatusReady
		}
		_ = e.Store.UpsertDbDeployCluster(cl)
		if cl.Status == store.DbDeployStatusReady && strings.TrimSpace(cl.HaClusterID) != "" {
			_ = e.Store.MarkDbDeployHAReady(cl.ID)
		}
	}
	return nil
}

func (e *Engine) execStep(ctx context.Context, cl *store.DbDeployCluster, nodeByID map[string]*store.DbDeployNode, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	key := step.StepKey
	switch {
	case key == "agents_health":
		return e.stepAgentsHealth(ctx, nodes, step)
	case strings.HasPrefix(key, "precheck_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepPrecheck(ctx, cl, n, step)
	case strings.HasPrefix(key, "install_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepInstall(ctx, cl, n, step)
	case strings.HasPrefix(key, "ensure_fs_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepEnsureFS(ctx, cl, n, step)
	case strings.HasPrefix(key, "put_conf_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepPutConf(ctx, cl, n, nodes, step)
	case strings.HasPrefix(key, "systemd_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepSystemdInstall(ctx, cl, n, step)
	case strings.HasPrefix(key, "start_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepStart(ctx, cl, n, step)
	case key == "repl_setup":
		return e.stepReplSetup(ctx, cl, nodes, step)
	case key == "repl_verify":
		return e.stepReplVerify(ctx, cl, nodes, step)
	case key == "codis_bootstrap":
		step.LogText = "Codis dashboard/proxy/server 已按序启动；请在 dashboard 完成 slot 分配（如需）"
		return nil
	case key == "component_health":
		return e.stepComponentHealth(ctx, cl, nodes, step)
	case key == "ha_precheck":
		return e.stepHAPrecheck(ctx, cl, nodes, step)
	case key == "ha_register":
		return e.stepHARegister(cl, nodes, step)
	case key == "ha_create_cluster":
		return e.stepHACreateCluster(cl, nodes, step)
	case key == "ha_mha_push":
		return e.stepHAMHAPush(ctx, cl, step)
	case key == "ha_mha_verify":
		return e.stepHAMHAVerify(ctx, cl, step)
	case key == "ha_orch_probe":
		return e.stepHAOrchProbe(ctx, cl, step)
	case key == "ha_orch_env":
		return e.stepHACtrlEnv(ctx, cl, step, "orchestrator")
	case key == "ha_proxysql_env":
		return e.stepHACtrlEnv(ctx, cl, step, "proxysql")
	case key == "ha_pgaf_env":
		return e.stepHAPGAFEnv(ctx, cl, step)
	case key == "ha_patroni_env":
		return e.stepHACtrlEnv(ctx, cl, step, "patroni")
	case key == "ha_repmgr_env":
		return e.stepHACtrlEnv(ctx, cl, step, "repmgr")
	case key == "ha_pgaf_probe", key == "ha_patroni_probe", key == "ha_repmgr_probe":
		return e.stepHAOrchProbe(ctx, cl, step)
	case key == "finalize":
		step.LogText = "ok"
		return nil
	case strings.HasPrefix(key, "start_node_"):
		n := nodeByID[step.NodeID]
		return e.stepStart(ctx, cl, n, step)
	case strings.HasPrefix(key, "stop_node_"):
		n := nodeByID[step.NodeID]
		return e.stepStop(ctx, cl, n, step)
	case strings.HasPrefix(key, "restart_node_"):
		n := nodeByID[step.NodeID]
		return e.stepRestart(ctx, cl, n, step)
	case strings.HasPrefix(key, "repl_setup_one_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepReplSetup(ctx, cl, []store.DbDeployNode{*n}, step)
	case strings.HasPrefix(key, "repl_verify_one_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点不存在")
		}
		return e.stepReplVerify(ctx, cl, []store.DbDeployNode{*n}, step)
	case strings.HasPrefix(key, "drop_node_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			step.LogText = "节点已不存在，跳过"
			return nil
		}
		if err := e.Store.DeleteDbDeployNode(n.ID); err != nil {
			return err
		}
		step.LogText = "已从平台移除登记（数据目录保留在主机）"
		return nil
	default:
		return fmt.Errorf("未知步骤 %s", key)
	}
}

func (e *Engine) stepAgentsHealth(ctx context.Context, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	var logs []string
	for _, n := range nodes {
		auth, err := e.agentAuthWithPassword(n.AgentID)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Host, err)
		}
		if err := e.Agent.Probe(ctx, auth); err != nil {
			return fmt.Errorf("%s agent 探活失败: %w", n.Host, err)
		}
		logs = append(logs, auth.BaseURL+" ok")
	}
	step.LogText = strings.Join(logs, "; ")
	return nil
}

func (e *Engine) stepPrecheck(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	resp, err := e.Agent.Precheck(ctx, auth, map[string]any{
		"engine": cl.Engine, "role": n.Role, "change_id": step.ChangeID, "step_id": step.ID,
	})
	if err != nil {
		return err
	}
	if resp != nil && !resp.OK {
		return fmt.Errorf("预检未通过: %s", resp.Msg)
	}
	step.LogText = "precheck ok"
	return nil
}

func (e *Engine) stepInstall(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	urlStr, _, sum, err := e.Svc.PresignRelease(cl.Engine, cl.ReleaseID, 2*time.Hour)
	if err != nil {
		return err
	}
	target := softBase(cl, n)
	body := map[string]any{
		"download_url": urlStr, "checksum": sum, "target_dir": target,
		"change_id": step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun,
	}
	// 官方 MySQL / PG 二进制包带一层顶级目录；新版 Agent 仅在包确为单目录时剥离，旧版 Agent 忽略该字段
	if cl.Engine == store.DbDeployEngineMySQL || cl.Engine == store.DbDeployEnginePostgres {
		body["strip_components"] = 1
	}
	resp, err := e.Agent.ArtifactInstall(ctx, auth, body)
	if err != nil {
		return err
	}
	if resp != nil {
		step.LogText = resp.Message
		if step.LogText == "" {
			step.LogText = "artifact installed → " + target
		}
	}
	return nil
}

func (e *Engine) stepEnsureFS(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	paths := needDirs(cl, n)
	_, err = e.Agent.EnsureFS(ctx, auth, map[string]any{"paths": paths, "change_id": step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun})
	if err != nil {
		return err
	}
	step.LogText = strings.Join(paths, ", ")
	return nil
}

func (e *Engine) stepPutConf(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	primary := pickPrimary(nodes)
	var path, body, srcLabel string
	if TemplatedConfEngine(cl.Engine) {
		var src ConfTplSource
		if path, body, src, err = RenderTemplatedConf(e.Store, cl, n, primary, nil); err != nil {
			return err
		}
		srcLabel = src.Label
	} else {
		path, body = renderConf(cl, n, primary, nodes)
	}
	_, err = e.Agent.FilePut(ctx, auth, map[string]any{
		"path": path, "content": body, "mode": "0644",
		"change_id": step.ChangeID, "step_id": step.ID,
	})
	if err != nil {
		return err
	}
	// PG replica: standby.signal
	if cl.Engine == store.DbDeployEnginePostgres && n.Role == "replica" {
		sig := dataRoot(cl) + "/standby.signal"
		_, _ = e.Agent.FilePut(ctx, auth, map[string]any{"path": sig, "content": "", "mode": "0644"})
	}
	step.LogText = path
	if srcLabel != "" {
		step.LogText += "（" + srcLabel + "）"
	}
	return nil
}

func (e *Engine) stepSystemdInstall(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	unit, content := renderUnit(cl, n)
	_, err = e.Agent.SystemdInstall(ctx, auth, map[string]any{
		"unit_name": unit, "content": content,
		"change_id": step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	n.SystemdUnit = unit
	n.Port = nodePort(cl, n)
	_ = e.Store.UpsertDbDeployNode(n)
	step.LogText = unit
	return nil
}

func (e *Engine) stepStart(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	if n == nil {
		return fmt.Errorf("节点为空")
	}
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	unit := defaultUnit(cl, n)
	_, err = e.Agent.SystemdCtrl(ctx, auth, map[string]any{
		"action": "enable", "unit": unit, "dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	resp, err := e.Agent.SystemdCtrl(ctx, auth, map[string]any{
		"action": "start", "unit": unit, "dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		_ = e.Store.UpdateDbDeployNodeStatus(n.ID, store.DbNodeFailed)
		return err
	}
	_ = e.Store.UpdateDbDeployNodeStatus(n.ID, store.DbNodeReady)
	if resp != nil {
		step.LogText = resp.Message
	}
	if step.LogText == "" {
		step.LogText = "started " + unit
	}
	return nil
}

func (e *Engine) stepStop(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	if n == nil {
		return fmt.Errorf("节点为空")
	}
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	unit := defaultUnit(cl, n)
	resp, err := e.Agent.SystemdCtrl(ctx, auth, map[string]any{
		"action": "stop", "unit": unit, "dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	_ = e.Store.UpdateDbDeployNodeStatus(n.ID, store.DbNodeStopped)
	if resp != nil {
		step.LogText = resp.Message
	}
	if step.LogText == "" {
		step.LogText = "stopped " + unit
	}
	return nil
}

func (e *Engine) stepRestart(ctx context.Context, cl *store.DbDeployCluster, n *store.DbDeployNode, step *store.DbDeployChangeStep) error {
	if n == nil {
		return fmt.Errorf("节点为空")
	}
	auth, err := e.agentAuthWithPassword(n.AgentID)
	if err != nil {
		return err
	}
	unit := defaultUnit(cl, n)
	resp, err := e.Agent.SystemdCtrl(ctx, auth, map[string]any{
		"action": "restart", "unit": unit, "dry_run": e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	_ = e.Store.UpdateDbDeployNodeStatus(n.ID, store.DbNodeReady)
	if resp != nil {
		step.LogText = resp.Message
	}
	if step.LogText == "" {
		step.LogText = "restarted " + unit
	}
	return nil
}

func (e *Engine) stepReplSetup(ctx context.Context, cl *store.DbDeployCluster, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	primary := pickPrimary(nodes)
	if primary == nil {
		return fmt.Errorf("无主节点")
	}
	var logs []string
	for i := range nodes {
		n := &nodes[i]
		if n.Role != "replica" {
			continue
		}
		auth, err := e.agentAuthWithPassword(n.AgentID)
		if err != nil {
			return err
		}
		body := map[string]any{
			"engine": cl.Engine, "role": "replica",
			"primary_host": primary.Host, "primary_port": nodePort(cl, primary),
			"local_port": nodePort(cl, n),
			"change_id":  step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun,
		}
		resp, err := e.Agent.Exec(ctx, auth, map[string]any{
			"command": "repl_setup", "args": []string{}, "payload": body,
		})
		if err != nil {
			// 回退：仅记录提示，由 repl_verify 做硬门禁
			logs = append(logs, fmt.Sprintf("%s: setup warn: %v", n.Host, err))
			continue
		}
		msg := ""
		if resp != nil {
			msg = resp.Message
		}
		logs = append(logs, n.Host+": "+msg)
	}
	if len(logs) == 0 {
		step.LogText = "无 replica，跳过"
		return nil
	}
	step.LogText = strings.Join(logs, "\n")
	return nil
}

func (e *Engine) stepReplVerify(ctx context.Context, cl *store.DbDeployCluster, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	primary := pickPrimary(nodes)
	var logs []string
	verified := 0
	for i := range nodes {
		n := &nodes[i]
		if n.Role != "replica" && n.Role != "sentinel" {
			continue
		}
		auth, err := e.agentAuthWithPassword(n.AgentID)
		if err != nil {
			return err
		}
		body := map[string]any{
			"engine": cl.Engine, "role": n.Role,
			"port": nodePort(cl, n),
		}
		if primary != nil {
			body["primary_host"] = primary.Host
			body["primary_port"] = nodePort(cl, primary)
		}
		resp, err := e.Agent.ReplVerify(ctx, auth, body)
		if err != nil {
			return fmt.Errorf("%s 复制验收失败: %w", n.Host, err)
		}
		if resp == nil || !resp.OK {
			msg := ""
			if resp != nil {
				msg = resp.Msg
				if msg == "" {
					msg = resp.Detail
				}
			}
			return fmt.Errorf("%s 复制验收未通过: %s", n.Host, msg)
		}
		verified++
		logs = append(logs, fmt.Sprintf("%s ok: %s", n.Host, resp.Detail))
	}
	if cl.Topology == store.DbDeployTopoMasterReplica && verified == 0 {
		return fmt.Errorf("主从拓扑必须至少验收一个 replica")
	}
	step.LogText = strings.Join(logs, "\n")
	if step.LogText == "" {
		step.LogText = "verify skipped"
	}
	return nil
}

func (e *Engine) stepHAPrecheck(ctx context.Context, cl *store.DbDeployCluster, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	if err := ValidateDeployHA(e.Store, cl, nodes); err != nil {
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(cl.HaMode))
	logs := []string{fmt.Sprintf("mode=%s controller=%s topology=%s ok", mode, cl.ControllerID, cl.Topology)}
	if strings.TrimSpace(cl.ControllerID) != "" {
		switch mode {
		case "mha", "orchestrator", "proxysql_repl", "pg_autofailover", "patroni", "repmgr":
			ctx2, cancel := context.WithTimeout(ctx, haInitTimeout())
			defer cancel()
			res, err := EnvCheckHAController(ctx2, e.Store, e.Secret, cl.ControllerID)
			if err != nil {
				return fmt.Errorf("%s 环境检查失败: %w", mode, err)
			}
			if ok, _ := res["ok"].(bool); !ok {
				sum, _ := res["summary"].(string)
				if sum == "" {
					sum = mode + " 环境检查未通过"
				}
				return fmt.Errorf("%s", sum)
			}
			logs = append(logs, fmt.Sprintf("env-check: %v", res["summary"]))
		}
	}
	for _, w := range CollectHAVIPWarnings(cl) {
		logs = append(logs, "warning: "+w)
	}
	if line := FormatHAVIPAuditLine(cl); line != "" {
		logs = append(logs, line)
	}
	step.LogText = strings.Join(logs, "\n")
	return nil
}

func (e *Engine) stepHARegister(cl *store.DbDeployCluster, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	members, err := EnsureMembersFromDeploy(e.Store, cl, nodes)
	if err != nil {
		return err
	}
	var lines []string
	for _, m := range members {
		lines = append(lines, fmt.Sprintf("%s %s %s:%d", m.Role, m.InstanceID, m.Host, m.Port))
	}
	step.LogText = strings.Join(lines, "\n")
	return nil
}

func (e *Engine) stepHACreateCluster(cl *store.DbDeployCluster, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	members, err := EnsureMembersFromDeploy(e.Store, cl, nodes)
	if err != nil {
		return err
	}
	res, err := BindHACluster(e.Store, cl, members, "db-deploy")
	if err != nil {
		return err
	}
	// 刷新集群字段
	if fresh, e2 := e.Store.GetDbDeployCluster(cl.ID); e2 == nil && fresh != nil {
		*cl = *fresh
	}
	if res.Skipped {
		step.LogText = "已绑定 HA 集群 " + res.HAClusterID + "（跳过创建）"
	} else {
		logs := []string{"已创建 HA 集群 " + res.HAClusterID}
		if line := FormatHAVIPAuditLine(cl); line != "" {
			logs = append(logs, line)
		}
		step.LogText = strings.Join(logs, "\n")
	}
	return nil
}

func (e *Engine) stepHAMHAPush(ctx context.Context, cl *store.DbDeployCluster, step *store.DbDeployChangeStep) error {
	ctx2, cancel := context.WithTimeout(ctx, haInitTimeout())
	defer cancel()
	path, err := BindMHAConf(ctx2, e.Store, e.Secret, cl, "db-deploy")
	if err != nil {
		return err
	}
	logs := []string{"已下发 " + path}
	if line := FormatHAVIPAuditLine(cl); line != "" {
		logs = append(logs, line)
	}
	step.LogText = strings.Join(logs, "\n")
	return nil
}

func (e *Engine) stepHAMHAVerify(ctx context.Context, cl *store.DbDeployCluster, step *store.DbDeployChangeStep) error {
	ctx2, cancel := context.WithTimeout(ctx, haInitTimeout())
	defer cancel()
	detail, err := VerifyMHARepl(ctx2, e.Store, e.Secret, cl)
	if err != nil {
		return err
	}
	logs := []string{detail}
	if line := FormatHAVIPAuditLine(cl); line != "" {
		logs = append(logs, line)
	}
	step.LogText = strings.Join(logs, "\n")
	return nil
}

func (e *Engine) stepHAOrchProbe(ctx context.Context, cl *store.DbDeployCluster, step *store.DbDeployChangeStep) error {
	ctx2, cancel := context.WithTimeout(ctx, haInitTimeout())
	defer cancel()
	detail, err := ProbeHATopology(ctx2, e.Store, e.Secret, cl)
	if err != nil {
		return err
	}
	step.LogText = detail
	return nil
}

func (e *Engine) stepHAPGAFEnv(ctx context.Context, cl *store.DbDeployCluster, step *store.DbDeployChangeStep) error {
	return e.stepHACtrlEnv(ctx, cl, step, "pg_autofailover")
}

func (e *Engine) stepHACtrlEnv(ctx context.Context, cl *store.DbDeployCluster, step *store.DbDeployChangeStep, label string) error {
	ctx2, cancel := context.WithTimeout(ctx, haInitTimeout())
	defer cancel()
	out, err := EnvCheckHAController(ctx2, e.Store, e.Secret, cl.ControllerID)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(out)
	step.LogText = string(b)
	if ok, _ := out["ok"].(bool); !ok {
		sum, _ := out["summary"].(string)
		if sum == "" {
			sum = label + " 环境检查未通过"
		}
		return fmt.Errorf("%s", sum)
	}
	return nil
}

// StartHaInit 对 ha_pending 集群重试 HA 接入。
func (e *Engine) StartHaInit(clusterID, operator string) (string, error) {
	cl, err := e.Store.GetDbDeployCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	if !cl.HaEnabled {
		return "", fmt.Errorf("集群未启用 HA")
	}
	if cl.Status != store.DbDeployStatusHaPending {
		return "", fmt.Errorf("仅 ha_pending 可发起 ha_init，当前 %s", cl.Status)
	}
	nodes, err := e.Store.ListDbDeployNodes(cl.ID)
	if err != nil {
		return "", err
	}
	if err := ValidateDeployHA(e.Store, cl, nodes); err != nil {
		return "", fmt.Errorf("HA 门禁未通过: %w", err)
	}
	if err := e.rejectIfActiveChange(cl.ID); err != nil {
		return "", err
	}
	ch := &store.DbDeployChange{
		ClusterID: cl.ID,
		Type:      store.DbChangeHaInit,
		Status:    store.DbChangePending,
		CreatedBy: operator,
	}
	if err := e.Store.CreateDbDeployChange(ch); err != nil {
		return "", err
	}
	steps := buildHaInitSteps(cl)
	for i := range steps {
		steps[i].ChangeID = ch.ID
		if err := e.Store.CreateDbDeployChangeStep(&steps[i]); err != nil {
			return "", err
		}
	}
	cl.UpdatedBy = operator
	_ = e.Store.UpsertDbDeployCluster(cl)
	go func() { _ = e.runChange(context.Background(), ch.ID) }()
	return ch.ID, nil
}

// RetryChange 从失败步骤重试。
func (e *Engine) RetryChange(changeID, operator string) error {
	ch, err := e.Store.GetDbDeployChange(changeID)
	if err != nil || ch == nil {
		return fmt.Errorf("变更不存在")
	}
	if ch.Status == store.DbChangeRunning {
		return fmt.Errorf("变更正在执行")
	}
	_ = operator
	steps, _ := e.Store.ListDbDeployChangeSteps(changeID)
	for i := range steps {
		if steps[i].Status == store.DbStepFailed {
			steps[i].Status = store.DbStepPending
			steps[i].LogText = ""
			steps[i].StartedAt, steps[i].EndedAt = "", ""
			_ = e.Store.UpdateDbDeployChangeStep(&steps[i])
		}
	}
	_ = e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangePending, "用户重试")
	if ch.Type == store.DbChangeClusterDeploy {
		if cl, _ := e.Store.GetDbDeployCluster(ch.ClusterID); cl != nil {
			cl.Status = store.DbDeployStatusDeploying
			_ = e.Store.UpsertDbDeployCluster(cl)
		}
	}
	go func() { _ = e.runChange(context.Background(), changeID) }()
	return nil
}

// CancelChange 标记取消（不杀已在跑的 goroutine 细粒度中断；新步骤前会读状态可后续扩展）。
func (e *Engine) CancelChange(changeID, operator string) error {
	_ = operator
	return e.Store.UpdateDbDeployChangeStatus(changeID, store.DbChangeCancelled, "用户取消")
}
