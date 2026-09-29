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

	"auto-dba/internal/backup"
	"auto-dba/internal/ha"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

type HAHandler struct {
	Store  *store.DB
	Secret string
}

func (h *HAHandler) passwordFn() ha.PasswordFn {
	return func(inst *store.Instance) (string, error) {
		if inst == nil {
			return "", fmt.Errorf("实例不存在")
		}
		if strings.TrimSpace(inst.Password) == "" {
			return "", nil
		}
		return secret.Decrypt(h.Secret, inst.Password)
	}
}

func (h *HAHandler) ensureLoader() {
	ha.SetInstanceLoader(func(instanceID string) (*store.Instance, error) {
		return h.Store.GetInstance(instanceID)
	})
}

func (h *HAHandler) Meta(c *gin.Context) {
	engines := []string{"mysql", "postgres", "redis"}
	modes := map[string][]store.HAModeOption{}
	defaults := map[string]string{}
	for _, e := range engines {
		modes[e] = store.HAModesForEngine(e)
		defaults[e] = store.DefaultHAMode(e)
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"modes":            modes,
		"defaults":         defaults,
		"controller_kinds": store.HAControllerKinds(),
	}})
}

func (h *HAHandler) bindController(engine, mode, controllerID string) (string, error) {
	cid := strings.TrimSpace(controllerID)
	if cid == "" {
		if store.HAAutoSwitchoverMode(mode) {
			return "", fmt.Errorf("模式 %s 必须绑定兼容的 HA 控制器", mode)
		}
		return "", nil
	}
	ctrl, err := h.Store.GetHAController(cid)
	if err != nil {
		return "", err
	}
	if ctrl == nil {
		return "", fmt.Errorf("控制器不存在")
	}
	if strings.ToLower(ctrl.Engine) != strings.ToLower(engine) {
		return "", fmt.Errorf("控制器引擎 %s 与集群 %s 不一致", ctrl.Engine, engine)
	}
	if !store.CompatibleHAController(engine, mode, ctrl.Kind) {
		return "", fmt.Errorf("控制器类型 %s 与模式 %s 不匹配", ctrl.Kind, mode)
	}
	return ctrl.ID, nil
}

func (h *HAHandler) bindStandbyControllers(engine, mode, primaryID string, ids []string) ([]string, error) {
	out := store.NormalizeStandbyControllerIDs(primaryID, ids)
	for _, id := range out {
		if _, err := h.bindController(engine, mode, id); err != nil {
			return nil, fmt.Errorf("备用控制器 %s: %w", id, err)
		}
	}
	return out, nil
}

