package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/instoverview"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// HostDiskTrend 提交后台磁盘分析。同一租户已有任务在跑时返回该任务，不重复查询。
func (h *OverviewHandler) HostDiskTrend(c *gin.Context) {
	var req struct {
		Domain string `json:"domain"`
		Host   string `json:"host"`
		Env    string `json:"env"`
		Start  string `json:"start"`
		End    string `json:"end"`
	}
	_ = c.ShouldBindJSON(&req)
	now := time.Now()
	start, end, days, err := instoverview.ParseTrendWindow(req.Start, req.End, now)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	tenantID := middleware.TenantID(c)
	h.mu.Lock()
	if h.trendJobs == nil {
		h.trendJobs = map[string]*trendJob{}
	}
	for _, job := range h.trendJobs {
		if job.TenantID == tenantID && job.Status == "running" {
			view := job.view()
			h.mu.Unlock()
			c.JSON(http.StatusOK, view)
			return
		}
	}
	job := &trendJob{ID: uuid.NewString(), TenantID: tenantID, Status: "running"}
	h.trendJobs[job.ID] = job
	view := job.view()
	h.mu.Unlock()

	tid := effectiveListTenantID(h.Store, c)
	go h.runHostDiskTrend(job, trendRequest{
		ListTenant: tid,
		TenantID:   tenantID,
		UserID:     middleware.UserID(c),
		Username:   middleware.Username(c),
		Domain:     strings.TrimSpace(req.Domain),
		Host:       store.HostAddrKey(req.Host),
		Env:        strings.TrimSpace(req.Env),
		Start:      start,
		End:        end,
		Days:       days,
	})
	c.JSON(http.StatusOK, view)
}

// HostDiskTrendJob 返回后台分析进度。完成后带 report_id。
func (h *OverviewHandler) HostDiskTrendJob(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	h.mu.Lock()
	defer h.mu.Unlock()
	job := h.trendJobs[id]
	if job == nil || job.TenantID != middleware.TenantID(c) {
		c.JSON(http.StatusNotFound, gin.H{"message": "分析任务不存在"})
		return
	}
	c.JSON(http.StatusOK, job.view())
}

type trendJob struct {
	ID       string
	TenantID string
	Status   string
	Message  string
	ReportID string
}

func (j *trendJob) view() gin.H {
	return gin.H{"job_id": j.ID, "status": j.Status, "message": j.Message, "report_id": j.ReportID}
}

type trendRequest struct {
	ListTenant string
	TenantID   string
	UserID     string
	Username   string
	Domain     string
	Host       string
	Env        string
	Start      time.Time
	End        time.Time
	Days       int
}

