package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/dqlexport"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
)

type ticketExportReq struct {
	FileFormat string `json:"file_format"`
}

const (
	dqlExportPhaseAccepted      = "accepted"
	dqlExportPhaseRouting       = "routing"
	dqlExportPhaseRemoteSubmit  = "remote_submit"
	dqlExportPhaseQuerying      = "querying"
	dqlExportPhaseTransferring  = "transferring"
	dqlExportPhaseReady         = "ready"
	dqlExportPhaseFailed        = "failed"
	dqlExportDirName            = "dql-exports"
	dqlExportRetain             = 7 * 24 * time.Hour
	dqlExportBGTimeout          = 15 * time.Minute
)

// Export POST /tickets/:id/export — 异步受理 DQL 导出（本机或就近执行器）。
func (h *TicketHandler) Export(c *gin.Context) {
	var req ticketExportReq
	_ = c.ShouldBindJSON(&req)
	format := strings.ToLower(strings.TrimSpace(req.FileFormat))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(c.Query("file_format")))
	}
	switch format {
	case "csv", "json", "sql":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "导出格式仅支持 csv / json / sql"})
		return
	}

	it, err := h.Store.GetIssueInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.IssueType != store.IssueTypeDQL {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅数据导出工单支持下载"})
		return
	}
	user := middleware.Username(c)
	if !strings.EqualFold(strings.TrimSpace(it.Submitter), user) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅限工单发起人可下载"})
		return
	}
	switch it.Status {
	case store.StatusApproved, store.StatusExecuted, store.StatusFailed:
	case store.StatusRunning:
		c.JSON(http.StatusBadRequest, gin.H{"message": "导出进行中，请稍候"})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "工单通过后才可下载"})
		return
	}

	rec, inst, err := h.resolveDQLExportTarget(c, it)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if _, err := dbprobe.RequireExportSQLLimit(it.SQLContent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	localCodes, execs, routeErr := h.executionCatalog()
	if routeErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": routeErr.Error()})
		return
	}
	dec := sqlexec.Decide(inst.IDC, localCodes, execs)
	if dec.Mode == sqlexec.ModeMissing {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": fmt.Sprintf("环境 %s 未绑定执行器，无法就近导出（不从本机硬跨网直连）", strings.TrimSpace(inst.IDC)),
		})
		return
	}

	_ = h.clearDQLExportFiles(it.ID)
	if err := h.patchDQLExportMeta(it.ID, func(ex *dqlApprovalExtra) {
		ex.ExportFormat = format
		ex.ExportFile = ""
		ex.ExportBytes = 0
		ex.ExportReadyAt = ""
		ex.ExportError = ""
		ex.ExportVia = ""
		ex.ExportExecutor = ""
		ex.ExportStage = dqlExportPhaseAccepted
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateIssueStatus(it.ID, store.StatusRunning, "dql export start "+format); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.saveProgress(it.ID, &store.ExecProgress{
		Total: 1, Phase: dqlExportPhaseAccepted, CurrentDB: rec.DBName,
		InstanceID: inst.ID, CurrentInstance: inst.Name, Host: resolveInstanceHost(inst, it.Domain),
		DBType: inst.DBType, Env: inst.IDC,
	})

	issueID := it.ID
	go h.runDQLExportAsync(issueID, format, dec)

	c.JSON(http.StatusAccepted, gin.H{"ok": true, "status": store.StatusRunning, "async": true, "message": "导出已开始，请在详情查看进度"})
}

// ExportFile GET /tickets/:id/export/file — 流式下载已生成的导出文件。
func (h *TicketHandler) ExportFile(c *gin.Context) {
	it, err := h.Store.GetIssueInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.IssueType != store.IssueTypeDQL {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅数据导出工单支持下载"})
		return
	}
	user := middleware.Username(c)
	if !strings.EqualFold(strings.TrimSpace(it.Submitter), user) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅限工单发起人可下载"})
		return
	}
	if it.Status == store.StatusRunning {
		c.JSON(http.StatusConflict, gin.H{"message": "导出尚未完成"})
		return
	}
	if it.Status == store.StatusFailed {
		ex, _ := parseDQLApprovalExtra(it.NeedRollback)
		msg := "导出失败，请重新导出"
		if ex != nil && strings.TrimSpace(ex.ExportError) != "" {
			msg = strings.TrimSpace(ex.ExportError)
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	if it.Status != store.StatusExecuted {
		c.JSON(http.StatusBadRequest, gin.H{"message": "导出文件未就绪"})
		return
	}
	ex, _ := parseDQLApprovalExtra(it.NeedRollback)
	if ex == nil || strings.TrimSpace(ex.ExportFile) == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "导出文件已失效，请重新导出"})
		return
	}
	path := filepath.Join(h.dqlExportDir(it.ID), filepath.Base(ex.ExportFile))
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"message": "导出文件已失效，请重新导出"})
		return
	}
	name := filepath.Base(ex.ExportFile)
	c.Header("Content-Disposition", `attachment; filename="`+name+`"; filename*=UTF-8''`+url.QueryEscape(name))
	c.File(path)
}

