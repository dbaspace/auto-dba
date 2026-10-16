package srcluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// StartUpgrade 滚动升级：先全部 BE → 校验 backends → 再 FE follower → 最后 FE leader。
// 每节点：停服 → 软件目录内备份并仅替换 bin/lib → 启服。任一步失败则停止后续动作。
func (e *Engine) StartUpgrade(clusterID, targetReleaseID, operator string) (string, error) {
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pre := e.PrecheckUpgrade(ctx, clusterID, targetReleaseID)
	if pre == nil || !pre.OK {
		msg := "升级前检查未通过"
		if pre != nil && strings.TrimSpace(pre.Summary) != "" {
			msg = pre.Summary
		}
		return "", fmt.Errorf("%s", msg)
	}
	rel, err := e.Store.GetSrRelease(targetReleaseID)
	if err != nil || rel == nil {
		return "", fmt.Errorf("目标版本不存在（release_id=%s），请到「设置 → 版本目录」确认", strings.TrimSpace(targetReleaseID))
	}
	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		return "", err
	}
	nodes = activeNodes(nodes)
	if len(nodes) == 0 {
		return "", fmt.Errorf("无节点")
	}
	fromReleaseID := cl.ReleaseID
	partial := strings.TrimSpace(cl.ReleaseID) == rel.ID
	if partial {
		// 绑定版本已是目标（例如上次误判成功）：只补升级实际版本不符的节点。
		actual, err := e.nodeActualVersions(ctx, cl, nodes)
		if err != nil {
			return "", fmt.Errorf("无法读取节点实际版本: %w", err)
		}
		bad, _ := mismatchedNodes(nodes, actual, rel.Version)
		if len(bad) == 0 {
			return "", fmt.Errorf("所有节点实际版本均为 %s，无需升级", rel.Version)
		}
		if rels, err := e.Store.ListSrReleases(false); err == nil {
			for _, n := range bad {
				if id := releaseIDForVersion(rels, actual[n.ID]); id != "" {
					fromReleaseID = id
					break
				}
			}
		}
		nodes = bad
	}
	suffix := time.Now().Format("20060102-150405")
	bes, feFollowers, feLeader, leaderSrc := orderUpgradeNodes(nodes, e.resolveFELeaderHost(cl))
	payloadMap := map[string]string{
		"from_release_id": fromReleaseID, "to_release_id": targetReleaseID, "to_version": rel.Version,
		"backup_suffix": suffix, "fe_leader_source": leaderSrc,
	}
	if partial {
		payloadMap["partial"] = "true"
	}
	if feLeader != nil {
		payloadMap["fe_leader_host"] = feLeader.Host
	}
	payload, _ := json.Marshal(payloadMap)
	// 临时把集群 release 切到目标，供 installRoleArtifact 解析（步骤失败时可由运维重试/改回）
	cl.ReleaseID = targetReleaseID
	cl.Version = rel.Version
	cl.UpdatedBy = operator
	if err := e.Store.UpdateSrCluster(cl); err != nil {
		return "", err
	}
	ch := &store.SrChange{ClusterID: clusterID, Type: store.SrChangeUpgrade, Status: store.SrChangePending, PayloadJSON: string(payload), CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	add("agents_health", "探活 Agent", "")
	add("jdk_prepare", fmt.Sprintf("检查 / 安装 JDK（%s 要求 %s）", rel.Version, JDKRangeText(releaseJDKRange(rel))), "")
	for _, n := range bes {
		add("upgrade_node_"+n.ID, "升级 BE "+n.Host+"（仅 bin/lib）", n.ID)
	}
	if len(bes) > 0 {
		add("verify_backends", "BE 全部升级后 SHOW PROC '/backends'", "")
	}
	for _, n := range feFollowers {
		add("upgrade_node_"+n.ID, "升级 FE follower "+n.Host+"（仅 bin/lib）", n.ID)
	}
	if feLeader != nil {
		title := "升级 FE leader " + feLeader.Host + "（仅 bin/lib）"
		if len(feFollowers) == 0 {
			title = "升级 FE " + feLeader.Host + "（仅 bin/lib）"
		}
		add("upgrade_node_"+feLeader.ID, title, feLeader.ID)
	}
	add("verify_frontends", "SHOW PROC '/frontends'", "")
	add("finalize", "升级完成", "")
	go e.runChange(ch.ID)
	return ch.ID, nil
}

