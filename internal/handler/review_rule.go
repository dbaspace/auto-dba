package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/goinception"
	"auto-dba/internal/store"
)

type ReviewRuleHandler struct {
	Store  *store.DB
	Secret string
}

func reviewEngineFrom(c *gin.Context, bodyEngine string) string {
	raw := strings.TrimSpace(bodyEngine)
	if raw == "" {
		raw = c.Query("engine")
	}
	if raw == "" {
		raw = c.Query("db_type")
	}
	return store.NormalizeReviewEngine(raw)
}

func (h *ReviewRuleHandler) giClient() (goinception.Config, bool) {
	s, err := h.Store.GetGoInceptionSettings(h.Secret)
	if err != nil || s == nil || !s.Ready() {
		return goinception.Config{}, false
	}
	return goinception.Config{Host: s.Host, Port: s.Port, User: s.User, Password: s.Password}, true
}

func (h *ReviewRuleHandler) syncGIVars(ctx context.Context, items []store.ReviewRule) error {
	cfg, ok := h.giClient()
	if !ok {
		return nil
	}
	var errs []string
	for _, it := range items {
		if err := goinception.SetVariable(ctx, cfg, it.Name, it.Value); err != nil {
			errs = append(errs, it.Name+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (h *ReviewRuleHandler) Ping(c *gin.Context) {
	cfg, ok := h.giClient()
	if !ok {
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "not_configured"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	if err := goinception.Ping(ctx, cfg); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": "ok"})
}

func (h *ReviewRuleHandler) GetGoInception(c *gin.Context) {
	cfg, err := h.Store.GetGoInceptionSettings(h.Secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg.Public()})
}

func (h *ReviewRuleHandler) UpdateGoInception(c *gin.Context) {
	var req struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	existing, _ := h.Store.GetGoInceptionSettings(h.Secret)
	cfg := &store.GoInceptionSettings{
		Enabled: req.Enabled,
		Host:    strings.TrimSpace(req.Host),
		Port:    req.Port,
		User:    strings.TrimSpace(req.User),
	}
	if req.Password != "" {
		cfg.Password = req.Password
	} else if existing != nil {
		cfg.Password = existing.Password
		cfg.PasswordEnc = existing.PasswordEnc
	}
	if cfg.Port <= 0 {
		cfg.Port = 4000
	}
	if err := h.Store.SetGoInceptionSettings(h.Secret, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": cfg.Public()})
}

func (h *ReviewRuleHandler) PingGoInception(c *gin.Context) {
	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	_ = c.ShouldBindJSON(&req)
	cfg := goinception.Config{
		Host:     strings.TrimSpace(req.Host),
		Port:     req.Port,
		User:     strings.TrimSpace(req.User),
		Password: req.Password,
	}
	saved, _ := h.Store.GetGoInceptionSettings(h.Secret)
	if cfg.Host == "" && saved != nil {
		cfg.Host = saved.Host
		if cfg.Port <= 0 {
			cfg.Port = saved.Port
		}
		if cfg.User == "" {
			cfg.User = saved.User
		}
	}
	if cfg.Password == "" && saved != nil {
		cfg.Password = saved.Password
	}
	if cfg.Port <= 0 {
		cfg.Port = 4000
	}
	if cfg.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先填写 goInception 地址"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	if err := goinception.Ping(ctx, cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "连接成功"})
}

func (h *ReviewRuleHandler) List(c *gin.Context) {
	engine := reviewEngineFrom(c, "")
	if engine == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "engine 须为 mysql / postgres / starrocks"})
		return
	}
	list, err := h.Store.ListReviewRules(engine)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, it := range list {
		items = append(items, gin.H{
			"name":     it.Name,
			"value":    it.Value,
			"label":    it.Label,
			"category": it.Category,
		})
	}
	if engine == "mysql" {
		c.JSON(http.StatusOK, gin.H{"data": items})
		return
	}
	ddl := []gin.H{}
	dml := []gin.H{}
	for _, it := range items {
		if store.ReviewRuleCategory(it["name"].(string), it["category"].(string)) == "dml" {
			dml = append(dml, it)
			continue
		}
		ddl = append(ddl, it)
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"ddl": ddl, "dml": dml}})
}

func (h *ReviewRuleHandler) Create(c *gin.Context) {
	var req struct {
		Engine   string `json:"engine"`
		DBType   string `json:"db_type"`
		Category string `json:"category"`
		Name     string `json:"name"`
		Label    string `json:"label"`
		Value    string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	engine := store.NormalizeReviewEngine(req.Engine)
	if engine == "" {
		engine = store.NormalizeReviewEngine(req.DBType)
	}
	if engine == "" {
		engine = reviewEngineFrom(c, "")
	}
	it, err := h.Store.CreateReviewRule(engine, req.Category, req.Name, req.Label, req.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if engine == "mysql" {
		if err := h.syncGIVars(c.Request.Context(), []store.ReviewRule{*it}); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": true, "data": it, "gi_synced": false, "message": "已写入本地，同步 goInception 失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": it, "gi_synced": true})
}

func (h *ReviewRuleHandler) Update(c *gin.Context) {
	var req struct {
		Engine string `json:"engine"`
		DBType string `json:"db_type"`
		Items  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	engine := store.NormalizeReviewEngine(req.Engine)
	if engine == "" {
		engine = store.NormalizeReviewEngine(req.DBType)
	}
	if engine == "" {
		engine = reviewEngineFrom(c, "")
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "没有需要保存的修改"})
		return
	}
	items := make([]store.ReviewRule, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, store.ReviewRule{Name: it.Name, Value: it.Value, Label: it.Label})
	}
	if err := h.Store.UpdateReviewRules(engine, items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if engine == "mysql" {
		if err := h.syncGIVars(c.Request.Context(), items); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": true, "gi_synced": false, "message": "已写入本地，同步 goInception 失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "gi_synced": true})
}
