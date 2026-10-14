package srcluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"

	_ "github.com/go-sql-driver/mysql"
)

// Config 引擎参数。
type Config struct {
	DryRun          bool   // true：Agent 侧也可传 dry_run；控制面跳过破坏性 SQL 时写日志说明
	AgentArtifact   string // node-agent 本地制品路径
	AgentRemoteDir  string
	AgentListenPort int
	SSHUser            string
	SSHIdentityFile    string
	SSHInsecureHostKey bool
}

// Engine StarRocks 集群控制面编排。
type Engine struct {
	Store  *store.DB
	Secret string
	Cfg    Config
	Agent  *AgentClient
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func New(st *store.DB, secretKey string, cfg Config) *Engine {
	_ = EnsureDefaultConfTemplates(st)
	return &Engine{
		Store:  st,
		Secret: secretKey,
		Cfg:    cfg,
		Agent:  NewAgentClient(),
		cancel: map[string]context.CancelFunc{},
	}
}

// StartClusterInit 创建 cluster_init 变更单并异步执行。
func (e *Engine) StartClusterInit(clusterID, operator string) (string, error) {
	if e == nil || e.Store == nil {
		return "", fmt.Errorf("engine 未初始化")
	}
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	if cl.Status == store.SrClusterReady {
		return "", fmt.Errorf("集群已就绪，请使用「重新初始化」（会先停服务并备份软件包与数据目录）")
	}
	if err := e.rejectIfActiveChange(clusterID); err != nil {
		return "", err
	}
	nodes, primary, err := e.validateInitTopology(clusterID)
	if err != nil {
		return "", err
	}
	if err := e.ensureClusterRelease(cl); err != nil {
		return "", fmt.Errorf("集群 %s 初始化失败：%v", cl.Name, err)
	}
	if err := e.Store.ValidateReleaseForEnv(cl.Environment, cl.ReleaseID); err != nil {
		return "", fmt.Errorf("集群 %s 初始化失败：%v；集群声明版本=%s release_id=%s",
			cl.Name, err, strings.TrimSpace(cl.Version), cl.ReleaseID)
	}

	ch := &store.SrChange{
		ClusterID: clusterID,
		Type:      store.SrChangeClusterInit,
		Status:    store.SrChangePending,
		CreatedBy: operator,
	}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	steps := buildInitSteps(nodes, primary)
	for i := range steps {
		steps[i].ChangeID = ch.ID
		if err := e.Store.CreateSrChangeStep(&steps[i]); err != nil {
			return "", err
		}
	}
	cl.Status = store.SrClusterIniting
	cl.UpdatedBy = operator
	if err := e.Store.UpdateSrCluster(cl); err != nil {
		return "", err
	}

	go e.runChange(ch.ID)
	return ch.ID, nil
}

// MarkClusterReady 将纳管生命周期从 failed（或已有节点的 draft）标为 ready。
// 默认先 FE 探活（ClusterLive）；force=true 时跳过探活（运维确认业务已正常）。
// 不等于重新初始化，不启停进程、不装包。
func (e *Engine) MarkClusterReady(ctx context.Context, clusterID, operator string, force bool) error {
	if e == nil || e.Store == nil {
		return fmt.Errorf("engine 未初始化")
	}
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return fmt.Errorf("集群不存在")
	}
	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		return err
	}
	if err := canMarkClusterReady(cl.Status, len(nodes)); err != nil {
		return err
	}
	if err := e.rejectIfActiveChange(clusterID); err != nil {
		return err
	}
	if !force {
		if _, _, err := e.ClusterLive(ctx, cl); err != nil {
			return fmt.Errorf("FE 探活失败，请先在实例管理/FE 管理补齐密码后再试；确认业务已正常可传 force=true 强制标记: %w", err)
		}
	}
	prev := cl.Status
	cl.Status = store.SrClusterReady
	cl.UpdatedBy = operator
	if err := e.Store.UpdateSrCluster(cl); err != nil {
		return err
	}
	_ = e.Store.AppendCmdbAudit(operator, "update", "sr_cluster", cl.ID, "mark-ready",
		map[string]any{"status": prev}, map[string]any{"status": store.SrClusterReady, "force": force})
	return nil
}

func canMarkClusterReady(status string, nodeCount int) error {
	switch strings.TrimSpace(status) {
	case store.SrClusterFailed:
		return nil
	case store.SrClusterDraft:
		if nodeCount == 0 {
			return fmt.Errorf("草稿集群无节点，请先登记节点或执行初始化")
		}
		return nil
	case store.SrClusterReady:
		return fmt.Errorf("集群已是 ready")
	case store.SrClusterIniting:
		return fmt.Errorf("集群正在初始化中，请等待完成或取消变更后再标记")
	default:
		return fmt.Errorf("仅 status=failed（或已有节点的 draft）可标记就绪，当前为 %s", status)
	}
}

