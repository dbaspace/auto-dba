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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"auto-dba/internal/k8s"
	"auto-dba/internal/k8s/operator"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// K8sChangePayload 生产变更工单载荷（存于 sql_content）。
type K8sChangePayload struct {
	ClusterID   string `json:"cluster_id"`
	ClusterName string `json:"cluster_name,omitempty"`
	Verb        string `json:"verb"` // apply|delete|drain|scale|db_create|db_delete|db_upgrade
	Namespace   string `json:"namespace,omitempty"`
	GVR         string `json:"gvr,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Name        string `json:"name,omitempty"`
	YAML        string `json:"yaml,omitempty"`
	Replicas    int32  `json:"replicas,omitempty"`
	Target      string `json:"target,omitempty"`
	Engine      string `json:"engine,omitempty"`
	Diff        string `json:"diff,omitempty"`
	SpecJSON    string `json:"spec_json,omitempty"`
}

// CreateK8sChangeTicket POST /k8s/change-tickets — 提交生产变更工单。
func (h *K8sHandler) CreateK8sChangeTicket(c *gin.Context) {
	var req struct {
		K8sChangePayload
		AssignedTo  string `json:"assigned_to"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.ClusterID) == "" || strings.TrimSpace(req.Verb) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "cluster_id 与 verb 必填"})
		return
	}
	cl, err := h.Store.GetK8sClusterInTenant(req.ClusterID, effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	// 生产判定：优先集群绑定环境
	if !h.Store.EnvIsProd(cl.EnvID) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "非生产环境可直接操作，无需工单"})
		return
	}
	req.ClusterName = cl.Name
	assigned, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.AssignedTo)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	payload, _ := json.Marshal(req.K8sChangePayload)
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = fmt.Sprintf("K8s %s · %s/%s", req.Verb, cl.Name, req.Name)
	}
	issue := &store.Issue{
		ID:          uuid.NewString(),
		TicketNo:    "K" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:   store.IssueTypeK8sChange,
		InstanceID:  cl.ID, // 复用字段挂集群 ID
		DBName:      req.Namespace,
		SQLContent:  string(payload),
		Description: desc,
		Submitter:   middleware.Username(c),
		AssignedTo:  assigned,
		NeedRollback: req.Diff,
	}
	stampTenantID(c, func(tid string) { issue.TenantID = tid })
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, req.Namespace, "ticket", req.Verb, req.Name, req.Diff, "ok", issue.TicketNo)
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": issue})
}

// ExecuteK8sChangeTicket 审批通过后执行。
func (h *K8sHandler) ExecuteK8sChangeTicket(ctx context.Context, it *store.Issue) (*ticketexec.Result, error) {
	started := time.Now().UTC().Format(time.RFC3339)
	res := &ticketexec.Result{StartedAt: started, Engine: "k8s", Success: true}
	var p K8sChangePayload
	if err := json.Unmarshal([]byte(it.SQLContent), &p); err != nil {
		res.Success = false
		res.Message = "载荷解析失败: " + err.Error()
		return res, nil
	}
	cl, err := h.Store.GetK8sCluster(p.ClusterID)
	if err != nil || cl == nil {
		res.Success = false
		res.Message = "集群不存在"
		return res, nil
	}
	cred, err := h.credFromCluster(cl, it.Submitter, "admin")
	if err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, nil
	}
	cli, err := h.Mgr.Get(cl.ID, cred)
	if err != nil {
		res.Success = false
		res.Message = err.Error()
		return res, nil
	}
	// 执行时放行生产拦截
	execCtx := context.WithValue(ctx, k8sTicketExecKey{}, true)

	switch strings.ToLower(p.Verb) {
	case "apply":
		out, err := cli.ApplyYAML(execCtx, p.YAML, false)
		if err != nil {
			res.Success = false
			res.Message = err.Error()
		} else {
			res.Summary = "apply " + out.Name
		}
	case "delete":
		gvr, err := k8s.ResolveGVR(p.GVR)
		if err != nil {
			res.Success = false
			res.Message = err.Error()
			break
		}
		if err := cli.Delete(execCtx, gvr, p.Namespace, p.Name); err != nil {
			res.Success = false
			res.Message = err.Error()
		} else {
			res.Summary = "deleted " + p.Name
		}
	case "drain":
		n, err := cli.Drain(execCtx, p.Name, 30)
		if err != nil {
			res.Success = false
			res.Message = err.Error()
		} else {
			res.Summary = fmt.Sprintf("drained, evicted=%d", n)
		}
	case "scale":
		kind := p.Kind
		if kind == "" {
			kind = p.GVR
		}
		if err := cli.Scale(execCtx, kind, p.Namespace, p.Name, p.Replicas); err != nil {
			res.Success = false
			res.Message = err.Error()
		} else {
			res.Summary = fmt.Sprintf("scaled to %d", p.Replicas)
		}
	case "db_create", "db_delete", "db_upgrade", "db_switchover":
		ad := h.Ops.Get(p.Engine)
		if ad == nil {
			res.Success = false
			res.Message = "无 Operator 适配器: " + p.Engine
			break
		}
		ref := operator.CRRef{Namespace: p.Namespace, Name: p.Name}
		var opErr error
		switch p.Verb {
		case "db_upgrade":
			opErr = ad.Upgrade(execCtx, cli, ref, p.Target)
		case "db_switchover":
			opErr = ad.Switchover(execCtx, cli, ref, p.Target)
		case "db_delete":
			gvr := engineGVR(p.Engine)
			opErr = cli.Dynamic.Resource(gvr).Namespace(p.Namespace).Delete(execCtx, p.Name, metav1.DeleteOptions{})
		case "db_create":
			var spec operator.DBClusterSpec
			_ = json.Unmarshal([]byte(p.SpecJSON), &spec)
			obj, rerr := ad.Render(spec)
			if rerr != nil {
				opErr = rerr
				break
			}
			gvr, gerr := guessCRGVR(obj)
			if gerr != nil {
				opErr = gerr
				break
			}
			_, opErr = cli.Dynamic.Resource(gvr).Namespace(spec.Namespace).Create(execCtx, obj, metav1.CreateOptions{})
		}
		if opErr != nil {
			res.Success = false
			res.Message = opErr.Error()
		} else {
			res.Summary = p.Verb + " ok"
		}
	default:
		res.Success = false
		res.Message = "未知 verb: " + p.Verb
	}
	_ = h.Store.CreateK8sAuditLog(&store.K8sAuditLog{
		TenantID: it.TenantID, ClusterID: cl.ID, Namespace: p.Namespace,
		Username: it.Submitter, Verb: "ticket_exec:" + p.Verb, Resource: p.GVR, Name: p.Name,
		Diff: p.Diff, Result: map[bool]string{true: "ok", false: "fail"}[res.Success], Message: res.Message,
	})
	return res, nil
}

type k8sTicketExecKey struct{}

func isK8sTicketExec(ctx context.Context) bool {
	v, _ := ctx.Value(k8sTicketExecKey{}).(bool)
	return v
}
