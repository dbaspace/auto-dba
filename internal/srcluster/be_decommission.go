package srcluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

var errNodeDecommissioning = errors.New("该 BE 正在下线（DECOMMISSION 迁移 tablet），不可启停/重启；如需中止下线请走工单「恢复节点」")

const (
	msgDecommissionDone   = "在线缩容已完成，节点已移出集群，无法恢复"
	msgNotDecommissioning = "该 BE 未处于下线状态"
)

func beHBPort(cl *store.SrCluster, n *store.SrNode) int {
	if n.HeartbeatPort > 0 {
		return n.HeartbeatPort
	}
	return cl.BeHeartbeatPort
}

// beRemoveSteps BE 缩容步骤（key, title）：只下 SQL 并确认状态，BE 进程不停。
func beRemoveSteps(nodeID string, forceDrop bool) [][2]string {
	if forceDrop {
		return [][2]string{
			{"drop_backend_" + nodeID, "DROP BACKEND"},
			{"verify_dropped_" + nodeID, "确认已移出 SHOW PROC '/backends'"},
		}
	}
	return [][2]string{
		{"decommission_be_" + nodeID, "DECOMMISSION BACKEND（在线缩容，不停服）"},
		{"verify_decommission_" + nodeID, "确认进入下线状态"},
	}
}

// activeNodes 排除下线中的 BE：它们即将移出集群，不参与升级、配置下发。
func activeNodes(nodes []store.SrNode) []store.SrNode {
	out := make([]store.SrNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Status == store.SrNodeDecommissioning {
			continue
		}
		out = append(out, n)
	}
	return out
}

func findProcRow(rows []procVersionRow, host string) *procVersionRow {
	for i := range rows {
		if hostsEqual(rows[i].Host, host) {
			return &rows[i]
		}
	}
	return nil
}

func (e *Engine) queryBackends(ctx context.Context, cl *store.SrCluster) ([]procVersionRow, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return e.queryProcRows(cctx, cl, "/backends", false)
}

