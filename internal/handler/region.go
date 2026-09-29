package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/store"
)

type RegionHandler struct {
	Store *store.DB
}

func (h *RegionHandler) List(c *gin.Context) {
	list, err := h.Store.ListRegions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Region{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *RegionHandler) Create(c *gin.Context) {
	var r store.Region
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateRegion(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *RegionHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetRegion(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "区域不存在"})
		return
	}
	var r store.Region
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	r.ID = exist.ID
	if err := h.Store.UpdateRegion(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *RegionHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteRegion(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *RegionHandler) ListEnvs(c *gin.Context) {
	list, err := h.Store.ListRegionEnvs(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Environment{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *RegionHandler) PutEnvs(c *gin.Context) {
	id := c.Param("id")
	if r, _ := h.Store.GetRegion(id); r == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "区域不存在"})
		return
	}
	var req struct {
		EnvIDs []string `json:"env_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "env_ids 必填"})
		return
	}
	if err := h.Store.SetRegionEnvs(id, req.EnvIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type EnvironmentHandler struct {
	Store *store.DB
}

func (h *EnvironmentHandler) List(c *gin.Context) {
	list, err := h.Store.ListEnvironments()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Environment{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *EnvironmentHandler) Create(c *gin.Context) {
	var e store.Environment
	if err := c.ShouldBindJSON(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateEnvironment(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": e})
}

func (h *EnvironmentHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetEnvironment(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "环境不存在"})
		return
	}
	var e store.Environment
	if err := c.ShouldBindJSON(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	e.ID = exist.ID
	if err := h.Store.UpdateEnvironment(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": e})
}

func (h *EnvironmentHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteEnvironment(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func applyInstanceLocation(st *store.DB, it *store.Instance) error {
	it.RegionID = strings.TrimSpace(it.RegionID)
	it.EnvID = strings.TrimSpace(it.EnvID)
	if it.RegionID == "" || it.EnvID == "" {
		return errLocationRequired
	}
	ok, err := st.HasRegionEnv(it.RegionID, it.EnvID)
	if err != nil {
		return err
	}
	if !ok {
		return errLocationUnbound
	}
	env, err := st.GetEnvironment(it.EnvID)
	if err != nil {
		return err
	}
	if env == nil {
		return errLocationUnbound
	}
	it.IDC = env.Code
	syncInstanceMonitorTags(it, env.Code)
	return nil
}

// syncInstanceMonitorTags 规范：tags 中 idc= 必须等于环境管理 code；
// Redis / StarRocks / ClickHouse / Kafka 缺 domain= 时用域名或主机补齐。
func syncInstanceMonitorTags(it *store.Instance, envCode string) {
	envCode = strings.TrimSpace(envCode)
	if envCode == "" {
		return
	}
	tags := make([]string, 0, len(it.Tags)+2)
	hasDomain := false
	for _, raw := range it.Tags {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		k, _, ok := splitInstanceTagKV(raw)
		if ok && strings.EqualFold(k, "idc") {
			continue // 稍后统一写入 idc=envCode
		}
		if ok && strings.EqualFold(k, "domain") {
			hasDomain = true
		}
		tags = append(tags, raw)
	}
	tags = append(tags, "idc="+envCode)

	if !hasDomain && needsDomainTag(it.DBType) {
		if d := firstInstanceDomain(it); d != "" {
			tags = append(tags, "domain="+d)
		}
	}
	it.Tags = tags
}

func needsDomainTag(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "redis", "starrocks", "clickhouse", "kafka":
		return true
	default:
		return false
	}
}

func firstInstanceDomain(it *store.Instance) string {
	if it == nil {
		return ""
	}
	if d := strings.TrimSpace(it.DomainName); d != "" {
		return d
	}
	for _, d := range it.Domains {
		if v := strings.TrimSpace(d); v != "" {
			return v
		}
	}
	return strings.TrimSpace(it.Host)
}

func splitInstanceTagKV(raw string) (key, val string, ok bool) {
	i := strings.IndexByte(raw, '=')
	if i <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(raw[:i])
	val = strings.TrimSpace(raw[i+1:])
	if key == "" || val == "" {
		return "", "", false
	}
	return key, val, true
}

var (
	errLocationRequired = &httpError{msg: "区域和环境必填"}
	errLocationUnbound  = &httpError{msg: "该区域未绑定所选环境"}
)

type httpError struct{ msg string }

func (e *httpError) Error() string { return e.msg }
