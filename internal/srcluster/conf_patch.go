package srcluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// ConfPatchNode 批量下发时某节点下发后的可编辑区内容。
type ConfPatchNode struct {
	NodeID  string `json:"node_id"`
	Host    string `json:"host"`
	Role    string `json:"role"`
	Source  bool   `json:"source"`
	Content string `json:"content"`
}

// ConfPatchPlan 同角色批量下发计划：源节点用编辑区全文，其他节点只应用公共参数改动。
type ConfPatchPlan struct {
	Role      string          `json:"role"`
	BaseLabel string          `json:"base_label"`
	Changes   []ConfKeyChange `json:"changes"`
	LocalKeys []string        `json:"local_keys"`
	Nodes     []ConfPatchNode `json:"nodes"`
}

// sourceConfBase 源节点「修改前」：远端文件 > 最近一次下发 > 当前已保存配置。
func (e *Engine) sourceConfBase(ctx context.Context, cl *store.SrCluster, n *store.SrNode) (string, string) {
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, resp, err := e.ReadRemoteConf(rctx, cl, n); err == nil && resp != nil && resp.Exists {
		return StripLockedDataDirKeys(resp.Content), "远端当前文件"
	}
	if rev, err := e.Store.LatestSrConfRevision(n.ID, store.SrConfRevApplied); err == nil && rev != nil {
		return StripLockedDataDirKeys(rev.Content), "最近一次下发"
	}
	if _, full, _, err := MaterializeConf(e.Store, cl, n); err == nil {
		return StripLockedDataDirKeys(full), "当前已保存配置"
	}
	return "", "空"
}

// PlanConfPatch 计算同角色批量下发后各节点内容（不落库）。
func (e *Engine) PlanConfPatch(ctx context.Context, clusterID, sourceNodeID, content string, nodeIDs []string) (*ConfPatchPlan, error) {
	if e == nil || e.Store == nil {
		return nil, fmt.Errorf("engine 未初始化")
	}
	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		return nil, fmt.Errorf("集群不存在")
	}
	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.SrNode{}
	for _, n := range activeNodes(nodes) {
		byID[n.ID] = n
	}
	src, ok := byID[strings.TrimSpace(sourceNodeID)]
	if !ok {
		return nil, fmt.Errorf("源节点不存在或已下线")
	}
	role := strings.ToLower(src.Component)
	var targets []store.SrNode
	seen := map[string]bool{}
	for _, id := range nodeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		n, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("节点 %s 不存在或已下线", id)
		}
		if strings.ToLower(n.Component) != role {
			return nil, fmt.Errorf("只能同时下发同一角色的节点（%s 是 %s，源节点是 %s）", n.Host, n.Component, src.Component)
		}
		targets = append(targets, n)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("请至少勾选一个下发节点")
	}

	edited := StripLockedDataDirKeys(content)
	if strings.TrimSpace(edited) == "" {
		return nil, fmt.Errorf("编辑区内容为空")
	}
	if _, err := RenderEditableConf(cl, &src, edited); err != nil {
		return nil, fmt.Errorf("编辑区内容无法渲染: %w", err)
	}
	base, baseLabel := e.sourceConfBase(ctx, cl, &src)
	changes := ConfKeyDiff(base, edited)
	common, local := SplitNodeLocalChanges(changes)
	plan := &ConfPatchPlan{Role: role, BaseLabel: baseLabel, Changes: changes, LocalKeys: []string{}}
	for _, c := range local {
		plan.LocalKeys = append(plan.LocalKeys, c.Key)
	}
	for i := range targets {
		n := &targets[i]
		pn := ConfPatchNode{NodeID: n.ID, Host: n.Host, Role: n.Component, Source: n.ID == src.ID}
		if pn.Source {
			pn.Content = edited
		} else {
			_, full, _, err := MaterializeConf(e.Store, cl, n)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", n.Host, err)
			}
			pn.Content = ConfKeyPatch(StripLockedDataDirKeys(full), common)
			if _, err := RenderEditableConf(cl, n, pn.Content); err != nil {
				return nil, fmt.Errorf("%s 应用改动后无法渲染: %w", n.Host, err)
			}
		}
		plan.Nodes = append(plan.Nodes, pn)
	}
	return plan, nil
}

// ApplyConfPatch 按计划写各节点草稿并发起 config_apply。
func (e *Engine) ApplyConfPatch(ctx context.Context, clusterID, operator, sourceNodeID, content string, nodeIDs []string, restart bool) (string, error) {
	plan, err := e.PlanConfPatch(ctx, clusterID, sourceNodeID, content, nodeIDs)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(plan.Nodes))
	for _, pn := range plan.Nodes {
		if err := e.Store.UpsertSrNodeConfig(&store.SrNodeConfig{
			NodeID: pn.NodeID, Content: pn.Content, UpdatedBy: operator,
		}); err != nil {
			return "", err
		}
		ids = append(ids, pn.NodeID)
	}
	return e.StartConfigApply(clusterID, operator, ConfigApplyOpts{
		Roles: []string{plan.Role}, NodeIDs: ids, Restart: restart,
	})
}
