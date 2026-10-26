package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/middleware"
	"auto-dba/internal/srcluster"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// SrClusterTicketPayload 存于 Issue.SQLContent。
type SrClusterTicketPayload struct {
	ClusterID    string        `json:"cluster_id"`
	ClusterName  string        `json:"cluster_name"`
	Action       string        `json:"action"`
	NodeID       string        `json:"node_id,omitempty"`
	ForceDrop    bool          `json:"force_drop,omitempty"`
	ReleaseID    string        `json:"release_id,omitempty"`
	Node         *store.SrNode `json:"node,omitempty"`
	SoftwareBase string        `json:"software_base,omitempty"`
}

func validSrClusterTicketAction(a string) bool {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "add_node", "cluster_reinit", "upgrade", "remove_node", "restore_node":
		return true
	default:
		return false
	}
}

func srClusterActionLabel(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "add_node":
		return "添加/加入节点"
	case "cluster_reinit":
		return "重新初始化"
	case "upgrade":
		return "升级"
	case "remove_node":
		return "下线节点"
	case "restore_node":
		return "恢复节点"
	default:
		return a
	}
}

// CreateTicket POST /sr-cluster/tickets — 高危操作走审批，审批后 ExecuteSrClusterTicket。
func (h *SrClusterHandler) CreateTicket(c *gin.Context) {
	var req struct {
		ClusterID   string          `json:"cluster_id"`
		Action      string          `json:"action"`
		AssignedTo  string          `json:"assigned_to"`
		Description string          `json:"description"`
		Payload     json.RawMessage `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if !validSrClusterTicketAction(action) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的操作类型"})
		return
	}
	clusterID := strings.TrimSpace(req.ClusterID)
	cl, err := h.Store.GetSrClusterInTenant(clusterID, effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "集群不存在"})
		return
	}

	var body map[string]any
	_ = json.Unmarshal(req.Payload, &body)
	if body == nil {
		body = map[string]any{}
	}

	tp := SrClusterTicketPayload{
		ClusterID:   cl.ID,
		ClusterName: cl.Name,
		Action:      action,
	}

	switch action {
	case "add_node":
		nodeID, _ := body["node_id"].(string)
		nodeID = strings.TrimSpace(nodeID)
		if nodeID != "" {
			n, err := h.Store.GetSrNode(nodeID)
			if err != nil || n == nil || n.ClusterID != cl.ID {
				c.JSON(http.StatusBadRequest, gin.H{"message": "节点不存在或不属于该集群"})
				return
			}
			tp.NodeID = nodeID
		} else {
			n, softBase, err := parseSrAddNodePayload(cl, body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
				return
			}
			chk := h.eng().PrecheckAddNode(c.Request.Context(), cl.ID, srcluster.AddNodeCheckInput{
				Node: nodeWithGlobalPaths(h.Store, cl, n), HostInstanceID: n.HostInstanceID, AssignedTo: req.AssignedTo, SkipAgentCalls: true,
			})
			blocking := []string{"agent_match", "duplicate", "role_rule"}
			if n.HostInstanceID != "" {
				blocking = append(blocking, "host_instance")
			}
			if bad := chk.Failed(blocking...); bad != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": bad.Message, "data": chk})
				return
			}
			tp.Node = n
			tp.SoftwareBase = softBase
		}
	case "upgrade":
		rid, _ := body["release_id"].(string)
		rid = strings.TrimSpace(rid)
		if rid == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "release_id 必填"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
		defer cancel()
		pre := h.eng().PrecheckUpgrade(ctx, cl.ID, rid)
		if pre == nil || !pre.OK {
			msg := "升级预检未通过"
			if pre != nil && strings.TrimSpace(pre.Summary) != "" {
				msg = pre.Summary
			}
			c.JSON(http.StatusBadRequest, gin.H{"message": msg, "data": pre})
			return
		}
		tp.ReleaseID = rid
		preJSON, _ := json.Marshal(pre)
		// NeedRollback 存预检结果（与 HA 类似）
		assigned, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.AssignedTo)
		if aerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
			return
		}
		desc := strings.TrimSpace(req.Description)
		if desc == "" {
			desc = fmt.Sprintf("SR %s · %s", srClusterActionLabel(action), cl.Name)
		}
		payloadBytes, _ := json.Marshal(tp)
		issue := &store.Issue{
			ID:           uuid.NewString(),
			TicketNo:     "S" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
			IssueType:    store.IssueTypeSrCluster,
			InstanceID:   "",
			DBName:       cl.ID,
			SQLContent:   string(payloadBytes),
			Description:  desc,
			Submitter:    middleware.Username(c),
			AssignedTo:   assigned,
			NeedRollback: string(preJSON),
		}
		stampTenantID(c, func(tid string) {
			if issue != nil {
				issue.TenantID = tid
			}
		})
		if err := h.Store.CreateIssue(issue); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"issue": issue, "precheck": pre}})
		return
	case "remove_node":
		nodeID, _ := body["node_id"].(string)
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "node_id 必填"})
			return
		}
		n, err := h.Store.GetSrNode(nodeID)
		if err != nil || n == nil || n.ClusterID != cl.ID {
			c.JSON(http.StatusBadRequest, gin.H{"message": "节点不存在或不属于该集群"})
			return
		}
		tp.NodeID = nodeID
		if v, ok := body["force_drop"].(bool); ok {
			tp.ForceDrop = v
		} else if s, ok := body["force_drop"].(string); ok {
			tp.ForceDrop = s == "1" || strings.EqualFold(s, "true")
		}
	case "restore_node":
		nodeID, _ := body["node_id"].(string)
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "node_id 必填"})
			return
		}
		if err := h.eng().CheckRecommissionBE(c.Request.Context(), cl.ID, nodeID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		tp.NodeID = nodeID
	case "cluster_reinit":
		// no extra fields
	}

	assigned, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.AssignedTo)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = fmt.Sprintf("SR %s · %s", srClusterActionLabel(action), cl.Name)
	}
	payloadBytes, _ := json.Marshal(tp)
	issue := &store.Issue{
		ID:          uuid.NewString(),
		TicketNo:    "S" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:   store.IssueTypeSrCluster,
		InstanceID:  "",
		DBName:      cl.ID,
		SQLContent:  string(payloadBytes),
		Description: desc,
		Submitter:   middleware.Username(c),
		AssignedTo:  assigned,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil {
			issue.TenantID = tid
		}
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"issue": issue}})
}

func (h *SrClusterHandler) srTicketApprover(assignedTo string) string {
	if v := strings.TrimSpace(assignedTo); v != "" {
		return v
	}
	v, _ := h.Store.GetGlobalArg("sync_default_approver")
	return strings.TrimSpace(v)
}

func parseSrAddNodePayload(cl *store.SrCluster, body map[string]any) (*store.SrNode, string, error) {
	comp, _ := body["component"].(string)
	comp = strings.ToLower(strings.TrimSpace(comp))
	if comp != "fe" && comp != "be" {
		return nil, "", fmt.Errorf("component 须为 fe 或 be")
	}
	host, _ := body["host"].(string)
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, "", fmt.Errorf("host 必填")
	}
	agentID, _ := body["agent_id"].(string)
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, "", fmt.Errorf("agent_id 必填")
	}
	softBase, _ := body["software_base"].(string)
	explicitBase := strings.TrimRight(strings.TrimSpace(softBase), "/")

	hostInstID, _ := body["host_instance_id"].(string)
	n := &store.SrNode{
		ClusterID:      cl.ID,
		Component:      comp,
		Host:           host,
		AgentID:        agentID,
		HostInstanceID: strings.TrimSpace(hostInstID),
	}
	if comp == "fe" {
		if v, ok := body["is_primary_fe"].(bool); ok {
			n.IsPrimaryFE = v
		}
		role, _ := body["fe_role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "leader" {
			return nil, "", fmt.Errorf("不可指定部署为 leader")
		}
		if n.IsPrimaryFE {
			n.FeRole = "follower"
		} else if role == "" {
			n.FeRole = "follower"
		} else if role != "follower" && role != "observer" {
			return nil, "", fmt.Errorf("FE 子角色仅支持 follower / observer")
		} else {
			n.FeRole = role
		}
		n.HTTPPort = cl.FeHTTPPort
		n.EditPort = cl.FeEditPort
		n.SystemdUnit = "fe-" + strconv.Itoa(cl.FeQueryPort)
		if explicitBase != "" {
			n.SoftwarePath = explicitBase + "/fe"
		}
	} else {
		dataRoot, _ := body["data_root"].(string)
		dataRoot = strings.TrimRight(strings.TrimSpace(dataRoot), "/")
		n.DataRoot = dataRoot
		n.HeartbeatPort = cl.BeHeartbeatPort
		n.SystemdUnit = "be-" + strconv.Itoa(n.HeartbeatPort)
		if explicitBase != "" {
			n.SoftwarePath = explicitBase + "/be"
		}
	}
	return n, explicitBase, nil
}

// nodeWithGlobalPaths 预检时把未填写的目录补成全局（或集群已填）路径，不写回节点登记。
func nodeWithGlobalPaths(st *store.DB, cl *store.SrCluster, n *store.SrNode) *store.SrNode {
	if n == nil {
		return nil
	}
	cp := *n
	fe, be, data := store.ResolveSrPaths(st, cl)
	if strings.TrimSpace(cp.SoftwarePath) == "" {
		base := fe
		if cp.Component == "be" {
			base = be
		}
		cp.SoftwarePath = strings.TrimRight(base, "/") + "/" + cp.Component
	}
	if cp.Component == "be" && strings.TrimSpace(cp.DataRoot) == "" {
		cp.DataRoot = data
	}
	return &cp
}

// ExecuteSrClusterTicket 审批通过后由 TicketHandler 调用。
func (h *SrClusterHandler) ExecuteSrClusterTicket(ctx context.Context, it *store.Issue) (*ticketexec.Result, error) {
	started := time.Now().UTC().Format(time.RFC3339)
	res := &ticketexec.Result{StartedAt: started, Engine: "sr_cluster", Success: true}
	var tp SrClusterTicketPayload
	if err := json.Unmarshal([]byte(it.SQLContent), &tp); err != nil {
		res.Success = false
		res.Message = "工单 payload 无效: " + err.Error()
		return res, nil
	}
	who := it.Submitter
	if who == "" {
		who = "ticket"
	}
	eng := h.eng()
	ops := srChangeOps(h.Store, eng.RetryChange)
	label := srClusterActionLabel(tp.Action)
	changeID, err := resumeClusterChange(prevChangeID(it.ExecuteResult), ops)
	if err != nil {
		res.Success = false
		res.ChangeID = changeID
		res.Message = err.Error()
		return res, nil
	}
	if changeID != "" {
		h.waitTicketChange(ctx, it, res, changeID, label, ops)
		return res, nil
	}

	switch strings.ToLower(strings.TrimSpace(tp.Action)) {
	case "add_node":
		nodeID := strings.TrimSpace(tp.NodeID)
		if nodeID == "" {
			if tp.Node == nil {
				res.Success = false
				res.Message = "缺少节点信息"
				return res, nil
			}
			n := *tp.Node
			n.ClusterID = tp.ClusterID
			if err := h.Store.CreateSrNode(&n); err != nil {
				res.Success = false
				res.Message = "登记节点失败: " + err.Error()
				return res, nil
			}
			nodeID = n.ID
		}
		changeID, err = eng.StartAddNode(tp.ClusterID, nodeID, who)
	case "cluster_reinit":
		changeID, err = eng.StartClusterReinit(tp.ClusterID, who)
	case "upgrade":
		if strings.TrimSpace(tp.ReleaseID) == "" {
			res.Success = false
			res.Message = "缺少 release_id"
			return res, nil
		}
		changeID, err = eng.StartUpgrade(tp.ClusterID, tp.ReleaseID, who)
	case "remove_node":
		if strings.TrimSpace(tp.NodeID) == "" {
			res.Success = false
			res.Message = "缺少 node_id"
			return res, nil
		}
		changeID, err = eng.StartRemoveNode(tp.ClusterID, tp.NodeID, who, tp.ForceDrop)
	case "restore_node":
		if strings.TrimSpace(tp.NodeID) == "" {
			res.Success = false
			res.Message = "缺少 node_id"
			return res, nil
		}
		changeID, err = eng.StartRecommissionBE(tp.ClusterID, tp.NodeID, who)
	default:
		res.Success = false
		res.Message = "未知操作: " + tp.Action
		return res, nil
	}
	if err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, nil
	}
	h.waitTicketChange(ctx, it, res, changeID, label, ops)
	return res, nil
}

// waitTicketChange 等变更结束再返回，工单状态与变更真实结果一致；执行中持续写入步骤进度。
func (h *SrClusterHandler) waitTicketChange(ctx context.Context, it *store.Issue, res *ticketexec.Result, changeID, label string, ops clusterChangeOps) {
	persistTicketProgress(h.Store, it.ID, res, changeID)
	waitClusterChange(ctx, res, changeID, label, ops, func(r *ticketexec.Result) {
		persistTicketProgress(h.Store, it.ID, r, changeID)
	})
}

func persistTicketProgress(st *store.DB, issueID string, res *ticketexec.Result, changeID string) {
	res.ChangeID = changeID
	if strings.TrimSpace(res.Summary) == "" {
		res.Summary = "集群变更执行中…"
	}
	_ = st.UpdateIssueExec(issueID, store.StatusRunning, ticketexec.ToJSON(res))
}