func (h *TicketHandler) runDQLExportAsync(issueID, format string, dec sqlexec.Decision) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("dql export panic %s: %v", issueID, r)
			h.failDQLExport(issueID, fmt.Sprintf("导出异常: %v", r))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), dqlExportBGTimeout)
	defer cancel()

	it, err := h.Store.GetIssue(issueID)
	if err != nil || it == nil {
		h.failDQLExport(issueID, "加载工单失败")
		return
	}
	rec, inst, err := h.resolveDQLExportTarget(nil, it)
	if err != nil {
		h.failDQLExport(issueID, err.Error())
		return
	}
	password, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		h.failDQLExport(issueID, "实例密码解密失败")
		return
	}
	host := resolveInstanceHost(inst, it.Domain)
	h.saveDQLExportProgress(it, inst, host, dqlExportPhaseRouting, "", dec)

	limit, err := dbprobe.RequireExportSQLLimit(it.SQLContent)
	if err != nil {
		h.failDQLExport(issueID, err.Error())
		return
	}

	dir := h.dqlExportDir(issueID)
	_ = os.MkdirAll(dir, 0o755)

	switch dec.Mode {
	case sqlexec.ModeRemote:
		if dec.Executor == nil {
			h.failDQLExport(issueID, "执行器配置无效")
			return
		}
		h.runDQLExportRemote(ctx, it, inst, rec, host, password, format, limit, dir, dec.Executor)
	default:
		h.runDQLExportLocal(ctx, it, inst, rec, host, password, format, limit, dir)
	}
}

func (h *TicketHandler) runDQLExportLocal(ctx context.Context, it *store.Issue, inst *store.Instance, rec *store.Database, host, password, format string, limit int, dir string) {
	h.saveDQLExportProgress(it, inst, host, dqlExportPhaseQuerying, "local", sqlexec.Decision{Mode: sqlexec.ModeLocal})
	_ = h.patchDQLExportMeta(it.ID, func(ex *dqlApprovalExtra) {
		ex.ExportVia = "local"
		ex.ExportStage = dqlExportPhaseQuerying
		ex.ExportExecutor = ""
	})
	res, err := dbprobe.ExecExport(ctx, inst, password, rec.DBName, it.SQLContent, limit)
	if err != nil {
		msg := err.Error()
		if ctx.Err() != nil {
			msg = "导出超时（异地库可能较慢，可缩小 LIMIT 后重试）: " + msg
		}
		h.failDQLExport(it.ID, msg)
		return
	}
	if res != nil {
		res.AppliedLimit = limit
		res.Limit = dbprobe.MaxExportLimit
	}
	body, ext, _ := dqlexport.Encode(res, format, dqlexport.TableNameFromSQL(it.SQLContent), strings.ToLower(inst.DBType))
	name := fmt.Sprintf("dql_%s_%s.%s", dqlexport.SanitizeFilePart(it.TicketNo), time.Now().Format("20060102_150405"), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		h.failDQLExport(it.ID, "写导出文件失败: "+err.Error())
		return
	}
	h.completeDQLExport(it.ID, name, int64(len(body)), "local", "")
}

