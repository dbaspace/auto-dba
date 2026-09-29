package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/proxy"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type proxyNodeJob struct {
	Node  proxy.HANodeSpec `json:"node"`
	JobID string           `json:"job_id"`
}

func (h *ProxyHandler) agentAuthNode(node proxy.HANodeSpec, fallbackPass string) (proxy.AgentAuth, error) {
	pass := fallbackPass
	user := strings.TrimSpace(node.AgentUser)
	base := strings.TrimSpace(node.AgentBaseURL)
	if strings.TrimSpace(node.AgentID) != "" {
		ag, err := h.Store.GetBackupAgent(node.AgentID)
		if err != nil || ag == nil {
			return proxy.AgentAuth{}, fmt.Errorf("平台 Agent 不存在: %s", node.AgentID)
		}
		if base == "" {
			base = strings.TrimSpace(ag.BaseURL)
		}
		if user == "" {
			user = ag.Username
		}
		if pass == "" && strings.TrimSpace(ag.PasswordEnc) != "" {
			if p, e := secret.Decrypt(h.Secret, ag.PasswordEnc); e == nil {
				pass = p
			}
		}
	}
	if base == "" {
		return proxy.AgentAuth{}, fmt.Errorf("节点 %s 缺少 Agent URL", node.Name)
	}
	return proxy.AgentAuth{BaseURL: base, Username: user, Password: pass}, nil
}

// applyHAToService 校验节点、写入 OptionsJSON，并把 master 同步到服务级 Agent 字段。
func (h *ProxyHandler) applyHAToService(s *store.ProxyService, nodes []proxy.HANodeSpec, lvs *proxy.LVSOptions, backends []proxy.Backend) error {
	norm, topo, err := proxy.ValidateAndNormalizeHANodes(s.Kind, nodes)
	if err != nil {
		return err
	}
	master := proxy.MasterNode(norm)
	if master == nil {
		return fmt.Errorf("缺少 master 节点")
	}
	s.AgentID = master.AgentID
	s.AgentBaseURL = master.AgentBaseURL
	s.AgentUser = master.AgentUser

	kaMaster, err := proxy.BuildKeepalivedConfForNode(s.Kind, s.Name, s.VIP, s.ListenPort, backends, lvs, *master)
	if err != nil {
		return err
	}
	s.OptionsJSON = proxy.AttachKeepalivedToOptions(s.OptionsJSON, proxy.DefaultKeepalivedPath, kaMaster, lvs)
	s.OptionsJSON = proxy.AttachHANodesToOptions(s.OptionsJSON, topo, norm, lvs)
	if s.Kind == "lvs" {
		s.ConfContent = kaMaster
		s.ConfPath = proxy.DefaultKeepalivedPath
	}
	return nil
}

func (h *ProxyHandler) checkVIPOnNodes(ctx context.Context, vip string, nodes []proxy.HANodeSpec, fallbackPass string) error {
	master := proxy.MasterNode(nodes)
	if master != nil {
		auth, err := h.agentAuthNode(*master, fallbackPass)
		if err != nil {
			return err
		}
		avail, msg, cerr := proxy.CheckVIP(ctx, auth, vip)
		if cerr != nil {
			return fmt.Errorf("VIP 探测失败: %w", cerr)
		}
		if !avail {
			return fmt.Errorf("%s", msg)
		}
	}
	return nil
}

