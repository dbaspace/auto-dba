package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/proxy"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// ProxyHandler 接入层代理（HAProxy/Nginx/LVS）配置与生命周期。
type ProxyHandler struct {
	Store  *store.DB
	Secret string
}

func (h *ProxyHandler) Meta(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"kinds": []map[string]any{
			{"code": "haproxy", "label": "HAProxy", "default_conf": proxy.DefaultConfPath("haproxy"), "default_unit": proxy.DefaultUnit("haproxy"), "ha_nodes": "2", "ha_note": "强制主备 2 节点"},
			{"code": "nginx", "label": "Nginx", "default_conf": proxy.DefaultConfPath("nginx"), "default_unit": proxy.DefaultUnit("nginx"), "ha_nodes": "2-8", "ha_note": "至少主备；可扩至多节点（同 VIP VRRP 链，扩的是冗余）"},
			{"code": "lvs", "label": "LVS (Keepalived+IPVS)", "default_conf": proxy.DefaultConfPath("lvs"), "default_unit": proxy.DefaultUnit("lvs"), "ha_nodes": "2", "ha_note": "Director 主备 2 节点；RS 走 backends"},
		},
		"ha": gin.H{
			"min_nodes": 2,
			"nginx_max_nodes": proxy.MaxNginxNodes,
			"roles":     []string{"master", "backup"},
			"notes":     "三种代理自建/登记均须主+备（各绑平台 Agent :8090）；Nginx 可追加 backup 节点。VIP 仅 MASTER 持有，多节点主要为故障切换冗余。",
		},
		"lvs": gin.H{
			"lb_algo": []string{"rr", "wrr", "lc", "wlc", "lblc", "lblcr", "dh", "sh", "mh", "sed", "nq", "fo", "ovf", "twos"},
			"lb_kind": []string{"DR", "NAT", "TUN"},
			"state":   []string{"MASTER", "BACKUP"},
			"notes":   "对齐 keepalived.conf(5)：VRRP + virtual_server；DR 需在 RS 配 lo VIP 与 arp_ignore/arp_announce",
		},
		"notes": "三种代理均部署 Keepalived 管理 VIP；须主备拓扑；VIP 须唯一且未被本机/网络占用。",
	}})
}

