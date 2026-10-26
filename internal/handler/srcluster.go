package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/srcluster"
	"auto-dba/internal/store"
)

type SrClusterHandler struct {
	Store  *store.DB
	Secret string
	Engine *srcluster.Engine
}

func (h *SrClusterHandler) eng() *srcluster.Engine {
	if h != nil && h.Engine != nil {
		return h.Engine
	}
	return srcluster.New(h.Store, h.Secret, srcluster.Config{})
}

func (h *SrClusterHandler) Meta(c *gin.Context) {
	layout := store.SrLayoutDefaults{}
	if h != nil && h.Store != nil {
		h.Store.EnsureSrLayoutDefaults()
		layout = h.Store.SrLayoutDefaults()
	} else {
		layout = store.SrLayoutDefaults{
			FeSoftwareBase: store.DefaultSrSoftwareBase,
			BeSoftwareBase: store.DefaultSrSoftwareBase,
			BeDataRoot:     store.DefaultSrDataRoot,
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"change_types": []string{
			store.SrChangeClusterInit, store.SrChangeClusterReinit, store.SrChangeAddFE, store.SrChangeAddBE,
			store.SrChangeRestart, store.SrChangeStartNode, store.SrChangeStopNode,
			store.SrChangeRemoveFE, store.SrChangeRemoveBE,
			store.SrChangeDecommissionBE, store.SrChangeUpgrade, store.SrChangeConfigApply, store.SrChangeConfigRollback,
		},
		"defaults": gin.H{
			"software_base":     layout.FeSoftwareBase,
			"be_software_base":  layout.BeSoftwareBase,
			"data_root":         layout.BeDataRoot,
			"fe_http_port":      8030,
			"fe_edit_port":      9010,
			"fe_query_port":     9030,
			"be_heartbeat_port": 9050,
			"oss_prefix":        "starrocks/",
			"agent_listen_port": 8091,
			"agent_remote_dir":  "/home/sysadmin/auto-dba/node-agent",
		},
		"conf_template_vars": []string{
			"Host", "LogDir", "MetaDir", "StorageRoot", "PriorityNetworks",
			"HTTPPort", "EditPort", "QueryPort", "RPCPort", "HeartbeatPort", "BEPort", "BRPCPort",
		},
		"notes": "版本包存 OSS；Node Agent 在本页 SSH 下发（:8091）；初始化/扩容/升级/配置下发走变更单。fe.conf/be.conf：集群草稿 > 平台模板 > 内置；meta_dir/storage_root_path 由平台按 data_root 锁定追加。",
	}})
}

// --- Agents ---

// hostInstanceAllowed 主机实例非空时须属于当前租户（或已授权），否则写 404 并返回 false。
func (h *SrClusterHandler) hostInstanceAllowed(c *gin.Context, instanceID string) bool {
	if strings.TrimSpace(instanceID) == "" {
		return true
	}
	if it, err := resolveInstanceInTenant(h.Store, c, instanceID); err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "主机实例不存在"})
		return false
	}
	return true
}

func (h *SrClusterHandler) ListAgents(c *gin.Context) {
	list, err := h.Store.ListSrAgentsInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) CreateAgent(c *gin.Context) {
	var req struct {
		Name           string `json:"name"`
		BaseURL        string `json:"base_url"`
		Username       string `json:"username"`
		Password       string `json:"password"`
		HostInstanceID string `json:"host_instance_id"`
		Remark         string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "base_url 必填"})
		return
	}
	if !h.hostInstanceAllowed(c, req.HostInstanceID) {
		return
	}
	enc, err := secret.Encrypt(h.Secret, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	who := middleware.Username(c)
	a := &store.SrAgent{
		Name: req.Name, BaseURL: strings.TrimRight(req.BaseURL, "/"),
		Username: req.Username, PasswordEnc: enc, HostInstID: req.HostInstanceID,
		Remark: req.Remark, CreatedBy: who, UpdatedBy: who,
	}
	if err := h.Store.CreateSrAgent(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if tid := middleware.TenantID(c); tid != "" {
		_ = h.Store.AssignTenantID("tbl_sr_agents", a.ID, tid)
	}
	rebound, _ := h.Store.RebindSrNodesAgent(a.HostInstID, agentAddrs(h.Store, a.HostInstID, a.BaseURL), a.ID)
	a.PasswordSet = enc != ""
	c.JSON(http.StatusOK, gin.H{"data": a, "rebound_nodes": rebound})
}

// DeployAgent POST /sr-cluster/agents/deploy — SSH 下发 node-agent（:8091），成功后登记 SrAgent。
func (h *SrClusterHandler) DeployAgent(c *gin.Context) {
	var req struct {
		HostInstanceID string `json:"host_instance_id"`
		ListenPort     int    `json:"listen_port"`
		RemoteDir      string `json:"remote_dir"`
		Escalate       string `json:"escalate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if !h.hostInstanceAllowed(c, req.HostInstanceID) {
		return
	}
	id, err := h.eng().StartDeploy(srcluster.DeployOpts{
		HostInstanceID: req.HostInstanceID,
		ListenPort:     req.ListenPort,
		RemoteDir:      req.RemoteDir,
		Escalate:       req.Escalate,
		Operator:       middleware.Username(c),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deploy_id": id}})
}

// DeployAgentBatch POST /sr-cluster/agents/deploy-batch — 对多台已登记主机批量 SSH 下发。
func (h *SrClusterHandler) DeployAgentBatch(c *gin.Context) {
	var req struct {
		HostInstanceIDs []string `json:"host_instance_ids"`
		ListenPort      int      `json:"listen_port"`
		RemoteDir       string   `json:"remote_dir"`
		Escalate        string   `json:"escalate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := make([]string, 0, len(req.HostInstanceIDs))
	seen := map[string]struct{}{}
	for _, id := range req.HostInstanceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "host_instance_ids 必填"})
		return
	}
	who := middleware.Username(c)
	type one struct {
		HostInstanceID string `json:"host_instance_id"`
		DeployID       string `json:"deploy_id,omitempty"`
		Error          string `json:"error,omitempty"`
	}
	out := make([]one, 0, len(ids))
	okCount, failCount := 0, 0
	for _, id := range ids {
		if it, err := resolveInstanceInTenant(h.Store, c, id); err != nil || it == nil {
			out = append(out, one{HostInstanceID: id, Error: "主机实例不存在或不在当前租户"})
			failCount++
			continue
		}
		deployID, err := h.eng().StartDeploy(srcluster.DeployOpts{
			HostInstanceID: id,
			ListenPort:     req.ListenPort,
			RemoteDir:      req.RemoteDir,
			Escalate:       req.Escalate,
			Operator:       who,
		})
		if err != nil {
			out = append(out, one{HostInstanceID: id, Error: err.Error()})
			failCount++
			continue
		}
		out = append(out, one{HostInstanceID: id, DeployID: deployID})
		okCount++
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"results": out, "ok_count": okCount, "fail_count": failCount,
	}})
}