// orderUpgradeNodes：BE → FE follower → FE leader。leaderHost 为空时回退 is_primary_fe。
func orderUpgradeNodes(nodes []store.SrNode, leaderHost string) (bes, followers []store.SrNode, leader *store.SrNode, leaderSrc string) {
	leaderHost = strings.TrimSpace(leaderHost)
	leaderSrc = "is_primary_fe"
	if leaderHost != "" {
		leaderSrc = "show_proc"
	}
	var primaryFallback *store.SrNode
	for i := range nodes {
		n := nodes[i]
		switch n.Component {
		case "be":
			bes = append(bes, n)
		case "fe":
			if leaderHost != "" && hostsEqual(n.Host, leaderHost) {
				cp := n
				leader = &cp
				continue
			}
			if n.IsPrimaryFE {
				cp := n
				primaryFallback = &cp
			}
			if leaderHost == "" && n.IsPrimaryFE {
				continue // 稍后作为 leader
			}
			followers = append(followers, n)
		}
	}
	if leader == nil && primaryFallback != nil {
		leader = primaryFallback
		leaderSrc = "is_primary_fe"
		// 从 followers 去掉 primary（若因 leaderHost 未命中而误入）
		var next []store.SrNode
		for _, f := range followers {
			if f.ID == primaryFallback.ID {
				continue
			}
			next = append(next, f)
		}
		followers = next
	}
	if leader == nil && len(followers) > 0 {
		// 仅有 FE 且未标记 primary：最后一个当 leader，前面当 follower
		cp := followers[len(followers)-1]
		leader = &cp
		followers = followers[:len(followers)-1]
		leaderSrc = "last_fe_fallback"
	}
	return bes, followers, leader, leaderSrc
}

func hostsEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	// 忽略端口后缀
	ah, _, _ := strings.Cut(a, ":")
	bh, _, _ := strings.Cut(b, ":")
	return strings.EqualFold(ah, bh)
}

// resolveFELeaderHost 查询当前 FE leader 的 IP/Name；失败返回空（由调用方回退）。
func (e *Engine) resolveFELeaderHost(cl *store.SrCluster) string {
	if e == nil || cl == nil || e.Cfg.DryRun {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	dsn, err := e.feDSN(cl)
	if err != nil {
		return ""
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return ""
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SHOW PROC '/frontends'")
	if err != nil {
		return ""
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(c)] = i
	}
	ipIdx := idx["ip"]
	nameIdx := idx["name"]
	roleIdx := idx["role"]
	masterIdx, okMaster := idx["ismaster"]
	if !okMaster {
		masterIdx = idx["is_master"]
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return ""
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
				return fmt.Sprint(v)
			}
		}
		role := strings.ToUpper(strings.TrimSpace(cell(roleIdx)))
		isMaster := procTruthy(cell(masterIdx))
		if role == "LEADER" || isMaster {
			if h := strings.TrimSpace(cell(ipIdx)); h != "" {
				return h
			}
			return strings.TrimSpace(cell(nameIdx))
		}
	}
	return ""
}

