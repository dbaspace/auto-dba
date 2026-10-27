package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/k8s"
	"auto-dba/internal/k8s/operator"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type K8sHandler struct {
	Store    *store.DB
	Secret   string
	Mgr      *k8s.ClusterManager
	Cache    *k8s.InformerCache
	Hub      *k8s.AgentHub
	Ops      *operator.Registry
	probeOnce sync.Once
}

func NewK8sHandler(db *store.DB, fieldSecret string) *K8sHandler {
	h := &K8sHandler{
		Store:  db,
		Secret: fieldSecret,
		Mgr:    k8s.NewClusterManager(),
		Cache:  k8s.NewInformerCache(),
		Hub:    k8s.NewAgentHub(),
		Ops:    operator.DefaultRegistry(),
	}
	return h
}

func (h *K8sHandler) StartProbeLoop() {
	h.probeOnce.Do(func() {
		go func() {
			t := time.NewTicker(5 * time.Minute)
			defer t.Stop()
			for range t.C {
				h.probeAll()
			}
		}()
	})
}

func (h *K8sHandler) probeAll() {
	list, err := h.Store.ListAllK8sClusters()
	if err != nil {
		return
	}
	for i := range list {
		c := list[i]
		cred, err := h.credFromCluster(&c, "", "")
		if err != nil {
			_ = h.Store.UpdateK8sClusterProbe(c.ID, store.K8sStatusUnreachable, c.Version, c.NodeCount, err.Error())
			continue
		}
		res := h.Mgr.Probe(context.Background(), c.ID, cred)
		status := store.K8sStatusHealthy
		if !res.OK {
			status = store.K8sStatusUnreachable
		} else if len(res.Warnings) > 0 || res.ReadyNodes < res.NodeCount {
			status = store.K8sStatusDegraded
		}
		_ = h.Store.UpdateK8sClusterProbe(c.ID, status, res.Version, res.NodeCount, res.Error)
	}
}

func (h *K8sHandler) Meta(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"access_modes": []string{store.K8sAccessDirect, store.K8sAccessAgent},
		"auth_types":   []string{store.K8sAuthKubeconfig, store.K8sAuthToken},
		"resources":    k8s.KnownAliases(),
		"engines":      []string{"postgres", "mysql", "redis", "starrocks", "kubeblocks"},
	}})
}

func (h *K8sHandler) ListClusters(c *gin.Context) {
	list, err := h.Store.ListK8sClustersInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *K8sHandler) GetCluster(c *gin.Context) {
	cl, ok := h.clusterInTenant(c)
	if !ok {
		return
	}
	cl.CredentialEnc = ""
	c.JSON(http.StatusOK, gin.H{"data": cl})
}

type k8sClusterReq struct {
	Name               string `json:"name"`
	EnvID              string `json:"env_id"`
	RegionID           string `json:"region_id"`
	AccessMode         string `json:"access_mode"`
	AuthType           string `json:"auth_type"`
	APIServer          string `json:"api_server"`
	Credential         string `json:"credential"`
	CAData             string `json:"ca_data"`
	AgentID            string `json:"agent_id"`
	ImpersonateEnabled bool   `json:"impersonate_enabled"`
	Visibility         string `json:"visibility"`
	Remark             string `json:"remark"`
}

func (h *K8sHandler) CreateCluster(c *gin.Context) {
	var req k8sClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "name 必填"})
		return
	}
	if strings.TrimSpace(req.Credential) == "" && req.AccessMode != store.K8sAccessAgent {
		c.JSON(http.StatusBadRequest, gin.H{"message": "credential 必填"})
		return
	}
	enc, err := secret.Encrypt(h.Secret, req.Credential)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "凭据加密失败: " + err.Error()})
		return
	}
	who := middleware.Username(c)
	cl := &store.K8sCluster{
		Name: req.Name, EnvID: req.EnvID, RegionID: req.RegionID,
		AccessMode: orDefault(req.AccessMode, store.K8sAccessDirect),
		AuthType:   orDefault(req.AuthType, store.K8sAuthKubeconfig),
		APIServer:  req.APIServer, CredentialEnc: enc, CAData: req.CAData,
		AgentID: req.AgentID, ImpersonateEnabled: req.ImpersonateEnabled,
		Visibility: orDefault(req.Visibility, store.K8sVisibilityGlobal),
		Remark: req.Remark, CreatedBy: who, UpdatedBy: who,
		Status: store.K8sStatusUnknown,
	}
	stampTenantID(c, func(tid string) { cl.TenantID = tid })
	if err := h.Store.CreateK8sCluster(cl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, "", "create", "cluster", cl.Name, "", "ok", "")
	cl.CredentialEnc = ""
	cl.CredentialSet = true
	c.JSON(http.StatusOK, gin.H{"data": cl})
}