// stepVerifyDecommission DECOMMISSION 后确认 BE 进入下线状态（或已迁移完被移出），节点标记为下线中。
func (e *Engine) stepVerifyDecommission(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	if e.Cfg.DryRun {
		step.LogText = "dry-run: SHOW PROC '/backends' 确认 SystemDecommissioned=true"
		n.Status = store.SrNodeDecommissioning
		_ = e.Store.UpdateSrNode(n)
		return nil
	}
	last := ""
	for attempt := 1; attempt <= 5; attempt++ {
		rows, err := e.queryBackends(ctx, cl)
		switch {
		case err != nil:
			last = err.Error()
		case len(rows) == 0:
			last = "SHOW PROC '/backends' 无行"
		default:
			row := findProcRow(rows, n.Host)
			if row == nil {
				step.LogText = fmt.Sprintf("BE %s 已不在 SHOW PROC '/backends'（tablet 已迁移完成，FE 已移除该 BE）", n.Host)
			} else if row.Decommissioned {
				step.LogText = fmt.Sprintf("BE %s SystemDecommissioned=true，剩余 tablet %s；FE 正在迁移，迁完后自动移出集群，平台随后删除节点登记。迁移期间可走工单「恢复节点」撤销", n.Host, orDash(row.TabletNum))
			} else {
				last = "SystemDecommissioned 仍为 false"
				break
			}
			n.Status = store.SrNodeDecommissioning
			_ = e.Store.UpdateSrNode(n)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("DECOMMISSION 已执行，但未确认 BE %s 进入下线状态：%s（请在 FE 执行 SHOW PROC '/backends' 核对）", n.Host, last)
}

// stepVerifyDropped DROP BACKEND 后确认该 BE 已移出 SHOW PROC '/backends'。
func (e *Engine) stepVerifyDropped(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	if e.Cfg.DryRun {
		step.LogText = "dry-run: SHOW PROC '/backends' 确认已移除"
		return nil
	}
	deadline := time.Now().Add(time.Minute)
	for {
		rows, err := e.queryBackends(ctx, cl)
		if err == nil && findProcRow(rows, n.Host) == nil {
			step.LogText = fmt.Sprintf("BE %s 已不在 SHOW PROC '/backends'；BE 进程未停止，如需回收主机请运维自行处理", n.Host)
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("SHOW PROC '/backends': %w", err)
			}
			return fmt.Errorf("DROP BACKEND 已执行，但 %s 仍在 SHOW PROC '/backends' 中", n.Host)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (e *Engine) stepCancelDecommission(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	q := fmt.Sprintf(`CANCEL DECOMMISSION BACKEND "%s:%d"`, n.Host, beHBPort(cl, n))
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

// stepVerifyRecommission 等待 BE 仍在集群、SystemDecommissioned=false 且 Alive，节点恢复为运行中。
func (e *Engine) stepVerifyRecommission(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	if !e.Cfg.DryRun {
		deadline := time.Now().Add(3 * time.Minute)
		attempt := 0
		for {
			attempt++
			rows, err := e.queryBackends(ctx, cl)
			var state string
			if err != nil {
				state = err.Error()
			} else if len(rows) > 0 {
				row := findProcRow(rows, n.Host)
				if row == nil {
					return fmt.Errorf("%s", msgDecommissionDone)
				}
				if !row.Decommissioned && procTruthy(row.Alive) {
					step.LogText = fmt.Sprintf("BE %s 已恢复：SystemDecommissioned=false Alive=true", n.Host)
					break
				}
				state = fmt.Sprintf("SystemDecommissioned=%v Alive=%s", row.Decommissioned, row.Alive)
			} else {
				state = "SHOW PROC '/backends' 无行"
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("BE %s 超时未恢复：%s", n.Host, state)
			}
			step.LogText = fmt.Sprintf("等待恢复（第 %d 次）: %s", attempt, state)
			_ = e.Store.UpdateSrChangeStep(step)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	} else {
		step.LogText = "dry-run: SHOW PROC '/backends' 确认已恢复"
	}
	n.Status = store.SrNodeReady
	_ = e.Store.UpdateSrNode(n)
	return nil
}

// checkRecommission 恢复前校验：BE 仍在 SHOW PROC '/backends' 且 SystemDecommissioned=true。
func (e *Engine) checkRecommission(ctx context.Context, clusterID, nodeID string) (*store.SrCluster, *store.SrNode, *procVersionRow, error) {
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return nil, nil, nil, fmt.Errorf("集群不存在")
	}
	n, err := e.Store.GetSrNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return nil, nil, nil, fmt.Errorf("节点不存在")
	}
	if n.Component != "be" {
		return nil, nil, nil, fmt.Errorf("仅 BE 支持恢复下线")
	}
	if e.Cfg.DryRun {
		return cl, n, &procVersionRow{Host: n.Host, Decommissioned: true, Alive: "true"}, nil
	}
	rows, err := e.queryBackends(ctx, cl)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("SHOW PROC '/backends' 失败: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, nil, fmt.Errorf("SHOW PROC '/backends' 无行，无法确认下线状态")
	}
	row := findProcRow(rows, n.Host)
	if row == nil {
		return nil, nil, nil, fmt.Errorf("%s", msgDecommissionDone)
	}
	if !row.Decommissioned {
		return nil, nil, nil, fmt.Errorf("%s", msgNotDecommissioning)
	}
	return cl, n, row, nil
}

// CheckRecommissionBE 工单提交前校验该 BE 仍处于下线状态（可 CANCEL DECOMMISSION）。
func (e *Engine) CheckRecommissionBE(ctx context.Context, clusterID, nodeID string) error {
	_, _, _, err := e.checkRecommission(ctx, clusterID, nodeID)
	return err
}

// StartRecommissionBE 恢复下线中的 BE：（进程已停则先启动）→ CANCEL DECOMMISSION → 确认恢复。
func (e *Engine) StartRecommissionBE(clusterID, nodeID, operator string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cl, n, row, err := e.checkRecommission(ctx, clusterID, nodeID)
	if err != nil {
		return "", err
	}
	needStart := n.Status == store.SrNodeStopped || !procTruthy(row.Alive)
	if needStart && (strings.TrimSpace(n.AgentID) == "" || strings.TrimSpace(n.SystemdUnit) == "") {
		return "", fmt.Errorf("BE 进程未运行，且节点未绑定 agent_id / systemd_unit，无法自动启动")
	}
	ch := &store.SrChange{ClusterID: clusterID, Type: store.SrChangeRecommissionBE, Status: store.SrChangePending, CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	seq := 0
	add := func(key, title string) {
		seq++
		_ = e.Store.CreateSrChangeStep(&store.SrChangeStep{ChangeID: ch.ID, Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	if needStart {
		add("start_node_"+n.ID, "启动 BE "+n.Host)
	}
	add("cancel_decommission_"+n.ID, fmt.Sprintf(`CANCEL DECOMMISSION BACKEND "%s:%d"`, n.Host, beHBPort(cl, n)))
	add("verify_recommission_"+n.ID, "确认已恢复（SystemDecommissioned=false 且 Alive）")
	add("finalize", "完成")
	go e.runChange(ch.ID)
	return ch.ID, nil
}

// changeNodes 变更单及其步骤涉及的节点。
type changeNodes struct {
	ID      string
	Type    string
	Status  string
	NodeIDs []string
}

// beLifecycleChange 决定 BE 是否在集群中的变更类型；最近一次为缩容且成功的节点才可能被自动删除。
func beLifecycleChange(typ string) bool {
	switch typ {
	case store.SrChangeAddBE, store.SrChangeDecommissionBE, store.SrChangeRemoveBE, store.SrChangeRecommissionBE:
		return true
	}
	return false
}

// pickRemovedBackends 返回可删除登记的 BE（node_id → 缩容变更 ID）：
// 该 BE 最近一次生命周期变更为成功的 DECOMMISSION/DROP（currentID 视为成功），且已不在 SHOW PROC '/backends'。
// changes 按创建时间倒序；存在其它进行中的变更时不删除。
func pickRemovedBackends(nodes []store.SrNode, changes []changeNodes, rows []procVersionRow, currentID string) map[string]string {
	if len(rows) == 0 {
		return nil
	}
	for _, c := range changes {
		if c.ID == currentID {
			continue
		}
		if c.Status == store.SrChangeRunning || c.Status == store.SrChangePending {
			return nil
		}
	}
	latest := map[string]changeNodes{}
	for _, c := range changes {
		if !beLifecycleChange(c.Type) {
			continue
		}
		for _, id := range c.NodeIDs {
			if _, ok := latest[id]; !ok {
				latest[id] = c
			}
		}
	}
	out := map[string]string{}
	for _, n := range nodes {
		if n.Component != "be" {
			continue
		}
		c, ok := latest[n.ID]
		if !ok || (c.Type != store.SrChangeDecommissionBE && c.Type != store.SrChangeRemoveBE) {
			continue
		}
		if c.ID != currentID && c.Status != store.SrChangeSuccess {
			continue
		}
		if findProcRow(rows, n.Host) != nil {
			continue
		}
		out[n.ID] = c.ID
	}
	return out
}

// reconcileRemovedBackends 缩容完成（BE 已不在 SHOW PROC '/backends'）后删除节点登记。
// currentID 为正在收尾的缩容变更（其日志由调用方追加）；其余缩容变更追加说明日志。
func (e *Engine) reconcileRemovedBackends(ctx context.Context, cl *store.SrCluster, currentID string) ([]store.SrNode, error) {
	if e == nil || e.Store == nil || cl == nil || e.Cfg.DryRun {
		return nil, nil
	}
	nodes, err := e.Store.ListSrNodes(cl.ID)
	if err != nil {
		return nil, err
	}
	return e.reconcileRemovedWith(cl, nodes, currentID, func() ([]procVersionRow, error) { return e.queryBackends(ctx, cl) })
}

// reconcileRemovedWith 同 reconcileRemovedBackends，节点与 /backends 结果由调用方提供；backends 仅在存在缩容变更时才调用。
func (e *Engine) reconcileRemovedWith(cl *store.SrCluster, nodes []store.SrNode, currentID string, backends func() ([]procVersionRow, error)) ([]store.SrNode, error) {
	hasBE := false
	for _, n := range nodes {
		if n.Component == "be" {
			hasBE = true
			break
		}
	}
	if !hasBE {
		return nil, nil
	}
	list, err := e.Store.ListSrChanges(cl.ID, 200)
	if err != nil {
		return nil, err
	}
	changes := make([]changeNodes, 0, len(list))
	candidate := false
	for _, c := range list {
		cn := changeNodes{ID: c.ID, Type: c.Type, Status: c.Status}
		if beLifecycleChange(c.Type) {
			steps, err := e.Store.ListSrChangeSteps(c.ID)
			if err != nil {
				return nil, err
			}
			seen := map[string]bool{}
			for _, s := range steps {
				if s.NodeID != "" && !seen[s.NodeID] {
					seen[s.NodeID] = true
					cn.NodeIDs = append(cn.NodeIDs, s.NodeID)
				}
			}
			if c.Type == store.SrChangeDecommissionBE || c.Type == store.SrChangeRemoveBE {
				candidate = true
			}
		}
		changes = append(changes, cn)
	}
	if !candidate {
		return nil, nil
	}
	rows, err := backends()
	if err != nil {
		return nil, err
	}
	picked := pickRemovedBackends(nodes, changes, rows, currentID)
	var removed []store.SrNode
	for _, n := range nodes {
		chID, ok := picked[n.ID]
		if !ok {
			continue
		}
		if err := e.Store.DeleteSrNode(n.ID); err != nil {
			return removed, err
		}
		removed = append(removed, n)
		if chID == currentID {
			continue
		}
		msg := "缩容完成：BE " + n.Host + " 已不在 SHOW PROC '/backends'，已移除节点登记"
		for _, c := range changes {
			if c.ID == chID {
				_ = e.Store.UpdateSrChangeStatus(chID, c.Status, msg)
				break
			}
		}
	}
	return removed, nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