// StartAddNode 扩容：已有节点登记后，对指定 node 执行加入流程。
func (e *Engine) StartAddNode(clusterID, nodeID, operator string) (string, error) {
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	n, err := e.Store.GetSrNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if strings.TrimSpace(n.AgentID) == "" {
		return "", fmt.Errorf("节点未绑定 agent_id")
	}
	if strings.TrimSpace(cl.ReleaseID) != "" {
		if err := e.Store.ValidateReleaseForEnv(cl.Environment, cl.ReleaseID); err != nil {
			return "", err
		}
	}
	typ := store.SrChangeAddFE
	if n.Component == "be" {
		typ = store.SrChangeAddBE
	} else {
		cnt, _ := e.Store.CountSrFENodes(clusterID)
		if err := store.ValidateOddFE(cnt); err != nil {
			return "", err
		}
	}
	ch := &store.SrChange{ClusterID: clusterID, Type: typ, Status: store.SrChangePending, CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	seq := 0
	add := func(key, title string) {
		seq++
		_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	add("agents_health", "探活 Agent")
	add("precheck_all", "预检")
	if n.Component == "fe" {
		add("add_follower_"+n.ID, "ADD FOLLOWER")
		add("start_follower_"+n.ID, "--helper 启动")
		add("verify_frontends", "SHOW PROC frontends")
	} else {
		add("init_be_"+n.ID, "BE 装包启动")
		add("add_backend_"+n.ID, "ADD BACKEND")
		add("verify_backends", "SHOW PROC backends")
	}
	add("finalize", "完成")
	// precheck_all 需要全集群节点列表里至少含本节点 — runChange 会 ListSrNodes，OK
	go e.runChange(ch.ID)
	return ch.ID, nil
}

// StartRestartNode 单节点 systemctl restart。
func (e *Engine) StartRestartNode(clusterID, nodeID, operator string) (string, error) {
	n, err := e.Store.GetSrNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if n.Status == store.SrNodeDecommissioning {
		return "", errNodeDecommissioning
	}
	ch := &store.SrChange{ClusterID: clusterID, Type: store.SrChangeRestart, Status: store.SrChangePending, CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: 1, StepKey: "restart_node_" + n.ID, Title: "重启 " + n.Host, NodeID: nodeID})
	_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: 2, StepKey: "finalize", Title: "完成"})
	go e.runChange(ch.ID)
	return ch.ID, nil
}

// StartNodeStart 单节点 systemctl start。
func (e *Engine) StartNodeStart(clusterID, nodeID, operator string) (string, error) {
	return e.startNodePower(clusterID, nodeID, operator, "start")
}

// StartNodeStop 单节点 systemctl stop。
func (e *Engine) StartNodeStop(clusterID, nodeID, operator string) (string, error) {
	return e.startNodePower(clusterID, nodeID, operator, "stop")
}

func (e *Engine) startNodePower(clusterID, nodeID, operator, action string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "start" && action != "stop" {
		return "", fmt.Errorf("action 仅支持 start|stop")
	}
	n, err := e.Store.GetSrNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if n.Status == store.SrNodeDecommissioning {
		return "", errNodeDecommissioning
	}
	if strings.TrimSpace(n.AgentID) == "" {
		return "", fmt.Errorf("节点未绑定 agent_id")
	}
	if strings.TrimSpace(n.SystemdUnit) == "" {
		return "", fmt.Errorf("节点未配置 systemd_unit")
	}
	typ := store.SrChangeStartNode
	title := "启动 " + n.Host
	stepKey := "start_node_" + n.ID
	if action == "stop" {
		typ = store.SrChangeStopNode
		title = "停止 " + n.Host
		stepKey = "stop_node_" + n.ID
	}
	ch := &store.SrChange{ClusterID: clusterID, Type: typ, Status: store.SrChangePending, CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: 1, StepKey: stepKey, Title: title, NodeID: nodeID})
	_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: 2, StepKey: "finalize", Title: "完成"})
	go e.runChange(ch.ID)
	return ch.ID, nil
}

