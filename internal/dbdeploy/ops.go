package dbdeploy

import (
	"context"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

func (e *Engine) startNodePower(clusterID, nodeID, action, operator string) (string, error) {
	cl, err := e.Store.GetDbDeployCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	if cl.DeployKind == store.DbDeployKindCloud || cl.Topology == store.DbDeployTopoCloudEndpoint {
		return "", fmt.Errorf("云托管单入口不支持本机启停（由云厂商管理）")
	}
	n, err := e.Store.GetDbDeployNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if strings.TrimSpace(n.AgentID) == "" {
		return "", fmt.Errorf("节点未绑定 agent_id")
	}
	unit := defaultUnit(cl, n)
	if strings.TrimSpace(unit) == "" {
		return "", fmt.Errorf("systemd_unit 为空")
	}
	if list, err := e.Store.ListDbDeployChanges(cl.ID, 10); err == nil {
		for _, ch := range list {
			if ch.Status == store.DbChangeRunning || ch.Status == store.DbChangePending {
				return "", fmt.Errorf("已有进行中的变更")
			}
		}
	}
	typ := store.DbChangeStartNode
	stepKey := "start_node_" + n.ID
	title := "启动 " + n.Host
	switch action {
	case "stop":
		typ = store.DbChangeStopNode
		stepKey = "stop_node_" + n.ID
		title = "停止 " + n.Host
	case "restart":
		typ = store.DbChangeRestartNode
		stepKey = "restart_node_" + n.ID
		title = "重启 " + n.Host
	}
	ch := &store.DbDeployChange{ClusterID: clusterID, Type: typ, Status: store.DbChangePending, CreatedBy: operator}
	if err := e.Store.CreateDbDeployChange(ch); err != nil {
		return "", err
	}
	_ = e.Store.CreateDbDeployChangeStep(&store.DbDeployChangeStep{ChangeID: ch.ID, Seq: 1, StepKey: stepKey, Title: title, NodeID: nodeID})
	_ = e.Store.CreateDbDeployChangeStep(&store.DbDeployChangeStep{ChangeID: ch.ID, Seq: 2, StepKey: "finalize", Title: "完成"})
	_ = unit
	go func() { _ = e.runChange(context.Background(), ch.ID) }()
	return ch.ID, nil
}

func (e *Engine) StartNodeStart(clusterID, nodeID, operator string) (string, error) {
	return e.startNodePower(clusterID, nodeID, "start", operator)
}

func (e *Engine) StartNodeStop(clusterID, nodeID, operator string) (string, error) {
	return e.startNodePower(clusterID, nodeID, "stop", operator)
}

func (e *Engine) StartNodeRestart(clusterID, nodeID, operator string) (string, error) {
	return e.startNodePower(clusterID, nodeID, "restart", operator)
}

func (e *Engine) rejectIfActiveChange(clusterID string) error {
	list, err := e.Store.ListDbDeployChanges(clusterID, 20)
	if err != nil {
		return err
	}
	for _, ch := range list {
		if ch.Status == store.DbChangeRunning || ch.Status == store.DbChangePending {
			return fmt.Errorf("已有进行中的变更 %s", ch.ID)
		}
	}
	return nil
}

// StartAddNode 对已就绪集群扩容：单节点走装包→配置→启动（replica 含复制验收）。
func (e *Engine) StartAddNode(clusterID, nodeID, operator string) (string, error) {
	cl, err := e.Store.GetDbDeployCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	if cl.Status != store.DbDeployStatusReady && cl.Status != store.DbDeployStatusHaPending {
		return "", fmt.Errorf("仅 ready/ha_pending 集群可扩容加入节点，当前状态 %s", cl.Status)
	}
	if cl.DeployKind == store.DbDeployKindCloud || cl.Topology == store.DbDeployTopoCloudEndpoint {
		return "", fmt.Errorf("云托管单入口不支持装机加入节点")
	}
	n, err := e.Store.GetDbDeployNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if strings.TrimSpace(n.AgentID) == "" {
		return "", fmt.Errorf("节点未绑定 agent_id")
	}
	if n.Status == store.DbNodeReady {
		return "", fmt.Errorf("节点已是 ready，无需加入")
	}
	if strings.TrimSpace(cl.ReleaseID) == "" {
		return "", fmt.Errorf("集群未绑定 release_id")
	}
	if err := e.rejectIfActiveChange(cl.ID); err != nil {
		return "", err
	}
	if _, _, _, err := e.Svc.PresignRelease(cl.Engine, cl.ReleaseID, 0); err != nil {
		return "", fmt.Errorf("制品签发失败: %w", err)
	}

	ch := &store.DbDeployChange{ClusterID: clusterID, Type: store.DbChangeAddNode, Status: store.DbChangePending, CreatedBy: operator}
	if err := e.Store.CreateDbDeployChange(ch); err != nil {
		return "", err
	}
	seq := 0
	add := func(key, title string) {
		seq++
		_ = e.Store.CreateDbDeployChangeStep(&store.DbDeployChangeStep{
			ChangeID: ch.ID, Seq: seq, StepKey: key, Title: title, NodeID: nodeID,
		})
	}
	add("precheck_"+n.ID, "预检 "+n.Host)
	add("install_"+n.ID, "安装制品 "+n.Host)
	add("ensure_fs_"+n.ID, "准备目录 "+n.Host)
	add("put_conf_"+n.ID, "写入配置 "+n.Host)
	add("systemd_"+n.ID, "安装 systemd "+n.Host)
	add("start_"+n.ID, "启动服务 "+n.Host)
	if n.Role == "replica" || n.Role == "sentinel" {
		add("repl_setup_one_"+n.ID, "配置复制 "+n.Host)
		add("repl_verify_one_"+n.ID, "复制验收 "+n.Host)
	}
	add("finalize", "完成")
	go func() { _ = e.runChange(context.Background(), ch.ID) }()
	return ch.ID, nil
}

// StartRemoveNode 停服并自平台删除节点登记（不执行引擎侧 DROP 语义，避免误删数据目录）。
func (e *Engine) StartRemoveNode(clusterID, nodeID, operator string) (string, error) {
	cl, err := e.Store.GetDbDeployCluster(clusterID)
	if err != nil || cl == nil {
		return "", fmt.Errorf("集群不存在")
	}
	n, err := e.Store.GetDbDeployNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if err := e.rejectIfActiveChange(cl.ID); err != nil {
		return "", err
	}
	// 主从禁止直接下线唯一 primary
	if n.Role == "primary" {
		nodes, _ := e.Store.ListDbDeployNodes(cl.ID)
		primaries := 0
		for _, x := range nodes {
			if x.Role == "primary" || x.Role == "single" {
				primaries++
			}
		}
		if primaries <= 1 && (cl.Topology == store.DbDeployTopoMasterReplica || cl.Topology == store.DbDeployTopoSentinel) {
			return "", fmt.Errorf("禁止下线唯一主节点，请先完成切主或调整拓扑")
		}
	}

	ch := &store.DbDeployChange{ClusterID: clusterID, Type: store.DbChangeRemoveNode, Status: store.DbChangePending, CreatedBy: operator}
	if err := e.Store.CreateDbDeployChange(ch); err != nil {
		return "", err
	}
	_ = e.Store.CreateDbDeployChangeStep(&store.DbDeployChangeStep{
		ChangeID: ch.ID, Seq: 1, StepKey: "stop_node_" + n.ID, Title: "停止 " + n.Host, NodeID: nodeID,
	})
	_ = e.Store.CreateDbDeployChangeStep(&store.DbDeployChangeStep{
		ChangeID: ch.ID, Seq: 2, StepKey: "drop_node_" + n.ID, Title: "移除登记 " + n.Host, NodeID: nodeID,
	})
	_ = e.Store.CreateDbDeployChangeStep(&store.DbDeployChangeStep{
		ChangeID: ch.ID, Seq: 3, StepKey: "finalize", Title: "完成",
	})
	go func() { _ = e.runChange(context.Background(), ch.ID) }()
	return ch.ID, nil
}