// StartClusterReinit 重新初始化：停旧服务 → 备份软件包与数据目录 → 再走完整初始化。
func (e *Engine) StartClusterReinit(clusterID, operator string) (string, error) {
	if e == nil || e.Store == nil {
		return "", fmt.Errorf("engine 未初始化")
	}
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	if err := e.rejectIfActiveChange(clusterID); err != nil {
		return "", err
	}
	// 上次 init/reinit 被取消后可能仍卡在 initializing；无活跃变更时允许重开
	if cl.Status == store.SrClusterIniting {
		cl.Status = store.SrClusterFailed
		_ = e.Store.UpdateSrCluster(cl)
	}
	nodes, primary, err := e.validateInitTopology(clusterID)
	if err != nil {
		return "", err
	}
	if err := e.ensureClusterRelease(cl); err != nil {
		return "", fmt.Errorf("集群 %s 重新初始化失败：%v", cl.Name, err)
	}
	if err := e.Store.ValidateReleaseForEnv(cl.Environment, cl.ReleaseID); err != nil {
		return "", fmt.Errorf("集群 %s 重新初始化失败：%v；集群声明版本=%s release_id=%s",
			cl.Name, err, strings.TrimSpace(cl.Version), cl.ReleaseID)
	}

	suffix := time.Now().Format("20060102-150405")
	payload, _ := json.Marshal(map[string]string{"backup_suffix": suffix})
	ch := &store.SrChange{
		ClusterID:   clusterID,
		Type:        store.SrChangeClusterReinit,
		Status:      store.SrChangePending,
		CreatedBy:   operator,
		PayloadJSON: string(payload),
	}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	steps := buildReinitPrepSteps(cl, nodes, suffix)
	steps = append(steps, buildInitSteps(nodes, primary)...)
	for i := range steps {
		steps[i].ChangeID = ch.ID
		steps[i].Seq = i + 1
		if err := e.Store.CreateSrChangeStep(&steps[i]); err != nil {
			return "", err
		}
	}
	cl.Status = store.SrClusterIniting
	cl.UpdatedBy = operator
	if err := e.Store.UpdateSrCluster(cl); err != nil {
		return "", err
	}
	go e.runChange(ch.ID)
	return ch.ID, nil
}

func (e *Engine) rejectIfActiveChange(clusterID string) error {
	list, err := e.Store.ListSrChanges(clusterID, 20)
	if err != nil {
		return err
	}
	for _, ch := range list {
		if ch.Status == store.SrChangeRunning || ch.Status == store.SrChangePending {
			return fmt.Errorf("集群仍有进行中的变更单 %s（%s），请先等待完成或取消后再操作", ch.ID, ch.Type)
		}
	}
	return nil
}

func (e *Engine) validateInitTopology(clusterID string) ([]store.SrNode, *store.SrNode, error) {
	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		return nil, nil, err
	}
	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("请先登记 FE/BE 节点")
	}
	feCount := 0
	var primary *store.SrNode
	for i := range nodes {
		n := &nodes[i]
		if n.Component == "fe" {
			feCount++
			if n.IsPrimaryFE {
				primary = n
			}
		}
		if strings.TrimSpace(n.AgentID) == "" {
			return nil, nil, fmt.Errorf("节点 %s (%s) 未绑定 agent_id", n.Host, n.Component)
		}
	}
	if err := store.ValidateOddFE(feCount); err != nil {
		return nil, nil, err
	}
	if primary == nil {
		return nil, nil, fmt.Errorf("请指定唯一首 FE（is_primary_fe）")
	}
	return nodes, primary, nil
}

// ensureClusterRelease 确保集群绑定到版本目录中仍可用的 release。
// - release_id 失效（删除重登记）→ 按 version 重绑
// - release 仍在但缺少可用 FE 分包（无 object_key / sha256 非 64 位）→ 按 version 重绑或给出明确缺口
func (e *Engine) ensureClusterRelease(cl *store.SrCluster) error {
	if cl == nil {
		return fmt.Errorf("集群为空")
	}
	oldID := strings.TrimSpace(cl.ReleaseID)
	ver := strings.TrimSpace(cl.Version)

	if oldID != "" {
		if rel, err := e.Store.GetSrRelease(oldID); err == nil && rel != nil {
			if ver == "" {
				ver = strings.TrimSpace(rel.Version)
				cl.Version = ver
			}
			if e.releaseHasInstallableRole(oldID, "fe") {
				if strings.TrimSpace(cl.Version) == "" && ver != "" {
					_ = e.Store.UpdateSrCluster(cl)
				}
				return nil
			}
			// 旧绑定仍在但 FE 分包不可用，继续尝试按版本名对齐
		}
	}

	if ver == "" {
		if oldID != "" {
			return fmt.Errorf("release_id=%s 的 FE 分包不可用，且集群未填 version，无法按版本重绑；请编辑集群重新选择版本目录中的版本", oldID)
		}
		return fmt.Errorf("未绑定版本：请编辑集群选择 release，或填写与版本目录一致的 version")
	}

	rel, err := e.Store.GetSrReleaseByVersion(ver)
	if err != nil || rel == nil {
		if oldID != "" {
			return fmt.Errorf("release_id=%s 的 FE 分包不可用，版本目录也无同名版本 %s；请到「设置 → 版本目录」登记并补全 FE 分包", oldID, ver)
		}
		return fmt.Errorf("声明版本 %s 未在「设置 → 版本目录」登记，请先登记并同步/补全 FE·BE 分包", ver)
	}

	if !e.releaseHasInstallableRole(rel.ID, "fe") {
		fe, _ := e.Store.GetSrReleaseArtifact(rel.ID, "fe")
		return artifactGapError(ver, rel.ID, "fe", fe)
	}

	if rel.ID != oldID {
		cl.ReleaseID = rel.ID
		cl.Version = rel.Version
		if err := e.Store.UpdateSrCluster(cl); err != nil {
			return fmt.Errorf("按版本 %s 重绑 release_id=%s 失败: %v", ver, rel.ID, err)
		}
	}
	return nil
}

func (e *Engine) releaseHasInstallableRole(releaseID, role string) bool {
	art, err := e.Store.GetSrReleaseArtifact(releaseID, role)
	if err != nil || art == nil {
		return false
	}
	return artifactInstallReady(art)
}