func (h *ProxyHandler) List(c *gin.Context) {
	list, err := h.Store.ListProxyServices(c.Query("kind"), c.Query("env_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	views := make([]gin.H, 0, len(list))
	for i := range list {
		h.Store.AttachProxyConfVersion(&list[i])
		views = append(views, proxyServiceView(&list[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

func (h *ProxyHandler) Get(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	s.PasswordEnc = ""
	h.Store.AttachProxyConfVersion(s)
	c.JSON(http.StatusOK, gin.H{"data": proxyServiceView(s)})
}

func (h *ProxyHandler) Create(c *gin.Context) {
	var req struct {
		Name         string            `json:"name"`
		Kind         string            `json:"kind"`
		EnvID        string            `json:"env_id"`
		AgentID      string            `json:"agent_id"`
		AgentBaseURL string            `json:"agent_base_url"`
		AgentUser    string            `json:"agent_user"`
		Password     string            `json:"password"`
		ConfPath     string            `json:"conf_path"`
		ConfContent  string            `json:"conf_content"`
		SystemdUnit  string            `json:"systemd_unit"`
		VIP          string            `json:"vip"`
		ListenPort   int               `json:"listen_port"`
		Backends     []proxy.Backend   `json:"backends"`
		Mode         string            `json:"mode"`
		LVS          *proxy.LVSOptions `json:"lvs"`
		Nodes        []proxy.HANodeSpec `json:"nodes"`
		Remark       string            `json:"remark"`
		Generate     bool              `json:"generate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" || !store.ValidProxyKind(req.Kind) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "name 与合法 kind(haproxy|nginx|lvs) 必填"})
		return
	}
	kind := strings.ToLower(req.Kind)
	if err := h.ensureVIPAvailable(c, req.VIP, "", true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.LVS != nil {
		if _, err := proxy.NormalizeLVSOptions(*req.LVS, req.Name); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	nodes := req.Nodes
	if len(nodes) == 0 && (strings.TrimSpace(req.AgentID) != "" || strings.TrimSpace(req.AgentBaseURL) != "") {
		// 兼容旧单 Agent 入参：自动拆成「缺备」错误提示前，要求前端传 nodes
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提交 nodes（至少 master+backup 各绑平台 Agent）"})
		return
	}
	who := middleware.Username(c)
	s := &store.ProxyService{
		Name: req.Name, Kind: kind, EnvID: req.EnvID,
		ConfPath: firstNonEmpty(req.ConfPath, proxy.DefaultConfPath(req.Kind)),
		SystemdUnit: firstNonEmpty(req.SystemdUnit, proxy.DefaultUnit(req.Kind)),
		VIP: req.VIP, ListenPort: req.ListenPort, Remark: req.Remark,
		Status: "draft", CreatedBy: who, UpdatedBy: who,
	}
	if s.ListenPort <= 0 {
		s.ListenPort = proxy.DefaultListenPort(req.Kind)
	}
	if len(req.Backends) > 0 {
		b, _ := json.Marshal(req.Backends)
		s.BackendsJSON = string(b)
	}
	opt := proxy.RenderOptions{Name: req.Name, VIP: req.VIP, ListenPort: s.ListenPort, Mode: req.Mode, Backends: req.Backends, LVS: req.LVS}
	if strings.TrimSpace(req.ConfContent) != "" {
		s.ConfContent = req.ConfContent
	} else {
		s.ConfContent = proxy.RenderDefaultConf(req.Kind, opt)
	}
	if err := h.applyHAToService(s, nodes, req.LVS, req.Backends); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if pw := strings.TrimSpace(req.Password); pw != "" {
		enc, err := secret.Encrypt(h.Secret, pw)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		s.PasswordEnc = enc
	}
	{
		ctxCheck, cancelCheck := context.WithTimeout(c.Request.Context(), 25*time.Second)
		cerr := h.checkVIPOnNodes(ctxCheck, req.VIP, proxy.ParseProxyOptions(s.OptionsJSON).Nodes, "")
		cancelCheck()
		if cerr != nil {
			// 登记时若 Agent 暂不可达，仅平台唯一性已校验；探测失败不阻断草稿（有 Agent 才严格）
			o := proxy.ParseProxyOptions(s.OptionsJSON)
			if len(o.Nodes) > 0 {
				if _, aerr := h.agentAuthNode(o.Nodes[0], ""); aerr == nil {
					c.JSON(http.StatusBadRequest, gin.H{"message": cerr.Error()})
					return
				}
			}
		}
	}
	if err := h.Store.UpsertProxyService(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if _, err := h.Store.SaveProxyConfVersionIfChanged(s, "create", who, c.ClientIP(), "登记"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入配置版本失败: " + err.Error()})
		return
	}
	s.PasswordEnc = ""
	s.PasswordSet = strings.TrimSpace(req.Password) != ""
	h.Store.AttachProxyConfVersion(s)
	c.JSON(http.StatusOK, gin.H{"data": proxyServiceView(s)})
}

func (h *ProxyHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	var req struct {
		Name         string          `json:"name"`
		EnvID        string          `json:"env_id"`
		AgentID      string          `json:"agent_id"`
		AgentBaseURL string          `json:"agent_base_url"`
		AgentUser    string          `json:"agent_user"`
		Password     string          `json:"password"`
		ConfPath     string          `json:"conf_path"`
		ConfContent  string          `json:"conf_content"`
		SystemdUnit  string          `json:"systemd_unit"`
		VIP          string          `json:"vip"`
		ListenPort   int             `json:"listen_port"`
		Backends     []proxy.Backend    `json:"backends"`
		Nodes        []proxy.HANodeSpec `json:"nodes"`
		Remark       string             `json:"remark"`
		Status       string             `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		exist.Name = req.Name
	}
	exist.EnvID = req.EnvID
	exist.AgentID = req.AgentID
	if strings.TrimSpace(req.AgentBaseURL) != "" {
		exist.AgentBaseURL = strings.TrimSpace(req.AgentBaseURL)
	}
	exist.AgentUser = req.AgentUser
	if strings.TrimSpace(req.ConfPath) != "" {
		exist.ConfPath = req.ConfPath
	}
	if req.ConfContent != "" {
		exist.ConfContent = req.ConfContent
	}
	if strings.TrimSpace(req.SystemdUnit) != "" {
		exist.SystemdUnit = req.SystemdUnit
	}
	if req.ListenPort > 0 {
		exist.ListenPort = req.ListenPort
	}
	vip := strings.TrimSpace(req.VIP)
	if vip == "" {
		vip = strings.TrimSpace(exist.VIP)
	}
	if err := h.ensureVIPAvailable(c, vip, exist.ID, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	// VIP 变更且已绑 Agent 时，探测本机/网络占用
	if proxy.NormalizeVIPHost(vip) != proxy.NormalizeVIPHost(exist.VIP) {
		if auth, aerr := h.agentAuth(exist); aerr == nil {
			ctxCheck, cancelCheck := context.WithTimeout(c.Request.Context(), 25*time.Second)
			avail, msg, cerr := proxy.CheckVIP(ctxCheck, auth, vip)
			cancelCheck()
			if cerr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "VIP 探测失败: " + cerr.Error()})
				return
			}
			if !avail {
				c.JSON(http.StatusBadRequest, gin.H{"message": msg})
				return
			}
		}
	}
	exist.VIP = vip
	if req.Backends != nil {
		b, _ := json.Marshal(req.Backends)
		exist.BackendsJSON = string(b)
	}
	exist.Remark = req.Remark
	if strings.TrimSpace(req.Status) != "" {
		exist.Status = req.Status
	}
	exist.UpdatedBy = middleware.Username(c)
	exist.PasswordEnc = "" // 默认不覆盖
	if pw := strings.TrimSpace(req.Password); pw != "" {
		enc, err := secret.Encrypt(h.Secret, pw)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		exist.PasswordEnc = enc
	}
	// VIP/后端/节点变更时刷新 Keepalived（按主备节点生成）
	{
		var backends []proxy.Backend
		_ = json.Unmarshal([]byte(exist.BackendsJSON), &backends)
		o := proxy.ParseProxyOptions(exist.OptionsJSON)
		nodes := o.Nodes
		if len(req.Nodes) > 0 {
			nodes = req.Nodes
		}
		if len(nodes) > 0 {
			if err := h.applyHAToService(exist, nodes, o.LVS, backends); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
				return
			}
		} else {
			ka, kerr := proxy.BuildKeepalivedConf(exist.Kind, exist.Name, exist.VIP, exist.ListenPort, backends, o.LVS)
			if kerr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": kerr.Error()})
				return
			}
			exist.OptionsJSON = proxy.AttachKeepalivedToOptions(exist.OptionsJSON, proxy.DefaultKeepalivedPath, ka, o.LVS)
			if exist.Kind == "lvs" && strings.TrimSpace(exist.ConfPath) == proxy.DefaultKeepalivedPath {
				exist.ConfContent = ka
			}
		}
	}
	if err := h.Store.UpsertProxyService(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if _, err := h.Store.SaveProxyConfVersionIfChanged(exist, "save", exist.UpdatedBy, c.ClientIP(), "保存草稿"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入配置版本失败: " + err.Error()})
		return
	}
	exist.PasswordEnc = ""
	h.Store.AttachProxyConfVersion(exist)
	c.JSON(http.StatusOK, gin.H{"data": proxyServiceView(exist)})
}

func (h *ProxyHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteProxyService(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ProxyHandler) Preview(c *gin.Context) {
	var req struct {
		Kind       string            `json:"kind"`
		Name       string            `json:"name"`
		VIP        string            `json:"vip"`
		ListenPort int               `json:"listen_port"`
		Mode       string            `json:"mode"`
		Backends   []proxy.Backend   `json:"backends"`
		LVS        *proxy.LVSOptions `json:"lvs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !store.ValidProxyKind(req.Kind) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "kind 与参数无效"})
		return
	}
	if strings.ToLower(req.Kind) == "lvs" && strings.TrimSpace(req.VIP) != "" {
		if err := proxy.ValidateVIP(req.VIP); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	content := proxy.RenderDefaultConf(req.Kind, proxy.RenderOptions{
		Name: req.Name, VIP: req.VIP, ListenPort: req.ListenPort, Mode: req.Mode, Backends: req.Backends, LVS: req.LVS,
	})
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"content": content, "conf_path": proxy.DefaultConfPath(req.Kind), "unit": proxy.DefaultUnit(req.Kind),
	}})
}

func (h *ProxyHandler) Push(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	var body struct {
		Validate bool   `json:"validate"`
		Reload   bool   `json:"reload"`
		Content  string `json:"content"`
	}
	_ = c.ShouldBindJSON(&body)
	content := s.ConfContent
	if strings.TrimSpace(body.Content) != "" {
		content = body.Content
		s.ConfContent = content
		s.UpdatedBy = middleware.Username(c)
		_ = h.Store.UpsertProxyService(s)
	}
	if _, err := h.Store.SaveProxyConfVersionIfChanged(s, "push", middleware.Username(c), c.ClientIP(), "下发"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入配置版本失败: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Minute)
	defer cancel()
	out, err := h.pushConfToAllNodes(ctx, s, body.Reload, "reload")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	_ = h.Store.MarkProxyPushed(s.ID, middleware.Username(c))
	h.Store.AttachProxyConfVersion(s)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"push": out, "conf_version": s.ConfVersion}})
}

func (h *ProxyHandler) Validate(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	auth, aerr := h.agentAuth(s)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	out, err := proxy.ValidateConf(ctx, auth, s.Kind, s.ConfPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *ProxyHandler) ServiceStatus(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	out, err := h.statusAllNodes(ctx, s)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *ProxyHandler) ServiceCtrl(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	action := strings.ToLower(c.Param("action"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	out, err := h.ctrlAllNodes(ctx, s, action)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// LVSIpvs 只读查询 Agent 本机 IPVS 表。
func (h *ProxyHandler) LVSIpvs(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	if strings.ToLower(s.Kind) != "lvs" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 LVS 服务支持 IPVS 查询"})
		return
	}
	auth, aerr := h.agentAuth(s)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	out, err := proxy.LVSIpvs(ctx, auth)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// LVSStatus Keepalived + VIP 汇总（三种代理均可：均部署 Keepalived）。
func (h *ProxyHandler) LVSStatus(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	auth, aerr := h.agentAuth(s)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	unit := s.SystemdUnit
	if strings.ToLower(s.Kind) != "lvs" {
		unit = "keepalived"
	}
	out, err := proxy.LVSStatus(ctx, auth, unit, s.VIP)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *ProxyHandler) VersionList(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	page := 1
	size := 20
	if v := strings.TrimSpace(c.Query("current")); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := strings.TrimSpace(c.DefaultQuery("page_size", c.Query("pageSize"))); v != "" {
		fmt.Sscanf(v, "%d", &size)
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	list, total, err := h.Store.ListProxyConfVersions(s.ID, size, (page-1)*size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// 列表不回传完整 conf，减小体积
	items := make([]gin.H, 0, len(list))
	for i := range list {
		items = append(items, gin.H{
			"id": list[i].ID, "service_id": list[i].ServiceID, "version_no": list[i].VersionNo,
			"conf_path": list[i].ConfPath, "vip": list[i].VIP, "listen_port": list[i].ListenPort,
			"change_source": list[i].ChangeSource, "remark": list[i].Remark,
			"operator": list[i].Operator, "operator_ip": list[i].OperatorIP, "created_at": list[i].CreatedAt,
			"content_bytes": len(list[i].ConfContent),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"list": items, "pagination": gin.H{"total": total, "current": page, "page_size": size},
	}})
}

func (h *ProxyHandler) VersionDetail(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	row, err := h.Store.GetProxyConfVersion(s.ID, c.Param("versionId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": row})
}

func (h *ProxyHandler) VersionRestore(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	row, err := h.Store.GetProxyConfVersion(s.ID, c.Param("versionId"))
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
		return
	}
	var body struct {
		Push   bool `json:"push"`
		Reload bool `json:"reload"`
	}
	_ = c.ShouldBindJSON(&body)
	who := middleware.Username(c)
	store.ApplyProxyConfVersion(s, row)
	s.UpdatedBy = who
	// 按恢复后的 VIP/后端重建 Keepalived 副配置
	{
		var backends []proxy.Backend
		_ = json.Unmarshal([]byte(s.BackendsJSON), &backends)
		o := proxy.ParseProxyOptions(s.OptionsJSON)
		if ka, kerr := proxy.BuildKeepalivedConf(s.Kind, s.Name, s.VIP, s.ListenPort, backends, o.LVS); kerr == nil {
			s.OptionsJSON = proxy.AttachKeepalivedToOptions(s.OptionsJSON, proxy.DefaultKeepalivedPath, ka, o.LVS)
			if s.Kind == "lvs" && strings.TrimSpace(s.ConfPath) == proxy.DefaultKeepalivedPath {
				s.ConfContent = ka
			}
		}
	}
	if err := h.Store.UpsertProxyService(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ver, err := h.Store.SaveProxyConfVersionIfChanged(s, "restore", who, c.ClientIP(),
		fmt.Sprintf("恢复自 v%d", row.VersionNo))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入配置版本失败: " + err.Error()})
		return
	}
	var pushOut map[string]any
	if body.Push {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Minute)
		defer cancel()
		pushOut, err = h.pushConfToAllNodes(ctx, s, body.Reload, "reload")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": pushOut})
			return
		}
		_ = h.Store.MarkProxyPushed(s.ID, who)
	}
	s.PasswordEnc = ""
	h.Store.AttachProxyConfVersion(s)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"service": proxyServiceView(s), "restored_from": row.VersionNo, "new_version": ver,
		"push": pushOut,
	}})
}

// Deploy 自建部署：各 HA 节点 Agent 装包 → 登记服务 → 可选下发 conf 并启动。
func (h *ProxyHandler) Deploy(c *gin.Context) {
	var req struct {
		Name         string             `json:"name"`
		Kind         string             `json:"kind"`
		EnvID        string             `json:"env_id"`
		AgentID      string             `json:"agent_id"`
		AgentBaseURL string             `json:"agent_base_url"`
		AgentUser    string             `json:"agent_user"`
		Password     string             `json:"password"`
		ConfPath     string             `json:"conf_path"`
		SystemdUnit  string             `json:"systemd_unit"`
		VIP          string             `json:"vip"`
		ListenPort   int                `json:"listen_port"`
		Backends     []proxy.Backend    `json:"backends"`
		Mode         string             `json:"mode"`
		LVS          *proxy.LVSOptions  `json:"lvs"`
		Nodes        []proxy.HANodeSpec `json:"nodes"`
		Remark       string             `json:"remark"`
		SkipInstall  bool               `json:"skip_install"`
		EnableStart  *bool              `json:"enable_start"`
		PushConf     *bool              `json:"push_conf"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if strings.TrimSpace(req.Name) == "" || !store.ValidProxyKind(kind) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "name 与合法 kind(haproxy|nginx|lvs) 必填"})
		return
	}
	if err := h.ensureVIPAvailable(c, req.VIP, "", true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.LVS != nil {
		if _, err := proxy.NormalizeLVSOptions(*req.LVS, req.Name); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	if len(req.Nodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提交 nodes：至少 master+backup 两个平台 Agent"})
		return
	}
	who := middleware.Username(c)
	enableStart := true
	if req.EnableStart != nil {
		enableStart = *req.EnableStart
	}
	pushConf := true
	if req.PushConf != nil {
		pushConf = *req.PushConf
	}
	s := &store.ProxyService{
		Name: req.Name, Kind: kind, EnvID: req.EnvID,
		ConfPath: firstNonEmpty(req.ConfPath, proxy.DefaultConfPath(kind)),
		SystemdUnit: firstNonEmpty(req.SystemdUnit, proxy.DefaultUnit(kind)),
		VIP: req.VIP, ListenPort: req.ListenPort, Remark: firstNonEmpty(req.Remark, "自建部署·主备"),
		Status: "draft", CreatedBy: who, UpdatedBy: who,
	}
	if s.ListenPort <= 0 {
		s.ListenPort = proxy.DefaultListenPort(kind)
	}
	if len(req.Backends) > 0 {
		b, _ := json.Marshal(req.Backends)
		s.BackendsJSON = string(b)
	}
	s.ConfContent = proxy.RenderDefaultConf(kind, proxy.RenderOptions{
		Name: req.Name, VIP: req.VIP, ListenPort: s.ListenPort, Mode: req.Mode, Backends: req.Backends, LVS: req.LVS,
	})
	if err := h.applyHAToService(s, req.Nodes, req.LVS, req.Backends); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if pw := strings.TrimSpace(req.Password); pw != "" {
		enc, err := secret.Encrypt(h.Secret, pw)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		s.PasswordEnc = enc
	}
	nodes := proxy.ParseProxyOptions(s.OptionsJSON).Nodes
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		if p, e := secret.Decrypt(h.Secret, s.PasswordEnc); e == nil {
			pass = p
		}
	}
	{
		ctxCheck, cancelCheck := context.WithTimeout(c.Request.Context(), 25*time.Second)
		cerr := h.checkVIPOnNodes(ctxCheck, req.VIP, nodes, pass)
		cancelCheck()
		if cerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": cerr.Error()})
			return
		}
	}
	var jobs []proxyNodeJob
	jobViews := make([]gin.H, 0, len(nodes))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	for _, n := range nodes {
		auth, aerr := h.agentAuthNode(n, pass)
		if aerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
			return
		}
		jobID, err := proxy.Deploy(ctx, auth, kind, req.VIP, enableStart, req.SkipInstall)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("节点 %s 触发部署失败: %v", n.Name, err)})
			return
		}
		jobs = append(jobs, proxyNodeJob{Node: n, JobID: jobID})
		jobViews = append(jobViews, gin.H{"node": n.Name, "role": n.Role, "job_id": jobID, "agent_id": n.AgentID})
	}
	if err := h.Store.UpsertProxyService(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_, _ = h.Store.SaveProxyConfVersionIfChanged(s, "create", who, c.ClientIP(), "自建部署·主备")

	go h.finishProxyDeployHA(s.ID, jobs, pushConf, enableStart, who)

	s.PasswordEnc = ""
	h.Store.AttachProxyConfVersion(s)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"service": proxyServiceView(s), "jobs": jobViews,
		"job_id": jobViews[0]["job_id"], // 兼容旧前端轮询字段
		"message": fmt.Sprintf("已在 %d 个节点触发装包；完成后将自动下发主备 Keepalived 配置", len(jobs)),
	}})
}