// StartRemoveNode FE DROP FOLLOWER 或 BE DECOMMISSION（默认）/ DROP。
func (e *Engine) StartRemoveNode(clusterID, nodeID, operator string, forceDropBE bool) (string, error) {
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	n, err := e.Store.GetSrNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	typ := store.SrChangeRemoveFE
	if n.Component == "be" {
		if forceDropBE {
			typ = store.SrChangeRemoveBE
		} else {
			if n.Status == store.SrNodeDecommissioning {
				return "", fmt.Errorf("该 BE 已在下线中（DECOMMISSION 迁移 tablet），无需重复执行；如需中止请走工单「恢复节点」")
			}
			typ = store.SrChangeDecommissionBE
		}
	} else {
		cnt, _ := e.Store.CountSrFENodes(clusterID)
		if err := store.ValidateOddFE(cnt - 1); err != nil {
			return "", fmt.Errorf("删除后 FE 数不合法: %w", err)
		}
		if n.IsPrimaryFE {
			return "", fmt.Errorf("不可直接删除首 FE，请先切换")
		}
	}
	ch := &store.SrChange{ClusterID: clusterID, Type: typ, Status: store.SrChangePending, CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	seq := 0
	add := func(key, title string) {
		seq++
		_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	if n.Component == "fe" {
		add("drop_follower_"+n.ID, "DROP FOLLOWER")
		add("stop_node_"+n.ID, "停止 FE")
		add("verify_frontends", "SHOW PROC frontends")
	} else {
		for _, s := range beRemoveSteps(n.ID, forceDropBE) {
			add(s[0], s[1])
		}
	}
	add("finalize", "完成")
	go e.runChange(ch.ID)
	return ch.ID, nil
}

// ConfigApplyOpts 配置下发参数。
type ConfigApplyOpts struct {
	Roles   []string // fe / be；空则按有草稿的角色
	NodeIDs []string // 空则该角色全部节点
	Restart bool
}

// StartConfigApply 将平台草稿 fe.conf/be.conf 经 Agent file/put 覆盖下发。
func (e *Engine) StartConfigApply(clusterID, operator string, opts ConfigApplyOpts) (string, error) {
	if e == nil || e.Store == nil {
		return "", fmt.Errorf("engine 未初始化")
	}
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		return "", err
	}
	nodes = activeNodes(nodes)
	if len(nodes) == 0 {
		return "", fmt.Errorf("无节点")
	}

	roleSet := map[string]bool{}
	for _, r := range opts.Roles {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "fe" || r == "be" {
			roleSet[r] = true
		}
	}
	if len(roleSet) == 0 {
		cfgs, _ := e.Store.ListSrClusterConfigs(clusterID)
		for _, c := range cfgs {
			if strings.TrimSpace(c.Content) != "" {
				roleSet[c.Role] = true
			}
		}
		tpls, _ := e.Store.ListSrConfTemplates()
		for _, t := range tpls {
			if strings.TrimSpace(t.Content) != "" {
				roleSet[t.Role] = true
			}
		}
		// 仍空则按节点角色 + 内置模板
		if len(roleSet) == 0 {
			for _, n := range nodes {
				r := strings.ToLower(n.Component)
				if r == "fe" || r == "be" {
					roleSet[r] = true
				}
			}
		}
	}
	if len(roleSet) == 0 {
		return "", fmt.Errorf("无可用 fe/be 配置模板")
	}

	hashByRole := map[string]string{}
	sourceByRole := map[string]string{}
	for role := range roleSet {
		body, src, err := EffectiveConfTemplate(e.Store, clusterID, role, "")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(body) == "" {
			return "", fmt.Errorf("角色 %s 无配置模板", role)
		}
		if err := RejectLockedDataDirKeys(body); err != nil {
			return "", fmt.Errorf("%s.conf (%s): %w", role, src, err)
		}
		hashByRole[role] = shortContentHash(body)
		sourceByRole[role] = src
	}

	nodeFilter := map[string]bool{}
	for _, id := range opts.NodeIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			nodeFilter[id] = true
		}
	}
	if len(nodeFilter) > 0 {
		pickedRoles := map[string]bool{}
		for _, n := range nodes {
			if nodeFilter[n.ID] {
				pickedRoles[strings.ToLower(n.Component)] = true
			}
		}
		if len(pickedRoles) > 1 {
			return "", fmt.Errorf("只能同时下发同一角色的节点，请分别下发 FE 与 BE")
		}
	}

	var targets []store.SrNode
	for _, n := range nodes {
		if !roleSet[n.Component] {
			continue
		}
		if len(nodeFilter) > 0 && !nodeFilter[n.ID] {
			continue
		}
		if strings.TrimSpace(n.AgentID) == "" {
			return "", fmt.Errorf("节点 %s (%s) 未绑定 agent_id", n.Host, n.Component)
		}
		targets = append(targets, n)
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("无匹配的下发目标节点")
	}

	payload, _ := json.Marshal(map[string]any{
		"roles":        keysOf(roleSet),
		"restart":      opts.Restart,
		"node_ids":     opts.NodeIDs,
		"content_hash": hashByRole,
		"source":       sourceByRole,
	})
	ch := &store.SrChange{
		ClusterID:   clusterID,
		Type:        store.SrChangeConfigApply,
		Status:      store.SrChangePending,
		PayloadJSON: string(payload),
		CreatedBy:   operator,
	}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{
			ChangeID: ch.ID, Seq: seq, StepKey: key, Title: title, NodeID: nodeID,
		})
	}
	for _, n := range targets {
		add("put_conf_"+n.ID, "下发 "+n.Component+".conf → "+n.Host, n.ID)
	}
	if opts.Restart {
		for _, n := range targets {
			add("restart_node_"+n.ID, "重启 "+n.Host, n.ID)
		}
	}
	add("finalize", "配置下发完成", "")
	go e.runChange(ch.ID)
	return ch.ID, nil
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (e *Engine) stepUpgradeNode(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	unit := n.SystemdUnit
	soft := n.SoftwarePath
	role := n.Component
	if soft == "" {
		soft = softwareBaseForRole(cl, role) + "/" + role
	}
	if unit == "" {
		return fmt.Errorf("节点未配置 systemd_unit，无法启服（未停服、未替换）")
	}
	prev := step.LogText
	if strings.TrimSpace(prev) != "" {
		step.LogText = prev + "\n---- 重试 " + time.Now().Format("2006-01-02 15:04:05") + " ----"
	}
	if err := e.checkUnitDaemon(ctx, auth, unit, step); err != nil {
		return err
	}
	// 重试也重新替换：旧日志标记不能证明磁盘上的 bin/lib 已是目标版本（可能首次未换成或被手工还原）。
	// Agent 发现 bak.<suffix>/<dir> 已存在时另存到 .retry-<时间>，首次备份（升级前版本）保留，回滚不受影响。
	e.stepLog(step, "停服 %s", unit)
	if _, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "stop", Unit: unit, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun}); err != nil {
		return fmt.Errorf("停服失败: %w", err)
	}
	suffix := upgradeBackupSuffix(e.Store, step.ChangeID)
	e.stepLog(step, "下载 %s %s 制品并替换 bin/lib（旧目录备份到 %s/bak.%s）", role, cl.Version, soft, suffix)
	keep := step.LogText
	if err := e.installRoleArtifactUpgrade(ctx, cl, auth, role, soft, suffix, step); err != nil {
		step.LogText = keep
		return fmt.Errorf("下载/替换失败: %w", err)
	}
	agentMsg := strings.TrimSpace(step.LogText)
	if agentMsg == strings.TrimSpace(keep) {
		agentMsg = "ok"
	}
	step.LogText = keep
	e.stepLog(step, "%s %s", stepMarkReplaced, agentMsg)
	n.SoftwarePath = soft
	e.stepLog(step, "启动 %s", unit)
	if _, err := e.startUnitWithDiag(ctx, auth, cl, n, unit, "start", step); err != nil {
		return fmt.Errorf("%s，已停止后续节点: %w", legacyStartFailed, err)
	}
	n.Status = store.SrNodeReady
	_ = e.Store.UpdateSrNode(n)
	e.stepLog(step, "已启动 %s", unit)
	return nil
}