func (h *OverviewHandler) runHostDiskTrend(job *trendJob, req trendRequest) {
	fail := func(msg string) {
		h.mu.Lock()
		job.Status = "error"
		job.Message = msg
		h.mu.Unlock()
	}
	rows, err := h.listRows(req.ListTenant, req.UserID, "", req.Env, "")
	if err != nil {
		fail(err.Error())
		return
	}
	var hosts []overviewRow
	for _, row := range rows {
		if !store.IsHostInstanceType(row.DBType) {
			continue
		}
		if req.Domain != "" && !strings.EqualFold(strings.TrimSpace(row.Domain), req.Domain) {
			continue
		}
		if req.Host != "" && store.HostAddrKey(row.Host) != req.Host {
			continue
		}
		hosts = append(hosts, row)
	}
	items := make([]instoverview.TrendItem, 0, len(hosts))
	var loadNotes []string
	if len(hosts) > 0 {
		cli, err := h.promClient()
		if err != nil || cli == nil {
			fail("未配置 Prometheus（全局参数 awr_prometheus_base_url），无法分析磁盘增长")
			return
		}
		raw := make([]string, 0, len(hosts))
		for _, row := range hosts {
			raw = append(raw, row.Host)
		}
		addrs := instoverview.HostAddrs(raw)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cur, err := queryHostFS(ctx, cli, addrs, req.End)
		if err != nil {
			fail(fmt.Sprintf("查询结束时间磁盘用量失败：%v", err))
			return
		}
		prev, err := queryHostFS(ctx, cli, addrs, req.Start)
		if err != nil {
			fail(fmt.Sprintf("查询开始时间磁盘用量失败：%v", err))
			return
		}
		trends := instoverview.HostDiskTrends(cur, prev, float64(req.Days))
		pressures, notes := instoverview.QueryHostPressure(ctx, cli, addrs, req.Start, req.End)
		loadNotes = notes
		for _, row := range hosts {
			key := store.HostAddrKey(row.Host)
			t := trends[key]
			items = append(items, instoverview.TrendItem{
				ID: row.ID, Name: row.Name, App: row.App, Domain: row.Domain, Host: row.Host, EnvName: row.EnvName,
				Data: t.Data, System: t.System, Pressure: pressures[key],
			})
		}
		instoverview.SortTrendItems(items)
	}
	summary := instoverview.SummarizeTrends(items, req.Days)
	if len(loadNotes) > 0 {
		summary.Conclusions = append(summary.Conclusions, loadNotes...)
	}
	envName := h.reportEnvName(req.Env)
	scope := map[string]any{
		"domain": req.Domain, "host": req.Host, "env": req.Env, "env_name": envName,
		"days": req.Days, "host_count": len(items),
		"start": req.Start.Format(time.RFC3339), "end": req.End.Format(time.RFC3339),
	}
	payload := map[string]any{
		"days": req.Days, "items": items, "summary": summary,
		"generated_at": req.End.Format(time.RFC3339),
		"start":        req.Start.Format(time.RFC3339),
		"end":          req.End.Format(time.RFC3339),
	}
	id, err := h.saveAnalysisReport(req.TenantID, req.UserID, req.Username, store.AnalysisKindHostDisk, instoverview.HostDiskReportTitle(req.Domain, req.Host, envName, len(items)), summary.Conclusions, scope, payload)
	if err != nil {
		log.Printf("save host disk report: %v", err)
		fail("报告未保存：" + err.Error())
		return
	}
	h.mu.Lock()
	job.Status = "done"
	job.ReportID = id
	h.mu.Unlock()
}

func (h *OverviewHandler) reportEnvName(env string) string {
	switch env {
	case "":
		return ""
	case "unset":
		return "未设置环境"
	}
	envs, err := h.Store.ListEnvironments()
	if err != nil {
		return env
	}
	for _, e := range envs {
		if e.ID != env {
			continue
		}
		if name := strings.TrimSpace(e.Name); name != "" {
			return name
		}
		if code := strings.TrimSpace(e.Code); code != "" {
			return code
		}
	}
	return env
}

func (h *OverviewHandler) saveAnalysisReport(tenantID, userID, username, kind, title string, conclusions []string, scope, payload any) (string, error) {
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return h.Store.InsertAnalysisReport(&store.AnalysisReport{
		TenantID: tenantID,
		Kind:     kind,
		UserID:   userID,
		Username: username,
		Title:    title,
		Summary:  strings.Join(conclusions, "\n"),
		Scope:    string(scopeJSON),
		Payload:  string(payloadJSON),
	})
}

// ListAnalysisReports 返回本租户某类分析的最近报告，不含全文。
func (h *OverviewHandler) ListAnalysisReports(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	if kind == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "缺少 kind"})
		return
	}
	items, err := h.Store.ListAnalysisReports(middleware.TenantID(c), kind, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if items == nil {
		items = []store.AnalysisReportListItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// GetAnalysisReport 返回本租户的一份完整报告。
func (h *OverviewHandler) GetAnalysisReport(c *gin.Context) {
	r, err := h.Store.GetAnalysisReport(middleware.TenantID(c), strings.TrimSpace(c.Param("id")))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"message": "报告不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": r.ID, "kind": r.Kind, "title": r.Title, "summary": r.Summary,
		"scope":    json.RawMessage(emptyJSON(r.Scope, "{}")),
		"payload":  json.RawMessage(emptyJSON(r.Payload, "{}")),
		"username": r.Username, "created_at": r.CreatedAt,
	})
}

func emptyJSON(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