func (h *SrClusterHandler) ListAgentDeploys(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListSrAgentDeploysInTenant(effectiveListTenantID(h.Store, c), c.Query("host_instance_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) GetAgentDeploy(c *gin.Context) {
	it, err := h.Store.GetSrAgentDeploy(c.Param("id"))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "下发记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *SrClusterHandler) DeleteAgent(c *gin.Context) {
	nodes, err := h.Store.ListSrNodesByAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.DeleteSrAgent(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "affected_nodes": srNodeRefs(newClusterNames(h.Store), nodes)})
}

func (h *SrClusterHandler) ProbeAgent(c *gin.Context) {
	ag, err := h.Store.GetSrAgent(c.Param("id"))
	if err != nil || ag == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	st := h.eng().ProbeAgentStatus(c.Request.Context(), ag)
	c.JSON(http.StatusOK, gin.H{"ok": st.Status == srcluster.AgentStatusRunning, "data": st})
}

func (h *SrClusterHandler) StartAgent(c *gin.Context) {
	ag, err := h.Store.GetSrAgent(c.Param("id"))
	if err != nil || ag == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	if err := h.eng().StartRemoteAgent(ag); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	st := h.eng().ProbeAgentStatus(c.Request.Context(), ag)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": st})
}

func (h *SrClusterHandler) StopAgent(c *gin.Context) {
	ag, err := h.Store.GetSrAgent(c.Param("id"))
	if err != nil || ag == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Agent 不存在"})
		return
	}
	if err := h.eng().StopRemoteAgent(ag); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	st := h.eng().ProbeAgentStatus(c.Request.Context(), ag)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": st})
}

func (h *SrClusterHandler) ListAgentStatus(c *gin.Context) {
	list, err := h.Store.ListSrAgentsInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]srcluster.AgentRuntimeStatus, 0, len(list))
	for i := range list {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		st := h.eng().ProbeAgentStatus(ctx, &list[i])
		cancel()
		out = append(out, st)
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// --- Clusters ---

func (h *SrClusterHandler) enrichSrClusterTenants(list []store.SrCluster) {
	names := tenantNameMap(h.Store)
	for i := range list {
		applyTenantName(names, &list[i].TenantID, &list[i].TenantName)
	}
}

func (h *SrClusterHandler) ListClusters(c *gin.Context) {
	list, err := h.Store.ListSrClustersInTenant(effectiveListTenantID(h.Store, c), c.Query("environment"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.SrCluster{}
	}
	h.enrichSrClusterTenants(list)
	c.JSON(http.StatusOK, gin.H{"data": list, "audit_scope": auditScope(h.Store, c)})
}

// AssignTenant POST /sr-cluster/clusters/:id/assign-tenant
func (h *SrClusterHandler) AssignTenant(c *gin.Context) {
	to, ok := parseAssignTenantID(h.Store, c)
	if !ok {
		return
	}
	id := c.Param("id")
	if err := h.Store.AssignSrClusterTenant(id, to); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(to, middleware.UserID(c), "resource.assign", "sr_cluster:"+id)
	cl, _ := h.Store.GetSrCluster(id)
	if cl != nil {
		h.enrichSrClusterTenants([]store.SrCluster{*cl})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": cl})
}

func (h *SrClusterHandler) GetCluster(c *gin.Context) {
	cl, err := resolveSrClusterInTenant(h.Store, c, c.Param("id"))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	h.enrichSrClusterTenants([]store.SrCluster{*cl})
	nodes, _ := h.Store.ListSrNodes(cl.ID)
	eng := h.eng()
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"cluster":     cl,
		"nodes":       nodes,
		"instance":    eng.LinkedInstance(cl, nodes),
		"monitor_job": eng.ClusterMonitor(cl, nodes),
	}})
}

// SyncRegistry 手动同步集群到配置中心：登记/关联资源实例，并同步监控目标注册。
func (h *SrClusterHandler) SyncRegistry(c *gin.Context) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	res := h.eng().SyncRegistry(cl, nil, middleware.Username(c), true)
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// GetClusterLive 节点实时角色/存活/版本（SHOW PROC）；FE 不可达时返回空 nodes 与原因，不报错。
func (h *SrClusterHandler) GetClusterLive(c *gin.Context) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	live, removed, err := h.eng().ClusterLive(c.Request.Context(), cl)
	removedOut := make([]gin.H, 0, len(removed))
	for _, n := range removed {
		removedOut = append(removedOut, gin.H{"id": n.ID, "host": n.Host, "component": n.Component})
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"nodes": gin.H{}, "removed_nodes": removedOut}, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"nodes": live, "removed_nodes": removedOut}})
}