func (h *ProxyHandler) pushConfToAllNodes(ctx context.Context, s *store.ProxyService, reload bool, act string) (map[string]any, error) {
	o := proxy.ParseProxyOptions(s.OptionsJSON)
	nodes := o.Nodes
	if len(nodes) == 0 {
		auth, err := h.agentAuth(s)
		if err != nil {
			return nil, err
		}
		out, err := proxy.PutConf(ctx, auth, s.Kind, s.ConfPath, s.ConfContent, true)
		if err != nil {
			return out, err
		}
		if err := h.pushKeepalivedConf(ctx, auth, s); err != nil {
			return out, err
		}
		if reload {
			if act == "" {
				act = "reload"
			}
			_, _ = proxy.ServiceCtrl(ctx, auth, s.Kind, act, s.SystemdUnit)
			if s.Kind != "lvs" {
				_, _ = proxy.ServiceCtrl(ctx, auth, "lvs", act, "keepalived")
			}
		}
		return map[string]any{"legacy": out}, nil
	}

	var backends []proxy.Backend
	_ = json.Unmarshal([]byte(s.BackendsJSON), &backends)
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		if p, e := secret.Decrypt(h.Secret, s.PasswordEnc); e == nil {
			pass = p
		}
	}
	results := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		auth, err := h.agentAuthNode(n, pass)
		if err != nil {
			return nil, err
		}
		ka, err := proxy.BuildKeepalivedConfForNode(s.Kind, s.Name, s.VIP, s.ListenPort, backends, o.LVS, n)
		if err != nil {
			return nil, fmt.Errorf("节点 %s keepalived: %w", n.Name, err)
		}
		item := map[string]any{"node": n.Name, "role": n.Role}
		if s.Kind == "lvs" {
			out, err := proxy.PutConf(ctx, auth, "lvs", proxy.DefaultKeepalivedPath, ka, true)
			item["push"] = out
			if err != nil {
				return nil, fmt.Errorf("节点 %s 下发失败: %w", n.Name, err)
			}
		} else {
			out, err := proxy.PutConf(ctx, auth, s.Kind, s.ConfPath, s.ConfContent, true)
			item["push"] = out
			if err != nil {
				return nil, fmt.Errorf("节点 %s 代理 conf 失败: %w", n.Name, err)
			}
			path := o.KeepalivedPath
			if path == "" {
				path = proxy.DefaultKeepalivedPath
			}
			kaOut, err := proxy.PutConf(ctx, auth, "lvs", path, ka, true)
			item["keepalived"] = kaOut
			if err != nil {
				return nil, fmt.Errorf("节点 %s Keepalived 失败: %w", n.Name, err)
			}
		}
		if reload {
			if act == "" {
				act = "reload"
			}
			if s.Kind != "lvs" {
				_, _ = proxy.ServiceCtrl(ctx, auth, s.Kind, act, s.SystemdUnit)
			}
			_, _ = proxy.ServiceCtrl(ctx, auth, "lvs", act, "keepalived")
		}
		results = append(results, item)
	}
	return map[string]any{"nodes": results}, nil
}

func (h *ProxyHandler) finishProxyDeployHA(serviceID string, jobs []proxyNodeJob, pushConf, enableStart bool, who string) {
	authCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	s, err := h.Store.GetProxyService(serviceID)
	if err != nil || s == nil {
		return
	}
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		if p, e := secret.Decrypt(h.Secret, s.PasswordEnc); e == nil {
			pass = p
		}
	}
	deadline := time.Now().Add(12 * time.Minute)
	pending := make(map[string]proxyNodeJob, len(jobs))
	for _, j := range jobs {
		pending[j.JobID] = j
	}
	for len(pending) > 0 && time.Now().Before(deadline) {
		select {
		case <-authCtx.Done():
			return
		default:
		}
		for id, j := range pending {
			auth, aerr := h.agentAuthNode(j.Node, pass)
			if aerr != nil {
				s.Status = "error"
				s.Remark = strings.TrimSpace(s.Remark + "；节点鉴权失败: " + aerr.Error())
				s.UpdatedBy = who
				_ = h.Store.UpsertProxyService(s)
				return
			}
			out, err := proxy.DeployJob(authCtx, auth, id)
			if err != nil {
				continue
			}
			job, _ := out["job"].(map[string]any)
			if job == nil {
				continue
			}
			st, _ := job["status"].(string)
			switch strings.ToLower(st) {
			case "success":
				delete(pending, id)
			case "failed":
				msg, _ := job["message"].(string)
				s.Status = "error"
				s.Remark = strings.TrimSpace(s.Remark + "；节点 " + j.Node.Name + " 部署失败: " + msg)
				s.UpdatedBy = who
				_ = h.Store.UpsertProxyService(s)
				return
			}
		}
		if len(pending) > 0 {
			time.Sleep(3 * time.Second)
		}
	}
	if len(pending) > 0 {
		s.Status = "error"
		s.Remark = strings.TrimSpace(s.Remark + "；部署超时，仍有节点未完成")
		s.UpdatedBy = who
		_ = h.Store.UpsertProxyService(s)
		return
	}
	if !pushConf {
		s.Status = "active"
		s.UpdatedBy = who
		_ = h.Store.UpsertProxyService(s)
		return
	}
	act := "reload"
	if enableStart {
		act = "restart"
	}
	ctx, ccancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer ccancel()
	if _, err := h.pushConfToAllNodes(ctx, s, true, act); err != nil {
		s.Status = "error"
		s.Remark = strings.TrimSpace(s.Remark + "；装包成功但下发失败: " + err.Error())
		s.UpdatedBy = who
		_ = h.Store.UpsertProxyService(s)
		return
	}
	_ = h.Store.MarkProxyPushed(s.ID, who)
}

// proxyServiceView 在 JSON 中附带 topology / nodes，便于前端展示。
func proxyServiceView(s *store.ProxyService) gin.H {
	if s == nil {
		return nil
	}
	o := proxy.ParseProxyOptions(s.OptionsJSON)
	b, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	topo := o.Topology
	if topo == "" && len(o.Nodes) >= 2 {
		if len(o.Nodes) == 2 {
			topo = proxy.TopologyHAPair
		} else {
			topo = proxy.TopologyHAMulti
		}
	}
	m["topology"] = topo
	m["nodes"] = o.Nodes
	return m
}

