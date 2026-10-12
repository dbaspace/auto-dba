package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/aiops"
	"auto-dba/internal/store"
)

// ListKnowledge AIOps 确定性知识库（内置案例目录，含 PostgreSQL）。
// GET /aiops/knowledge?engine=postgres|mysql|host|all
func (h *AlertHandler) ListKnowledge(c *gin.Context) {
	engine := strings.TrimSpace(c.Query("engine"))
	list := aiops.KnowledgeCasesByEngine(engine)
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"engine": engine,
			"total":  len(list),
			"items":  list,
		},
	})
}

// ListKnowledgeDB 库内案例（SeedSystemCases / 手工 SQL）。
// GET /aiops/knowledge/db?engine=mysql|postgres|redis|starrocks|host|all&q=&limit=
func (h *AlertHandler) ListKnowledgeDB(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"total": 0, "filtered": 0, "items": []any{}, "engine": c.Query("engine")}})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "300"))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	q := strings.TrimSpace(c.Query("q"))
	engine := strings.TrimSpace(c.Query("engine"))
	rows, total, err := h.Store.ListSqlInsightCases(c.Request.Context(), 0, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	type item struct {
		ID                string `json:"id"`
		Fingerprint       string `json:"fingerprint"`
		MetricKey         string `json:"metric_key"`
		Severity          string `json:"severity"`
		Direction         string `json:"direction"`
		Title             string `json:"title"`
		RootCauseTemplate string `json:"root_cause_template"`
		ActionTemplate    string `json:"action_template"`
		AlertNames        string `json:"alert_names"`
		Engine            string `json:"engine,omitempty"`
		HitCount          int    `json:"hit_count"`
		ConfirmCount      int    `json:"confirm_count"`
		DismissCount      int    `json:"dismiss_count"`
		System            bool   `json:"system"`
	}
	var out []item
	ql := strings.ToLower(q)
	for _, r := range rows {
		if r == nil {
			continue
		}
		eng := aiops.InferCaseEngine(r.Fingerprint, r.Title, r.AlertNames, r.MetricKey)
		if !aiops.CaseMatchesEngine(engine, r.Fingerprint, r.Title, r.AlertNames, r.MetricKey) {
			continue
		}
		if ql != "" {
			hay := strings.ToLower(r.Title + " " + r.Fingerprint + " " + r.MetricKey + " " + r.AlertNames + " " + eng)
			if !strings.Contains(hay, ql) {
				continue
			}
		}
		out = append(out, item{
			ID: r.ID, Fingerprint: r.Fingerprint, MetricKey: r.MetricKey, Severity: r.Severity,
			Direction: r.Direction, Title: r.Title, RootCauseTemplate: r.RootCauseTemplate,
			ActionTemplate: r.ActionTemplate, AlertNames: r.AlertNames, Engine: eng,
			HitCount: r.HitCount, ConfirmCount: r.ConfirmCount, DismissCount: r.DismissCount, System: r.System,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"total": total, "filtered": len(out), "engine": engine, "items": out,
	}})
}

// LearnKnowledge 人工确认将大模型分析写入知识库。
// POST /aiops/knowledge/learn  body: {question, answer, title?, as_system?, merge_system?, confirm:true}
func (h *AlertHandler) LearnKnowledge(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "store 不可用"})
		return
	}
	var req struct {
		Question    string `json:"question"`
		Answer      string `json:"answer"`
		Title       string `json:"title"`
		AsSystem    *bool  `json:"as_system"`
		MergeSystem *bool  `json:"merge_system"`
		Confirm     bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !req.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请确认学习入库（confirm=true）"})
		return
	}
	asSys := true
	if req.AsSystem != nil {
		asSys = *req.AsSystem
	}
	merge := true
	if req.MergeSystem != nil {
		merge = *req.MergeSystem
	}
	svc := aiops.New(h.Store)
	// 用问题再检索一次，便于主题对齐
	rag, _ := svc.RAGSearch(c.Request.Context(), req.Question, 5)
	res, err := svc.ConfirmLearn(c.Request.Context(), req.Question, req.Answer, rag, aiops.LearnOptions{
		AsSystem: asSys, Title: req.Title, MergeSystem: merge,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// DeleteKnowledgeDB 删除库内案例（仅自定义/学习补充，禁止删系统种子）。
// DELETE /aiops/knowledge/db?fingerprint=learn|xxx
func (h *AlertHandler) DeleteKnowledgeDB(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "store 不可用"})
		return
	}
	fp := strings.TrimSpace(c.Query("fingerprint"))
	if fp == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "fingerprint 必填"})
		return
	}
	row, err := h.Store.GetSqlInsightCaseByFingerprint(c.Request.Context(), fp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "案例不存在"})
		return
	}
	if row.System && !strings.HasPrefix(fp, "learn|") {
		c.JSON(http.StatusForbidden, gin.H{"message": "系统案例不可删除"})
		return
	}
	if err := h.Store.DeleteSqlInsightCase(c.Request.Context(), fp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": fp}})
}

// MatchKnowledgePreview 按 alertname 试算案例对齐（不落库，用内置知识库）。
// GET /aiops/knowledge/match?alertname=PGConnHigh
func (h *AlertHandler) MatchKnowledgePreview(c *gin.Context) {
	alertName := strings.TrimSpace(c.Query("alertname"))
	if alertName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "alertname 必填"})
		return
	}
	var rows []*store.SqlInsightCaseRow
	for _, kc := range aiops.AllKnowledgeCases() {
		rows = append(rows, &store.SqlInsightCaseRow{
			Fingerprint: kc.Fingerprint, MetricKey: kc.MetricKey, Severity: kc.Severity,
			Direction: kc.Direction, Title: kc.Title,
			RootCauseTemplate: kc.RootCauseTemplate, ActionTemplate: kc.ActionTemplate,
			AlertNames: aiops.JoinAlertNames(kc.AlertNames),
		})
	}
	if h.Store != nil {
		if dbRows, _, err := h.Store.ListSqlInsightCases(c.Request.Context(), 0, 200); err == nil {
			rows = append(rows, dbRows...)
		}
	}
	a := &store.Alert{Labels: map[string]string{"alertname": alertName}, Service: "postgres"}
	hints := aiops.MatchCasesForAlert(rows, a, nil)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"alertname": alertName, "total": len(hints), "cases": hints}})
}

// ListIncidents 多告警关联（按实例+时间窗）。
// GET /aiops/incidents?window=30m
func (h *AlertHandler) ListIncidents(c *gin.Context) {
	window := strings.TrimSpace(c.DefaultQuery("window", "30m"))
	dur, err := time.ParseDuration(window)
	if err != nil || dur <= 0 {
		dur = 30 * time.Minute
	}
	tid := effectiveListTenantID(h.Store, c)
	list, err := h.aiopsSvc().CorrelateIncidents(c.Request.Context(), tid, dur)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"window": dur.String(), "total": len(list), "items": list}})
}

// RAGSearch 关键词检索知识库。
// POST /aiops/rag/search  body: {q, limit}
func (h *AlertHandler) RAGSearch(c *gin.Context) {
	var body struct {
		Q     string `json:"q"`
		Limit int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	hits, err := h.aiopsSvc().RAGSearch(c.Request.Context(), body.Q, body.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"q": body.Q, "total": len(hits), "items": hits}})
}