// upgradeBackupSuffix 同一次升级所有节点共用的备份后缀（存于变更 payload）。
func upgradeBackupSuffix(st *store.DB, changeID string) string {
	if ch, err := st.GetSrChange(changeID); err == nil && ch != nil && ch.PayloadJSON != "" {
		var p map[string]string
		if json.Unmarshal([]byte(ch.PayloadJSON), &p) == nil {
			if s := strings.TrimSpace(p["backup_suffix"]); s != "" {
				return s
			}
		}
	}
	return time.Now().Format("20060102-150405")
}

func (e *Engine) stepRestartNode(ctx context.Context, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	if n.SystemdUnit == "" {
		return fmt.Errorf("节点未配置 systemd_unit")
	}
	resp, err := e.startUnitWithDiag(ctx, auth, e.clusterOf(n), n, n.SystemdUnit, "restart", step)
	if err != nil {
		return err
	}
	if resp != nil {
		step.LogText = resp.Message
	}
	n.Status = store.SrNodeReady
	_ = e.Store.UpdateSrNode(n)
	return nil
}

func (e *Engine) stepStartNode(ctx context.Context, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	if n.SystemdUnit == "" {
		return fmt.Errorf("节点未配置 systemd_unit")
	}
	resp, err := e.startUnitWithDiag(ctx, auth, e.clusterOf(n), n, n.SystemdUnit, "start", step)
	if err != nil {
		return err
	}
	if resp != nil {
		step.LogText = resp.Message
	}
	n.Status = store.SrNodeReady
	_ = e.Store.UpdateSrNode(n)
	if step.LogText == "" {
		step.LogText = "started " + n.SystemdUnit
	}
	return nil
}

