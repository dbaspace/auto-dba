package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// DbDeployTicketPayload 存于 Issue.SQLContent。
type DbDeployTicketPayload struct {
	Engine      string              `json:"engine"`
	ClusterID   string              `json:"cluster_id"`
	ClusterName string              `json:"cluster_name"`
	Action      string              `json:"action"`
	NodeID      string              `json:"node_id,omitempty"`
	Node        *store.DbDeployNode `json:"node,omitempty"`
}

func validDbDeployTicketAction(a string) bool {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "deploy", "add_node", "remove_node", "delete_cluster":
		return true
	default:
		return false
	}
}

func dbDeployActionLabel(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "deploy":
		return "开始部署"
	case "add_node":
		return "添加节点"
	case "remove_node":
		return "删除节点"
	case "delete_cluster":
		return "删除集群"
	default:
		return a
	}
}

func dbDeployEngineLabel(e string) string {
	switch strings.ToLower(strings.TrimSpace(e)) {
	case "mysql":
		return "MySQL"
	case "postgres", "postgresql":
		return "PostgreSQL"
	case "redis":
		return "Redis"
	case "codis":
		return "Codis"
	default:
		return e
	}
}

// PrecheckAddNode POST /db-deploy/clusters/:id/nodes/precheck — 添加节点工单提交前的基础校验。
func (h *DbDeployHandler) PrecheckAddNode(c *gin.Context) {
	var req struct {
		store.DbDeployNode
		AssignedTo string `json:"assigned_to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cl, err := h.Store.GetDbDeployClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "集群不存在"})
		return
	}
	n := req.DbDeployNode
	n.ID = ""
	n.ClusterID = cl.ID
	if strings.TrimSpace(n.Host) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "节点 host 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	res := h.eng().PrecheckAddNode(ctx, cl.ID, dbdeploy.AddNodeCheckInput{
		Node: &n, HostInstanceID: n.HostInstanceID, AssignedTo: req.AssignedTo,
	})
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// CreateTicket POST /db-deploy/tickets — 高危操作须审批（引擎编排可后补）。
func (h *DbDeployHandler) CreateTicket(c *gin.Context) {
	var req struct {
		Engine      string          `json:"engine"`
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
	if !validDbDeployTicketAction(action) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的操作类型"})
		return
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	clusterID := strings.TrimSpace(req.ClusterID)
	if clusterID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "cluster_id 必填"})
		return
	}
	cl, err := h.Store.GetDbDeployClusterInTenant(clusterID, effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "集群不存在"})
		return
	}
	if engine == "" {
		engine = strings.ToLower(strings.TrimSpace(cl.Engine))
	}

	var body map[string]any
	_ = json.Unmarshal(req.Payload, &body)
	if body == nil {
		body = map[string]any{}
	}

	tp := DbDeployTicketPayload{
		Engine:      engine,
		ClusterID:   cl.ID,
		ClusterName: cl.Name,
		Action:      action,
	}

	switch action {
	case "add_node":
		raw, _ := json.Marshal(body)
		var n store.DbDeployNode
		_ = json.Unmarshal(raw, &n)
		n.ClusterID = cl.ID
		if strings.TrimSpace(n.Host) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "节点 host 必填"})
			return
		}
		chk := h.eng().PrecheckAddNode(c.Request.Context(), cl.ID, dbdeploy.AddNodeCheckInput{
			Node: &n, HostInstanceID: n.HostInstanceID, AssignedTo: req.AssignedTo, SkipAgentCalls: true,
		})
		blocking := []string{"agent_match", "duplicate", "role_rule"}
		if strings.TrimSpace(n.HostInstanceID) != "" {
			blocking = append(blocking, "host_instance")
		}
		if bad := chk.Failed(blocking...); bad != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": bad.Message, "data": chk})
			return
		}
		tp.Node = &n
	case "remove_node":
		nid, _ := body["node_id"].(string)
		nid = strings.TrimSpace(nid)
		if nid == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "node_id 必填"})
			return
		}
		tp.NodeID = nid
	case "deploy", "delete_cluster":
		// cluster_id already set
	}

	assigned, err := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.AssignedTo)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = fmt.Sprintf("%s %s · %s", dbDeployEngineLabel(engine), dbDeployActionLabel(action), cl.Name)
	}
	payloadBytes, _ := json.Marshal(tp)
	issue := &store.Issue{
		ID:          uuid.NewString(),
		TicketNo:    "D" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:   store.IssueTypeDbDeploy,
		InstanceID:  "",
		DBName:      cl.ID,
		SQLContent:  string(payloadBytes),
		Description: desc,
		Submitter:   middleware.Username(c),
		AssignedTo:  assigned,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil { issue.TenantID = tid }
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"issue": issue}})
}

// ExecuteDbDeployTicket 审批通过后执行；未实现的编排返回明确失败原因。
func (h *DbDeployHandler) ExecuteDbDeployTicket(ctx context.Context, it *store.Issue) (*ticketexec.Result, error) {
	started := time.Now().UTC().Format(time.RFC3339)
	res := &ticketexec.Result{StartedAt: started, Engine: "db_deploy", Success: true}
	var tp DbDeployTicketPayload
	if err := json.Unmarshal([]byte(it.SQLContent), &tp); err != nil {
		res.Success = false
		res.Message = "工单 payload 无效: " + err.Error()
		return res, nil
	}
	who := it.Submitter
	if who == "" {
		who = "ticket"
	}

	switch strings.ToLower(strings.TrimSpace(tp.Action)) {
	case "deploy":
		eng := h.eng()
		ops := dbDeployChangeOps(h.Store, func(id string) error { return eng.RetryChange(id, who) })
		changeID, err := resumeClusterChange(prevChangeID(it.ExecuteResult), ops)
		if err != nil {
			res.Success = false
			res.ChangeID = changeID
			res.Message = err.Error()
			return res, nil
		}
		if changeID == "" {
			changeID, err = eng.StartDeploy(tp.ClusterID, who)
		}
		if err != nil {
			res.Success = false
			res.Message = err.Error()
			// 引擎未就绪时仍给出可读提示
			if strings.Contains(err.Error(), "not implemented") || strings.Contains(err.Error(), "尚未") {
				res.Message = "该引擎编排尚未开放: " + err.Error()
			}
			return res, nil
		}
		persistTicketProgress(h.Store, it.ID, res, changeID)
		waitClusterChange(ctx, res, changeID, "集群部署", ops, func(r *ticketexec.Result) {
			persistTicketProgress(h.Store, it.ID, r, changeID)
		})
		return res, nil
	case "add_node":
		if tp.Node == nil {
			res.Success = false
			res.Message = "缺少节点信息"
			return res, nil
		}
		n := *tp.Node
		n.ClusterID = tp.ClusterID
		if err := h.Store.UpsertDbDeployNode(&n); err != nil {
			res.Success = false
			res.Message = err.Error()
			return res, nil
		}
		res.Summary = "节点已登记（扩容编排按引擎实现进度启用）"
	case "remove_node":
		if strings.TrimSpace(tp.NodeID) == "" {
			res.Success = false
			res.Message = "缺少 node_id"
			return res, nil
		}
		if err := h.Store.DeleteDbDeployNode(tp.NodeID); err != nil {
			res.Success = false
			res.Message = err.Error()
			return res, nil
		}
		res.Summary = "节点已删除"
	case "delete_cluster":
		if err := h.Store.DeleteDbDeployCluster(tp.ClusterID); err != nil {
			res.Success = false
			res.Message = err.Error()
			return res, nil
		}
		res.Summary = "集群已删除"
	default:
		res.Success = false
		res.Message = "未知操作: " + tp.Action
		return res, nil
	}
	res.EndedAt = time.Now().UTC().Format(time.RFC3339)
	return res, nil
}
