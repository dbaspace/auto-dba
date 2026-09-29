package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type DbDeployHandler struct {
	Store  *store.DB
	Secret string
	Engine *dbdeploy.Engine
	Service *dbdeploy.Service
}

func (h *DbDeployHandler) eng() *dbdeploy.Engine {
	if h.Engine != nil {
		return h.Engine
	}
	h.Engine = dbdeploy.New(h.Store, h.Secret, dbdeploy.Config{})
	return h.Engine
}

func (h *DbDeployHandler) svc() *dbdeploy.Service {
	if h.Service != nil {
		return h.Service
	}
	h.Service = dbdeploy.NewService(h.Store, h.Secret)
	return h.Service
}

func actorOfDb(c *gin.Context) string {
	if v, ok := c.Get(middleware.CtxUsername); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

func (h *DbDeployHandler) Meta(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": dbdeploy.Meta(c.Query("engine"))})
}

// ListEngines GET /db-deploy/engines — Hub 卡片与能力分级。
func (h *DbDeployHandler) ListEngines(c *gin.Context) {
	data := dbdeploy.ListEnginesForAPI()
	out := gin.H{"data": data, "agents": dbdeploy.AgentsOverview()}
	if raw, err := h.Store.GetGlobalArg("ticket_instance_types"); err == nil {
		warns := dbdeploy.DiffAgainstInstanceTypes(dbdeploy.ParseTicketInstanceTypeValues(raw))
		if len(warns) > 0 {
			out["warnings"] = warns
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *DbDeployHandler) ListReleases(c *gin.Context) {
	list, err := h.Store.ListDbDeployReleases(c.Query("engine"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DbDeployHandler) UpsertRelease(c *gin.Context) {
	var r store.DbDeployRelease
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if id := c.Param("id"); id != "" {
		r.ID = id
	}
	r.UpdatedBy = actorOfDb(c)
	if r.CreatedBy == "" {
		r.CreatedBy = r.UpdatedBy
	}
	if !r.Enabled && c.Request.Method == http.MethodPost {
		r.Enabled = true
	}
	if err := h.Store.UpsertDbDeployRelease(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *DbDeployHandler) DeleteRelease(c *gin.Context) {
	if err := h.Store.DeleteDbDeployRelease(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DbDeployHandler) PresignRelease(c *gin.Context) {
	urlStr, key, sum, err := h.svc().PresignRelease(c.Query("engine"), c.Param("id"), 2*time.Hour)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"download_url": urlStr,
		"object_key":   key,
		"checksum":     sum,
		"expires_in":   7200,
	}})
}

func (h *DbDeployHandler) ListClusters(c *gin.Context) {
	list, err := h.Store.ListDbDeployClusters(c.Query("engine"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DbDeployHandler) GetCluster(c *gin.Context) {
	cl, err := h.Store.GetDbDeployCluster(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	nodes, _ := h.Store.ListDbDeployNodes(cl.ID)
	c.JSON(http.StatusOK, gin.H{"data": cl, "nodes": nodes})
}

func (h *DbDeployHandler) UpsertCluster(c *gin.Context) {
	var req struct {
		store.DbDeployCluster
		RegisterExisting bool `json:"register_existing"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cl := req.DbDeployCluster
	if id := c.Param("id"); id != "" {
		cl.ID = id
	}
	cl.UpdatedBy = actorOfDb(c)
	if cl.CreatedBy == "" {
		cl.CreatedBy = cl.UpdatedBy
	}
	if req.RegisterExisting {
		if cl.HaEnabled {
			cl.Status = store.DbDeployStatusHaPending
		} else {
			cl.Status = store.DbDeployStatusReady
		}
		if strings.TrimSpace(cl.Remark) == "" {
			cl.Remark = "登记已有集群"
		} else if !strings.Contains(cl.Remark, "登记已有集群") {
			cl.Remark = cl.Remark + "；登记已有集群"
		}
	}
	if engDesc := dbdeploy.GetEngine(cl.Engine); engDesc != nil && engDesc.Workbench == dbdeploy.WorkbenchDbDeploy && !engDesc.AllowDeploy {
		req.RegisterExisting = true
		if cl.HaEnabled {
			cl.Status = store.DbDeployStatusHaPending
		} else {
			cl.Status = store.DbDeployStatusReady
		}
		if strings.TrimSpace(cl.Remark) == "" {
			cl.Remark = "登记已有集群"
		} else if !strings.Contains(cl.Remark, "登记已有集群") {
			cl.Remark = cl.Remark + "；登记已有集群"
		}
	}
	// 云拓扑规范化
	if cl.Topology == store.DbDeployTopoCloudEndpoint || cl.DeployKind == store.DbDeployKindCloud {
		cl.DeployKind = store.DbDeployKindCloud
		cl.Topology = store.DbDeployTopoCloudEndpoint
		if cl.Engine == store.DbDeployEngineCodis {
			c.JSON(http.StatusBadRequest, gin.H{"message": "云托管不支持 Codis，请使用 Redis 单入口"})
			return
		}
		if strings.TrimSpace(cl.Engine) == "" {
			cl.Engine = store.DbDeployEngineRedis
		}
		if req.RegisterExisting {
			if cl.HaEnabled {
				cl.Status = store.DbDeployStatusHaPending
			} else {
				cl.Status = store.DbDeployStatusReady
			}
		}
	}
	if err := dbdeploy.ValidateClusterAgainstRegistry(cl.Engine, cl.DeployKind, cl.Topology, cl.HaEnabled); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpsertDbDeployCluster(&cl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	// 云：确保有唯一 endpoint 节点
	if cl.DeployKind == store.DbDeployKindCloud {
		nodes, _ := h.Store.ListDbDeployNodes(cl.ID)
		var ep *store.DbDeployNode
		for i := range nodes {
			if nodes[i].Role == "endpoint" {
				ep = &nodes[i]
				break
			}
		}
		if ep == nil {
			ep = &store.DbDeployNode{
				ClusterID: cl.ID,
				Role:      "endpoint",
				Host:      cl.EndpointHost,
				Port:      cl.EndpointPort,
				Status:    store.DbNodeReady,
				Remark:    "云托管访问入口",
			}
		} else {
			ep.Host = cl.EndpointHost
			ep.Port = cl.EndpointPort
			ep.Status = store.DbNodeReady
		}
		_ = h.Store.UpsertDbDeployNode(ep)
	}
	c.JSON(http.StatusOK, gin.H{"data": cl})
}

func (h *DbDeployHandler) DeleteCluster(c *gin.Context) {
	if err := h.Store.DeleteDbDeployCluster(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DbDeployHandler) ListNodes(c *gin.Context) {
	list, err := h.Store.ListDbDeployNodes(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DbDeployHandler) UpsertNode(c *gin.Context) {
	var n store.DbDeployNode
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	n.ClusterID = c.Param("id")
	if err := h.Store.UpsertDbDeployNode(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": n})
}

func (h *DbDeployHandler) DeleteNode(c *gin.Context) {
	if err := h.Store.DeleteDbDeployNode(c.Param("nodeId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DbDeployHandler) ListAgents(c *gin.Context) {
	list, err := h.Store.ListDbDeployAgents()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DbDeployHandler) UpsertAgent(c *gin.Context) {
	var req struct {
		ID             string `json:"id"`
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
	a := &store.DbDeployAgent{
		ID: req.ID, Name: req.Name, BaseURL: req.BaseURL, Username: req.Username,
		HostInstID: req.HostInstanceID, Remark: req.Remark,
		CreatedBy: actorOfDb(c), UpdatedBy: actorOfDb(c),
	}
	if strings.TrimSpace(req.Password) != "" {
		enc, err := secret.Encrypt(h.Secret, req.Password)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		a.PasswordEnc = enc
	}
	if err := h.Store.UpsertDbDeployAgent(a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	rebound, _ := h.Store.RebindDbDeployNodesAgent(a.HostInstID, agentAddrs(h.Store, a.HostInstID, a.BaseURL), a.ID)
	a.PasswordEnc = ""
	a.PasswordSet = true
	c.JSON(http.StatusOK, gin.H{"data": a, "rebound_nodes": rebound})
}

func (h *DbDeployHandler) DeleteAgent(c *gin.Context) {
	nodes, err := h.Store.ListDbDeployNodesByAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.DeleteDbDeployAgent(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "affected_nodes": dbNodeRefs(newClusterNames(h.Store), nodes)})
}

// DeployAgent POST /db-deploy/agents/deploy — SSH 下发 db-agent（默认 :8092）。
func (h *DbDeployHandler) DeployAgent(c *gin.Context) {
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
	id, err := h.eng().StartAgentDeploy(dbdeploy.DeployOpts{
		HostInstanceID: req.HostInstanceID,
		ListenPort:     req.ListenPort,
		RemoteDir:      req.RemoteDir,
		Escalate:       req.Escalate,
		Operator:       actorOfDb(c),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deploy_id": id}})
}

func (h *DbDeployHandler) ListAgentDeploys(c *gin.Context) {
	list, err := h.Store.ListDbDeployAgentDeploys(c.Query("host_instance_id"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DbDeployHandler) GetAgentDeploy(c *gin.Context) {
	it, err := h.Store.GetDbDeployAgentDeploy(c.Param("id"))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "下发记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DbDeployHandler) GetOSS(c *gin.Context) {
	eng := c.Query("engine")
	if eng == "" {
		eng = c.Param("engine")
	}
	b, err := h.Store.GetDbDeployOSSBind(eng)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *DbDeployHandler) PutOSS(c *gin.Context) {
	var b store.DbDeployOSSBind
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if b.Engine == "" {
		b.Engine = c.Query("engine")
	}
	b.UpdatedBy = actorOfDb(c)
	if err := h.Store.UpsertDbDeployOSSBind(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetDbDeployOSSBind(b.Engine)
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// StartDeploy 创建变更单并异步编排装机（制品/配置/systemd/复制验收）。
func (h *DbDeployHandler) StartDeploy(c *gin.Context) {
	changeID, err := h.eng().StartDeploy(c.Param("id"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cl, _ := h.Store.GetDbDeployCluster(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{"cluster": cl, "change_id": changeID},
		"message": "已提交部署变更",
	})
}

// StartHaInit 对 ha_pending 集群重试 HA 接入。
func (h *DbDeployHandler) StartHaInit(c *gin.Context) {
	changeID, err := h.eng().StartHaInit(c.Param("id"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cl, _ := h.Store.GetDbDeployCluster(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{
		"data":    gin.H{"cluster": cl, "change_id": changeID},
		"message": "已提交 HA 接入变更",
	})
}

// HAPreflight 创建/部署前 HA 门禁预检。
func (h *DbDeployHandler) HAPreflight(c *gin.Context) {
	cl, err := h.Store.GetDbDeployCluster(c.Param("id"))
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	nodes, _ := h.Store.ListDbDeployNodes(cl.ID)
	out := gin.H{
		"ha_enabled": cl.HaEnabled,
		"ha_mode":    cl.HaMode,
		"controller_id": cl.ControllerID,
		"topology":   cl.Topology,
		"status":     cl.Status,
	}
	if err := dbdeploy.ValidateDeployHA(h.Store, cl, nodes); err != nil {
		out["ok"] = false
		out["message"] = err.Error()
		c.JSON(http.StatusOK, gin.H{"data": out})
		return
	}
	out["ok"] = true
	out["message"] = "门禁通过"
	if warns := dbdeploy.CollectHAVIPWarnings(cl); len(warns) > 0 {
		out["vip_warnings"] = warns
	}
	out["vip_audit"] = dbdeploy.FormatHAVIPAuditLine(cl)
	if strings.EqualFold(cl.HaMode, "mha") && strings.TrimSpace(cl.ControllerID) != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
		defer cancel()
		res, err := dbdeploy.EnvCheckMHA(ctx, h.Store, h.Secret, cl.ControllerID)
		if err != nil {
			out["ok"] = false
			out["message"] = "门禁通过但 MHA 环境检查失败: " + err.Error()
			out["env_check_error"] = err.Error()
		} else {
			out["env_check"] = res
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *DbDeployHandler) ListChanges(c *gin.Context) {
	limit := 50
	list, err := h.Store.ListDbDeployChanges(c.Query("cluster_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DbDeployHandler) GetChange(c *gin.Context) {
	ch, err := h.Store.GetDbDeployChange(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "变更不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ch})
}

func (h *DbDeployHandler) RetryChange(c *gin.Context) {
	if err := h.eng().RetryChange(c.Param("id"), actorOfDb(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DbDeployHandler) CancelChange(c *gin.Context) {
	if err := h.eng().CancelChange(c.Param("id"), actorOfDb(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DbDeployHandler) StartNode(c *gin.Context) {
	id, err := h.eng().StartNodeStart(c.Param("id"), c.Param("nodeId"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *DbDeployHandler) StopNode(c *gin.Context) {
	id, err := h.eng().StartNodeStop(c.Param("id"), c.Param("nodeId"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *DbDeployHandler) RestartNode(c *gin.Context) {
	id, err := h.eng().StartNodeRestart(c.Param("id"), c.Param("nodeId"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *DbDeployHandler) AddNode(c *gin.Context) {
	id, err := h.eng().StartAddNode(c.Param("id"), c.Param("nodeId"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}

func (h *DbDeployHandler) RemoveNode(c *gin.Context) {
	id, err := h.eng().StartRemoveNode(c.Param("id"), c.Param("nodeId"), actorOfDb(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"change_id": id}})
}
