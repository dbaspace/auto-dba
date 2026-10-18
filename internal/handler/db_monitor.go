package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbmonitor"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// DbMonitorHandler 数据库监控 API。
type DbMonitorHandler struct {
	Svc *dbmonitor.Service
}

func (h *DbMonitorHandler) Catalog(c *gin.Context) {
	engine := c.Query("engine")
	if engine == "" {
		engine = c.Query("db_type")
	}
	c.JSON(http.StatusOK, gin.H{"data": dbmonitor.Catalog(engine), "message": "ok"})
}

func (h *DbMonitorHandler) Instances(c *gin.Context) {
	dbType := c.Query("db_type")
	if dbType == "" {
		dbType = c.Query("engine")
	}
	if dbType == "" {
		dbType = "postgres"
	}
	list, err := h.Svc.ListInstancesInTenant(h.monitorListTenantID(c), dbType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "message": "ok"})
}

func (h *DbMonitorHandler) Clusters(c *gin.Context) {
	engine := c.Query("engine")
	if engine == "" {
		engine = c.Query("db_type")
	}
	if engine == "" {
		engine = "starrocks"
	}
	list, err := h.Svc.ListClustersInTenant(h.monitorListTenantID(c), engine)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "message": "ok"})
}

// monitorListTenantID 监控下拉范围：平台 admin 与 dba 租户有效成员不受当前租户限制（空串 = 全平台），其余按租户。
func (h *DbMonitorHandler) monitorListTenantID(c *gin.Context) string {
	if h.Svc != nil && h.Svc.Store != nil {
		uid := middleware.UserID(c)
		if ok, err := h.Svc.Store.UserIsAdmin(uid); err == nil && ok {
			return ""
		}
		if t, err := h.Svc.Store.GetTenantByCode(store.DBATenantCode); err == nil && t != nil {
			if ok, err := h.Svc.Store.IsActiveTenantMember(t.ID, uid); err == nil && ok {
				return ""
			}
		}
	}
	return effectiveListTenantID(h.Svc.Store, c)
}

func (h *DbMonitorHandler) Overview(c *gin.Context) {
	includeSettings := c.Query("include_settings") == "1" || c.Query("include_settings") == "true"
	engine := c.Query("engine")
	if engine == "" {
		engine = c.Query("db_type")
	}
	data, err := h.Svc.Overview(c.Request.Context(), c.Query("instance_id"), includeSettings, engine)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

type dbMonitorQueryReq struct {
	InstanceID   string   `json:"instance_id"`
	Engine       string   `json:"engine"`
	DBType       string   `json:"db_type"`
	IDC          string   `json:"idc"`
	Domain       string   `json:"domain"`
	ClusterID    string   `json:"cluster_id"`
	FEInstances  []string `json:"fe_instances"`
	BEInstances  []string `json:"be_instances"`
	Start        string   `json:"start"`
	End          string   `json:"end"`
	Step         int      `json:"step"`
	Sections     []string `json:"sections"`
}

func (h *DbMonitorHandler) Query(c *gin.Context) {
	var req dbMonitorQueryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body: " + err.Error()})
		return
	}
	start, end, step, err := parseTimes(req.Start, req.End, req.Step)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	engine := req.Engine
	if engine == "" {
		engine = req.DBType
	}
	idc, domain := strings.TrimSpace(req.IDC), strings.TrimSpace(req.Domain)
	if domain == "" && strings.TrimSpace(req.ClusterID) != "" {
		parts := strings.SplitN(req.ClusterID, "|", 2)
		if len(parts) == 2 {
			if idc == "" {
				idc = strings.TrimSpace(parts[0])
			}
			domain = strings.TrimSpace(parts[1])
		}
	}
	data, err := h.Svc.QueryAll(c.Request.Context(), dbmonitor.QueryOpts{
		InstanceID: req.InstanceID, Engine: engine,
		IDC: idc, Domain: domain,
		FEInstances: req.FEInstances, BEInstances: req.BEInstances,
		Sections: req.Sections, Start: start, End: end, Step: step,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *DbMonitorHandler) Panels(c *gin.Context) {
	start, end, step, err := parseTimeRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var keys []string
	if raw := strings.TrimSpace(c.Query("keys")); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				keys = append(keys, k)
			}
		}
	}
	engine := c.Query("engine")
	if engine == "" {
		engine = c.Query("db_type")
	}
	data, err := h.Svc.QueryPanels(c.Request.Context(),
		c.Query("instance_id"), c.Query("section"), keys, start, end, step, engine)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func parseTimes(startRaw, endRaw string, stepSec int) (start, end time.Time, step time.Duration, err error) {
	now := time.Now()
	end = now
	start = now.Add(-3 * time.Hour)
	if v := strings.TrimSpace(startRaw); v != "" {
		start, err = parseTime(v)
		if err != nil {
			return
		}
	}
	if v := strings.TrimSpace(endRaw); v != "" {
		end, err = parseTime(v)
		if err != nil {
			return
		}
	}
	step = time.Minute
	if stepSec > 0 {
		step = time.Duration(stepSec) * time.Second
	}
	return start, end, step, nil
}

func parseTimeRange(c *gin.Context) (start, end time.Time, step time.Duration, err error) {
	now := time.Now()
	end = now
	start = now.Add(-3 * time.Hour)
	if v := strings.TrimSpace(c.Query("start")); v != "" {
		start, err = parseTime(v)
		if err != nil {
			return
		}
	}
	if v := strings.TrimSpace(c.Query("end")); v != "" {
		end, err = parseTime(v)
		if err != nil {
			return
		}
	}
	step = time.Minute
	if v := strings.TrimSpace(c.Query("step")); v != "" {
		if sec, e := strconv.Atoi(v); e == nil && sec > 0 {
			step = time.Duration(sec) * time.Second
		} else if d, e := time.ParseDuration(v); e == nil {
			step = d
		}
	}
	return start, end, step, nil
}

func parseTime(v string) (time.Time, error) {
	if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
		if unix > 1e12 {
			return time.UnixMilli(unix), nil
		}
		return time.Unix(unix, 0), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, v)
}