func artifactInstallReady(a *store.SrReleaseArtifact) bool {
	if a == nil {
		return false
	}
	if strings.TrimSpace(a.ObjectKey) == "" {
		return false
	}
	sum := strings.TrimSpace(a.SHA256)
	if len(sum) != 64 {
		return false
	}
	for _, c := range sum {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func artifactGapError(ver, releaseID, role string, art *store.SrReleaseArtifact) error {
	if art == nil || strings.TrimSpace(art.ObjectKey) == "" {
		return fmt.Errorf("版本 %s（release_id=%s）缺少 %s 分包：请到「设置 → 版本目录」同步或登记 object_key/sha256 后再重试", ver, releaseID, role)
	}
	sum := strings.TrimSpace(art.SHA256)
	if sum == "" {
		return fmt.Errorf("版本 %s 的 %s 分包缺少 sha256（object_key=%s）。脚本上传后请执行 shasum -a 256 文件名，把 64 位结果粘贴到版本目录并保存", ver, role, art.ObjectKey)
	}
	if len(sum) != 64 {
		return fmt.Errorf("版本 %s 的 %s 分包 sha256 不完整（当前 %d 位，须 64 位十六进制，object_key=%s）。请用 shasum -a 256 重算后在版本目录保存完整值", ver, role, len(sum), art.ObjectKey)
	}
	return fmt.Errorf("版本 %s 的 %s 分包不可用（release_id=%s）", ver, role, releaseID)
}

func buildInitSteps(nodes []store.SrNode, primary *store.SrNode) []store.SrChangeStep {
	var steps []store.SrChangeStep
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		steps = append(steps, store.SrChangeStep{Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	add("agents_health", "探活全部 Node Agent", "")
	add("precheck_all", "分角色预检", "")
	add("bootstrap_fe_primary", "首 FE 装机与启动", primary.ID)
	add("wait_fe_ready", "等待首 FE:9030 就绪", primary.ID)
	for _, n := range nodes {
		if n.Component != "fe" || n.ID == primary.ID {
			continue
		}
		add("add_follower_"+n.ID, "ADD FOLLOWER "+n.Host, n.ID)
		add("start_follower_"+n.ID, "从 FE --helper 启动", n.ID)
	}
	add("verify_frontends", "SHOW PROC '/frontends'", "")
	for _, n := range nodes {
		if n.Component != "be" {
			continue
		}
		add("init_be_"+n.ID, "BE 盘初始化与启动 "+n.Host, n.ID)
		add("add_backend_"+n.ID, "ADD BACKEND "+n.Host, n.ID)
	}
	add("verify_backends", "SHOW PROC '/backends'", "")
	add("finalize", "标记集群就绪", "")
	return steps
}

// buildReinitPrepSteps 停服务（先 BE 后 FE）并备份软件包/数据目录。
func buildReinitPrepSteps(cl *store.SrCluster, nodes []store.SrNode, suffix string) []store.SrChangeStep {
	_ = cl
	_ = suffix
	var steps []store.SrChangeStep
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		steps = append(steps, store.SrChangeStep{Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	add("agents_health", "探活 Agent（重装前）", "")
	for _, n := range nodes {
		if n.Component == "be" {
			add("stop_node_"+n.ID, "停止 BE "+n.Host, n.ID)
		}
	}
	var primary *store.SrNode
	for i := range nodes {
		n := &nodes[i]
		if n.Component != "fe" {
			continue
		}
		if n.IsPrimaryFE {
			primary = n
			continue
		}
		add("stop_node_"+n.ID, "停止 FE "+n.Host, n.ID)
	}
	if primary != nil {
		add("stop_node_"+primary.ID, "停止首 FE "+primary.Host, primary.ID)
	}
	for _, n := range nodes {
		add("backup_dirs_"+n.ID, "备份 "+n.Component+" 软件包/数据 "+n.Host, n.ID)
	}
	return steps
}

// CancelChange 停止后续步骤调度。
func (e *Engine) CancelChange(changeID string) error {
	e.mu.Lock()
	if cancel, ok := e.cancel[changeID]; ok {
		cancel()
	}
	e.mu.Unlock()
	if err := e.Store.UpdateSrChangeStatus(changeID, store.SrChangeCancelled, "用户取消"); err != nil {
		return err
	}
	e.markClusterFailedIfInitChange(changeID)
	return nil
}

// changeUpdatesClusterLifecycle 仅 init/reinit 改写集群 status（纳管生命周期）。
// 启停、扩缩、升级等运维失败只落在变更单，不污染集群列表态。
func changeUpdatesClusterLifecycle(typ string) bool {
	return typ == store.SrChangeClusterInit || typ == store.SrChangeClusterReinit
}

// markClusterFailedIfInitChange 取消/中止 init·reinit 后，避免集群卡在 initializing。
func (e *Engine) markClusterFailedIfInitChange(changeID string) {
	ch, err := e.Store.GetSrChange(changeID)
	if err != nil || ch == nil {
		return
	}
	if !changeUpdatesClusterLifecycle(ch.Type) {
		return
	}
	cl, err := e.Store.GetSrCluster(ch.ClusterID)
	if err != nil || cl == nil {
		return
	}
	if cl.Status != store.SrClusterIniting {
		return
	}
	// 仍有其它进行中的变更则不动状态
	if list, err := e.Store.ListSrChanges(cl.ID, 20); err == nil {
		for _, other := range list {
			if other.ID == changeID {
				continue
			}
			if other.Status == store.SrChangeRunning || other.Status == store.SrChangePending {
				return
			}
		}
	}
	cl.Status = store.SrClusterFailed
	_ = e.Store.UpdateSrCluster(cl)
}

// RetryChange 从首个失败/取消后的 pending 步继续。
func (e *Engine) RetryChange(changeID string) error {
	ch, err := e.Store.GetSrChange(changeID)
	if err != nil || ch == nil {
		return fmt.Errorf("变更单不存在")
	}
	if ch.Status == store.SrChangeRunning {
		return fmt.Errorf("变更单仍在执行")
	}
	if ch.Type == store.SrChangeUpgrade {
		// 期间可能有人改了 alternatives / JAVA_HOME，启服前重新校验 JDK。
		for i := range ch.Steps {
			s := &ch.Steps[i]
			if s.StepKey == "jdk_prepare" && s.Status == store.SrStepSuccess {
				s.Status = store.SrStepPending
				s.LogText = appendStepLine(s.LogText, time.Now(), "---- 重试：重新校验 JDK ----")
				_ = e.Store.UpdateSrChangeStep(s)
			}
		}
	}
	// 同步置 pending：调用方（工单等待）紧接着轮询时不会误读到旧的 failed。
	_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangePending, "用户重试")
	go e.runChange(changeID)
	return nil
}

func (e *Engine) runChange(changeID string) {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel[changeID] = cancel
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		delete(e.cancel, changeID)
		e.mu.Unlock()
	}()

	_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangeRunning, "开始执行")
	ch, err := e.Store.GetSrChange(changeID)
	if err != nil || ch == nil {
		return
	}
	cl, err := e.Store.GetSrCluster(ch.ClusterID)
	if err != nil || cl == nil {
		_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangeFailed, "集群不存在")
		return
	}
	if err := e.ensureClusterRelease(cl); err != nil {
		_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangeFailed, err.Error())
		if changeUpdatesClusterLifecycle(ch.Type) {
			cl.Status = store.SrClusterFailed
			_ = e.Store.UpdateSrCluster(cl)
		}
		return
	}
	nodes, _ := e.Store.ListSrNodes(ch.ClusterID)
	nodeByID := map[string]*store.SrNode{}
	for i := range nodes {
		nodeByID[nodes[i].ID] = &nodes[i]
	}
	if ch.Type == store.SrChangeUpgrade || ch.Type == store.SrChangeUpgradeRollback {
		nodes = activeNodes(nodes)
	}

	for _, step := range ch.Steps {
		if step.Status == store.SrStepSuccess || step.Status == store.SrStepSkipped {
			continue
		}
		select {
		case <-ctx.Done():
			_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangeCancelled, "已取消")
			e.markClusterFailedIfInitChange(changeID)
			return
		default:
		}
		step.Status = store.SrStepRunning
		step.StartedAt = time.Now().Format(time.RFC3339)
		_ = e.Store.UpdateSrChangeStep(&step)

		err := e.execStep(ctx, cl, nodeByID, nodes, &step)
		if err != nil {
			step.Status = store.SrStepFailed
			step.LogText = appendStepLine(step.LogText, time.Now(), "失败: "+err.Error())
			step.EndedAt = time.Now().Format(time.RFC3339)
			_ = e.Store.UpdateSrChangeStep(&step)
			_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangeFailed, "步骤失败: "+step.StepKey+": "+firstLine(err.Error()))
			if changeUpdatesClusterLifecycle(ch.Type) {
				cl.Status = store.SrClusterFailed
				_ = e.Store.UpdateSrCluster(cl)
			}
			return
		}
		step.Status = store.SrStepSuccess
		if step.LogText == "" {
			step.LogText = "ok"
		}
		step.EndedAt = time.Now().Format(time.RFC3339)
		_ = e.Store.UpdateSrChangeStep(&step)
	}
	if changeUpdatesClusterLifecycle(ch.Type) {
		cl.Status = store.SrClusterReady
		_ = e.Store.UpdateSrCluster(cl)
	}
	msg := "全部步骤完成"
	if extra := e.syncRegistryAfterChange(ch, cl); extra != "" {
		msg += "；" + extra
	}
	_ = e.Store.UpdateSrChangeStatus(changeID, store.SrChangeSuccess, msg)
}