func (h *TicketHandler) runDQLExportRemote(ctx context.Context, it *store.Issue, inst *store.Instance, rec *store.Database, host, password, format string, limit int, dir string, exec *sqlexec.Executor) {
	h.saveDQLExportProgress(it, inst, host, dqlExportPhaseRemoteSubmit, exec.Name, sqlexec.Decision{Mode: sqlexec.ModeRemote, Executor: exec})
	if err := sqlexec.Probe(ctx, exec.BaseURL, exec.Username, exec.Password); err != nil {
		h.failDQLExport(it.ID, "执行器不可达: "+err.Error())
		return
	}
	jobID, err := sqlexec.SubmitExport(ctx, exec.BaseURL, exec.Username, exec.Password, sqlexec.ExportRequest{
		Format: format, SQL: it.SQLContent, Limit: limit, TicketNo: it.TicketNo,
		Target: sqlexec.Target{
			InstanceID: inst.ID, InstanceName: inst.Name, Host: host, Port: inst.Port,
			Username: inst.Username, Password: password, DBName: rec.DBName, DBType: inst.DBType, Env: inst.IDC,
		},
	})
	if err != nil {
		h.failDQLExport(it.ID, "下发导出任务失败: "+err.Error())
		return
	}
	_ = h.patchDQLExportMeta(it.ID, func(ex *dqlApprovalExtra) {
		ex.ExportVia = "remote"
		ex.ExportExecutor = exec.Name
		ex.ExportStage = dqlExportPhaseRemoteSubmit
	})
	prog := &store.ExecProgress{
		Total: 1, Phase: dqlExportPhaseRemoteSubmit, CurrentDB: rec.DBName,
		InstanceID: inst.ID, CurrentInstance: inst.Name, Host: host, DBType: inst.DBType,
		Env: inst.IDC, Executor: exec.Name, JobID: jobID, ExecutorURL: exec.BaseURL, TargetUser: inst.Username,
	}
	h.saveProgress(it.ID, prog)

	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_ = sqlexec.StopExport(stopCtx, exec.BaseURL, exec.Username, exec.Password, jobID)
			cancel()
			h.failDQLExport(it.ID, "导出超时或已取消（异地执行）")
			return
		case <-ticker.C:
		}
		view, err := sqlexec.GetExport(ctx, exec.BaseURL, exec.Username, exec.Password, jobID)
		if err != nil {
			prog.GetFailures++
			prog.LastError = err.Error()
			prog.LastGetAt = time.Now().UTC().Format(time.RFC3339)
			h.saveProgress(it.ID, prog)
			if prog.GetFailures >= 8 {
				h.failDQLExport(it.ID, "轮询执行器失败: "+err.Error())
				return
			}
			continue
		}
		prog.GetFailures = 0
		prog.LastError = ""
		prog.LastGetAt = time.Now().UTC().Format(time.RFC3339)
		phase := mapExportAgentPhase(view.Phase, view.Status)
		prog.Phase = phase
		h.saveProgress(it.ID, prog)
		_ = h.patchDQLExportMeta(it.ID, func(ex *dqlApprovalExtra) {
			ex.ExportStage = phase
		})
		switch view.Status {
		case sqlexec.ExportFailed, sqlexec.ExportStopped:
			msg := strings.TrimSpace(view.Message)
			if msg == "" {
				msg = "执行器导出失败"
			}
			h.failDQLExport(it.ID, msg)
			return
		case sqlexec.ExportDone:
			h.saveDQLExportProgress(it, inst, host, dqlExportPhaseTransferring, exec.Name, sqlexec.Decision{Mode: sqlexec.ModeRemote, Executor: exec})
			name := strings.TrimSpace(view.Filename)
			if name == "" {
				name = fmt.Sprintf("dql_%s.%s", dqlexport.SanitizeFilePart(it.TicketNo), format)
			}
			name = filepath.Base(name)
			dest := filepath.Join(dir, name)
			n, err := sqlexec.FetchExportFile(ctx, exec.BaseURL, exec.Username, exec.Password, jobID, dest)
			if err != nil {
				h.failDQLExport(it.ID, "回传导出文件失败: "+err.Error())
				return
			}
			h.completeDQLExport(it.ID, name, n, "remote", exec.Name)
			return
		}
	}
}

func mapExportAgentPhase(agentPhase, status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case sqlexec.ExportFailed:
		return dqlExportPhaseFailed
	case sqlexec.ExportDone:
		return dqlExportPhaseQuerying
	}
	switch strings.ToLower(strings.TrimSpace(agentPhase)) {
	case sqlexec.ExportQuerying, sqlexec.ExportEncoding, sqlexec.ExportQueued:
		return dqlExportPhaseQuerying
	default:
		return dqlExportPhaseQuerying
	}
}

func (h *TicketHandler) completeDQLExport(issueID, filename string, bytes int64, via, executorName string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_ = h.patchDQLExportMeta(issueID, func(ex *dqlApprovalExtra) {
		ex.ExportFile = filename
		ex.ExportBytes = bytes
		ex.ExportReadyAt = now
		ex.ExportError = ""
		ex.ExportVia = via
		ex.ExportExecutor = executorName
		ex.ExportStage = dqlExportPhaseReady
	})
	_ = h.Store.UpdateIssueStatus(issueID, store.StatusExecuted, "dql export done")
	it, _ := h.Store.GetIssue(issueID)
	if it != nil {
		h.saveProgress(issueID, &store.ExecProgress{
			Total: 1, Done: 1, Phase: dqlExportPhaseReady,
			CurrentDB: it.DBName, InstanceID: it.InstanceID, Env: "",
			Executor: executorName,
		})
	}
}

