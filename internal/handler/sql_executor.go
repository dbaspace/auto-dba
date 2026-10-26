package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
)

type SQLExecutorHandler struct {
	Store  *store.DB
	Secret string
	Cfg    *config.Config
}

type sqlExecutorBody struct {
	Name     string   `json:"name"`
	DeployID string   `json:"deploy_id"`
	EnvCodes []string `json:"env_codes"`
	Enabled  *bool    `json:"enabled"`
}

func (h *SQLExecutorHandler) List(c *gin.Context) {
	list, err := h.Store.ListSQLExecutors()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	envs, err := h.Store.ListEnvironments()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	names := map[string]string{}
	for _, env := range envs {
		names[env.Code] = env.Name
	}
	local, err := h.Store.GetGlobalArg(store.ArgTicketLocalIDC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, e := range list {
		var bound []gin.H
		for _, code := range e.EnvCodes {
			bound = append(bound, gin.H{"code": code, "name": names[code]})
		}
		out = append(out, gin.H{
			"id": e.ID, "name": e.Name, "base_url": e.BaseURL,
			"env_codes": e.EnvCodes, "environments": bound,
			"enabled": e.Enabled, "updated_at": e.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "local_idc_codes": splitCodes(local)})
}

func (h *SQLExecutorHandler) SaveLocalIDC(c *gin.Context) {
	var body struct {
		Codes []string `json:"codes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	codes := splitCodes(strings.Join(body.Codes, ","))
	if err := h.Store.SetGlobalArg(store.ArgTicketLocalIDC, strings.Join(codes, ","), "本服务所在环境 code，这些环境的 DDL/DML 在本进程直连"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "local_idc_codes": codes})
}

func (h *SQLExecutorHandler) Create(c *gin.Context) {
	h.save(c, "")
}

func (h *SQLExecutorHandler) Update(c *gin.Context) {
	h.save(c, c.Param("id"))
}

func (h *SQLExecutorHandler) save(c *gin.Context, id string) {
	var body sqlExecutorBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	name := strings.TrimSpace(body.Name)
	codes := splitCodes(strings.Join(body.EnvCodes, ","))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "名称必填"})
		return
	}
	if len(codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请从环境管理选择要绑定的环境"})
		return
	}
	var baseFromDeploy string
	if id == "" {
		dep, err := h.Store.GetBackupAgentDeploy(strings.TrimSpace(body.DeployID))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if dep == nil || dep.Kind != sqlexec.DeployKind || dep.Status != store.BackupDeploySuccess || strings.TrimSpace(dep.AgentID) == "" || strings.TrimSpace(dep.BaseURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请选择一条下发成功的记录。执行器登录凭据由下发生成，这里不填写数据库账号"})
			return
		}
		id = dep.AgentID
		baseFromDeploy = strings.TrimSpace(dep.BaseURL)
	}
	old, err := h.Store.GetSQLExecutor(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if old == nil || old.PasswordEnc == "" || strings.TrimSpace(old.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "这次下发没有留下执行器登录凭据，请重新下发"})
		return
	}
	if other, code := h.envBoundElsewhere(codes, old.ID); other != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境 " + code + " 已经绑在执行器「" + other + "」上"})
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	baseURL := old.BaseURL
	if baseFromDeploy != "" {
		baseURL = baseFromDeploy
	}
	row := &store.SQLExecutor{
		ID: old.ID, Name: name, BaseURL: baseURL, Username: old.Username, PasswordEnc: old.PasswordEnc,
		EnvCodes: codes, Enabled: enabled, CreatedAt: old.CreatedAt,
	}
	if err := h.Store.SaveSQLExecutor(row); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": row.ID})
}

func (h *SQLExecutorHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteSQLExecutor(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SQLExecutorHandler) Probe(c *gin.Context) {
	row, err := h.Store.GetSQLExecutor(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "执行器不存在"})
		return
	}
	if other, code := h.envBoundElsewhere(row.EnvCodes, row.ID); other != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境 " + code + " 已经绑在执行器「" + other + "」上"})
		return
	}
	pass, err := secret.Decrypt(h.Secret, row.PasswordEnc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "账号密码不对"})
		return
	}
	if err := sqlexec.Probe(c.Request.Context(), row.BaseURL, row.Username, pass); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "探活通过，这些环境上的工单会发到这台执行器"})
}

func (h *SQLExecutorHandler) envBoundElsewhere(codes []string, selfID string) (otherName, code string) {
	list, err := h.Store.ListSQLExecutors()
	if err != nil {
		return "", ""
	}
	want := map[string]string{}
	for _, code := range codes {
		want[strings.ToLower(code)] = code
	}
	for _, e := range list {
		if e.ID == selfID {
			continue
		}
		for _, code := range e.EnvCodes {
			if orig, ok := want[strings.ToLower(strings.TrimSpace(code))]; ok {
				return e.Name, orig
			}
		}
	}
	return "", ""
}

func (h *SQLExecutorHandler) Deploy(c *gin.Context) {
	var body struct {
		HostInstanceID string   `json:"host_instance_id"`
		ListenPort     int      `json:"listen_port"`
		RemoteDir      string   `json:"remote_dir"`
		Escalate       string   `json:"escalate"`
		Name           string   `json:"name"`
		EnvCodes       []string `json:"env_codes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	codes := splitCodes(strings.Join(body.EnvCodes, ","))
	if len(codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请从环境管理选择要绑定的环境"})
		return
	}
	inst, err := h.Store.GetInstance(strings.TrimSpace(body.HostInstanceID))
	if err != nil || inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "主机实例不存在"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "sql-agent-" + inst.Name
	}
	selfID := ""
	if exist, _ := h.Store.FindSQLExecutorByName(name); exist != nil {
		selfID = exist.ID
	}
	if other, code := h.envBoundElsewhere(codes, selfID); other != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境 " + code + " 已经绑在执行器「" + other + "」上"})
		return
	}
	if h.Cfg == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "未配置 SSH，无法下发"})
		return
	}
	id, err := sqlexec.StartDeploy(h.Store, sqlexec.DeployOpts{
		HostInstanceID:  body.HostInstanceID,
		ListenPort:      body.ListenPort,
		RemoteDir:       body.RemoteDir,
		Escalate:        body.Escalate,
		Operator:        middleware.Username(c),
		Name:            name,
		EnvCodes:        codes,
		Secret:          h.Secret,
		SSHIdentityFile: h.Cfg.Terminal.SSHIdentityFile,
		SSHUser:         h.Cfg.Terminal.SSHUser,
		SSHInsecure:     h.Cfg.Terminal.SSHInsecureHostKey,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deploy_id": id}})
}

func (h *SQLExecutorHandler) ListDeploys(c *gin.Context) {
	list, err := h.Store.ListSQLExecutorDeploys(c.Query("host_instance_id"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func splitCodes(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\n' || r == '\t'
	})
	seen := map[string]struct{}{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}