// ProbeLayout POST /sr-cluster/probe-layout — 登记前自动发现 FE/BE software_base 与 BE storage_root_path（不写库）。
func (h *SrClusterHandler) ProbeLayout(c *gin.Context) {
	var req struct {
		FeHost      string   `json:"fe_host"`
		FePassword  string   `json:"fe_password"`
		FeUser      string   `json:"fe_user"`
		FeQueryPort int      `json:"fe_query_port"`
		Hosts       []string `json:"hosts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	host := strings.TrimSpace(req.FeHost)
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "fe_host 必填"})
		return
	}
	user := strings.TrimSpace(req.FeUser)
	pass := req.FePassword
	if strings.TrimSpace(pass) == "" {
		cl := &store.SrCluster{FeHost: host, FeUser: user}
		nodes := []store.SrNode{{Component: "fe", Host: host}}
		for _, hst := range req.Hosts {
			hst = strings.TrimSpace(hst)
			if hst != "" && !strings.EqualFold(hst, host) {
				nodes = append(nodes, store.SrNode{Component: "fe", Host: hst})
			}
		}
		if u, p, ok := h.eng().LookupFeAdminCredForProbe(cl, nodes); ok {
			user, pass = u, p
		}
	}
	res, err := h.eng().ProbeLayout(c.Request.Context(), host, user, pass, req.FeQueryPort)
	if err != nil {
		port := req.FeQueryPort
		if port <= 0 {
			port = 9030
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("无法探测布局 %s:%d（%v）", host, port, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// ProbeClusterLayout POST /sr-cluster/clusters/:id/probe-layout — 用已存凭据探测并可选写回路径（apply=true）。
func (h *SrClusterHandler) ProbeClusterLayout(c *gin.Context) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	apply := c.Query("apply") == "1" || c.Query("apply") == "true"
	var body struct {
		Apply bool `json:"apply"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Apply {
		apply = true
	}
	nodes, _ := h.Store.ListSrNodes(cl.ID)
	user, pass, err := h.eng().ExportFeCredentials(cl, nodes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无法取得 FE 凭据: " + err.Error()})
		return
	}
	host := strings.TrimSpace(cl.FeHost)
	if host == "" {
		for _, n := range nodes {
			if n.Component == "fe" && n.IsPrimaryFE {
				host = n.Host
				break
			}
		}
	}
	if host == "" {
		for _, n := range nodes {
			if n.Component == "fe" {
				host = n.Host
				break
			}
		}
	}
	res, err := h.eng().ProbeLayout(c.Request.Context(), host, user, pass, cl.FeQueryPort)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if apply {
		if err := h.eng().ApplyLayoutProbe(cl, nodes, res, middleware.Username(c)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		cl, _ = h.Store.GetSrCluster(cl.ID)
		nodes, _ = h.Store.ListSrNodes(cl.ID)
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"layout": res, "cluster": cl, "nodes": nodes, "applied": apply}})
}

