package srcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

// ReadRemoteConf 经 Agent file/get 读取节点当前 fe.conf / be.conf。
func (e *Engine) ReadRemoteConf(ctx context.Context, cl *store.SrCluster, n *store.SrNode) (string, *FileGetResp, error) {
	if e == nil || e.Store == nil {
		return "", nil, fmt.Errorf("engine 未初始化")
	}
	if strings.TrimSpace(n.AgentID) == "" {
		return "", nil, fmt.Errorf("节点 %s 未绑定 Node Agent", n.Host)
	}
	path, _, _, err := MaterializeConf(e.Store, cl, n)
	if err != nil {
		return "", nil, err
	}
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return path, nil, err
	}
	resp, err := e.Agent.FileGet(ctx, auth, path)
	return path, resp, err
}

// RenderEditableConf 把编辑区正文渲染成将要下发的完整文件（含平台锁定路径）。
func RenderEditableConf(cl *store.SrCluster, n *store.SrNode, editable string) (string, error) {
	body := StripLockedDataDirKeys(editable)
	if err := RejectLockedDataDirKeys(body); err != nil {
		return "", err
	}
	vars := BuildConfVars(cl, n)
	rendered, err := RenderConf(body, vars)
	if err != nil {
		return "", err
	}
	return AppendLockedDataPaths(n.Component, rendered, vars), nil
}

// ConfigRollbackOpts 二选一：单节点回退到指定版本，或整单回退某次下发（各节点用其 before_apply 版本）。
type ConfigRollbackOpts struct {
	NodeID     string
	RevisionID string
	ChangeID   string
	Restart    bool
}

type rollbackTarget struct {
	node store.SrNode
	rev  store.SrConfRevision
}

// StartConfigRollback 把节点草稿同步为目标版本，再复用 put_conf / restart_node / finalize 步骤下发。
func (e *Engine) StartConfigRollback(clusterID, operator string, opts ConfigRollbackOpts) (string, error) {
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
	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		return "", err
	}
	nodeByID := map[string]store.SrNode{}
	for _, n := range activeNodes(nodes) {
		nodeByID[n.ID] = n
	}

	var targets []rollbackTarget
	switch {
	case strings.TrimSpace(opts.RevisionID) != "":
		rev, err := e.Store.GetSrConfRevision(strings.TrimSpace(opts.RevisionID))
		if err != nil {
			return "", err
		}
		if rev == nil || rev.ClusterID != clusterID {
			return "", fmt.Errorf("配置版本不存在")
		}
		if opts.NodeID != "" && rev.NodeID != opts.NodeID {
			return "", fmt.Errorf("配置版本不属于该节点")
		}
		n, ok := nodeByID[rev.NodeID]
		if !ok {
			return "", fmt.Errorf("版本对应节点不存在或已下线")
		}
		targets = append(targets, rollbackTarget{node: n, rev: *rev})
	case strings.TrimSpace(opts.ChangeID) != "":
		src, err := e.Store.GetSrChange(strings.TrimSpace(opts.ChangeID))
		if err != nil {
			return "", err
		}
		if src == nil || src.ClusterID != clusterID {
			return "", fmt.Errorf("变更单不存在")
		}
		if src.Type != store.SrChangeConfigApply && src.Type != store.SrChangeConfigRollback {
			return "", fmt.Errorf("仅配置下发 / 配置回退变更支持回退")
		}
		revs, err := e.Store.ListSrConfRevisionsByChange(src.ID)
		if err != nil {
			return "", fmt.Errorf("读取配置版本失败（请确认已执行 tbl_sr_conf_revisions 建表 SQL）: %w", err)
		}
		for _, r := range revs {
			if r.Kind != store.SrConfRevBeforeApply {
				continue
			}
			n, ok := nodeByID[r.NodeID]
			if !ok {
				continue
			}
			targets = append(targets, rollbackTarget{node: n, rev: r})
		}
		if len(targets) == 0 {
			return "", fmt.Errorf("该变更未记录下发前的远端原文件（首次下发或当时 Agent 版本过低），无法整单回退；可在节点「历史版本」中选择版本回退")
		}
	default:
		return "", fmt.Errorf("请指定 revision_id 或 change_id")
	}

	drafts := make([]string, len(targets))
	for i, t := range targets {
		if strings.TrimSpace(t.node.AgentID) == "" {
			return "", fmt.Errorf("节点 %s (%s) 未绑定 agent_id", t.node.Host, t.node.Component)
		}
		if strings.ToLower(t.rev.Role) != strings.ToLower(t.node.Component) {
			return "", fmt.Errorf("版本角色 %s 与节点 %s (%s) 不符", t.rev.Role, t.node.Host, t.node.Component)
		}
		draft := StripLockedDataDirKeys(t.rev.Content)
		if strings.TrimSpace(draft) == "" {
			return "", fmt.Errorf("节点 %s 的目标版本内容为空", t.node.Host)
		}
		n := t.node
		if _, err := RenderEditableConf(cl, &n, draft); err != nil {
			return "", fmt.Errorf("节点 %s 的目标版本无法渲染: %w", t.node.Host, err)
		}
		drafts[i] = draft
	}

	revIDs := map[string]string{}
	for i, t := range targets {
		if err := e.Store.UpsertSrNodeConfig(&store.SrNodeConfig{
			NodeID: t.node.ID, Content: drafts[i], UpdatedBy: operator,
		}); err != nil {
			return "", err
		}
		revIDs[t.node.ID] = t.rev.ID
	}

	payload, _ := json.Marshal(map[string]any{
		"source_change_id": strings.TrimSpace(opts.ChangeID),
		"revision_ids":     revIDs,
		"restart":          opts.Restart,
	})
	ch := &store.SrChange{
		ClusterID:   clusterID,
		Type:        store.SrChangeConfigRollback,
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
	for _, t := range targets {
		add("put_conf_"+t.node.ID, "回退 "+t.node.Component+".conf → "+t.node.Host, t.node.ID)
	}
	if opts.Restart {
		for _, t := range targets {
			add("restart_node_"+t.node.ID, "重启 "+t.node.Host, t.node.ID)
		}
	}
	add("finalize", "配置回退完成", "")
	go e.runChange(ch.ID)
	return ch.ID, nil
}
