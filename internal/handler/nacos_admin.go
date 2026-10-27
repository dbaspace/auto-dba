package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type NacosAdminHandler struct {
	Store  *store.DB
	Secret string
}

func (h *NacosAdminHandler) List(c *gin.Context) {
	env := c.Query("environment")
	if env == "" {
		env = c.Query("env")
	}
	list, err := h.Store.ListNacosAdminsInTenant(effectiveListTenantID(h.Store, c), env, c.Query("keyword"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.NacosAdmin{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *NacosAdminHandler) Get(c *gin.Context) {
	it, err := h.Store.GetNacosAdminInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Nacos 配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *NacosAdminHandler) Create(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it := store.NacosAdmin{
		Environment: firstNonEmpty(asString(raw["environment"]), asString(raw["env"])),
		ConsoleURL:  firstNonEmpty(asString(raw["console_url"]), asString(raw["login_url"])),
		Username:    asString(raw["username"]),
		Password:    asString(raw["password"]),
	}
	if strings.TrimSpace(it.Environment) == "" || strings.TrimSpace(it.ConsoleURL) == "" || strings.TrimSpace(it.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境、控制台地址、用户名不能为空"})
		return
	}
	if strings.TrimSpace(it.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请输入密码"})
		return
	}
	enc, err := secret.Encrypt(h.Secret, it.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it.PasswordEnc = enc
	it.CreatedBy = actorName(c)
	it.UpdatedBy = it.CreatedBy
	it.Password = ""
	stampTenantID(c, func(tid string) { it.TenantID = tid })
	if err := h.Store.CreateNacosAdmin(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *NacosAdminHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetNacosAdminInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Nacos 配置不存在"})
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it := store.NacosAdmin{
		ID:          exist.ID,
		Environment: firstNonEmpty(asString(raw["environment"]), asString(raw["env"]), exist.Environment),
		ConsoleURL:  firstNonEmpty(asString(raw["console_url"]), asString(raw["login_url"]), exist.ConsoleURL),
		Username:    firstNonEmpty(asString(raw["username"]), exist.Username),
		PasswordEnc: exist.PasswordEnc,
		CreatedBy:   exist.CreatedBy,
		UpdatedBy:   actorName(c),
	}
	if plain := strings.TrimSpace(asString(raw["password"])); plain != "" && plain != "******" {
		enc, err := secret.Encrypt(h.Secret, plain)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		it.PasswordEnc = enc
	}
	if err := h.Store.UpdateNacosAdmin(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *NacosAdminHandler) Delete(c *gin.Context) {
	exist, err := h.Store.GetNacosAdminInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置不存在"})
		return
	}
	if err := h.Store.DeleteNacosAdmin(exist.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Credential returns plaintext login fields for ops console bootstrap (authenticated only).
func (h *NacosAdminHandler) Credential(c *gin.Context) {
	env := strings.TrimSpace(c.Query("env"))
	if env == "" {
		env = strings.TrimSpace(c.Query("environment"))
	}
	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定 env"})
		return
	}
	it, err := h.Store.GetNacosAdminByEnvInTenant(env, effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "该环境未配置 Nacos 控制台"})
		return
	}
	plain, err := secret.Decrypt(h.Secret, it.PasswordEnc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "解密失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"environment": it.Environment,
		"console_url": it.ConsoleURL,
		"username":    it.Username,
		"password":    plain,
	}})
}