// ProbeFERoles POST /sr-cluster/probe-fe-roles — 登记前探测 SHOW PROC '/frontends' 实时角色（不写库）。
func (h *SrClusterHandler) ProbeFERoles(c *gin.Context) {
	var req struct {
		FeHost      string   `json:"fe_host"`
		FePassword  string   `json:"fe_password"`
		FeUser      string   `json:"fe_user"`
		FeQueryPort int      `json:"fe_query_port"`
		Hosts       []string `json:"hosts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	host := strings.TrimSpace(req.FeHost)
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "fe_host 必填"})
		return
	}
	user := strings.TrimSpace(req.FeUser)
	pass := req.FePassword
	if strings.TrimSpace(pass) == "" {
		// 表单未填时按 FE 管理凭据回退
		cl := &store.SrCluster{FeHost: host, FeUser: user}
		nodes := []store.SrNode{{Component: "fe", Host: host}}
		for _, hst := range req.Hosts {
			hst = strings.TrimSpace(hst)
			if hst != "" && !strings.EqualFold(hst, host) {
				nodes = append(nodes, store.SrNode{Component: "fe", Host: hst})
			}
		}
		if u, p, ok := h.eng().LookupFeAdminCredForProbe(cl, nodes); ok {
			user, pass = u, p
		}
	}
	roles, err := h.eng().ProbeFERoles(c.Request.Context(), host, user, pass, req.FeQueryPort)
	if err != nil {
		port := req.FeQueryPort
		if port <= 0 {
			port = 9030
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("无法连接 FE %s:%d（%v）。请填写密码或先在 FE 管理配置凭据", host, port, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": roles})
}

func (h *SrClusterHandler) CreateCluster(c *gin.Context) {
	var req struct {
		store.SrCluster
		FePassword        string         `json:"fe_password"`
		Nodes             []store.SrNode `json:"nodes"`
		InitNow           bool           `json:"init_now"`
		RegisterExisting  bool           `json:"register_existing"`
		SourceMonitorTask string         `json:"source_monitor_task"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "name 必填"})
		return
	}
	if len(req.Nodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少配置 FE/BE 节点（须带角色，禁止无角色部署）"})
		return
	}
	feCount, primaryCount := 0, 0
	var primaryHost string
	for i := range req.Nodes {
		n := &req.Nodes[i]
		n.Component = strings.ToLower(strings.TrimSpace(n.Component))
		if n.Component != "fe" && n.Component != "be" {
			c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("节点 %s component 须为 fe 或 be", n.Host)})
			return
		}
		if strings.TrimSpace(n.Host) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "节点 host 必填"})
			return
		}
		if strings.TrimSpace(n.AgentID) == "" && !req.RegisterExisting {
			c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("节点 %s 须绑定 Node Agent", n.Host)})
			return
		}
		if n.Component == "fe" {
			feCount++
			// 不可指定部署为 leader；观测角色仅 follower/observer
			role := strings.ToLower(strings.TrimSpace(n.FeRole))
			if role == "leader" {
				c.JSON(http.StatusBadRequest, gin.H{"message": "不可指定部署为 leader，由选举产生"})
				return
			}
			if n.IsPrimaryFE {
				primaryCount++
				primaryHost = n.Host
				n.FeRole = "follower" // 首 FE 也是 follower 语义，bootstrap 无 --helper
			} else {
				if role == "" {
					n.FeRole = "follower"
				} else if role != "follower" && role != "observer" {
					c.JSON(http.StatusBadRequest, gin.H{"message": "FE 子角色仅支持 follower / observer"})
					return
				}
			}
		} else {
			n.IsPrimaryFE = false
			n.FeRole = ""
		}
		if req.RegisterExisting {
			n.Status = store.SrNodeReady
		}
	}
	if err := store.ValidateOddFE(feCount); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if primaryCount != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "须且仅能指定 1 个首 FE（bootstrap，启动不带 --helper）"})
		return
	}

	who := middleware.Username(c)
	cl := req.SrCluster
	cl.CreatedBy, cl.UpdatedBy = who, who
	if strings.TrimSpace(primaryHost) != "" && strings.TrimSpace(cl.FeHost) == "" {
		cl.FeHost = primaryHost
	}
	if strings.TrimSpace(req.ReleaseID) != "" {
		if err := h.Store.ValidateReleaseForEnv(cl.Environment, req.ReleaseID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if rel, _ := h.Store.GetSrRelease(req.ReleaseID); rel != nil {
			cl.Version = rel.Version
			cl.ReleaseID = rel.ID
		}
	} else if req.RegisterExisting {
		// 登记已有集群允许仅写 version（常来自监控目标 tag），不强制绑定制品 Release
		if strings.TrimSpace(cl.Version) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "登记已有集群须填写 version，或选择已登记的 release_id"})
			return
		}
	} else if strings.TrimSpace(cl.Version) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择版本（release_id）"})
		return
	}
	if strings.TrimSpace(req.FePassword) != "" {
		enc, err := secret.Encrypt(h.Secret, req.FePassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		cl.FePasswordEnc = enc
	}
	var warnings []string
	if req.RegisterExisting {
		diffs, err := h.eng().ProbeAndAlignFERoles(c.Request.Context(), &cl, req.Nodes)
		if err != nil {
			port := cl.FeQueryPort
			if port <= 0 {
				port = 9030
			}
			c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf(
				"无法连接 FE %s:%d（%v）。请填写正确的 FE 密码，或先在「资源配置 → 实例管理 → StarRocks」登记该集群并填写密码",
				cl.FeHost, port, err)})
			return
		}
		warnings = append(warnings, diffs...)
	}
	if req.RegisterExisting {
		cl.Source = store.SrSourceRegister
		cl.Status = store.SrClusterReady
		note := "登记已有集群"
		if t := strings.TrimSpace(req.SourceMonitorTask); t != "" {
			note += "；监控任务=" + t
		}
		if strings.TrimSpace(cl.Remark) == "" {
			cl.Remark = note
		} else if !strings.Contains(cl.Remark, "登记已有集群") {
			cl.Remark = cl.Remark + "；" + note
		}
	} else {
		cl.Source = store.SrSourceDeploy
	}
	stampTenantID(c, func(tid string) { cl.TenantID = tid })
	if err := h.Store.CreateSrCluster(&cl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	for i := range req.Nodes {
		n := req.Nodes[i]
		n.ClusterID = cl.ID
		if n.Component == "fe" {
			if n.HTTPPort <= 0 {
				n.HTTPPort = cl.FeHTTPPort
			}
			if n.EditPort <= 0 {
				n.EditPort = cl.FeEditPort
			}
			if n.SystemdUnit == "" {
				n.SystemdUnit = "fe-" + strconv.Itoa(cl.FeQueryPort)
			}
			n.SoftwarePath = strings.TrimRight(strings.TrimSpace(n.SoftwarePath), "/")
			n.DataRoot = ""
		} else {
			if n.HeartbeatPort <= 0 {
				n.HeartbeatPort = cl.BeHeartbeatPort
			}
			if n.SystemdUnit == "" {
				n.SystemdUnit = "be-" + strconv.Itoa(n.HeartbeatPort)
			}
			n.SoftwarePath = strings.TrimRight(strings.TrimSpace(n.SoftwarePath), "/")
			n.DataRoot = strings.TrimRight(strings.TrimSpace(n.DataRoot), "/")
		}
		if err := h.Store.CreateSrNode(&n); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "节点写入失败: " + err.Error()})
			return
		}
	}
	cl.FePasswordSet = cl.FePasswordEnc != ""
	out := gin.H{"cluster": cl, "nodes": req.Nodes}
	if req.RegisterExisting {
		res := h.eng().SyncRegistry(&cl, nil, who, true)
		out["registry"] = res
		warnings = append(warnings, res.Warnings...)
		if len(warnings) > 0 {
			c.JSON(http.StatusOK, gin.H{"data": out, "warning": strings.Join(warnings, "；")})
			return
		}
	}
	if req.InitNow && !req.RegisterExisting {
		changeID, err := h.eng().StartClusterInit(cl.ID, who)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"data": out, "warning": "集群已创建但初始化未启动: " + err.Error()})
			return
		}
		out["change_id"] = changeID
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *SrClusterHandler) UpdateCluster(c *gin.Context) {
	exist, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	var req struct {
		store.SrCluster
		FePassword string `json:"fe_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cl := *exist
	if req.Name != "" {
		cl.Name = req.Name
	}
	if req.Environment != "" {
		cl.Environment = req.Environment
	}
	if req.Version != "" {
		cl.Version = req.Version
	}
	if req.ReleaseID != "" {
		cl.ReleaseID = req.ReleaseID
		if rel, _ := h.Store.GetSrRelease(req.ReleaseID); rel != nil {
			cl.Version = rel.Version
		}
	}
	if req.SoftwareBase != "" {
		cl.SoftwareBase = req.SoftwareBase
	}
	if req.BeSoftwareBase != "" {
		cl.BeSoftwareBase = req.BeSoftwareBase
	}
	if req.DataRoot != "" {
		cl.DataRoot = req.DataRoot
	}
	if req.FeHost != "" {
		cl.FeHost = req.FeHost
	}
	if req.FeUser != "" {
		cl.FeUser = req.FeUser
	}
	if req.FeHTTPPort > 0 {
		cl.FeHTTPPort = req.FeHTTPPort
	}
	if req.FeEditPort > 0 {
		cl.FeEditPort = req.FeEditPort
	}
	if req.FeQueryPort > 0 {
		cl.FeQueryPort = req.FeQueryPort
	}
	if req.BeHeartbeatPort > 0 {
		cl.BeHeartbeatPort = req.BeHeartbeatPort
	}
	if req.Remark != "" {
		cl.Remark = req.Remark
	}
	if strings.TrimSpace(req.FePassword) != "" {
		enc, err := secret.Encrypt(h.Secret, req.FePassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		cl.FePasswordEnc = enc
	}
	cl.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpdateSrCluster(&cl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	cl.FePasswordSet = cl.FePasswordEnc != ""
	c.JSON(http.StatusOK, gin.H{"data": cl})
}

func (h *SrClusterHandler) DeleteCluster(c *gin.Context) {
	exist, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	// 仅删平台登记（集群/节点/变更/配置）；不停 FE/BE、不删 Node Agent、不删资源配置实例
	if err := h.Store.DeleteSrCluster(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已注销登记（未影响远程服务与 Agent）"})
}

// --- Nodes ---

func (h *SrClusterHandler) ListNodes(c *gin.Context) {
	if cl, err := resolveSrClusterInTenant(h.Store, c, c.Param("id")); err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	list, err := h.Store.ListSrNodes(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) CreateNode(c *gin.Context) {
	clusterID := c.Param("id")
	cl, err := h.Store.GetSrClusterInTenant(clusterID, effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	var req struct {
		store.SrNode
		SoftwareBase string `json:"software_base"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	n := req.SrNode
	n.ClusterID = clusterID
	n.Component = strings.ToLower(strings.TrimSpace(n.Component))
	if n.Component != "fe" && n.Component != "be" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "component 须为 fe 或 be"})
		return
	}
	if strings.TrimSpace(n.Host) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "host 必填"})
		return
	}
	if strings.TrimSpace(n.AgentID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "agent_id 必填（须先登记 Node Agent）"})
		return
	}
	explicitBase := strings.TrimRight(strings.TrimSpace(req.SoftwareBase), "/")
	if n.Component == "fe" {
		if n.HTTPPort <= 0 {
			n.HTTPPort = cl.FeHTTPPort
		}
		if n.EditPort <= 0 {
			n.EditPort = cl.FeEditPort
		}
		if n.SystemdUnit == "" {
			n.SystemdUnit = "fe-" + strconv.Itoa(cl.FeQueryPort)
		}
		if strings.TrimSpace(n.SoftwarePath) == "" && explicitBase != "" {
			n.SoftwarePath = explicitBase + "/fe"
		}
		n.DataRoot = "" // FE 不挂数据盘
		role := strings.ToLower(strings.TrimSpace(n.FeRole))
		if role == "leader" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "不可指定部署为 leader"})
			return
		}
		if n.IsPrimaryFE {
			n.FeRole = "follower"
		} else if role == "" {
			n.FeRole = "follower"
		} else if role != "follower" && role != "observer" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "FE 子角色仅支持 follower / observer"})
			return
		}
	} else {
		n.IsPrimaryFE = false
		n.FeRole = ""
		if n.HeartbeatPort <= 0 {
			n.HeartbeatPort = cl.BeHeartbeatPort
		}
		if n.SystemdUnit == "" {
			n.SystemdUnit = "be-" + strconv.Itoa(n.HeartbeatPort)
		}
		if strings.TrimSpace(n.SoftwarePath) == "" && explicitBase != "" {
			n.SoftwarePath = explicitBase + "/be"
		}
		n.DataRoot = strings.TrimRight(strings.TrimSpace(n.DataRoot), "/")
	}
	if err := h.Store.CreateSrNode(&n); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": n})
}

