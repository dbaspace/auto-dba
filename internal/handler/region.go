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
	return nil
}

var (
	errLocationRequired = &httpError{msg: "区域和环境必填"}
	errLocationUnbound  = &httpError{msg: "该区域未绑定所选环境"}
)

type httpError struct{ msg string }

func (e *httpError) Error() string { return e.msg }
