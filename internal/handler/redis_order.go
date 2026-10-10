package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/middleware"
	"auto-dba/internal/rediscmd"
	"auto-dba/internal/redisquery"
	"auto-dba/internal/store"
)

type RedisHandler struct {
	Store  *store.DB
	Secret string
}

func (h *RedisHandler) loadRules() (*rediscmd.RulesConfig, error) {
	return h.Store.GetRedisCommandRules()
}

func (h *RedisHandler) Catalog(c *gin.Context) {
	saved, err := h.loadRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      rediscmd.EnabledCatalog(saved),
		"blacklist": rediscmd.MergeRules(saved).Blacklist,
	})
}

func (h *RedisHandler) GetCommandRules(c *gin.Context) {
	merged, err := h.Store.GetRedisCommandRulesMerged()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"blacklist": merged.Blacklist,
			"commands":  rediscmd.EffectiveCatalog(&merged),
		},
	})
}

func (h *RedisHandler) UpdateCommandRules(c *gin.Context) {
	var req rediscmd.RulesConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	// 允许只传 commands 覆盖项 + blacklist；先与默认合并再存完整快照，便于下次读取
	merged := rediscmd.MergeRules(&req)
	if err := h.Store.SaveRedisCommandRules(merged); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"blacklist": merged.Blacklist,
			"commands":  rediscmd.EffectiveCatalog(&merged),
		},
	})
}

func (h *RedisHandler) Instances(c *gin.Context) {
	list, err := h.Store.ListInstancesInTenant(effectiveListTenantID(h.Store, c), "redis", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *RedisHandler) Create(c *gin.Context) {
	var req struct {
		InstanceID  string `json:"instance_id"`
		DBName      string `json:"db_name"`
		Command     string `json:"command"`
		Title       string `json:"title"`
		AssignedTo  string `json:"assigned_to"`
		Description string `json:"description"`
		Env         string `json:"env"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	instanceID := strings.TrimSpace(req.InstanceID)
	command := strings.TrimSpace(req.Command)
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择 Redis 实例"})
		return
	}
	rules, err := h.loadRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := rediscmd.ValidateScriptWith(rules, command); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, instanceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例不存在"})
		return
	}
	if !strings.EqualFold(strings.TrimSpace(inst.DBType), "redis") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持 db_type=redis 的实例"})
		return
	}
	dbName := strings.TrimSpace(req.DBName)
	if dbName == "" {
		dbName = "db0"
	}
	if _, err := redisquery.ParseDBIndex(dbName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	assigned, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.AssignedTo)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	stats := rediscmd.ParseScriptWith(rules, command)
	cmdName := ""
	if len(stats.Valid) > 0 {
		parts := rediscmd.ParseLine(stats.Valid[0])
		if len(parts) > 0 {
			cmdName = strings.ToUpper(parts[0])
		}
	}
	title := strings.TrimSpace(req.Title)
	remark := strings.TrimSpace(req.Description)
	extra, _ := json.Marshal(map[string]any{
		"env":           strings.TrimSpace(req.Env),
		"db_index":      dbName,
		"command_name":  cmdName,
		"command_count": len(stats.Valid),
		"highest_crud":  stats.HighestCRUD,
		"remark":        remark,
	})
	desc := remark
	if desc == "" {
		desc = title
	}
	if desc == "" {
		desc = fmt.Sprintf("Redis %s (%d 条)", cmdName, len(stats.Valid))
	}
	issue := &store.Issue{
		ID:           uuid.NewString(),
		TicketNo:     "R" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:    store.IssueTypeRedis,
		InstanceID:   instanceID,
		DBName:       dbName,
		SQLContent:   command,
		Description:  desc,
		Submitter:    middleware.Username(c),
		AssignedTo:   assigned,
		NeedRollback: string(extra),
	}
	stampTenantID(c, func(tid string) {
		if issue != nil { issue.TenantID = tid }
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": issue})
}