func (h *ProxyHandler) finishProxyDeploy(serviceID, jobID string, pushConf, enableStart bool, who string) {
	authCtx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	s, err := h.Store.GetProxyService(serviceID)
	if err != nil || s == nil {
		return
	}
	auth, aerr := h.agentAuth(s)
	if aerr != nil {
		return
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-authCtx.Done():
			return
		default:
		}
		out, err := proxy.DeployJob(authCtx, auth, jobID)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		job, _ := out["job"].(map[string]any)
		if job == nil {
			time.Sleep(2 * time.Second)
			continue
		}
		st, _ := job["status"].(string)
		switch strings.ToLower(st) {
		case "success":
			if pushConf {
				ctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if _, err := proxy.PutConf(ctx, auth, s.Kind, s.ConfPath, s.ConfContent, true); err != nil {
					s.Status = "error"
					s.Remark = strings.TrimSpace(s.Remark + "；装包成功但下发配置失败: " + err.Error())
					s.UpdatedBy = who
					_ = h.Store.UpsertProxyService(s)
					ccancel()
					return
				}
				if err := h.pushKeepalivedConf(ctx, auth, s); err != nil {
					s.Status = "error"
					s.Remark = strings.TrimSpace(s.Remark + "；代理 conf 已下发，Keepalived 失败: " + err.Error())
					s.UpdatedBy = who
					_ = h.Store.UpsertProxyService(s)
					ccancel()
					return
				}
				act := "reload"
				if enableStart {
					act = "restart"
				}
				if _, err := proxy.ServiceCtrl(ctx, auth, s.Kind, act, s.SystemdUnit); err != nil {
					s.Status = "error"
					s.Remark = strings.TrimSpace(s.Remark + "；配置已下发但 " + act + " 失败: " + err.Error())
					s.UpdatedBy = who
					_ = h.Store.UpsertProxyService(s)
					ccancel()
					return
				}
				if s.Kind != "lvs" {
					_, _ = proxy.ServiceCtrl(ctx, auth, "lvs", act, "keepalived")
				}
				ccancel()
				_ = h.Store.MarkProxyPushed(s.ID, who)
			} else {
				s.Status = "active"
				s.UpdatedBy = who
				_ = h.Store.UpsertProxyService(s)
			}
			return
		case "failed":
			msg, _ := job["message"].(string)
			logTxt, _ := job["log"].(string)
			s.Status = "error"
			detail := msg
			if logTxt != "" && len(logTxt) < 500 {
				detail = msg + " | " + logTxt
			}
			s.Remark = strings.TrimSpace(s.Remark + "；部署失败: " + detail)
			s.UpdatedBy = who
			_ = h.Store.UpsertProxyService(s)
			return
		default:
			time.Sleep(3 * time.Second)
		}
	}
}