func (e *Engine) stepStopNode(ctx context.Context, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	if n.SystemdUnit == "" {
		step.LogText = "no unit skip"
		return nil
	}
	resp, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "stop", Unit: n.SystemdUnit, DryRun: e.Cfg.DryRun})
	if err != nil {
		return err
	}
	if resp != nil && resp.Message != "" {
		step.LogText = resp.Message
	} else {
		step.LogText = "stopped " + n.SystemdUnit
	}
	n.Status = store.SrNodeStopped
	_ = e.Store.UpdateSrNode(n)
	return nil
}

func (e *Engine) stepBackupNodeDirs(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	soft := strings.TrimSpace(n.SoftwarePath)
	if soft == "" {
		role := "fe"
		if n.Component == "be" {
			role = "be"
		}
		soft = softwareBaseForRole(cl, role) + "/" + role
	}
	paths := []string{soft}
	if n.Component == "be" {
		dataRoot := strings.TrimSpace(n.DataRoot)
		if dataRoot == "" {
			dataRoot = strings.TrimSpace(cl.DataRoot)
		}
		if dataRoot == "" {
			dataRoot = "/data"
		}
		if dataRoot != soft {
			paths = append(paths, dataRoot)
		}
	}
	suffix := ""
	if ch, err := e.Store.GetSrChange(step.ChangeID); err == nil && ch != nil && ch.PayloadJSON != "" {
		var p map[string]string
		if json.Unmarshal([]byte(ch.PayloadJSON), &p) == nil {
			suffix = strings.TrimSpace(p["backup_suffix"])
		}
	}
	if suffix == "" {
		suffix = time.Now().Format("20060102-150405")
	}
	resp, err := e.Agent.EnsureFS(ctx, auth, EnsureFSReq{
		Paths: paths, BackupSuffix: suffix, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun,
	})
	if err != nil {
		return err
	}
	if resp != nil {
		step.LogText = resp.Message
		if !resp.OK && resp.Message != "" {
			return fmt.Errorf("%s", resp.Message)
		}
	}
	n.Status = store.SrNodePending
	_ = e.Store.UpdateSrNode(n)
	return nil
}

func (e *Engine) stepDropFollower(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	edit := n.EditPort
	if edit <= 0 {
		edit = cl.FeEditPort
	}
	q := fmt.Sprintf(`ALTER SYSTEM DROP FOLLOWER "%s:%d"`, n.Host, edit)
	if e.Cfg.DryRun {
		step.LogText = "dry-run: " + q
		return nil
	}
	return e.execFESQL(ctx, cl, q)
}

func (e *Engine) stepDecommissionBE(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	hb := n.HeartbeatPort
	if hb <= 0 {
		hb = cl.BeHeartbeatPort
	}
	q := fmt.Sprintf(`ALTER SYSTEM DECOMMISSION BACKEND "%s:%d"`, n.Host, hb)
	if e.Cfg.DryRun {
		step.LogText = "dry-run: " + q
		return nil
	}
	return e.execFESQL(ctx, cl, q)
}

func (e *Engine) stepDropBackend(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	hb := n.HeartbeatPort
	if hb <= 0 {
		hb = cl.BeHeartbeatPort
	}
	q := fmt.Sprintf(`ALTER SYSTEM DROP BACKEND "%s:%d"`, n.Host, hb)
	if e.Cfg.DryRun {
		step.LogText = "dry-run: " + q
		return nil
	}
	return e.execFESQL(ctx, cl, q)
}