func (h *HAHandler) ListClusters(c *gin.Context) {
	list, err := h.Store.ListHAClusters(c.Query("engine"), c.Query("env_id"), c.Query("keyword"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) GetCluster(c *gin.Context) {
	it, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

type haMemberReq struct {
	InstanceID string `json:"instance_id"`
	Role       string `json:"role"`
	Priority   int    `json:"priority"`
	Enabled    *bool  `json:"enabled"`
}

func (h *HAHandler) CreateCluster(c *gin.Context) {
	var req struct {
		Name                   string        `json:"name"`
		Engine                 string        `json:"engine"`
		EnvID                  string        `json:"env_id"`
		Mode                   string        `json:"mode"`
		ControllerID           string        `json:"controller_id"`
		StandbyControllerIDs   []string      `json:"standby_controller_ids"`
		VIP                    string        `json:"vip"`
		Remark                 string        `json:"remark"`
		Members                []haMemberReq `json:"members"`
		DbDeployClusterID      string        `json:"dbdeploy_cluster_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" || !store.ValidHAEngine(req.Engine) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称与引擎（mysql/postgres/redis）必填"})
		return
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	mode := firstNonEmpty(strings.TrimSpace(req.Mode), store.DefaultHAMode(engine))
	if !store.ValidHAMode(engine, mode) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "模式与引擎不匹配"})
		return
	}
	ctrlID, err := h.bindController(engine, mode, req.ControllerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	standbys, err := h.bindStandbyControllers(engine, mode, ctrlID, req.StandbyControllerIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	members, err := h.buildMembers(engine, mode, req.Members)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	who := middleware.Username(c)
	cluster := &store.HACluster{
		Name:                 strings.TrimSpace(req.Name),
		Engine:               engine,
		EnvID:                strings.TrimSpace(req.EnvID),
		Mode:                 mode,
		ControllerID:         ctrlID,
		StandbyControllerIDs: standbys,
		VIP:                  strings.TrimSpace(req.VIP),
		Remark:               strings.TrimSpace(req.Remark),
		MaintStatus:          "normal",
		CreatedBy:            who,
		UpdatedBy:            who,
	}
	if err := h.Store.CreateHACluster(cluster); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.ReplaceHAMembers(cluster.ID, members); err != nil {
		_ = h.Store.DeleteHACluster(cluster.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if depID := strings.TrimSpace(req.DbDeployClusterID); depID != "" && engine == "mysql" {
		_ = h.Store.SetDbDeployClusterHALink(depID, cluster.ID, ctrlID)
		_ = h.Store.MarkDbDeployHAReady(depID)
	}
	out, _ := h.Store.GetHACluster(cluster.ID)
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HAHandler) UpdateCluster(c *gin.Context) {
	exist, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	var req struct {
		Name                 string        `json:"name"`
		Engine               string        `json:"engine"`
		EnvID                string        `json:"env_id"`
		Mode                 string        `json:"mode"`
		ControllerID         string        `json:"controller_id"`
		StandbyControllerIDs []string      `json:"standby_controller_ids"`
		VIP                  string        `json:"vip"`
		Remark               string        `json:"remark"`
		Members              []haMemberReq `json:"members"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	engine := firstNonEmpty(strings.TrimSpace(req.Engine), exist.Engine)
	if !store.ValidHAEngine(engine) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "引擎无效"})
		return
	}
	mode := firstNonEmpty(strings.TrimSpace(req.Mode), exist.Mode)
	if !store.ValidHAMode(engine, mode) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "模式与引擎不匹配，请改选该引擎支持的架构"})
		return
	}
	ctrlID, err := h.bindController(engine, mode, firstNonEmpty(strings.TrimSpace(req.ControllerID), exist.ControllerID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	standbyIn := req.StandbyControllerIDs
	if standbyIn == nil {
		standbyIn = exist.StandbyControllerIDs
	}
	standbys, err := h.bindStandbyControllers(engine, mode, ctrlID, standbyIn)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	members, err := h.buildMembers(engine, mode, req.Members)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.Name = firstNonEmpty(strings.TrimSpace(req.Name), exist.Name)
	exist.Engine = engine
	exist.EnvID = req.EnvID
	exist.Mode = mode
	exist.ControllerID = ctrlID
	exist.StandbyControllerIDs = standbys
	exist.VIP = strings.TrimSpace(req.VIP)
	exist.Remark = strings.TrimSpace(req.Remark)
	exist.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpdateHACluster(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if req.Members != nil {
		if err := h.Store.ReplaceHAMembers(exist.ID, members); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	out, _ := h.Store.GetHACluster(exist.ID)
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HAHandler) DeleteCluster(c *gin.Context) {
	if err := h.Store.DeleteHACluster(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *HAHandler) buildMembers(engine, mode string, reqs []haMemberReq) ([]store.HAMember, error) {
	var out []store.HAMember
	primaries := 0
	for _, r := range reqs {
		id := strings.TrimSpace(r.InstanceID)
		if id == "" {
			continue
		}
		inst, err := h.Store.GetInstance(id)
		if err != nil {
			return nil, err
		}
		if inst == nil {
			return nil, fmt.Errorf("实例不存在: %s", id)
		}
		if err := store.AssertHAEngineMatch(engine, inst.DBType); err != nil {
			return nil, err
		}
		role := strings.TrimSpace(r.Role)
		if role == "" {
			role = "unknown"
		}
		if role == "sentinel" && !(strings.EqualFold(engine, "redis") && strings.EqualFold(mode, "sentinel")) {
			return nil, fmt.Errorf("sentinel 角色仅适用于 Redis Sentinel 模式")
		}
		if role == "primary" {
			primaries++
		}
		en := true
		if r.Enabled != nil {
			en = *r.Enabled
		}
		out = append(out, store.HAMember{
			InstanceID: id,
			Role:       role,
			Priority:   r.Priority,
			Enabled:    en,
		})
	}
	if primaries > 1 {
		return nil, fmt.Errorf("同一集群只能有一个 primary")
	}
	// MySQL MHA：至少 1 主 1 从（可一主多从）
	if strings.EqualFold(engine, "mysql") && strings.EqualFold(mode, "mha") {
		if len(out) < 2 {
			return nil, fmt.Errorf("MHA 至少需要 1 主 + 1 从（可一主多从），当前成员数 %d", len(out))
		}
		if primaries != 1 {
			return nil, fmt.Errorf("MHA 必须恰好指定 1 个 primary，当前 %d 个", primaries)
		}
		replicas := 0
		for _, m := range out {
			if m.Role == "replica" {
				replicas++
			}
		}
		if replicas < 1 {
			return nil, fmt.Errorf("MHA 至少需要 1 个 replica（可一主多从）")
		}
	}
	return out, nil
}

func (h *HAHandler) Topology(c *gin.Context) {
	h.ensureLoader()
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	topo, err := ha.ProbeTopology(c.Request.Context(), cluster, h.passwordFn())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	h.enrichTopologyWithController(c.Request.Context(), cluster, topo)
	c.JSON(http.StatusOK, gin.H{"data": topo})
}

func (h *HAHandler) enrichTopologyWithController(ctx context.Context, cluster *store.HACluster, topo *ha.Topology) {
	if h == nil || cluster == nil || topo == nil || strings.TrimSpace(cluster.ControllerID) == "" {
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		topo.Warnings = append(topo.Warnings, "控制器读取失败")
		return
	}
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		topo.Warnings = append(topo.Warnings, aerr.Error())
		return
	}
	switch strings.ToLower(ctrl.Kind) {
	case "orchestrator":
		hintHost, hintPort := "", 0
		for _, m := range cluster.Members {
			if m.Enabled && (strings.EqualFold(m.Role, "primary") || hintHost == "") {
				hintHost, hintPort = m.Host, m.Port
				if strings.EqualFold(m.Role, "primary") {
					break
				}
			}
		}
		_, err := ha.OrchestratorGetCluster(ctx, auth, hintHost, hintPort)
		if err != nil {
			topo.Warnings = append(topo.Warnings, "Orchestrator: "+err.Error())
			return
		}
		topo.Warnings = append(topo.Warnings, "Orchestrator 拓扑可读")
	case "repmgr":
		ok, summary, _ := ha.RepmgrEnvCheck(ctx, auth)
		if !ok {
			topo.Warnings = append(topo.Warnings, "repmgr: "+summary)
			return
		}
		topo.Warnings = append(topo.Warnings, "repmgr cluster show 可读")
	case "mha":
		ok, detail := ha.MHAAgentStatus(ctx, auth)
		if !ok {
			topo.Warnings = append(topo.Warnings, "MHA Agent: "+detail)
			return
		}
		topo.Warnings = append(topo.Warnings, detail)
	case "pg_autofailover":
		st, err := ha.PGAFAgentShowState(ctx, auth)
		if err != nil {
			topo.Warnings = append(topo.Warnings, "pg_autofailover: "+err.Error())
			return
		}
		topo.Warnings = append(topo.Warnings, "pg_autoctl show state 可读")
		_ = st
	case "patroni":
		cl, err := ha.PatroniGetCluster(ctx, auth)
		if err != nil {
			topo.Warnings = append(topo.Warnings, "Patroni: "+err.Error())
			return
		}
		leaders := 0
		for _, m := range cl.Members {
			role := strings.ToLower(m.Role)
			if role == "leader" || role == "master" {
				leaders++
				topo.Warnings = append(topo.Warnings, fmt.Sprintf("Patroni leader=%s host=%s state=%s", m.Name, m.Host, m.State))
			}
		}
		if leaders == 0 {
			topo.Warnings = append(topo.Warnings, "Patroni /cluster 未发现 leader")
		}
	}
}

func (h *HAHandler) Precheck(c *gin.Context) {
	h.ensureLoader()
	var req struct {
		ClusterID        string `json:"cluster_id"`
		Action           string `json:"action"`
		TargetInstanceID string `json:"target_instance_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cluster, err := h.Store.GetHACluster(req.ClusterID)
	if err != nil || cluster == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "集群不存在"})
		return
	}
	res := ha.Precheck(c.Request.Context(), cluster, req.Action, req.TargetInstanceID, h.passwordFn())
	h.enrichPrecheckHACtrl(c.Request.Context(), cluster, req.Action, res)
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *HAHandler) enrichPrecheckHACtrl(ctx context.Context, cluster *store.HACluster, action string, res *ha.PrecheckResult) {
	if h == nil || cluster == nil || res == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
	switch mode {
	case "mha":
		h.enrichPrecheckMHA(ctx, cluster, action, res)
	case "pg_autofailover":
		h.enrichPrecheckPGAF(ctx, cluster, action, res)
	case "orchestrator":
		h.enrichPrecheckOrch(ctx, cluster, action, res)
	case "proxysql_repl":
		h.enrichPrecheckProxySQL(ctx, cluster, action, res)
	case "repmgr":
		h.enrichPrecheckRepmgr(ctx, cluster, action, res)
	case "patroni":
		if action == "failover_dead" || action == "switchover" {
			add := func(name string, ok bool, msg string) {
				res.Checks = append(res.Checks, ha.PrecheckItem{Name: name, OK: ok, Message: msg})
				if !ok {
					res.OK = false
				}
			}
			if strings.TrimSpace(cluster.ControllerID) == "" {
				add("Patroni 控制器", false, "须绑定 Patroni 控制器")
				res.Summary = "预检未通过，请处理后再提交"
				return
			}
			msg := "将调用 REST /switchover"
			if action == "failover_dead" {
				msg = "将调用 REST /failover"
				add("风险确认", true, "强制切换可能丢事务，须审批后执行")
			}
			add("Patroni 控制器", true, msg)
			if !res.OK {
				res.Summary = "预检未通过，请处理后再提交"
			}
		}
	}
}

func (h *HAHandler) enrichPrecheckOrch(ctx context.Context, cluster *store.HACluster, action string, res *ha.PrecheckResult) {
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, ha.PrecheckItem{Name: name, OK: ok, Message: msg})
		if !ok {
			res.OK = false
		}
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		add("Orchestrator 控制器", false, "须绑定 Orchestrator 控制器")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil || !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
		add("Orchestrator 控制器", false, "控制器不可用或不兼容")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	add("Orchestrator 控制器", true, ctrl.Name)
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		add("鉴权", false, aerr.Error())
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	hintHost, hintPort := "", 0
	for _, m := range cluster.Members {
		if m.Enabled && strings.EqualFold(m.Role, "primary") {
			hintHost, hintPort = m.Host, m.Port
			break
		}
	}
	ok, summary, _ := ha.OrchestratorEnvCheck(ctx, auth, hintHost, hintPort)
	add("Orchestrator 可达", ok, summary)
	if action == "failover_dead" {
		add("风险确认", true, "force-master-failover 可能丢事务；须审批后执行")
	}
	if !res.OK {
		res.Summary = "预检未通过，请处理后再提交"
	}
}

func (h *HAHandler) enrichPrecheckProxySQL(ctx context.Context, cluster *store.HACluster, action string, res *ha.PrecheckResult) {
	if action != "switchover" {
		return
	}
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, ha.PrecheckItem{Name: name, OK: ok, Message: msg})
		if !ok {
			res.OK = false
		}
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		add("ProxySQL 控制器", false, "须绑定 ProxySQL 控制器")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil || !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
		add("ProxySQL 控制器", false, "控制器不可用或不兼容")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		add("鉴权", false, aerr.Error())
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ok, summary, _ := ha.ProxySQLEnvCheck(ctx, auth)
	add("ProxySQL Admin", ok, summary)
	add("说明", true, "计划切换将更新 writer hostgroup；请确认目标节点已可写或已提升")
	if !res.OK {
		res.Summary = "预检未通过，请处理后再提交"
	}
}

func (h *HAHandler) enrichPrecheckRepmgr(ctx context.Context, cluster *store.HACluster, action string, res *ha.PrecheckResult) {
	if action != "switchover" {
		return
	}
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, ha.PrecheckItem{Name: name, OK: ok, Message: msg})
		if !ok {
			res.OK = false
		}
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		add("repmgr 控制器", false, "须绑定 repmgr Agent 控制器")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil || !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
		add("repmgr 控制器", false, "控制器不可用或不兼容")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		add("鉴权", false, aerr.Error())
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ok, summary, _ := ha.RepmgrEnvCheck(ctx, auth)
	add("repmgr Agent", ok, summary)
	if !res.OK {
		res.Summary = "预检未通过，请处理后再提交"
	}
}

func (h *HAHandler) enrichPrecheckPGAF(ctx context.Context, cluster *store.HACluster, action string, res *ha.PrecheckResult) {
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, ha.PrecheckItem{Name: name, OK: ok, Message: msg})
		if !ok {
			res.OK = false
		}
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		add("pg_autofailover 控制器", false, "须绑定 pg_autofailover 控制器（Monitor Agent）")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		add("pg_autofailover 控制器", false, "控制器不存在")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	if !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
		add("pg_autofailover 控制器", false, "控制器类型与模式不匹配")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	add("pg_autofailover 控制器", true, ctrl.Name+" ("+ctrl.Kind+")")
	if action != "switchover" && action != "failover_dead" {
		return
	}
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		add("鉴权", false, aerr.Error())
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	st, err := ha.PGAFAgentShowState(ctx, auth)
	if err != nil {
		add("pg_autoctl show state", false, err.Error())
	} else {
		add("pg_autoctl show state", true, "Monitor 状态可读")
		_ = st
	}
	if action == "failover_dead" {
		add("风险确认", true, "强制切换可能丢未落盘事务；自动 failover 也可由 Monitor 自行完成")
	}
	if !res.OK {
		res.Summary = "预检未通过，请处理后再提交"
	}
}

func (h *HAHandler) enrichPrecheckMHA(ctx context.Context, cluster *store.HACluster, action string, res *ha.PrecheckResult) {
	if h == nil || cluster == nil || res == nil {
		return
	}
	if !strings.EqualFold(cluster.Mode, "mha") {
		return
	}
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, ha.PrecheckItem{Name: name, OK: ok, Message: msg})
		if !ok {
			res.OK = false
		}
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		add("MHA 控制器", false, "mode=mha 必须绑定 MHA 控制器")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		add("MHA 控制器", false, "控制器不存在")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	if !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
		add("MHA 控制器", false, "控制器类型与模式不匹配")
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	add("MHA 控制器", true, ctrl.Name+" ("+ctrl.Kind+")")
	if action != "switchover" && action != "failover_dead" {
		return
	}
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		add("鉴权", false, aerr.Error())
		res.Summary = "预检未通过，请处理后再提交"
		return
	}
	if action == "switchover" {
		ok, detail := ha.MHAAgentReplOK(ctx, auth)
		add("MHA check_repl", ok, detail)
	} else {
		st, err := ha.MHAAgentManagerCtrl(ctx, auth, "status", "")
		if err != nil {
			add("MHA manager", false, err.Error())
		} else {
			active := false
			if v, ok := st["active"].(bool); ok {
				active = v
			}
			if active {
				add("MHA manager", true, "当前运行中；执行强制切换前将自动 stop")
			} else {
				add("MHA manager", true, "当前未运行（符合切换编排）")
			}
		}
		add("风险确认", true, "主宕机强制切换可能丢未落盘事务，须审批后执行")
	}
	if !res.OK {
		res.Summary = "预检未通过，请处理后再提交"
	}
}

func (h *HAHandler) SubmitChange(c *gin.Context) {
	h.ensureLoader()
	var req struct {
		ClusterID        string `json:"cluster_id"`
		Action           string `json:"action"`
		TargetInstanceID string `json:"target_instance_id"`
		AssignedTo       string `json:"assigned_to"`
		Description      string `json:"description"`
		SkipPrecheck     bool   `json:"skip_precheck"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if !store.ValidHAAction(req.Action) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的操作类型"})
		return
	}
	cluster, err := h.Store.GetHACluster(req.ClusterID)
	if err != nil || cluster == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "集群不存在"})
		return
	}
	pre := ha.Precheck(c.Request.Context(), cluster, req.Action, req.TargetInstanceID, h.passwordFn())
	h.enrichPrecheckHACtrl(c.Request.Context(), cluster, req.Action, pre)
	if req.Action == "failover_dead" {
		mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
		if mode != "mha" && mode != "pg_autofailover" && mode != "patroni" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "强制切换仅支持 MHA / pg_autofailover / Patroni"})
			return
		}
		if req.SkipPrecheck {
			c.JSON(http.StatusBadRequest, gin.H{"message": "强制切换不允许跳过预检"})
			return
		}
	}
	if !req.SkipPrecheck && !pre.OK {
		c.JSON(http.StatusBadRequest, gin.H{"message": pre.Summary, "data": pre})
		return
	}
	preJSON, _ := json.Marshal(pre)
	checklist := ha.SwitchoverChecklist(cluster.Engine, cluster.Mode)
	steps, _ := json.Marshal(checklist)

	primaryID := ""
	for _, m := range cluster.Members {
		if m.Role == "primary" {
			primaryID = m.InstanceID
			break
		}
	}
	if primaryID == "" && len(cluster.Members) > 0 {
		primaryID = cluster.Members[0].InstanceID
	}

	assigned := strings.TrimSpace(req.AssignedTo)
	if assigned == "" {
		assigned, _ = h.Store.GetGlobalArg("sync_default_approver")
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = fmt.Sprintf("HA %s · %s", store.HAActionLabel(req.Action), cluster.Name)
	}
	if req.Action == "failover_dead" {
		desc = "[风险] 主已判定宕机、可能丢事务 · " + desc
	}
	payload, _ := json.Marshal(map[string]any{
		"cluster_id":         cluster.ID,
		"cluster_name":       cluster.Name,
		"engine":             cluster.Engine,
		"action":             req.Action,
		"target_instance_id": req.TargetInstanceID,
		"checklist":          checklist,
	})

	issue := &store.Issue{
		ID:           uuid.NewString(),
		TicketNo:     "H" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:    store.IssueTypeHA,
		InstanceID:   primaryID,
		DBName:       cluster.ID,
		SQLContent:   string(payload),
		Description:  desc,
		Submitter:    middleware.Username(c),
		AssignedTo:   assigned,
		NeedRollback: string(preJSON),
	}
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	task := &store.HATask{
		ClusterID:        cluster.ID,
		IssueID:          issue.ID,
		Action:           req.Action,
		Status:           store.StatusPending,
		TargetInstanceID: req.TargetInstanceID,
		PrecheckJSON:     string(preJSON),
		StepsJSON:        string(steps),
		Operator:         issue.Submitter,
	}
	if err := h.Store.CreateHATask(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"issue": issue, "task": task, "precheck": pre}})
}