// DeployJob 查询 Agent 部署任务状态（自动在主备节点中定位 job）。
func (h *ProxyHandler) DeployJob(c *gin.Context) {
	s, err := h.Store.GetProxyService(c.Param("id"))
	if err != nil || s == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "服务不存在"})
		return
	}
	jobID := strings.TrimSpace(c.Param("jobId"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "jobId 必填"})
		return
	}
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		if p, e := secret.Decrypt(h.Secret, s.PasswordEnc); e == nil {
			pass = p
		}
	}
	nodes := proxy.ParseProxyOptions(s.OptionsJSON).Nodes
	candidates := nodes
	if len(candidates) == 0 {
		candidates = []proxy.HANodeSpec{{AgentID: s.AgentID, AgentBaseURL: s.AgentBaseURL, AgentUser: s.AgentUser, Name: "master"}}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	var lastErr error
	for _, n := range candidates {
		auth, aerr := h.agentAuthNode(n, pass)
		if aerr != nil {
			lastErr = aerr
			continue
		}
		out, err := proxy.DeployJob(ctx, auth, jobID)
		if err != nil {
			lastErr = err
			continue
		}
		c.JSON(http.StatusOK, gin.H{"data": out, "node": n.Name, "role": n.Role})
		return
	}
	msg := "未找到部署任务"
	if lastErr != nil {
		msg = lastErr.Error()
	}
	c.JSON(http.StatusBadRequest, gin.H{"message": msg})
}