func (h *ProxyHandler) statusAllNodes(ctx context.Context, s *store.ProxyService) (map[string]any, error) {
	o := proxy.ParseProxyOptions(s.OptionsJSON)
	nodes := o.Nodes
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		if p, e := secret.Decrypt(h.Secret, s.PasswordEnc); e == nil {
			pass = p
		}
	}
	if len(nodes) == 0 {
		auth, err := h.agentAuth(s)
		if err != nil {
			return nil, err
		}
		out, err := proxy.ServiceCtrl(ctx, auth, s.Kind, "status", s.SystemdUnit)
		if err != nil {
			return out, err
		}
		if s.Kind != "lvs" {
			if ka, kerr := proxy.ServiceCtrl(ctx, auth, "lvs", "status", "keepalived"); kerr == nil {
				out["keepalived"] = ka
			}
		}
		out["ha_nodes"] = []any{}
		return out, nil
	}
	items := make([]map[string]any, 0, len(nodes))
	var masterOut map[string]any
	var masterKA any
	anyActive := false
	for _, n := range nodes {
		auth, err := h.agentAuthNode(n, pass)
		item := map[string]any{"name": n.Name, "role": n.Role, "agent_id": n.AgentID, "priority": n.Priority}
		if err != nil {
			item["ok"] = false
			item["error"] = err.Error()
			items = append(items, item)
			continue
		}
		st, serr := proxy.ServiceCtrl(ctx, auth, s.Kind, "status", s.SystemdUnit)
		if serr != nil {
			item["ok"] = false
			item["error"] = serr.Error()
			item["status"] = st
		} else {
			item["ok"] = true
			item["status"] = st
			if active, _ := st["active"].(bool); active {
				anyActive = true
			}
		}
		if s.Kind != "lvs" {
			if ka, kerr := proxy.ServiceCtrl(ctx, auth, "lvs", "status", "keepalived"); kerr == nil {
				item["keepalived"] = ka
			}
		} else if st != nil {
			item["keepalived"] = st
		}
		if n.Role == proxy.RoleMaster {
			masterOut = st
			masterKA = item["keepalived"]
		}
		items = append(items, item)
	}
	out := map[string]any{
		"ha_nodes": items,
		"topology": o.Topology,
		"active":   anyActive,
		"unit":     s.SystemdUnit,
	}
	if masterOut != nil {
		for k, v := range masterOut {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	if masterKA != nil {
		out["keepalived"] = masterKA
	}
	return out, nil
}

func (h *ProxyHandler) ctrlAllNodes(ctx context.Context, s *store.ProxyService, action string) (map[string]any, error) {
	o := proxy.ParseProxyOptions(s.OptionsJSON)
	nodes := o.Nodes
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		if p, e := secret.Decrypt(h.Secret, s.PasswordEnc); e == nil {
			pass = p
		}
	}
	if len(nodes) == 0 {
		auth, err := h.agentAuth(s)
		if err != nil {
			return nil, err
		}
		out, err := proxy.ServiceCtrl(ctx, auth, s.Kind, action, s.SystemdUnit)
		if err != nil {
			return out, err
		}
		if s.Kind != "lvs" && (action == "start" || action == "stop" || action == "reload" || action == "restart") {
			if ka, kerr := proxy.ServiceCtrl(ctx, auth, "lvs", action, "keepalived"); kerr == nil {
				out["keepalived"] = ka
			} else {
				out["keepalived_error"] = kerr.Error()
			}
		}
		return out, nil
	}
	results := make([]map[string]any, 0, len(nodes))
	var firstErr error
	for _, n := range nodes {
		auth, err := h.agentAuthNode(n, pass)
		item := map[string]any{"name": n.Name, "role": n.Role}
		if err != nil {
			item["ok"] = false
			item["error"] = err.Error()
			if firstErr == nil {
				firstErr = err
			}
			results = append(results, item)
			continue
		}
		out, serr := proxy.ServiceCtrl(ctx, auth, s.Kind, action, s.SystemdUnit)
		item["result"] = out
		if serr != nil {
			item["ok"] = false
			item["error"] = serr.Error()
			if firstErr == nil {
				firstErr = serr
			}
		} else {
			item["ok"] = true
		}
		if s.Kind != "lvs" && (action == "start" || action == "stop" || action == "reload" || action == "restart") {
			if ka, kerr := proxy.ServiceCtrl(ctx, auth, "lvs", action, "keepalived"); kerr == nil {
				item["keepalived"] = ka
			} else {
				item["keepalived_error"] = kerr.Error()
			}
		}
		results = append(results, item)
	}
	return map[string]any{"nodes": results, "topology": o.Topology}, firstErr
}