func (h *TicketHandler) failDQLExport(issueID, msg string) {
	msg = strings.TrimSpace(msg)
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	_ = h.clearDQLExportFiles(issueID)
	_ = h.patchDQLExportMeta(issueID, func(ex *dqlApprovalExtra) {
		ex.ExportError = msg
		ex.ExportFile = ""
		ex.ExportBytes = 0
		ex.ExportReadyAt = ""
		ex.ExportStage = dqlExportPhaseFailed
	})
	_ = h.Store.UpdateIssueStatus(issueID, store.StatusFailed, msg)
	h.saveProgress(issueID, &store.ExecProgress{Total: 1, Failed: 1, Phase: dqlExportPhaseFailed, LastError: msg})
	if it, err := h.Store.GetIssue(issueID); err == nil && it != nil {
		NotifyIssueExecFailedAsync(h.Store, h.Secret, it, msg)
	}
}

func (h *TicketHandler) saveDQLExportProgress(it *store.Issue, inst *store.Instance, host, phase, executorName string, dec sqlexec.Decision) {
	if it == nil || inst == nil {
		return
	}
	p := &store.ExecProgress{
		Total: 1, Phase: phase, CurrentDB: it.DBName,
		InstanceID: inst.ID, CurrentInstance: inst.Name, Host: host,
		DBType: inst.DBType, Env: inst.IDC, Executor: executorName,
	}
	if dec.Executor != nil {
		p.ExecutorURL = dec.Executor.BaseURL
		if p.Executor == "" {
			p.Executor = dec.Executor.Name
		}
	}
	h.saveProgress(it.ID, p)
	_ = h.patchDQLExportMeta(it.ID, func(ex *dqlApprovalExtra) {
		ex.ExportStage = phase
		if executorName != "" {
			ex.ExportExecutor = executorName
		}
	})
}

func (h *TicketHandler) patchDQLExportMeta(issueID string, fn func(*dqlApprovalExtra)) error {
	it, err := h.Store.GetIssue(issueID)
	if err != nil || it == nil {
		return fmt.Errorf("工单不存在")
	}
	ex, err := parseDQLApprovalExtra(it.NeedRollback)
	if err != nil {
		ex = &dqlApprovalExtra{}
	}
	fn(ex)
	raw, err := ex.encode()
	if err != nil {
		return err
	}
	return h.Store.UpdateIssueNeedRollback(issueID, raw)
}

func (h *TicketHandler) resolveDQLExportTarget(c *gin.Context, it *store.Issue) (*store.Database, *store.Instance, error) {
	var rec *store.Database
	var err error
	tid := ""
	if c != nil {
		tid = effectiveListTenantID(h.Store, c)
	} else {
		tid = strings.TrimSpace(it.TenantID)
	}
	if strings.TrimSpace(it.DatabaseID) != "" {
		if tid != "" {
			rec, err = h.Store.GetDatabaseInTenant(it.DatabaseID, tid)
		} else {
			rec, err = h.Store.GetDatabase(it.DatabaseID)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if rec == nil && it.InstanceID != "" && it.DBName != "" {
		rec, err = h.Store.FindDatabase(it.InstanceID, it.DBName)
		if err != nil {
			return nil, nil, err
		}
	}
	if rec == nil {
		return nil, nil, fmt.Errorf("库未登记，无法导出")
	}
	var inst *store.Instance
	if c != nil {
		inst, err = resolveInstanceInTenant(h.Store, c, rec.InstanceID)
	} else {
		inst, err = h.Store.GetInstance(rec.InstanceID)
	}
	if err != nil || inst == nil {
		return nil, nil, fmt.Errorf("所属实例不存在")
	}
	return rec, inst, nil
}

func (h *TicketHandler) dqlExportRoot() string {
	root := filepath.Join("data", dqlExportDirName)
	_ = os.MkdirAll(root, 0o755)
	return root
}

func (h *TicketHandler) dqlExportDir(issueID string) string {
	return filepath.Join(h.dqlExportRoot(), strings.TrimSpace(issueID))
}

func (h *TicketHandler) clearDQLExportFiles(issueID string) error {
	dir := h.dqlExportDir(issueID)
	return os.RemoveAll(dir)
}

// ReapDQLExportFiles 清理过期导出目录（启动时可调）。
func (h *TicketHandler) ReapDQLExportFiles() {
	root := h.dqlExportRoot()
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-dqlExportRetain)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.ModTime().Before(cutoff) {
			_ = os.RemoveAll(p)
		}
	}
}

// 兼容旧测试/调用：同步编码辅助（已迁至 dqlexport）。
func encodeExport(res *dbprobe.QueryResult, format, table, dbType string) ([]byte, string, string) {
	return dqlexport.Encode(res, format, table, dbType)
}

func exportTableName(sqlText string) string {
	return dqlexport.TableNameFromSQL(sqlText)
}

func sanitizeFilePart(s string) string {
	return dqlexport.SanitizeFilePart(s)
}