func (h *ProxyHandler) agentAuth(s *store.ProxyService) (proxy.AgentAuth, error) {
	pass := ""
	if strings.TrimSpace(s.PasswordEnc) != "" {
		p, err := secret.Decrypt(h.Secret, s.PasswordEnc)
		if err != nil {
			return proxy.AgentAuth{}, err
		}
		pass = p
	}
	base := strings.TrimSpace(s.AgentBaseURL)
	if base == "" && strings.TrimSpace(s.AgentID) != "" {
		ag, err := h.Store.GetBackupAgent(s.AgentID)
		if err == nil && ag != nil {
			base = strings.TrimSpace(ag.BaseURL)
			if s.AgentUser == "" {
				s.AgentUser = ag.Username
			}
			if pass == "" && strings.TrimSpace(ag.PasswordEnc) != "" {
				if p, e := secret.Decrypt(h.Secret, ag.PasswordEnc); e == nil {
					pass = p
				}
			}
		}
	}
	if base == "" {
		return proxy.AgentAuth{}, fmt.Errorf("请配置 Agent Base URL 或绑定平台 Agent")
	}
	return proxy.AgentAuth{BaseURL: base, Username: s.AgentUser, Password: pass}, nil
}

// ensureVIPAvailable 校验 VIP 合法，且未被代理服务 / HA 集群占用；可选经 Agent 探测本机与网络。
func (h *ProxyHandler) ensureVIPAvailable(c *gin.Context, vip, excludeID string, requireVIP bool) error {
	vip = strings.TrimSpace(vip)
	if vip == "" {
		if requireVIP {
			return fmt.Errorf("VIP 必填（三种代理均由 Keepalived 接管 VIP）")
		}
		return nil
	}
	if err := proxy.ValidateVIP(vip); err != nil {
		return err
	}
	if hit, err := h.Store.FindProxyByVIP(vip, excludeID); err != nil {
		return err
	} else if hit != nil {
		return fmt.Errorf("VIP %s 已被代理服务「%s」(%s) 占用", proxy.NormalizeVIPHost(vip), hit.Name, hit.Kind)
	}
	if ha, err := h.Store.FindHAClusterByVIP(vip); err != nil {
		return err
	} else if ha != nil {
		return fmt.Errorf("VIP %s 已被 HA 集群「%s」占用", proxy.NormalizeVIPHost(vip), ha.Name)
	}
	return nil
}