func (h *K8sHandler) UpdateCluster(c *gin.Context) {
	exist, ok := h.clusterOwned(c)
	if !ok {
		return
	}
	var req k8sClusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		exist.Name = req.Name
	}
	exist.EnvID = req.EnvID
	exist.RegionID = req.RegionID
	if req.AccessMode != "" {
		exist.AccessMode = req.AccessMode
	}
	if req.AuthType != "" {
		exist.AuthType = req.AuthType
	}
	exist.APIServer = req.APIServer
	exist.CAData = req.CAData
	exist.AgentID = req.AgentID
	exist.ImpersonateEnabled = req.ImpersonateEnabled
	if req.Visibility != "" {
		exist.Visibility = req.Visibility
	}
	exist.Remark = req.Remark
	exist.UpdatedBy = middleware.Username(c)
	if strings.TrimSpace(req.Credential) != "" {
		enc, err := secret.Encrypt(h.Secret, req.Credential)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "凭据加密失败"})
			return
		}
		exist.CredentialEnc = enc
		h.Mgr.Invalidate(exist.ID)
	}
	if err := h.Store.UpdateK8sCluster(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, exist.ID, "", "update", "cluster", exist.Name, "", "ok", "credential_changed="+boolStr(strings.TrimSpace(req.Credential) != ""))
	exist.CredentialEnc = ""
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *K8sHandler) DeleteCluster(c *gin.Context) {
	exist, ok := h.clusterOwned(c)
	if !ok {
		return
	}
	h.Mgr.Invalidate(exist.ID)
	h.Cache.Stop(exist.ID)
	if err := h.Store.DeleteK8sCluster(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, exist.ID, "", "delete", "cluster", exist.Name, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) ProbeCluster(c *gin.Context) {
	cl, ok := h.clusterInTenant(c)
	if !ok {
		return
	}
	cred, err := h.credFromCluster(cl, middleware.Username(c), middleware.Role(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	h.Mgr.Invalidate(cl.ID)
	res := h.Mgr.Probe(c.Request.Context(), cl.ID, cred)
	status := store.K8sStatusHealthy
	if !res.OK {
		status = store.K8sStatusUnreachable
	} else if len(res.Warnings) > 0 {
		status = store.K8sStatusDegraded
	}
	_ = h.Store.UpdateK8sClusterProbe(cl.ID, status, res.Version, res.NodeCount, res.Error)
	h.audit(c, cl.ID, "", "probe", "cluster", cl.Name, "", map[bool]string{true: "ok", false: "fail"}[res.OK], res.Error)
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *K8sHandler) Overview(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	ov, err := cli.GetOverview(c.Request.Context(), allow)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	_ = cl
	c.JSON(http.StatusOK, gin.H{"data": ov})
}

func (h *K8sHandler) ListNamespaceBindings(c *gin.Context) {
	cl, ok := h.clusterOwned(c)
	if !ok {
		return
	}
	list, err := h.Store.ListK8sNamespaceBindings(cl.ID, c.Query("tenant_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *K8sHandler) AssignNamespace(c *gin.Context) {
	cl, ok := h.clusterOwned(c)
	if !ok {
		return
	}
	var req struct {
		Namespace  string `json:"namespace"`
		TenantID   string `json:"tenant_id"`
		Visibility string `json:"visibility"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.TenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "namespace 与 tenant_id 必填"})
		return
	}
	b := &store.K8sNamespaceBinding{
		ClusterID: cl.ID, Namespace: req.Namespace, TenantID: req.TenantID,
		Visibility: orDefault(req.Visibility, store.K8sVisibilityGlobal),
		CreatedBy:  middleware.Username(c),
	}
	if err := h.Store.UpsertK8sNamespaceBinding(b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, req.Namespace, "assign", "namespace", req.Namespace, "", "ok", "tenant="+req.TenantID)
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *K8sHandler) DeleteNamespaceBinding(c *gin.Context) {
	if _, ok := h.clusterOwned(c); !ok {
		return
	}
	if err := h.Store.DeleteK8sNamespaceBinding(c.Param("bindId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) ListAudits(c *gin.Context) {
	cl, ok := h.clusterInTenant(c)
	if !ok {
		return
	}
	list, err := h.Store.ListK8sAuditLogs(cl.ID, "", 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *K8sHandler) AgentTunnel(c *gin.Context) {
	agentID := c.Param("agentId")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "agentId 必填"})
		return
	}
	// 加固开关开启时校验 agent token（与 k8s-agent --token / KEEPLUS_AGENT_TOKEN 对齐）
	if h.Store != nil && h.Store.HardenK8sTunnel() {
		expect := h.Store.K8sAgentTunnelToken()
		if expect == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"message": "已开启 K8s tunnel 校验，但未配置全局参数 k8s_agent_tunnel_token",
				"code":    "k8s_tunnel_token_unconfigured",
			})
			return
		}
		got := strings.TrimSpace(c.Query("token"))
		if got == "" {
			got = strings.TrimSpace(c.GetHeader("X-Agent-Token"))
		}
		if got == "" {
			raw := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
				got = strings.TrimSpace(raw[7:])
			}
		}
		if got != expect {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid agent token", "code": "k8s_tunnel_unauthorized"})
			return
		}
	}
	_ = h.Hub.ServeAgentWS(c.Writer, c.Request, agentID)
}

// --- helpers ---

func (h *K8sHandler) clusterInTenant(c *gin.Context) (*store.K8sCluster, bool) {
	cl, err := h.Store.GetK8sClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return nil, false
	}
	return cl, true
}

func (h *K8sHandler) clusterOwned(c *gin.Context) (*store.K8sCluster, bool) {
	tid := effectiveListTenantID(h.Store, c)
	cl, err := h.Store.GetK8sCluster(c.Param("id"))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return nil, false
	}
	if tid != "" && cl.TenantID != tid && !platformAdminAuditing(h.Store, c) {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return nil, false
	}
	return cl, true
}

func (h *K8sHandler) credFromCluster(cl *store.K8sCluster, username, role string) (k8s.ClusterCred, error) {
	plain, err := secret.Decrypt(h.Secret, cl.CredentialEnc)
	if err != nil {
		return k8s.ClusterCred{}, err
	}
	cred := k8s.ClusterCred{
		AccessMode: cl.AccessMode,
		AuthType:   cl.AuthType,
		APIServer:  cl.APIServer,
		Credential: plain,
		CAData:     cl.CAData,
		AgentID:    cl.AgentID,
	}
	if cl.AccessMode == store.K8sAccessAgent && cl.AgentID != "" {
		h.Mgr.SetAgentTransport(cl.AgentID, h.Hub.RoundTripper(cl.AgentID))
	}
	if cl.ImpersonateEnabled && role != "" {
		u, g := store.K8sImpersonateUser(role)
		cred.ImpersonateEnabled = true
		cred.ImpersonateUser = u
		cred.ImpersonateGroups = g
	}
	_ = username
	return cred, nil
}

func (h *K8sHandler) liveClient(c *gin.Context) (*k8s.Client, *store.K8sCluster, []string, bool) {
	cl, ok := h.clusterInTenant(c)
	if !ok {
		return nil, nil, nil, false
	}
	cred, err := h.credFromCluster(cl, middleware.Username(c), middleware.Role(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return nil, nil, nil, false
	}
	cli, err := h.Mgr.Get(cl.ID, cred)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return nil, nil, nil, false
	}
	tid := effectiveListTenantID(h.Store, c)
	allow, all, err := h.Store.AllowedNamespaces(cl.ID, tid, cl.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil, nil, nil, false
	}
	if all {
		return cli, cl, nil, true
	}
	return cli, cl, allow, true
}

func (h *K8sHandler) audit(c *gin.Context, clusterID, ns, verb, resource, name, diff, result, msg string) {
	a := &store.K8sAuditLog{
		ClusterID: clusterID, Namespace: ns, Username: middleware.Username(c),
		Verb: verb, Resource: resource, Name: name, Diff: diff, Result: result, Message: msg,
	}
	stampTenantID(c, func(tid string) { a.TenantID = tid })
	_ = h.Store.CreateK8sAuditLog(a)
}

func (h *K8sHandler) nsAllowed(allow []string, ns string) bool {
	if allow == nil {
		return true
	}
	if ns == "" {
		return false // 集群级资源：非归属方拒绝
	}
	for _, a := range allow {
		if a == ns {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