func (h *SrClusterHandler) DeleteNode(c *gin.Context) {
	if n, err := h.Store.GetSrNode(c.Param("nodeId")); err != nil || n == nil || n.ClusterID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"message": "节点不存在"})
		return
	}
	if err := h.Store.DeleteSrNode(c.Param("nodeId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- Changes ---

func (h *SrClusterHandler) ListChanges(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListSrChangesInTenant(effectiveListTenantID(h.Store, c), c.Query("cluster_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) GetChange(c *gin.Context) {
	ch, err := h.Store.GetSrChange(c.Param("id"))
	if err != nil || ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "变更单不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ch})
}

func (h *SrClusterHandler) StartInit(c *gin.Context) {
	clusterID := c.Param("id")
	id, err := h.eng().StartClusterInit(clusterID, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartReinit(c *gin.Context) {
	clusterID := c.Param("id")
	id, err := h.eng().StartClusterReinit(clusterID, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

// MarkReady POST /sr-cluster/clusters/:id/mark-ready — 将 failed（或已有节点的 draft）标为 ready。
// 默认 FE 探活；body {"force":true} 跳过探活。不等于重新初始化。
func (h *SrClusterHandler) MarkReady(c *gin.Context) {
	cl, err := resolveSrClusterInTenant(h.Store, c, c.Param("id"))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := h.eng().MarkClusterReady(c.Request.Context(), cl.ID, middleware.Username(c), req.Force); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	updated, _ := h.Store.GetSrCluster(cl.ID)
	if updated == nil {
		updated = cl
		updated.Status = store.SrClusterReady
	}
	c.JSON(http.StatusOK, gin.H{"data": updated})
}

func (h *SrClusterHandler) CancelChange(c *gin.Context) {
	if err := h.eng().CancelChange(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SrClusterHandler) RetryChange(c *gin.Context) {
	if err := h.eng().RetryChange(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RollbackUpgradeNode 失败/取消的升级变更中，把某节点 bin/lib 还原为升级前备份并启动。
func (h *SrClusterHandler) RollbackUpgradeNode(c *gin.Context) {
	var req struct {
		NodeID string `json:"node_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.NodeID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "node_id 必填"})
		return
	}
	up, err := h.Store.GetSrChange(c.Param("id"))
	if err != nil || up == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "变更单不存在"})
		return
	}
	id, err := h.eng().StartUpgradeRollback(up.ClusterID, up.ID, strings.TrimSpace(req.NodeID), middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartUpgrade(c *gin.Context) {
	var req struct {
		ReleaseID string `json:"release_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ReleaseID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "release_id 必填"})
		return
	}
	id, err := h.eng().StartUpgrade(c.Param("id"), req.ReleaseID, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) PrecheckUpgrade(c *gin.Context) {
	var req struct {
		ReleaseID string `json:"release_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ReleaseID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "release_id 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	res := h.eng().PrecheckUpgrade(ctx, c.Param("id"), req.ReleaseID)
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// PrecheckAddNode POST /sr-cluster/clusters/:id/nodes/precheck — 添加节点工单提交前的基础校验。
func (h *SrClusterHandler) PrecheckAddNode(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "集群不存在"})
		return
	}
	n, _, err := parseSrAddNodePayload(cl, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	assigned, _ := body["assigned_to"].(string)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	res := h.eng().PrecheckAddNode(ctx, cl.ID, srcluster.AddNodeCheckInput{
		Node: nodeWithGlobalPaths(h.Store, cl, n), HostInstanceID: n.HostInstanceID, AssignedTo: assigned,
	})
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *SrClusterHandler) GetConfigs(c *gin.Context) {
	clusterID := c.Param("id")
	list, err := h.Store.ListSrClusterConfigs(clusterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := gin.H{"fe": "", "be": "", "items": list}
	for _, it := range list {
		out[it.Role] = it.Content
	}
	sources := gin.H{}
	for _, role := range []string{"fe", "be"} {
		eff, src, _ := srcluster.EffectiveConfTemplate(h.Store, clusterID, role, "")
		sources[role] = gin.H{"source": src, "effective": eff}
	}
	out["sources"] = sources
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *SrClusterHandler) PreviewConfigs(c *gin.Context) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	nodes, err := h.Store.ListSrNodes(cl.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	previews, err := srcluster.PreviewClusterConfs(h.Store, cl, nodes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": previews})
}

func (h *SrClusterHandler) PutNodeConfig(c *gin.Context) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	n, err := h.Store.GetSrNode(c.Param("nodeId"))
	if err != nil || n == nil || n.ClusterID != cl.ID {
		c.JSON(http.StatusNotFound, gin.H{"message": "节点不存在"})
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	body := srcluster.StripLockedDataDirKeys(req.Content)
	if err := srcluster.RejectLockedDataDirKeys(body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	who := middleware.Username(c)
	if strings.TrimSpace(body) == "" {
		_ = h.Store.DeleteSrNodeConfig(n.ID)
		c.JSON(http.StatusOK, gin.H{"ok": true, "cleared": true})
		return
	}
	if err := h.Store.UpsertSrNodeConfig(&store.SrNodeConfig{
		NodeID: n.ID, Content: body, UpdatedBy: who,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// 返回刷新后的预览
	path, content, source, err := srcluster.MaterializeConf(h.Store, cl, n)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{
		"node_id": n.ID, "remote_path": path, "content": content,
		"editable": srcluster.StripLockedDataDirKeys(content), "source": source,
	}})
}

func (h *SrClusterHandler) PutConfigs(c *gin.Context) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	var req struct {
		FE *string `json:"fe"`
		BE *string `json:"be"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.FE == nil && req.BE == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供 fe 或 be 正文"})
		return
	}
	who := middleware.Username(c)
	save := func(role string, content *string) error {
		if content == nil {
			return nil
		}
		if err := srcluster.RejectLockedDataDirKeys(*content); err != nil {
			return fmt.Errorf("%s.conf: %w", role, err)
		}
		return h.Store.UpsertSrClusterConfig(&store.SrClusterConfig{
			ClusterID: cl.ID, Role: role, Content: *content, UpdatedBy: who,
		})
	}
	if err := save("fe", req.FE); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := save("be", req.BE); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	list, _ := h.Store.ListSrClusterConfigs(cl.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": list})
}

func (h *SrClusterHandler) StartConfigApply(c *gin.Context) {
	var req struct {
		Roles   []string `json:"roles"`
		NodeIDs []string `json:"node_ids"`
		Restart bool     `json:"restart"`
	}
	_ = c.ShouldBindJSON(&req)
	id, err := h.eng().StartConfigApply(c.Param("id"), middleware.Username(c), srcluster.ConfigApplyOpts{
		Roles: req.Roles, NodeIDs: req.NodeIDs, Restart: req.Restart,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

// clusterNode 取路径 :id / :nodeId 对应的集群与节点；失败时已写响应。
func (h *SrClusterHandler) clusterNode(c *gin.Context) (*store.SrCluster, *store.SrNode, bool) {
	cl, err := h.Store.GetSrClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return nil, nil, false
	}
	n, err := h.Store.GetSrNode(c.Param("nodeId"))
	if err != nil || n == nil || n.ClusterID != cl.ID {
		c.JSON(http.StatusNotFound, gin.H{"message": "节点不存在"})
		return nil, nil, false
	}
	return cl, n, true
}

// GetRemoteConf 读取节点远端当前 fe.conf / be.conf（只读）。
func (h *SrClusterHandler) GetRemoteConf(c *gin.Context) {
	cl, n, ok := h.clusterNode(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	path, resp, err := h.eng().ReadRemoteConf(ctx, cl, n)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"node_id": n.ID, "remote_path": path, "exists": resp.Exists,
		"content": resp.Content, "sha256": resp.SHA256, "mtime": resp.MTime,
	}})
}

// ConfDiff 节点配置对比：base（远端 / 最近下发 / 指定版本）→ target（编辑区正文或已保存草稿渲染结果）。
func (h *SrClusterHandler) ConfDiff(c *gin.Context) {
	cl, n, ok := h.clusterNode(c)
	if !ok {
		return
	}
	var req struct {
		Base    string  `json:"base"` // remote | applied
		Content *string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var target string
	if req.Content != nil {
		full, err := srcluster.RenderEditableConf(h.Store, cl, n, *req.Content)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		target = full
	} else {
		_, full, _, err := srcluster.MaterializeConf(h.Store, cl, n)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		target = full
	}

	var baseLabel, baseContent string
	var drift gin.H
	switch strings.ToLower(strings.TrimSpace(req.Base)) {
	case "", "remote":
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		_, resp, err := h.eng().ReadRemoteConf(ctx, cl, n)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": "读取远端配置失败: " + err.Error()})
			return
		}
		baseContent = resp.Content
		baseLabel = "远端当前文件"
		if !resp.Exists {
			baseLabel = "远端当前文件（不存在）"
		}
		if applied, err := h.Store.LatestSrConfRevision(n.ID, store.SrConfRevApplied); err == nil && applied != nil {
			keys := srcluster.ConfKeyDiff(applied.Content, resp.Content)
			drift = gin.H{
				"detected": len(keys) > 0, "keys": keys,
				"applied_at": applied.CreatedAt, "change_id": applied.ChangeID, "revision_id": applied.ID,
			}
		}
	case "applied":
		rev, err := h.Store.LatestSrConfRevision(n.ID, store.SrConfRevApplied)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if rev == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "该节点尚无平台下发记录"})
			return
		}
		baseContent = rev.Content
		baseLabel = "最近一次下发（" + rev.CreatedAt + "）"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "base 须为 remote 或 applied"})
		return
	}
	lines := srcluster.ConfLineDiff(baseContent, target)
	added, deleted := srcluster.ConfLineStats(lines)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"base_label": baseLabel, "base_content": baseContent, "target_content": target,
		"keys": srcluster.ConfKeyDiff(baseContent, target), "lines": lines,
		"added": added, "deleted": deleted, "drift": drift,
	}})
}

// ConfPatch 同角色批量下发：源节点用编辑区全文，其他勾选节点只应用公共参数改动。dry_run 只返回计划。
func (h *SrClusterHandler) ConfPatch(c *gin.Context) {
	var req struct {
		SourceNodeID string   `json:"source_node_id"`
		Content      string   `json:"content"`
		NodeIDs      []string `json:"node_ids"`
		Restart      bool     `json:"restart"`
		DryRun       bool     `json:"dry_run"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if req.DryRun {
		plan, err := h.eng().PlanConfPatch(ctx, c.Param("id"), req.SourceNodeID, req.Content, req.NodeIDs)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": plan})
		return
	}
	id, err := h.eng().ApplyConfPatch(ctx, c.Param("id"), middleware.Username(c), req.SourceNodeID, req.Content, req.NodeIDs, req.Restart)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

// ConfHistory 节点下发记录：每次下发（config_apply / config_rollback 等）合并为一行「修改前 → 修改后」。
func (h *SrClusterHandler) ConfHistory(c *gin.Context) {
	_, n, ok := h.clusterNode(c)
	if !ok {
		return
	}
	revs, err := h.Store.ListSrConfApplyRevisions(n.ID, store.SrConfRevKeepPerNode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "读取下发记录失败（请确认已执行 tbl_sr_conf_revisions 建表 SQL）: " + err.Error()})
		return
	}
	type entry struct {
		key           string
		before, after *store.SrConfRevision
	}
	var order []*entry
	byKey := map[string]*entry{}
	for i := range revs {
		r := &revs[i]
		k := r.ChangeID
		if k == "" {
			k = "rev:" + r.ID
		}
		en := byKey[k]
		if en == nil {
			en = &entry{key: k}
			byKey[k] = en
			order = append(order, en)
		}
		if r.Kind == store.SrConfRevBeforeApply {
			en.before = r
		} else {
			en.after = r
		}
	}
	changeTypes := map[string]string{}
	out := make([]gin.H, 0, len(order))
	for _, en := range order {
		if en.after == nil {
			continue
		}
		item := gin.H{
			"change_id": en.after.ChangeID, "created_at": en.after.CreatedAt, "created_by": en.after.CreatedBy,
			"after_revision_id": en.after.ID, "before_revision_id": "", "keys": []srcluster.ConfKeyChange{},
			"added": 0, "deleted": 0, "change_type": "",
		}
		if cid := en.after.ChangeID; cid != "" {
			t, ok := changeTypes[cid]
			if !ok {
				if ch, err := h.Store.GetSrChange(cid); err == nil && ch != nil {
					t = ch.Type
				}
				changeTypes[cid] = t
			}
			item["change_type"] = t
		}
		if en.before != nil {
			item["before_revision_id"] = en.before.ID
			item["keys"] = srcluster.ConfKeyDiff(en.before.Content, en.after.Content)
			item["added"], item["deleted"] = srcluster.ConfLineStats(srcluster.ConfLineDiff(en.before.Content, en.after.Content))
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// ConfRevisionDiff GET /clusters/:id/conf-revision-diff?from=&to= 两个版本之间的差异。
func (h *SrClusterHandler) ConfRevisionDiff(c *gin.Context) {
	load := func(id string) (*store.SrConfRevision, bool) {
		rev, err := h.Store.GetSrConfRevision(strings.TrimSpace(id))
		if err != nil || rev == nil || rev.ClusterID != c.Param("id") {
			c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
			return nil, false
		}
		return rev, true
	}
	from, ok := load(c.Query("from"))
	if !ok {
		return
	}
	to, ok := load(c.Query("to"))
	if !ok {
		return
	}
	lines := srcluster.ConfLineDiff(from.Content, to.Content)
	added, deleted := srcluster.ConfLineStats(lines)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"base_label": "修改前（" + from.CreatedAt + "）", "base_content": from.Content, "target_content": to.Content,
		"keys": srcluster.ConfKeyDiff(from.Content, to.Content), "lines": lines,
		"added": added, "deleted": deleted,
	}})
}

func (h *SrClusterHandler) GetConfRevision(c *gin.Context) {
	rev, err := h.Store.GetSrConfRevision(c.Param("rid"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if rev == nil || rev.ClusterID != c.Param("id") {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rev})
}

func (h *SrClusterHandler) StartConfigRollback(c *gin.Context) {
	var req struct {
		NodeID     string `json:"node_id"`
		RevisionID string `json:"revision_id"`
		ChangeID   string `json:"change_id"`
		Restart    bool   `json:"restart"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := h.eng().StartConfigRollback(c.Param("id"), middleware.Username(c), srcluster.ConfigRollbackOpts{
		NodeID: req.NodeID, RevisionID: req.RevisionID, ChangeID: req.ChangeID, Restart: req.Restart,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartAddNode(c *gin.Context) {
	id, err := h.eng().StartAddNode(c.Param("id"), c.Param("nodeId"), middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartRestartNode(c *gin.Context) {
	id, err := h.eng().StartRestartNode(c.Param("id"), c.Param("nodeId"), middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartNodeStart(c *gin.Context) {
	id, err := h.eng().StartNodeStart(c.Param("id"), c.Param("nodeId"), middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartNodeStop(c *gin.Context) {
	id, err := h.eng().StartNodeStop(c.Param("id"), c.Param("nodeId"), middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *SrClusterHandler) StartRemoveNode(c *gin.Context) {
	force := c.Query("force_drop") == "1" || c.Query("force_drop") == "true"
	id, err := h.eng().StartRemoveNode(c.Param("id"), c.Param("nodeId"), middleware.Username(c), force)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

// --- OSS / Releases / Policies ---

func (h *SrClusterHandler) GetLayoutDefaults(c *gin.Context) {
	if h == nil || h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "存储未初始化"})
		return
	}
	h.Store.EnsureSrLayoutDefaults()
	c.JSON(http.StatusOK, gin.H{"data": h.Store.SrLayoutDefaults()})
}

func (h *SrClusterHandler) PutLayoutDefaults(c *gin.Context) {
	if h == nil || h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "存储未初始化"})
		return
	}
	var req store.SrLayoutDefaults
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.SetSrLayoutDefaults(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.Store.SrLayoutDefaults()})
}

func (h *SrClusterHandler) GetOSS(c *gin.Context) {
	s, err := h.Store.GetSrOSSSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	options, _ := h.Store.ListOSSSettings("")
	c.JSON(http.StatusOK, gin.H{"data": s, "options": options})
}

func (h *SrClusterHandler) PutOSS(c *gin.Context) {
	var req struct {
		OSSSettingID string `json:"oss_setting_id"`
		Prefix       string `json:"prefix"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.OSSSettingID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择配置中心中的 OSS"})
		return
	}
	oss, err := h.Store.GetOSSSetting(req.OSSSettingID)
	if err != nil || oss == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "所选 OSS 不存在，请到「配置中心 → OSS 配置」维护"})
		return
	}
	s := &store.SrOSSSettings{
		OSSSettingID: req.OSSSettingID,
		Prefix:       req.Prefix,
		UpdatedBy:    middleware.Username(c),
	}
	if err := h.Store.UpsertSrOSSSettings(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetSrOSSSettings()
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *SrClusterHandler) TestOSS(c *gin.Context) {
	ok, msg := h.eng().TestOSS(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"ok": ok, "message": msg})
}

func (h *SrClusterHandler) ListReleases(c *gin.Context) {
	only := c.Query("enabled") == "1"
	list, err := h.Store.ListSrReleases(only)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) CreateRelease(c *gin.Context) {
	var r store.SrRelease
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(r.Version) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "version 必填"})
		return
	}
	lo, hi, err := srcluster.NormalizeReleaseJDK(r.Version, r.JDKMin, r.JDKMax)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	r.JDKMin, r.JDKMax = lo, hi
	r.Enabled = true
	r.CreatedBy = middleware.Username(c)
	r.UpdatedBy = r.CreatedBy
	if err := h.Store.CreateSrRelease(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *SrClusterHandler) UpdateRelease(c *gin.Context) {
	exist, err := h.Store.GetSrRelease(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var req struct {
		store.SrRelease
		JDKMax *string `json:"jdk_max"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.Version = coalesce(req.Version, exist.Version)
	exist.Channel = coalesce(req.Channel, exist.Channel)
	exist.JDKMin = coalesce(req.JDKMin, exist.JDKMin)
	if req.JDKMax != nil {
		exist.JDKMax = strings.TrimSpace(*req.JDKMax)
	}
	lo, hi, err := srcluster.NormalizeReleaseJDK(exist.Version, exist.JDKMin, exist.JDKMax)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.JDKMin, exist.JDKMax = lo, hi
	exist.Remark = req.Remark
	exist.Enabled = req.Enabled
	exist.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpdateSrRelease(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (h *SrClusterHandler) DeleteRelease(c *gin.Context) {
	if err := h.Store.DeleteSrRelease(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SrClusterHandler) UpsertArtifact(c *gin.Context) {
	var a store.SrReleaseArtifact
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	a.ReleaseID = c.Param("id")
	a.Role = strings.ToLower(strings.TrimSpace(a.Role))
	if a.Role != "fe" && a.Role != "be" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "role 须为 fe 或 be"})
		return
	}
	// 脚本上传后再补 sha256：与已有分包合并，避免空字段冲掉 object_key/大小
	if exist, _ := h.Store.GetSrReleaseArtifact(a.ReleaseID, a.Role); exist != nil {
		a.ID = exist.ID
		if strings.TrimSpace(a.ObjectKey) == "" {
			a.ObjectKey = exist.ObjectKey
		}
		if strings.TrimSpace(a.Filename) == "" {
			a.Filename = exist.Filename
		}
		if a.SizeBytes <= 0 {
			a.SizeBytes = exist.SizeBytes
		}
		if strings.TrimSpace(a.Format) == "" {
			a.Format = exist.Format
		}
		if strings.TrimSpace(a.SHA256) == "" {
			a.SHA256 = exist.SHA256
		}
		a.CreatedAt = exist.CreatedAt
	}
	oss, _ := h.Store.GetSrOSSSettings()
	rel, _ := h.Store.GetSrRelease(a.ReleaseID)
	if rel != nil && strings.TrimSpace(a.ObjectKey) == "" && strings.TrimSpace(a.Filename) != "" {
		prefix := "starrocks/"
		if oss != nil && oss.Prefix != "" {
			prefix = oss.Prefix
		}
		a.ObjectKey = srcluster.BuildObjectKey(prefix, rel.Version, a.Role, a.Filename)
	}
	if err := h.Store.UpsertSrReleaseArtifact(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *SrClusterHandler) UploadArtifact(c *gin.Context) {
	role := strings.ToLower(c.PostForm("role"))
	if role != "fe" && role != "be" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "role=fe|be"})
		return
	}
	rel, err := h.Store.GetSrRelease(c.Param("id"))
	if err != nil || rel == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "file 必填"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer f.Close()
	oss, _ := h.Store.GetSrOSSSettings()
	prefix := "starrocks/"
	if oss != nil && oss.Prefix != "" {
		prefix = oss.Prefix
	}
	key := srcluster.BuildObjectKey(prefix, rel.Version, role, fh.Filename)
	// 边读边算 sha256 需缓冲到临时文件
	tmp, err := os.CreateTemp("", "sr-upload-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	sum := sha256.New()
	w := io.MultiWriter(tmp, sum)
	n, err := io.Copy(w, f)
	_ = tmp.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	rf, err := os.Open(tmpPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rf.Close()
	if err := h.eng().UploadObject(c.Request.Context(), key, rf, n, "application/gzip"); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	art := &store.SrReleaseArtifact{
		ReleaseID: rel.ID, Role: role, ObjectKey: key, Filename: fh.Filename,
		SHA256: hex.EncodeToString(sum.Sum(nil)), SizeBytes: n, Format: "tar.gz",
	}
	if err := h.Store.UpsertSrReleaseArtifact(art); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": art})
}

func (h *SrClusterHandler) SyncArtifactsFromOSS(c *gin.Context) {
	rel, err := h.Store.GetSrRelease(c.Param("id"))
	if err != nil || rel == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	res, err := h.eng().SyncArtifactsFromOSS(c.Request.Context(), rel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *SrClusterHandler) ListEnvPolicies(c *gin.Context) {
	list, err := h.Store.ListSrEnvVersionPolicies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) UpsertEnvPolicy(c *gin.Context) {
	var p store.SrEnvVersionPolicy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(p.Environment) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "environment 必填"})
		return
	}
	p.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpsertSrEnvVersionPolicy(&p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": p})
}

// --- Conf templates & cluster configs ---

func (h *SrClusterHandler) ListConfTemplates(c *gin.Context) {
	_ = srcluster.EnsureDefaultConfTemplates(h.Store)
	list, err := h.Store.ListSrConfTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SrClusterHandler) UpsertConfTemplate(c *gin.Context) {
	var t store.SrConfTemplate
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := srcluster.RejectLockedDataDirKeys(t.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	t.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpsertSrConfTemplate(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *SrClusterHandler) ResetConfTemplates(c *gin.Context) {
	if err := srcluster.ResetConfTemplatesToBuiltin(h.Store, middleware.Username(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	list, _ := h.Store.ListSrConfTemplates()
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": list})
}