func (e *Engine) execStep(ctx context.Context, cl *store.SrCluster, nodeByID map[string]*store.SrNode, nodes []store.SrNode, step *store.SrChangeStep) error {
	key := step.StepKey
	switch {
	case key == "agents_health":
		return e.stepAgentsHealth(ctx, nodes, step)
	case key == "precheck_all":
		return e.stepPrecheckAll(ctx, cl, nodes, step)
	case key == "jdk_prepare":
		return e.stepJDKPrepare(ctx, cl, nodes, step)
	case key == "bootstrap_fe_primary":
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("首 FE 节点缺失")
		}
		return e.stepBootstrapFE(ctx, cl, n, step)
	case key == "wait_fe_ready":
		return e.stepWaitFEReady(ctx, cl, step)
	case strings.HasPrefix(key, "add_follower_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepAddFollower(ctx, cl, n, step)
	case strings.HasPrefix(key, "start_follower_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepStartFollower(ctx, cl, n, step)
	case key == "verify_frontends":
		return e.stepShowProc(ctx, cl, "/frontends", upgradeTargetVersion(e.Store, step.ChangeID), step)
	case strings.HasPrefix(key, "init_be_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepInitBE(ctx, cl, n, step)
	case strings.HasPrefix(key, "add_backend_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepAddBackend(ctx, cl, n, step)
	case key == "verify_backends":
		return e.stepShowProc(ctx, cl, "/backends", upgradeTargetVersion(e.Store, step.ChangeID), step)
	case key == "finalize":
		step.LogText = "cluster status -> ready"
		return nil
	case strings.HasPrefix(key, "upgrade_node_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepUpgradeNode(ctx, cl, n, step)
	case strings.HasPrefix(key, "rollback_node_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepRollbackNode(ctx, cl, n, step)
	case strings.HasPrefix(key, "put_conf_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepPutConf(ctx, cl, n, step)
	case strings.HasPrefix(key, "restart_node_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepRestartNode(ctx, n, step)
	case strings.HasPrefix(key, "start_node_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepStartNode(ctx, n, step)
	case strings.HasPrefix(key, "drop_follower_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepDropFollower(ctx, cl, n, step)
	case strings.HasPrefix(key, "decommission_be_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepDecommissionBE(ctx, cl, n, step)
	case strings.HasPrefix(key, "drop_backend_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepDropBackend(ctx, cl, n, step)
	case strings.HasPrefix(key, "verify_decommission_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepVerifyDecommission(ctx, cl, n, step)
	case strings.HasPrefix(key, "verify_dropped_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepVerifyDropped(ctx, cl, n, step)
	case strings.HasPrefix(key, "cancel_decommission_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepCancelDecommission(ctx, cl, n, step)
	case strings.HasPrefix(key, "verify_recommission_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepVerifyRecommission(ctx, cl, n, step)
	case strings.HasPrefix(key, "stop_node_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepStopNode(ctx, n, step)
	case strings.HasPrefix(key, "backup_dirs_"):
		n := nodeByID[step.NodeID]
		if n == nil {
			return fmt.Errorf("节点缺失")
		}
		return e.stepBackupNodeDirs(ctx, cl, n, step)
	default:
		return fmt.Errorf("未知步骤 %s", key)
	}
}

func (e *Engine) agentAuth(agentID string) (AgentAuth, error) {
	ag, err := e.Store.GetSrAgent(agentID)
	if err != nil || ag == nil {
		return AgentAuth{}, fmt.Errorf("Node Agent %s 不存在（可能已删除），请在该主机重新下发 Agent", agentID)
	}
	pass, err := secret.Decrypt(e.Secret, ag.PasswordEnc)
	if err != nil {
		return AgentAuth{}, err
	}
	return AgentAuth{BaseURL: ag.BaseURL, Username: ag.Username, Password: pass}, nil
}

func (e *Engine) stepAgentsHealth(ctx context.Context, nodes []store.SrNode, step *store.SrChangeStep) error {
	var logs []string
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.AgentID] {
			continue
		}
		seen[n.AgentID] = true
		auth, err := e.agentAuth(n.AgentID)
		if err != nil {
			return err
		}
		if err := e.Agent.Probe(ctx, auth); err != nil {
			return fmt.Errorf("agent %s healthz: %w", n.AgentID, err)
		}
		logs = append(logs, auth.BaseURL+" ok")
	}
	step.LogText = strings.Join(logs, "; ")
	return nil
}

func (e *Engine) stepPrecheckAll(ctx context.Context, cl *store.SrCluster, nodes []store.SrNode, step *store.SrChangeStep) error {
	var logs []string
	for _, n := range nodes {
		if n.Status == store.SrNodeReady {
			logs = append(logs, n.Host+" skip(ready)")
			continue
		}
		auth, err := e.agentAuth(n.AgentID)
		if err != nil {
			return err
		}
		req := PrecheckReq{Role: n.Component, ChangeID: step.ChangeID, StepID: step.ID,
			HTTPPort: n.HTTPPort, QueryPort: cl.FeQueryPort, EditPort: n.EditPort, HBPort: n.HeartbeatPort}
		if n.Component == "fe" && req.HTTPPort <= 0 {
			req.HTTPPort = cl.FeHTTPPort
		}
		if n.Component == "be" {
			if req.HBPort <= 0 {
				req.HBPort = cl.BeHeartbeatPort
			}
			req.DataRoot = beDataRoot(cl, &n)
		}
		resp, err := e.Agent.Precheck(ctx, auth, req)
		if err != nil {
			return fmt.Errorf("%s (%s) precheck 请求失败: %w", n.Host, n.Component, err)
		}
		if n.Component == "be" && !hasPrecheckItem(resp, "data_root") {
			return fmt.Errorf("%s (be) 数据目录 %s：%s", n.Host, req.DataRoot, OldAgentDataRootMsg)
		}
		detail := formatPrecheckItems(resp)
		if resp != nil && !resp.OK {
			msg := strings.TrimSpace(resp.Msg)
			if msg == "" {
				msg = detail
			} else if detail != "" {
				msg = msg + "; " + detail
			}
			if msg == "" {
				msg = "Agent 未返回失败项明细"
			}
			return fmt.Errorf("%s (%s) precheck 未通过: %s", n.Host, n.Component, msg)
		}
		if detail != "" {
			logs = append(logs, fmt.Sprintf("%s (%s) ok: %s", n.Host, n.Component, detail))
		} else {
			logs = append(logs, fmt.Sprintf("%s (%s) ok", n.Host, n.Component))
		}
	}
	step.LogText = strings.Join(logs, "\n")
	return nil
}

func formatPrecheckItems(resp *PrecheckResp) string {
	if resp == nil || len(resp.Items) == 0 {
		return ""
	}
	var parts []string
	for _, it := range resp.Items {
		status := "ok"
		if it.Level == "warn" {
			status = "warn"
		} else if !it.OK {
			status = "FAIL"
		}
		msg := strings.TrimSpace(it.Message)
		if msg == "" {
			parts = append(parts, fmt.Sprintf("%s=%s", it.Name, status))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s(%s)", it.Name, status, msg))
		}
	}
	return strings.Join(parts, "; ")
}

func (e *Engine) installRoleArtifact(ctx context.Context, cl *store.SrCluster, auth AgentAuth, role, targetDir string, step *store.SrChangeStep) error {
	return e.installRoleArtifactWithOpts(ctx, cl, auth, role, targetDir, nil, "", step)
}

// installRoleArtifactUpgrade 升级专用：仅替换 bin/lib，并在软件目录内 bak.<suffix> 备份旧目录。
func (e *Engine) installRoleArtifactUpgrade(ctx context.Context, cl *store.SrCluster, auth AgentAuth, role, targetDir, backupSuffix string, step *store.SrChangeStep) error {
	return e.installRoleArtifactWithOpts(ctx, cl, auth, role, targetDir, []string{"bin", "lib"}, backupSuffix, step)
}

func (e *Engine) installRoleArtifactWithOpts(ctx context.Context, cl *store.SrCluster, auth AgentAuth, role, targetDir string, replaceDirs []string, backupSuffix string, step *store.SrChangeStep) error {
	urlStr, sum, format, filename, err := e.ResolveInstallSpec(ctx, cl, role)
	if err != nil {
		return err
	}
	body := map[string]any{
		"role": role, "target_dir": targetDir, "version": cl.Version,
		"download_url": urlStr, "sha256": sum, "format": format, "filename": filename,
		"strip_components": 1, "change_id": step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun,
	}
	if len(replaceDirs) > 0 {
		body["replace_dirs"] = replaceDirs
	}
	if strings.TrimSpace(backupSuffix) != "" {
		body["backup_suffix"] = strings.TrimSpace(backupSuffix)
	}
	resp, err := e.Agent.ArtifactInstall(ctx, auth, body)
	if err != nil {
		return err
	}
	if resp != nil && resp.Message != "" {
		step.LogText = resp.Message
	}
	return nil
}

func (e *Engine) stepBootstrapFE(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	soft := n.SoftwarePath
	if soft == "" {
		soft = strings.TrimRight(cl.SoftwareBase, "/") + "/fe"
	}
	unit := n.SystemdUnit
	if unit == "" {
		unit = fmt.Sprintf("fe-%d", cl.FeQueryPort)
	}
	if _, err := e.Agent.EnsureFS(ctx, auth, EnsureFSReq{Paths: []string{filepath.Dir(soft)}, ChangeID: step.ChangeID, StepID: step.ID}); err != nil {
		return err
	}
	if err := e.installRoleArtifact(ctx, cl, auth, "fe", soft, step); err != nil {
		return err
	}
	n.SoftwarePath = soft
	if confLog, err := e.ApplyConfToNode(ctx, cl, n, step.ChangeID, step.ID); err != nil {
		return fmt.Errorf("写入 fe.conf: %w", err)
	} else if confLog != "" {
		step.LogText = confLog
	}
	unitBody := fmt.Sprintf(`[Unit]
Description=StarRocks FE %d
After=network.target

[Service]
Type=forking
ExecStart=%s/bin/start_fe.sh --daemon
ExecStop=%s/bin/stop_fe.sh
Restart=on-failure
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, cl.FeQueryPort, soft, soft)
	if _, err := e.Agent.SystemdInstall(ctx, auth, map[string]any{
		"unit": unit, "content": unitBody, "change_id": step.ChangeID, "step_id": step.ID, "dry_run": e.Cfg.DryRun,
	}); err != nil {
		return err
	}
	if _, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "enable", Unit: unit, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun}); err != nil {
		return err
	}
	n.SoftwarePath = soft
	if _, err := e.startUnitWithDiag(ctx, auth, cl, n, unit, "start", step); err != nil {
		return err
	}
	n.Status = store.SrNodeReady
	n.SystemdUnit = unit
	_ = e.Store.UpdateSrNode(n)
	if strings.TrimSpace(cl.FeHost) == "" {
		cl.FeHost = n.Host
		_ = e.Store.UpdateSrCluster(cl)
	}
	if step.LogText == "" {
		step.LogText = "bootstrap fe ok unit=" + unit
	}
	return nil
}

func (e *Engine) stepWaitFEReady(ctx context.Context, cl *store.SrCluster, step *store.SrChangeStep) error {
	host := strings.TrimSpace(cl.FeHost)
	if host == "" {
		return fmt.Errorf("fe_host 为空")
	}
	addr := fmt.Sprintf("%s:%d", host, cl.FeQueryPort)
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 FE %s 超时", addr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		d := net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			// 尝试 SELECT 1
			if err := e.pingFE(ctx, cl); err == nil {
				step.LogText = addr + " ready"
				return nil
			}
			step.LogText = addr + " tcp ok, waiting mysql"
		}
		time.Sleep(2 * time.Second)
	}
}

func (e *Engine) feDSN(cl *store.SrCluster) (string, error) {
	if strings.TrimSpace(cl.FeHost) == "" {
		return "", fmt.Errorf("fe_host 为空")
	}
	var nodes []store.SrNode
	if e.Store != nil && cl.ID != "" {
		nodes, _ = e.Store.ListSrNodes(cl.ID)
	}
	return e.feDSNWithNodes(cl, nodes)
}

// feDSNWithNodes 同 feDSN，节点列表由调用方提供（避免重复读库）。
func (e *Engine) feDSNWithNodes(cl *store.SrCluster, nodes []store.SrNode) (string, error) {
	host := strings.TrimSpace(cl.FeHost)
	if host == "" {
		return "", fmt.Errorf("fe_host 为空")
	}
	// 初装允许空密码；后期上密码后由资源配置中的 StarRocks 实例提供
	user, pass, err := e.feCredentials(cl, nodes, false)
	if err != nil {
		return "", err
	}
	return buildFEDSN(user, pass, host, cl.FeQueryPort), nil
}

func (e *Engine) pingFE(ctx context.Context, cl *store.SrCluster) error {
	dsn, err := e.feDSN(cl)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(cctx)
}

func (e *Engine) execFESQL(ctx context.Context, cl *store.SrCluster, query string) error {
	if e.Cfg.DryRun {
		return nil
	}
	dsn, err := e.feDSN(cl)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = db.ExecContext(cctx, query)
	return err
}

func (e *Engine) stepAddFollower(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	edit := n.EditPort
	if edit <= 0 {
		edit = cl.FeEditPort
	}
	q := fmt.Sprintf(`ALTER SYSTEM ADD FOLLOWER "%s:%d"`, n.Host, edit)
	if e.Cfg.DryRun {
		step.LogText = "dry-run: " + q
		return nil
	}
	if err := e.execFESQL(ctx, cl, q); err != nil {
		return err
	}
	step.LogText = q
	return nil
}

func (e *Engine) stepStartFollower(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	soft := n.SoftwarePath
	if soft == "" {
		soft = strings.TrimRight(cl.SoftwareBase, "/") + "/fe"
	}
	helperHost := cl.FeHost
	helperPort := cl.FeEditPort
	cmd := soft + "/bin/start_fe.sh"
	args := []string{"--helper", fmt.Sprintf("%s:%d", helperHost, helperPort), "--daemon"}
	if _, err := e.Agent.EnsureFS(ctx, auth, EnsureFSReq{Paths: []string{filepath.Dir(soft)}, ChangeID: step.ChangeID, StepID: step.ID}); err != nil {
		return err
	}
	if err := e.installRoleArtifact(ctx, cl, auth, "fe", soft, step); err != nil {
		return err
	}
	n.SoftwarePath = soft
	if confLog, err := e.ApplyConfToNode(ctx, cl, n, step.ChangeID, step.ID); err != nil {
		return fmt.Errorf("写入 fe.conf: %w", err)
	} else if confLog != "" {
		step.LogText = confLog
	}
	if _, err := e.Agent.Exec(ctx, auth, ExecReq{Command: cmd, Args: args, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun}); err != nil {
		return err
	}
	unit := n.SystemdUnit
	if unit == "" {
		unit = fmt.Sprintf("fe-%d", cl.FeQueryPort)
	}
	unitBody := fmt.Sprintf(`[Unit]
Description=StarRocks FE follower
After=network.target
[Service]
Type=forking
ExecStart=%s/bin/start_fe.sh --helper %s:%d --daemon
Restart=on-failure
[Install]
WantedBy=multi-user.target
`, soft, helperHost, helperPort)
	if _, err := e.Agent.SystemdInstall(ctx, auth, map[string]any{"unit": unit, "content": unitBody, "dry_run": e.Cfg.DryRun}); err != nil {
		return err
	}
	if _, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "enable", Unit: unit, DryRun: e.Cfg.DryRun}); err != nil {
		return err
	}
	n.Status = store.SrNodeReady
	n.SoftwarePath = soft
	n.SystemdUnit = unit
	_ = e.Store.UpdateSrNode(n)
	step.LogText = fmt.Sprintf("helper %s:%d", helperHost, helperPort)
	return nil
}

func (e *Engine) stepInitBE(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	soft := n.SoftwarePath
	if soft == "" {
		soft = strings.TrimRight(cl.SoftwareBase, "/") + "/be"
	}
	dataRoot := beDataRoot(cl, n)
	diskResp, err := e.Agent.DiskInit(ctx, auth, DiskInitReq{
		MountPoint: dataRoot, FSType: "xfs", PersistFstab: true, Auto: true,
		ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun,
	})
	if err != nil {
		return fmt.Errorf("数据目录 %s: %w", dataRoot, err)
	}
	diskLog := ""
	if diskResp != nil && strings.TrimSpace(diskResp.Message) != "" {
		diskLog = "数据目录: " + strings.TrimSpace(diskResp.Message)
	}
	if _, err := e.Agent.EnsureFS(ctx, auth, EnsureFSReq{Paths: []string{filepath.Dir(soft)}, ChangeID: step.ChangeID, StepID: step.ID}); err != nil {
		return err
	}
	if err := e.installRoleArtifact(ctx, cl, auth, "be", soft, step); err != nil {
		return err
	}
	n.SoftwarePath = soft
	n.DataRoot = dataRoot
	if confLog, err := e.ApplyConfToNode(ctx, cl, n, step.ChangeID, step.ID); err != nil {
		return fmt.Errorf("写入 be.conf: %w", err)
	} else if confLog != "" {
		step.LogText = confLog
	}
	hb := n.HeartbeatPort
	if hb <= 0 {
		hb = cl.BeHeartbeatPort
	}
	unit := n.SystemdUnit
	if unit == "" {
		unit = fmt.Sprintf("be-%d", hb)
	}
	unitBody := fmt.Sprintf(`[Unit]
Description=StarRocks BE %d
After=network.target
[Service]
Type=forking
ExecStart=%s/bin/start_be.sh --daemon
Restart=on-failure
[Install]
WantedBy=multi-user.target
`, hb, soft)
	if _, err := e.Agent.SystemdInstall(ctx, auth, map[string]any{"unit": unit, "content": unitBody, "dry_run": e.Cfg.DryRun}); err != nil {
		return err
	}
	if _, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "enable", Unit: unit, DryRun: e.Cfg.DryRun}); err != nil {
		return err
	}
	if _, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "start", Unit: unit, DryRun: e.Cfg.DryRun}); err != nil {
		return err
	}
	n.Status = store.SrNodeReady
	n.SoftwarePath = soft
	n.DataRoot = dataRoot
	n.SystemdUnit = unit
	n.HeartbeatPort = hb
	_ = e.Store.UpdateSrNode(n)
	step.LogText = "be started " + unit + " data_root=" + dataRoot
	if diskLog != "" {
		step.LogText += "\n" + diskLog
	}
	return nil
}

func beDataRoot(cl *store.SrCluster, n *store.SrNode) string {
	if v := strings.TrimSpace(n.DataRoot); v != "" {
		return v
	}
	if v := strings.TrimSpace(cl.DataRoot); v != "" {
		return v
	}
	return "/data"
}

func hasPrecheckItem(resp *PrecheckResp, name string) bool {
	if resp == nil {
		return false
	}
	for _, it := range resp.Items {
		if it.Name == name {
			return true
		}
	}
	return false
}

func (e *Engine) stepAddBackend(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	hb := n.HeartbeatPort
	if hb <= 0 {
		hb = cl.BeHeartbeatPort
	}
	q := fmt.Sprintf(`ALTER SYSTEM ADD BACKEND "%s:%d"`, n.Host, hb)
	if e.Cfg.DryRun {
		step.LogText = "dry-run: " + q
		return nil
	}
	if err := e.execFESQL(ctx, cl, q); err != nil {
		return err
	}
	step.LogText = q
	return nil
}

// verMismatchGrace 全部 Alive 后版本仍不符时再等一会儿（刚重启的节点可能尚未随心跳上报新版本）。
const verMismatchGrace = 30 * time.Second

// stepShowProc 等待 SHOW PROC 全部 Alive；wantVersion 非空时还要求 Version 列与目标版本一致。
func (e *Engine) stepShowProc(ctx context.Context, cl *store.SrCluster, path, wantVersion string, step *store.SrChangeStep) error {
	if e.Cfg.DryRun {
		step.LogText = "dry-run: SHOW PROC '" + path + "'"
		return nil
	}
	deadline := time.Now().Add(3 * time.Minute)
	var lastErr error
	var verSince time.Time
	attempt := 0
	for {
		attempt++
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		summary, notAlive, verBad, err := e.queryProcAlive(ctx, cl, path, wantVersion)
		if err == nil && notAlive == 0 {
			if len(verBad) == 0 {
				if wantVersion != "" {
					summary += "；版本均为 " + wantVersion
				}
				step.LogText = summary
				return nil
			}
			if verSince.IsZero() {
				verSince = time.Now()
			}
			if time.Since(verSince) >= verMismatchGrace {
				step.LogText = summary
				return fmt.Errorf("版本未升级到 %s：%s（bin/lib 可能未替换成功或已被还原，请核对节点软件目录后重试）", wantVersion, strings.Join(verBad, "；"))
			}
			summary += "；版本不符: " + strings.Join(verBad, "；")
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("%s", summary)
		}
		step.LogText = fmt.Sprintf("等待 Alive（第 %d 次）: %s", attempt, summary)
		_ = e.Store.UpdateSrChangeStep(step)
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("SHOW PROC '%s' 超时仍未全部 Alive: %v", path, lastErr)
	}
	return fmt.Errorf("SHOW PROC '%s' 超时仍未全部 Alive", path)
}

func procTruthy(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes" || s == "y" || s == "t"
}

// queryProcAlive 执行 SHOW PROC，返回摘要；notAlive 为 Alive!=true 的行数；verBad 为 Version 与 wantVersion 不符的行。
func (e *Engine) queryProcAlive(ctx context.Context, cl *store.SrCluster, path, wantVersion string) (summary string, notAlive int, verBad []string, err error) {
	dsn, err := e.feDSN(cl)
	if err != nil {
		return "", 0, nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", 0, nil, err
	}
	defer db.Close()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows, err := db.QueryContext(cctx, "SHOW PROC '"+path+"'")
	if err != nil {
		return "", 0, nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(c)] = i
	}
	col := func(name string) int {
		if i, ok := idx[name]; ok {
			return i
		}
		return -1
	}
	aliveIdx, okAlive := idx["alive"]
	ipIdx := col("ip")
	if ipIdx < 0 {
		ipIdx = col("host")
	}
	nameIdx := col("name")
	roleIdx := col("role")
	errIdx := col("errmsg")
	joinIdx := col("join")
	verIdx := col("version")
	decomIdx := col("systemdecommissioned")
	checkVer := strings.TrimSpace(wantVersion) != "" && verIdx >= 0

	count := 0
	var bad []string
	var okRows []string
	for rows.Next() {
		count++
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", 0, nil, err
		}
		cell := func(i int) string {
			if i < 0 || i >= len(vals) {
				return ""
			}
			switch v := vals[i].(type) {
			case nil:
				return ""
			case []byte:
				return string(v)
			default:
				return strings.TrimSpace(fmt.Sprint(v))
			}
		}
		alive := ""
		if okAlive {
			alive = cell(aliveIdx)
		}
		label := cell(nameIdx)
		if label == "" {
			label = cell(ipIdx)
		}
		role := cell(roleIdx)
		join := cell(joinIdx)
		errmsg := cell(errIdx)
		ver := cell(verIdx)
		line := fmt.Sprintf("%s role=%s Alive=%s", label, role, alive)
		if join != "" {
			line += " Join=" + join
		}
		if ver != "" {
			line += " Version=" + ver
		}
		if errmsg != "" {
			line += " ErrMsg=" + errmsg
		}
		decommissioned := procTruthy(cell(decomIdx))
		if decommissioned {
			line += " Decommissioned"
		}
		// 下线中的 BE 不参与升级，版本不符不算失败
		if checkVer && !decommissioned && !versionMatches(ver, wantVersion) {
			if ver == "" {
				ver = "（未上报）"
			}
			verBad = append(verBad, fmt.Sprintf("%s 版本 %s", label, ver))
		}
		if okAlive && !procTruthy(alive) && !decommissioned {
			notAlive++
			bad = append(bad, line)
		} else {
			okRows = append(okRows, line)
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, nil, err
	}
	if count == 0 {
		return "SHOW PROC '" + path + "' 无行", 1, nil, fmt.Errorf("无行")
	}
	if !okAlive {
		// 无 Alive 列时仅确认有行
		return fmt.Sprintf("SHOW PROC '%s' rows=%d (无 Alive 列，已跳过存活校验)", path, count), 0, verBad, nil
	}
	if notAlive > 0 {
		return fmt.Sprintf("SHOW PROC '%s': %d/%d 未 Alive: %s", path, notAlive, count, strings.Join(bad, " | ")), notAlive, verBad, nil
	}
	return fmt.Sprintf("SHOW PROC '%s' rows=%d all alive: %s", path, count, strings.Join(okRows, " | ")), 0, verBad, nil
}

// versionMatches SHOW PROC 的 Version 形如 3.3.22-abc1234；须整段匹配，避免 3.3.2 误配 3.3.22。
func versionMatches(got, want string) bool {
	got, want = strings.TrimSpace(got), strings.TrimSpace(want)
	if got == "" || want == "" {
		return got == want
	}
	if got == want {
		return true
	}
	rest, ok := strings.CutPrefix(got, want)
	return ok && (rest[0] == '-' || rest[0] == ' ' || rest[0] == '_')
}

// upgradeTargetVersion 升级变更 payload 中的目标版本；非升级变更返回空（不校验版本）。
func upgradeTargetVersion(st *store.DB, changeID string) string {
	if st == nil {
		return ""
	}
	ch, err := st.GetSrChange(changeID)
	if err != nil || ch == nil || ch.Type != store.SrChangeUpgrade || ch.PayloadJSON == "" {
		return ""
	}
	var p map[string]string
	if json.Unmarshal([]byte(ch.PayloadJSON), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p["to_version"])
}
