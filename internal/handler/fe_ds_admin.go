package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type FeAdminHandler struct {
	Store  *store.DB
	Secret string
}

type DsAdminHandler struct {
	Store  *store.DB
	Secret string
}

func (h *FeAdminHandler) List(c *gin.Context) {
	env := c.Query("environment")
	if env == "" {
		env = c.Query("env")
	}
	list, err := h.Store.ListFeAdmins(env, c.Query("keyword"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.FeAdmin{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *FeAdminHandler) Get(c *gin.Context) {
	it, err := h.Store.GetFeAdmin(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "FE 凭据不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *FeAdminHandler) Create(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it := store.FeAdmin{
		TaskName:    asString(raw["task_name"]),
		Environment: firstNonEmpty(asString(raw["environment"]), asString(raw["env"])),
		Domain:      asString(raw["domain"]),
		Port:        asInt(raw["port"]),
		FeNode:      asString(raw["fe_node"]),
		Username:    asString(raw["username"]),
		Password:    asString(raw["password"]),
	}
	if strings.TrimSpace(it.Environment) == "" || strings.TrimSpace(it.Domain) == "" || strings.TrimSpace(it.FeNode) == "" || strings.TrimSpace(it.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境、域名、FE 节点、用户名不能为空"})
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
	if err := h.Store.CreateFeAdmin(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *FeAdminHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetFeAdmin(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "FE 凭据不存在"})
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it := store.FeAdmin{
		ID:          exist.ID,
		TaskName:    firstNonEmpty(asString(raw["task_name"]), exist.TaskName),
		Environment: firstNonEmpty(asString(raw["environment"]), asString(raw["env"]), exist.Environment),
		Domain:      firstNonEmpty(asString(raw["domain"]), exist.Domain),
		Port:        exist.Port,
		FeNode:      firstNonEmpty(asString(raw["fe_node"]), exist.FeNode),
		Username:    firstNonEmpty(asString(raw["username"]), exist.Username),
		PasswordEnc: exist.PasswordEnc,
		CreatedBy:   exist.CreatedBy,
		UpdatedBy:   actorName(c),
	}
	if _, ok := raw["port"]; ok {
		it.Port = asInt(raw["port"])
	}
	if plain := strings.TrimSpace(asString(raw["password"])); plain != "" && plain != "******" {
		enc, err := secret.Encrypt(h.Secret, plain)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		it.PasswordEnc = enc
	}
	if err := h.Store.UpdateFeAdmin(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *FeAdminHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteFeAdmin(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DsAdminHandler) List(c *gin.Context) {
	env := c.Query("environment")
	if env == "" {
		env = c.Query("env")
	}
	list, err := h.Store.ListDsAdmins(env, c.Query("keyword"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.DsAdmin{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DsAdminHandler) Get(c *gin.Context) {
	it, err := h.Store.GetDsAdmin(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "海豚配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DsAdminHandler) Create(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it := store.DsAdmin{
		Environment: firstNonEmpty(asString(raw["environment"]), asString(raw["env"])),
		LoginURL:    asString(raw["login_url"]),
		Username:    asString(raw["username"]),
		Password:    asString(raw["password"]),
	}
	if strings.TrimSpace(it.Environment) == "" || strings.TrimSpace(it.LoginURL) == "" || strings.TrimSpace(it.Username) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "环境、登录地址、用户名不能为空"})
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
	if err := h.Store.CreateDsAdmin(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DsAdminHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetDsAdmin(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "海豚配置不存在"})
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it := store.DsAdmin{
		ID:          exist.ID,
		Environment: firstNonEmpty(asString(raw["environment"]), asString(raw["env"]), exist.Environment),
		LoginURL:    firstNonEmpty(asString(raw["login_url"]), exist.LoginURL),
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
	if err := h.Store.UpdateDsAdmin(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DsAdminHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteDsAdmin(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Credential returns plaintext login fields for ops console bootstrap (authenticated only).
func (h *DsAdminHandler) Credential(c *gin.Context) {
	env := strings.TrimSpace(c.Query("env"))
	if env == "" {
		env = strings.TrimSpace(c.Query("environment"))
	}
	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定 env"})
		return
	}
	it, err := h.Store.GetDsAdminByEnv(env)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "该环境未配置海豚调度"})
		return
	}
	plain, err := secret.Decrypt(h.Secret, it.PasswordEnc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "解密失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"environment": it.Environment,
		"login_url":   it.LoginURL,
		"username":    it.Username,
		"password":    plain,
	}})
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(t)))
		return n
	}
}
