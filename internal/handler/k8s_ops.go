package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
)

func (h *K8sHandler) ScaleWorkload(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	ns, kind, name := c.Param("ns"), c.Param("kind"), c.Param("name")
	if !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	var req struct {
		Replicas int32 `json:"replicas"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.Replicas == 0 {
		if h.requireProdTicket(c, cl, "scale_to_zero", kind+"/"+name) {
			return
		}
	}
	if err := cli.Scale(c.Request.Context(), kind, ns, name, req.Replicas); err != nil {
		h.audit(c, cl.ID, ns, "scale", kind, name, "", "fail", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, ns, "scale", kind, name, "", "ok", "replicas="+strconv.Itoa(int(req.Replicas)))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) RestartWorkload(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	ns, kind, name := c.Param("ns"), c.Param("kind"), c.Param("name")
	if !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	if err := cli.RestartWorkload(c.Request.Context(), kind, ns, name); err != nil {
		h.audit(c, cl.ID, ns, "restart", kind, name, "", "fail", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, ns, "restart", kind, name, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) CordonNode(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	if allow != nil {
		c.JSON(http.StatusForbidden, gin.H{"message": "无权操作节点"})
		return
	}
	var req struct {
		Unschedulable bool `json:"unschedulable"`
	}
	_ = c.ShouldBindJSON(&req)
	name := c.Param("name")
	if err := cli.Cordon(c.Request.Context(), name, req.Unschedulable); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	verb := "uncordon"
	if req.Unschedulable {
		verb = "cordon"
	}
	h.audit(c, cl.ID, "", verb, "nodes", name, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) DrainNode(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	if allow != nil {
		c.JSON(http.StatusForbidden, gin.H{"message": "无权操作节点"})
		return
	}
	if h.requireProdTicket(c, cl, "drain", c.Param("name")) {
		return
	}
	grace := int64(30)
	if g := c.Query("grace"); g != "" {
		if n, err := strconv.ParseInt(g, 10, 64); err == nil {
			grace = n
		}
	}
	n, err := cli.Drain(c.Request.Context(), c.Param("name"), grace)
	if err != nil {
		h.audit(c, cl.ID, "", "drain", "nodes", c.Param("name"), "", "fail", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, "", "drain", "nodes", c.Param("name"), "", "ok", "evicted="+strconv.Itoa(n))
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"evicted": n}})
}

func (h *K8sHandler) ExecPod(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	ns, name := c.Param("ns"), c.Param("name")
	if !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	container := c.Query("container")
	var cmd []string
	if s := c.Query("command"); s != "" {
		cmd = strings.Fields(s)
	}
	h.audit(c, cl.ID, ns, "exec", "pods", name, "", "ok", "user="+middleware.Username(c))
	_ = cli.ExecPodWS(c, ns, name, container, cmd)
}

// MultiClusterSummary P4：多集群聚合摘要。
func (h *K8sHandler) MultiClusterSummary(c *gin.Context) {
	list, err := h.Store.ListK8sClustersInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	type row struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Version  string `json:"version"`
		Nodes    int    `json:"nodes"`
		EnvID    string `json:"env_id"`
	}
	out := make([]row, 0, len(list))
	healthy, degraded, down := 0, 0, 0
	for _, cl := range list {
		out = append(out, row{ID: cl.ID, Name: cl.Name, Status: cl.Status, Version: cl.Version, Nodes: cl.NodeCount, EnvID: cl.EnvID})
		switch cl.Status {
		case "healthy":
			healthy++
		case "degraded":
			degraded++
		default:
			down++
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"clusters": out,
		"summary":  gin.H{"healthy": healthy, "degraded": degraded, "unreachable": down, "total": len(list)},
	}})
}

// CostEstimate P4：粗略成本占位（按节点数估算）。
func (h *K8sHandler) CostEstimate(c *gin.Context) {
	list, err := h.Store.ListK8sClustersInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	const perNodeMonth = 120.0 // 占位单价
	var totalNodes int
	items := []gin.H{}
	for _, cl := range list {
		totalNodes += cl.NodeCount
		items = append(items, gin.H{
			"cluster_id": cl.ID, "name": cl.Name, "nodes": cl.NodeCount,
			"estimate_cny_month": float64(cl.NodeCount) * perNodeMonth,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"currency": "CNY",
		"note":     "占位估算，非真实账单；可对接云成本 API",
		"total_nodes": totalNodes,
		"estimate_cny_month": float64(totalNodes) * perNodeMonth,
		"items": items,
	}})
}
