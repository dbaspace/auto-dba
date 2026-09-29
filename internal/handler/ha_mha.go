package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/backup"
	"auto-dba/internal/ha"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

func (h *HAHandler) controllerAuth(ctrl *store.HAController) (ha.ControllerAuth, error) {
	pass := ""
	if ctrl != nil && strings.TrimSpace(ctrl.PasswordEnc) != "" {
		p, err := secret.Decrypt(h.Secret, ctrl.PasswordEnc)
		if err != nil {
			return ha.ControllerAuth{}, fmt.Errorf("解密控制器密码失败: %w", err)
		}
		pass = p
	}
	appConf := strings.TrimSpace(ctrl.Remark)
	if strings.EqualFold(ctrl.Kind, "mha") {
		appConf = ha.ParseMHAAppConf(ctrl.Remark)
	}
	return ha.ControllerAuth{
		Kind: ctrl.Kind, BaseURL: ctrl.BaseURL, Username: ctrl.Username, Password: pass,
		AppConf: appConf,
	}, nil
}


// ListCandidateSources 成员候选：已登记实例 + 集群部署 ready 的 MySQL 主从节点。
func (h *HAHandler) ListCandidateSources(c *gin.Context) {
	engine := strings.ToLower(strings.TrimSpace(c.Query("engine")))
	if engine == "" {
		engine = "mysql"
	}
	dbType := engine
	if engine == "postgres" {
		dbType = "postgres"
	}

	insts, err := h.Store.ListInstances(dbType, c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var instances []gin.H
	for _, it := range insts {
		instances = append(instances, gin.H{
			"source":      "instance",
			"instance_id": it.ID,
			"name":        it.Name,
			"host":        it.Host,
			"port":        it.Port,
			"db_type":     it.DBType,
			"env_id":      it.EnvID,
			"label":       fmt.Sprintf("[实例] %s (%s:%d)", it.Name, it.Host, it.Port),
		})
	}

	var deployNodes []gin.H
	if engine == "mysql" {
		clusters, err := h.Store.ListDbDeployClusters("mysql")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		for _, cl := range clusters {
			st := strings.ToLower(cl.Status)
			if st != store.DbDeployStatusReady && st != store.DbDeployStatusHaPending {
				continue
			}
			topo := strings.ToLower(cl.Topology)
			if topo == "standalone" || topo == "cloud_endpoint" {
				continue
			}
			nodes, err := h.Store.ListDbDeployNodes(cl.ID)
			if err != nil {
				continue
			}
			for _, n := range nodes {
				role := strings.ToLower(n.Role)
				if role != "primary" && role != "replica" && role != "single" {
					continue
				}
				haRole := "replica"
				if role == "primary" || role == "single" {
					haRole = "primary"
				}
				port := n.Port
				if port <= 0 {
					port = 3306
				}
				deployNodes = append(deployNodes, gin.H{
					"source":           "dbdeploy",
					"dbdeploy_cluster": cl.ID,
					"dbdeploy_node":    n.ID,
					"cluster_name":     cl.Name,
					"name":             fmt.Sprintf("%s/%s", cl.Name, n.Host),
					"host":             n.Host,
					"port":             port,
					"role_hint":        haRole,
					"status":           n.Status,
					"label":            fmt.Sprintf("[部署·%s] %s %s (%s:%d)", cl.Name, haRole, n.Status, n.Host, port),
				})
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"instances":    instances,
		"deploy_nodes": deployNodes,
	}})
}

// ImportDbDeployMembers 将 db-deploy 集群节点登记为实例并返回 HA 成员草案。
func (h *HAHandler) ImportDbDeployMembers(c *gin.Context) {
	var req struct {
		DbDeployClusterID string `json:"dbdeploy_cluster_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DbDeployClusterID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "dbdeploy_cluster_id 必填"})
		return
	}
	cl, err := h.Store.GetDbDeployCluster(req.DbDeployClusterID)
	if err != nil || cl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "部署集群不存在"})
		return
	}
	if !strings.EqualFold(cl.Engine, "mysql") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "本期仅支持 MySQL 部署集群导入 MHA"})
		return
	}
	st := strings.ToLower(cl.Status)
	if st != store.DbDeployStatusReady && st != store.DbDeployStatusHaPending {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 ready / ha_pending 的部署集群可导入"})
		return
	}
	nodes, err := h.Store.ListDbDeployNodes(cl.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var members []gin.H
	primaries, replicas := 0, 0
	for _, n := range nodes {
		role := strings.ToLower(n.Role)
		haRole := ""
		switch role {
		case "primary", "single":
			haRole = "primary"
			primaries++
		case "replica":
			haRole = "replica"
			replicas++
		default:
			continue
		}
		port := n.Port
		if port <= 0 {
			port = 3306
		}
		name := fmt.Sprintf("%s-%s", cl.Name, n.Host)
		inst, err := h.Store.EnsureInstanceFromHost("mysql", name, n.Host, port, "")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "登记实例失败: " + err.Error()})
			return
		}
		members = append(members, gin.H{
			"instance_id": inst.ID,
			"role":        haRole,
			"priority":    0,
			"enabled":     true,
			"host":        inst.Host,
			"port":        inst.Port,
			"name":        inst.Name,
		})
	}
	if primaries != 1 || replicas < 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": fmt.Sprintf("导入后须满足 1 主 ≥1 从，当前 primary=%d replica=%d", primaries, replicas),
			"data":    members,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"cluster_name": cl.Name,
		"controller_id": cl.ControllerID,
		"members":      members,
		"dbdeploy_id":  cl.ID,
	}})
}

// GetMHAConf 读取已保存 conf，若无则按成员生成预览。
func (h *HAHandler) GetMHAConf(c *gin.Context) {
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	if !strings.EqualFold(cluster.Engine, "mysql") || !strings.EqualFold(cluster.Mode, "mha") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 MySQL · MHA 模式支持配置管理"})
		return
	}
	saved, _ := h.Store.GetHAMHAConf(cluster.ID)
	opts := ha.DefaultMHAConfOptions(cluster.Name)
	if saved != nil && strings.TrimSpace(saved.OptionsJSON) != "" {
		_ = json.Unmarshal([]byte(saved.OptionsJSON), &opts)
	}
	content := ""
	path := opts.ConfPath
	if saved != nil {
		content = saved.Content
		if saved.ConfPath != "" {
			path = saved.ConfPath
		}
	}
	generated, genErr := ha.RenderMHAAppConf(cluster, opts)
	if content == "" {
		content = generated
	}
	genErrMsg := ""
	if genErr != nil {
		genErrMsg = genErr.Error()
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"cluster_id": cluster.ID,
		"conf_path":  path,
		"content":    content,
		"options":    opts,
		"generated":  generated,
		"saved":      saved != nil && strings.TrimSpace(saved.Content) != "",
		"gen_error":  genErrMsg,
	}})
}

// PreviewMHAConf 按选项重新生成模板。
func (h *HAHandler) PreviewMHAConf(c *gin.Context) {
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	if !strings.EqualFold(cluster.Engine, "mysql") || !strings.EqualFold(cluster.Mode, "mha") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 MySQL · MHA 模式支持配置管理"})
		return
	}
	var opts ha.MHAConfOptions
	if err := c.ShouldBindJSON(&opts); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	content, err := ha.RenderMHAAppConf(cluster, opts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	def := ha.DefaultMHAConfOptions(cluster.Name)
	path := opts.ConfPath
	if path == "" {
		path = def.ConfPath
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"conf_path": path,
		"content":   content,
		"options":   opts,
	}})
}

// SaveMHAConf 保存平台侧草稿。
func (h *HAHandler) SaveMHAConf(c *gin.Context) {
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	if !strings.EqualFold(cluster.Engine, "mysql") || !strings.EqualFold(cluster.Mode, "mha") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 MySQL · MHA 模式支持配置管理"})
		return
	}
	var req struct {
		ConfPath string            `json:"conf_path"`
		Content  string            `json:"content"`
		Options  ha.MHAConfOptions `json:"options"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "content 不能为空"})
		return
	}
	path := strings.TrimSpace(req.ConfPath)
	if path == "" {
		path = ha.DefaultMHAConfOptions(cluster.Name).ConfPath
	}
	optJSON, _ := json.Marshal(req.Options)
	it := &store.HAMHAConf{
		ClusterID:   cluster.ID,
		ConfPath:    path,
		Content:     req.Content,
		OptionsJSON: string(optJSON),
		UpdatedBy:   middleware.Username(c),
	}
	if err := h.Store.UpsertHAMHAConf(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// 同步控制器 remark 中的路径，便于切换/预检读取同一 conf
	if strings.TrimSpace(cluster.ControllerID) != "" {
		ctrl, _ := h.Store.GetHAController(cluster.ControllerID)
		if ctrl != nil && strings.EqualFold(ctrl.Kind, "mha") {
			ctrl.Remark = path
			ctrl.UpdatedBy = middleware.Username(c)
			_ = h.Store.UpdateHAController(ctrl)
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

// PushMHAConf 下发到 Manager Agent，可选跑 check_repl。
func (h *HAHandler) PushMHAConf(c *gin.Context) {
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	if !strings.EqualFold(cluster.Engine, "mysql") || !strings.EqualFold(cluster.Mode, "mha") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 MySQL · MHA 模式支持配置下发"})
		return
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先绑定 MHA 控制器"})
		return
	}
	var req struct {
		Content    string `json:"content"`
		ConfPath   string `json:"conf_path"`
		VerifyRepl bool   `json:"verify_repl"`
	}
	_ = c.ShouldBindJSON(&req)
	saved, _ := h.Store.GetHAMHAConf(cluster.ID)
	content := strings.TrimSpace(req.Content)
	path := strings.TrimSpace(req.ConfPath)
	if content == "" && saved != nil {
		content = saved.Content
	}
	if path == "" && saved != nil {
		path = saved.ConfPath
	}
	if content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无配置内容，请先生成并保存"})
		return
	}
	if path == "" {
		path = ha.DefaultMHAConfOptions(cluster.Name).ConfPath
	}
	// 先落库
	optJSON := ""
	if saved != nil {
		optJSON = saved.OptionsJSON
	}
	_ = h.Store.UpsertHAMHAConf(&store.HAMHAConf{
		ClusterID: cluster.ID, ConfPath: path, Content: content, OptionsJSON: optJSON,
		UpdatedBy: middleware.Username(c),
	})

	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "控制器不存在"})
		return
	}
	auth, err := h.controllerAuth(ctrl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	if err := ha.MHAAgentPutConf(ctx, auth, path, content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctrl.Remark = path
	ctrl.UpdatedBy = middleware.Username(c)
	_ = h.Store.UpdateHAController(ctrl)

	out := gin.H{"ok": true, "conf_path": path, "pushed": true}
	if req.VerifyRepl {
		ok, detail := ha.MHAAgentReplOK(ctx, auth)
		out["repl_ok"] = ok
		out["repl_detail"] = detail
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// EnvCheckController 对 MHA / Orchestrator / ProxySQL / pg_autofailover / Patroni / repmgr 做环境检查。
func (h *HAHandler) EnvCheckController(c *gin.Context) {
	ctrl, err := h.Store.GetHAController(c.Param("id"))
	if err != nil || ctrl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "控制器不存在"})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(ctrl.Kind))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": aerr.Error()})
		return
	}

	switch kind {
	case "patroni":
		ok, summary, checks := ha.PatroniEnvCheck(ctx, auth)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": ok, "summary": summary, "checks": checks}})
		return
	case "orchestrator":
		hintHost, hintPort := "", 0
		// 可选：body 带 host/port
		var body struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		}
		_ = c.ShouldBindJSON(&body)
		hintHost, hintPort = strings.TrimSpace(body.Host), body.Port
		ok, summary, checks := ha.OrchestratorEnvCheck(ctx, auth, hintHost, hintPort)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": ok, "summary": summary, "checks": checks}})
		return
	case "proxysql":
		ok, summary, checks := ha.ProxySQLEnvCheck(ctx, auth)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": ok, "summary": summary, "checks": checks}})
		return
	case "repmgr":
		ok, summary, checks := ha.RepmgrEnvCheck(ctx, auth)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": ok, "summary": summary, "checks": checks}})
		return
	case "mha", "pg_autofailover":
		pass := ""
		if strings.TrimSpace(ctrl.PasswordEnc) != "" {
			if p, e := secret.Decrypt(h.Secret, ctrl.PasswordEnc); e == nil {
				pass = p
			}
		}
		conf := strings.TrimSpace(ctrl.Remark)
		if kind == "mha" {
			conf = ha.ParseMHAAppConf(ctrl.Remark)
		}
		var body struct {
			Conf string `json:"conf"`
		}
		_ = c.ShouldBindJSON(&body)
		if strings.TrimSpace(body.Conf) != "" {
			conf = strings.TrimSpace(body.Conf)
		}
		agentAuth := backup.AgentAuth{BaseURL: ctrl.BaseURL, Username: ctrl.Username, Password: pass}
		res, err := backup.CallHAEnvCheck(ctx, agentAuth, kind, conf)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": res})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境检查支持 mha / orchestrator / proxysql / pg_autofailover / patroni / repmgr"})
	}
}
