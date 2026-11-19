package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/pgdiag"
	"auto-dba/internal/promclient"
)

func (h *InsightHandler) Topology(c *gin.Context) {
	id := strings.TrimSpace(c.Query("instance_id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择实例"})
		return
	}
	inst, err := h.Store.GetInstance(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	rows, err := pgdiag.QueryActivity(c.Request.Context(), h.Secret, inst)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	addrs := make([]string, len(rows))
	for i, row := range rows {
		addrs[i] = row.Host
	}
	prom := h.topologyProm()
	hits, note := pgdiag.MapPods(c.Request.Context(), prom, addrs)
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		host := strings.TrimSpace(row.Host)
		ip, lookup := pgdiag.LookupPodIP(host)
		if lookup {
			host = ip
		}
		item := gin.H{
			"host":        host,
			"database":    row.Database,
			"connections": row.Connections,
			"pod":         "",
			"namespace":   "",
			"node":        "",
			"local":       host == "",
			"lookup":      lookup,
			"pool":        false,
		}
		if lookup {
			if hit, ok := hits[ip]; ok {
				item["pod"] = hit.Pod
				item["namespace"] = hit.Namespace
				item["node"] = hit.Node
			}
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      out,
		"prom_note": note,
	})
}

func (h *InsightHandler) TopologyPools(c *gin.Context) {
	var req struct {
		Pods []string `json:"pods"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供 Pod 名"})
		return
	}
	names, err := pgdiag.ValidPoolPodNames(req.Pods)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	pools, note := pgdiag.MapPools(c.Request.Context(), h.topologyProm(), names)
	data := gin.H{}
	for _, name := range names {
		usage := pools[name]
		if usage.HasData {
			usage.Advice = pgdiag.PoolAdvice(usage)
		}
		data[name] = poolUsageJSON(usage)
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      data,
		"prom_note": note,
	})
}

func poolUsageJSON(usage pgdiag.PoolUsage) gin.H {
	item := gin.H{"pool": usage.HasData}
	if !usage.HasData {
		return item
	}
	item["pool_current"] = usage.Current
	item["pool_active"] = usage.Active
	item["pool_idle"] = usage.Idle
	item["pool_pending"] = usage.Pending
	item["pool_max"] = usage.Max
	item["pool_min"] = usage.Min
	item["pool_active_max_7d"] = usage.ActiveMax7d
	item["pool_pending_max_7d"] = usage.PendingMax7d
	item["pool_advice"] = usage.Advice
	item["pool_max_limit"] = usage.MaxLimit
	item["idle_samples"] = usage.IdleSamples
	item["samples"] = usage.Samples
	return item
}

func (h *InsightHandler) topologyProm() *promclient.Client {
	if h == nil || h.Store == nil {
		return nil
	}
	base, _ := h.Store.GetGlobalArg("awr_prometheus_base_url")
	token, _ := h.Store.GetGlobalArg("awr_prometheus_auth_token")
	partial, _ := h.Store.GetGlobalArg("awr_prometheus_partial_response")
	dedup, _ := h.Store.GetGlobalArg("awr_prometheus_dedup")
	return promclient.New(promclient.Config{
		BaseURL:         base,
		AuthToken:       token,
		PartialResponse: strings.EqualFold(strings.TrimSpace(partial), "true"),
		Dedup:           strings.EqualFold(strings.TrimSpace(dedup), "true"),
		Timeout:         8 * time.Second,
	})
}
