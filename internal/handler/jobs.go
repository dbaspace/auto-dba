package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/jobreg"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type JobsHandler struct {
	Reg   *jobreg.Registry
	Store *store.DB
}

func (h *JobsHandler) List(c *gin.Context) {
	if h == nil || h.Reg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "job registry unavailable"})
		return
	}
	list, err := h.Reg.ListViews()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	running, paused, fail24, leased := 0, 0, 0, 0
	for _, j := range list {
		if j.Enabled {
			running++
		} else {
			paused++
		}
		fail24 += j.FailCount24h
		if j.LeaseHeld {
			leased++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"items": list,
		"summary": gin.H{
			"enabled": running, "paused": paused,
			"fail_24h": fail24, "leased": leased,
			"local_owner_id": h.Reg.OwnerID, "local_host": h.Reg.OwnerHost,
		},
	})
}

func (h *JobsHandler) Get(c *gin.Context) {
	v, err := h.Reg.GetView(c.Param("key"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *JobsHandler) Update(c *gin.Context) {
	var body struct {
		Enabled      *bool   `json:"enabled"`
		ScheduleSpec *string `json:"schedule_spec"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	who := ""
	if v, ok := c.Get(middleware.CtxUsername); ok {
		who, _ = v.(string)
	}
	st, err := h.Reg.UpdateSetting(c.Param("key"), body.Enabled, body.ScheduleSpec, who)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, _ := h.Reg.GetView(c.Param("key"))
	c.JSON(http.StatusOK, gin.H{"setting": st, "job": v})
}

func (h *JobsHandler) Trigger(c *gin.Context) {
	key := c.Param("key")
	res, err := h.Reg.TryFire(c.Request.Context(), key, jobreg.TriggerManual, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if res != nil && res.Conflict {
		c.JSON(http.StatusConflict, gin.H{
			"error": "lease held by another replica",
			"holder": res.Holder, "expires_at": res.Expires, "result": res,
		})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *JobsHandler) Runs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListJobRuns(c.Param("key"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func (h *JobsHandler) Cluster(c *gin.Context) {
	owners, err := h.Store.ListActiveJobLeaseOwners(0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"local_owner_id": h.Reg.OwnerID,
		"local_host":     h.Reg.OwnerHost,
		"owners":         owners,
	})
}

func (h *JobsHandler) BusinessSchedules(c *gin.Context) {
	tid := strings.TrimSpace(c.GetHeader("X-Tenant-Id"))
	if tid == "" {
		if v, ok := c.Get(middleware.CtxTenantID); ok {
			tid, _ = v.(string)
		}
	}
	list, err := jobreg.ListBusinessSchedules(h.Store, tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}