func (h *HAHandler) ListTasks(c *gin.Context) {
	list, err := h.Store.ListHATasks(c.Query("cluster_id"), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) ListControllers(c *gin.Context) {
	list, err := h.Store.ListHAControllers(c.Query("engine"), c.Query("env_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	refs, _ := h.Store.CountHAControllerRefs()
	for i := range list {
		if v, ok := refs[list[i].ID]; ok {
			list[i].RefPrimary = v[0]
			list[i].RefStandby = v[1]
		}
		list[i].PasswordEnc = ""
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) CreateController(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Engine   string `json:"engine"`
		Kind     string `json:"kind"`
		EnvID    string `json:"env_id"`
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Password string `json:"password"`
		AgentID  string `json:"agent_id"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" || !store.ValidHAEngine(req.Engine) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称、引擎必填"})
		return
	}
	if !store.ValidHAControllerKind(req.Engine, req.Kind) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "控制器类型与引擎不匹配"})
		return
	}
	agentID := strings.TrimSpace(req.AgentID)
	baseURL := strings.TrimSpace(req.BaseURL)
	username := req.Username
	enc := ""
	if agentID != "" {
		a, err := h.Store.GetBackupAgent(agentID)
		if err != nil || a == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "平台 Agent 不存在"})
			return
		}
		groups := backup.ParseGroupsCSV(a.EnabledGroups)
		hasHA := false
		for _, g := range groups {
			if g == backup.GroupHA {
				hasHA = true
				break
			}
		}
		if !hasHA {
			r := backup.DecodeCapabilities(a.Capabilities)
			hasHA = backup.HasGroupEnabled(r, backup.GroupHA)
		}
		if !hasHA {
			c.JSON(http.StatusBadRequest, gin.H{"message": "所选 Agent 未勾选「高可用」归属，请在配置中心 → 平台 Agent 中设置"})
			return
		}
		kind := strings.ToLower(strings.TrimSpace(req.Kind))
		if kind != "mha" && kind != "pg_autofailover" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "选用平台 Agent 仅支持 kind=mha 或 pg_autofailover"})
			return
		}
		baseURL = strings.TrimSpace(a.BaseURL)
		if username == "" {
			username = a.Username
		}
		if strings.TrimSpace(a.PasswordEnc) != "" {
			plain, err := secret.Decrypt(h.Secret, a.PasswordEnc)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": "解密 Agent 密码失败: " + err.Error()})
				return
			}
			enc, err = secret.Encrypt(h.Secret, plain)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
		}
	} else if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请填写 Base URL，或选用平台 Agent"})
		return
	}
	if enc == "" && strings.TrimSpace(req.Password) != "" {
		var err error
		enc, err = secret.Encrypt(h.Secret, req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	who := middleware.Username(c)
	it := &store.HAController{
		Name: req.Name, Engine: strings.ToLower(strings.TrimSpace(req.Engine)), Kind: strings.ToLower(strings.TrimSpace(req.Kind)),
		EnvID: req.EnvID, BaseURL: baseURL, Username: username, PasswordEnc: enc,
		AgentID: agentID, Remark: req.Remark, CreatedBy: who, UpdatedBy: who,
	}
	if err := h.Store.CreateHAController(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it.PasswordEnc = ""
	it.PasswordSet = enc != ""
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *HAHandler) DeleteController(c *gin.Context) {
	if err := h.Store.DeleteHAController(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ExecuteHATicket 由 TicketHandler 在审批后调用；一期：维护态落库 + 清单确认，无控制器不自动切主。
func (h *HAHandler) ExecuteHATicket(ctx context.Context, it *store.Issue) (*ticketexec.Result, error) {
	h.ensureLoader()
	started := time.Now().UTC().Format(time.RFC3339)
	res := &ticketexec.Result{StartedAt: started, Engine: "ha", Success: true}
	task, err := h.Store.GetHATaskByIssue(it.ID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		res.Success = false
		res.Message = "未找到关联 HA 任务"
		return res, nil
	}
	cluster, err := h.Store.GetHACluster(task.ClusterID)
	if err != nil || cluster == nil {
		res.Success = false
		res.Message = "集群不存在"
		return res, nil
	}
	who := it.Submitter
	steps := []map[string]any{}
	addStep := func(name, status, msg string) {
		steps = append(steps, map[string]any{"name": name, "status": status, "message": msg, "at": time.Now().UTC().Format(time.RFC3339)})
	}

	switch task.Action {
	case "enter_maint":
		if err := h.Store.SetHAClusterMaintStatus(cluster.ID, "maintaining", who); err != nil {
			res.Success = false
			res.Message = err.Error()
			addStep("enter_maint", "failed", err.Error())
		} else {
			addStep("enter_maint", "ok", "集群已标记为维护中")
			res.Summary = "已进入维护模式"
		}
	case "exit_maint":
		if err := h.Store.SetHAClusterMaintStatus(cluster.ID, "normal", who); err != nil {
			res.Success = false
			res.Message = err.Error()
			addStep("exit_maint", "failed", err.Error())
		} else {
			addStep("exit_maint", "ok", "集群已退出维护")
			res.Summary = "已退出维护模式"
		}
	case "switchover":
		checklist := ha.SwitchoverChecklist(cluster.Engine, cluster.Mode)
		auto := false
		if store.HAAutoSwitchoverMode(cluster.Mode) && strings.TrimSpace(cluster.ControllerID) != "" {
			ctrl, cerr := h.Store.GetHAController(cluster.ControllerID)
			if cerr != nil || ctrl == nil {
				addStep("controller", "skip", "控制器读取失败，按检查清单人工切换")
			} else if !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
				addStep("controller", "skip", "控制器类型与模式不匹配，按清单执行")
			} else {
				cand, _ := h.Store.GetInstance(task.TargetInstanceID)
				auth, aerr := h.controllerAuth(ctrl)
				if aerr != nil {
					addStep("controller", "failed", aerr.Error())
					res.Success = false
					res.Message = aerr.Error()
				} else if cand == nil {
					addStep("controller", "failed", "目标实例不存在")
					res.Success = false
					res.Message = "缺少目标实例"
				} else {
					restartAfter := true
					if strings.EqualFold(ctrl.Kind, "mha") {
						if _, err := ha.MHAAgentManagerCtrl(ctx, auth, "stop", ""); err != nil {
							addStep("manager_stop", "warn", "stop manager: "+err.Error()+"（继续切换）")
						} else {
							addStep("manager_stop", "ok", "已停止 masterha_manager")
						}
					}
					if err := ha.PlannedSwitchover(ctx, cluster, auth, cand); err != nil {
						addStep("controller", "failed", err.Error())
						res.Success = false
						res.Message = err.Error()
						restartAfter = false
					} else {
						auto = true
						addStep("controller", "ok", "已通过 "+ctrl.Kind+" 执行计划切换")
						res.Summary = "已通过控制器完成计划切换"
						if herr := ha.MaybeRunPostSwitchHook(ctx, cluster, auth, cand); herr != nil {
							addStep("vip_hook", "warn", herr.Error())
						} else if strings.TrimSpace(ha.ParseHAHookOptions(cluster.Remark).Script) != "" {
							addStep("vip_hook", "ok", "已执行 VIP/流量钩子")
						}
					}
					if strings.EqualFold(ctrl.Kind, "mha") && restartAfter {
						if _, err := ha.MHAAgentManagerCtrl(ctx, auth, "start", ""); err != nil {
							addStep("manager_start", "warn", "start manager: "+err.Error())
						} else {
							addStep("manager_start", "ok", "已拉起 masterha_manager")
						}
					}
				}
			}
		} else {
			addStep("controller", "skip", "未配置自动控制器或当前模式不支持自动切换")
		}
		if !auto {
			for _, item := range checklist {
				addStep("checklist", "pending", item)
			}
		}
		if res.Success && strings.TrimSpace(task.TargetInstanceID) != "" {
			if err := h.Store.UpdateHAMemberRoles(cluster.ID, task.TargetInstanceID); err != nil {
				res.Success = false
				res.Message = "更新登记角色失败: " + err.Error()
				addStep("update_roles", "failed", err.Error())
			} else {
				addStep("update_roles", "ok", "已按目标实例更新平台登记角色")
				if res.Summary == "" {
					res.Summary = "计划切换已登记角色变更；请按清单确认现网切换完成"
				}
			}
		} else if res.Success {
			res.Success = false
			res.Message = "缺少目标实例"
		}
	case "failover_dead":
		mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
		if (mode != "mha" && mode != "orchestrator" && mode != "pg_autofailover" && mode != "patroni") || strings.TrimSpace(cluster.ControllerID) == "" {
			res.Success = false
			res.Message = "强制切换仅支持已绑定控制器的 MHA / Orchestrator / pg_autofailover / Patroni 集群"
			addStep("controller", "failed", res.Message)
			break
		}
		ctrl, cerr := h.Store.GetHAController(cluster.ControllerID)
		if cerr != nil || ctrl == nil || !store.CompatibleHAController(cluster.Engine, cluster.Mode, ctrl.Kind) {
			res.Success = false
			res.Message = "控制器不可用或不兼容"
			addStep("controller", "failed", res.Message)
			break
		}
		auth, aerr := h.controllerAuth(ctrl)
		if aerr != nil {
			res.Success = false
			res.Message = aerr.Error()
			addStep("controller", "failed", aerr.Error())
			break
		}
		var cand *store.Instance
		if strings.TrimSpace(task.TargetInstanceID) != "" {
			cand, _ = h.Store.GetInstance(task.TargetInstanceID)
		}
		if mode == "mha" {
			if _, err := ha.MHAAgentManagerCtrl(ctx, auth, "stop", ""); err != nil {
				addStep("manager_stop", "warn", "stop manager: "+err.Error()+"（继续切换）")
			} else {
				addStep("manager_stop", "ok", "已停止 masterha_manager")
			}
		}
		if err := ha.ForcedFailover(ctx, cluster, auth, cand); err != nil {
			res.Success = false
			res.Message = err.Error()
			addStep("forced_failover", "failed", err.Error())
		} else {
			addStep("forced_failover", "ok", "已通过 "+ctrl.Kind+" 执行强制切换")
			res.Summary = "强制切换已完成"
			if cand != nil {
				if err := h.Store.UpdateHAMemberRoles(cluster.ID, cand.ID); err != nil {
					addStep("update_roles", "warn", err.Error())
				} else {
					addStep("update_roles", "ok", "已更新平台登记角色")
				}
			} else {
				addStep("update_roles", "skip", "未指定新主，请人工核对拓扑后改登记角色")
			}
			if herr := ha.MaybeRunPostSwitchHook(ctx, cluster, auth, cand); herr != nil {
				addStep("vip_hook", "warn", herr.Error())
			} else if strings.TrimSpace(ha.ParseHAHookOptions(cluster.Remark).Script) != "" {
				addStep("vip_hook", "ok", "已执行 VIP/流量钩子")
			}
			if mode == "mha" {
				if _, err := ha.MHAAgentManagerCtrl(ctx, auth, "start", ""); err != nil {
					addStep("manager_start", "warn", "start manager: "+err.Error())
				} else {
					addStep("manager_start", "ok", "已拉起 masterha_manager")
				}
			}
		}
	default:
		res.Success = false
		res.Message = "未知操作"
	}

	stepsJSON, _ := json.Marshal(steps)
	resultJSON, _ := json.Marshal(res)
	task.StepsJSON = string(stepsJSON)
	task.ResultJSON = string(resultJSON)
	if res.Success {
		task.Status = store.StatusExecuted
	} else {
		task.Status = store.StatusFailed
	}
	_ = h.Store.UpdateHATask(task)
	res.EndedAt = time.Now().UTC().Format(time.RFC3339)
	if res.Summary == "" && res.Success {
		res.Summary = "HA 变更执行完成"
	}
	return res, nil
}
