package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/k8s"
	"auto-dba/internal/store"
)

func (h *K8sHandler) ListResources(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	gvr, err := k8s.ResolveGVR(c.Param("gvr"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ns := c.Query("ns")
	if k8s.ClusterScoped(gvr) {
		if allow != nil {
			c.JSON(http.StatusForbidden, gin.H{"message": "当前租户无权查看集群级资源"})
			return
		}
	} else if ns != "" && ns != "*" && !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "200"), 10, 64)
	opts := k8s.ListOpts{
		Namespace:     ns,
		LabelSelector: c.Query("labelSelector"),
		FieldSelector: c.Query("fieldSelector"),
		Limit:         limit,
		Continue:      c.Query("continue"),
	}
	// 非归属方且未指定 ns：仅列举已绑定命名空间并合并
	if allow != nil && (ns == "" || ns == "*") && !k8s.ClusterScoped(gvr) {
		var all []map[string]any
		for _, n := range allow {
			opts.Namespace = n
			res, err := cli.List(c.Request.Context(), gvr, opts)
			if err != nil {
				continue
			}
			all = append(all, res.Items...)
		}
		c.JSON(http.StatusOK, gin.H{"data": k8s.ListResult{Items: all}})
		return
	}
	res, err := cli.List(c.Request.Context(), gvr, opts)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	_ = cl
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *K8sHandler) GetResource(c *gin.Context) {
	cli, _, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	gvr, err := k8s.ResolveGVR(c.Param("gvr"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ns := c.Param("ns")
	name := c.Param("name")
	if ns == "_" {
		ns = ""
	}
	if k8s.ClusterScoped(gvr) {
		if allow != nil {
			c.JSON(http.StatusForbidden, gin.H{"message": "当前租户无权查看集群级资源"})
			return
		}
	} else if !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	asYAML := c.Query("format") == "yaml" || c.Query("yaml") == "1"
	obj, err := cli.Get(c.Request.Context(), gvr, ns, name, asYAML)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": obj})
}

func (h *K8sHandler) GetPodLog(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	ns, name := c.Param("ns"), c.Param("name")
	if !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	opts := k8s.LogOpts{
		Container:  c.Query("container"),
		Follow:     c.Query("follow") == "1" || c.Query("follow") == "true",
		Previous:   c.Query("previous") == "1",
		Timestamps: c.Query("timestamps") == "1",
	}
	if t := c.Query("tailLines"); t != "" {
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			opts.TailLines = n
		}
	}
	h.audit(c, cl.ID, ns, "log", "pods", name, "", "ok", "")
	if opts.Follow {
		_ = cli.StreamPodLogsWS(c, ns, name, opts)
		return
	}
	text, err := cli.GetPodLogs(c.Request.Context(), ns, name, opts)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"log": text}})
}

func (h *K8sHandler) DiscoverResources(c *gin.Context) {
	cli, _, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	if allow != nil {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅集群归属租户可浏览全部 API 资源"})
		return
	}
	list, err := cli.DiscoverAPIResources(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *K8sHandler) DeleteResource(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	gvr, err := k8s.ResolveGVR(c.Param("gvr"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ns, name := c.Param("ns"), c.Param("name")
	if ns == "_" {
		ns = ""
	}
	if k8s.ClusterScoped(gvr) {
		if allow != nil {
			c.JSON(http.StatusForbidden, gin.H{"message": "无权操作集群级资源"})
			return
		}
	} else if !h.nsAllowed(allow, ns) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	if h.requireProdTicket(c, cl, "delete", gvr.Resource+"/"+name) {
		return
	}
	if err := cli.Delete(c.Request.Context(), gvr, ns, name); err != nil {
		h.audit(c, cl.ID, ns, "delete", gvr.Resource, name, "", "fail", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, ns, "delete", gvr.Resource, name, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) ApplyYAML(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	var req struct {
		YAML   string `json:"yaml"`
		DryRun bool   `json:"dry_run"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "yaml 必填"})
		return
	}
	// dry-run 始终允许；真正 apply 检查生产工单
	if !req.DryRun {
		if h.requireProdTicket(c, cl, "apply", "yaml") {
			return
		}
	}
	res, err := cli.ApplyYAML(c.Request.Context(), req.YAML, req.DryRun)
	if err != nil {
		h.audit(c, cl.ID, "", "apply", "yaml", "", "", "fail", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	if allow != nil && res.Namespace != "" && !h.nsAllowed(allow, res.Namespace) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	result := "ok"
	if req.DryRun {
		result = "dry_run"
	}
	h.audit(c, cl.ID, res.Namespace, "apply", res.GVR, res.Name, res.Diff, result, "")
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// requireProdTicket 生产环境高危操作拦截：返回 true 表示已响应、调用方应 return。
func (h *K8sHandler) requireProdTicket(c *gin.Context, cl *store.K8sCluster, verb, target string) bool {
	if cl == nil || !h.Store.EnvIsProd(cl.EnvID) {
		return false
	}
	if c.Query("ticket_id") != "" || c.GetHeader("X-K8s-Ticket") != "" {
		return false // 已带工单放行（执行器调用）
	}
	c.JSON(http.StatusConflict, gin.H{
		"message": "生产环境变更需提交 K8s 变更工单",
		"code":    "k8s_change_required",
		"hint":    "请创建类型为 k8s_change 的工单，审批通过后由执行器带 ticket 执行",
		"verb":    verb,
		"target":  target,
		"cluster": cl.ID,
	})
	return true
}