func (h *ProxyHandler) pushKeepalivedConf(ctx context.Context, auth proxy.AgentAuth, s *store.ProxyService) error {
	if s == nil {
		return nil
	}
	o := proxy.ParseProxyOptions(s.OptionsJSON)
	content := strings.TrimSpace(o.KeepalivedContent)
	path := strings.TrimSpace(o.KeepalivedPath)
	if path == "" {
		path = proxy.DefaultKeepalivedPath
	}
	// LVS 主 conf 已是 keepalived，避免重复下发同一内容到同路径
	if strings.ToLower(s.Kind) == "lvs" {
		if strings.TrimSpace(s.ConfPath) == path || content == "" || content == s.ConfContent {
			return nil
		}
	}
	if content == "" {
		// 尝试按当前 VIP 生成
		if strings.TrimSpace(s.VIP) == "" {
			return fmt.Errorf("缺少 Keepalived 配置内容且未设置 VIP")
		}
		var backends []proxy.Backend
		_ = json.Unmarshal([]byte(s.BackendsJSON), &backends)
		ka, err := proxy.BuildKeepalivedConf(s.Kind, s.Name, s.VIP, s.ListenPort, backends, o.LVS)
		if err != nil {
			return err
		}
		content = ka
	}
	_, err := proxy.PutConf(ctx, auth, "lvs", path, content, true)
	return err
}
