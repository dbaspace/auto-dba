package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/mysqldiag"
	"auto-dba/internal/pgdiag"
	"auto-dba/internal/redisquery"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func (h *TicketHandler) actorCanStopIssue(c *gin.Context, it *store.Issue) bool {
	if h == nil || h.Store == nil || c == nil || it == nil {
		return false
	}
	user := strings.TrimSpace(middleware.Username(c))
	if user != "" && assigneeIs(it.AssignedTo, user) {
		return true
	}
	if middleware.IsAdminRole(middleware.Role(c)) {
		return true
	}
	if ok, err := h.Store.UserIsAdmin(middleware.UserID(c)); err == nil && ok {
		return true
	}
	return h.userInDBATenant(middleware.UserID(c))
}

func assigneeIs(assigned, user string) bool {
	user = strings.TrimSpace(user)
	if user == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(assigned), user) {
		return true
	}
	for _, name := range store.ExpandApproverNames([]string{assigned}) {
		if strings.EqualFold(name, user) {
			return true
		}
	}
	return false
}

func (h *TicketHandler) userInDBATenant(userID string) bool {
	if h == nil || h.Store == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	list, err := h.Store.ListUserTenants(userID)
	if err != nil {
		return false
	}
	for _, t := range list {
		if strings.EqualFold(strings.TrimSpace(t.Code), store.DBATenantCode) {
			return true
		}
	}
	return false
}

func (h *TicketHandler) Stop(c *gin.Context) {
	it, err := h.loadIssueForActor(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.Status != store.StatusRunning {
		c.JSON(http.StatusBadRequest, gin.H{"message": "当前状态不可终止"})
		return
	}
	if !h.actorCanStopIssue(c, it) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅审核人、admin 或 DBA 租户成员可终止执行"})
		return
	}
	user := middleware.Username(c)
	killErr := h.killTicketSession(c.Request.Context(), it)
	if killErr != nil {
		if again := h.killTicketSession(c.Request.Context(), it); again != nil {
			killErr = again
		} else {
			killErr = nil
		}
	}
	if err := h.markStopped(it, user, killErr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	cancelExec(it.ID)
	h.stopLinkedRunner(it)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已终止执行"})
}

func (h *TicketHandler) killTicketSession(ctx context.Context, it *store.Issue) error {
	if h == nil || it == nil || it.ExecProgress == nil || it.ExecProgress.SessionID <= 0 {
		return nil
	}
	p := it.ExecProgress
	inst, err := h.Store.GetInstance(p.InstanceID)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("实例不存在")
	}
	if strings.TrimSpace(p.Host) != "" {
		inst.Host = p.Host
	}
	plain := ""
	if strings.TrimSpace(inst.Password) != "" {
		plain, err = secret.Decrypt(h.Secret, inst.Password)
		if err != nil {
			return fmt.Errorf("解密实例密码失败: %w", err)
		}
	}
	engine := strings.TrimSpace(p.Engine)
	if engine == "" {
		engine = ticketexec.EngineOf(inst.DBType)
	}
	switch engine {
	case "postgres":
		return pgdiag.TerminateBackend(ctx, inst, plain, p.SessionID)
	case "mysql", "starrocks":
		return mysqldiag.Kill(ctx, inst, plain, p.SessionID)
	case "redis":
		return redisquery.KillClientID(ctx, inst, redisquery.Options{
			Username: inst.Username, Password: plain, Host: inst.Host,
		}, p.CurrentDB, p.SessionID)
	default:
		return fmt.Errorf("数据库类型 %s 没有对应的终止命令", engine)
	}
}

func (h *TicketHandler) markStopped(it *store.Issue, user string, killErr error) error {
	res := ticketexec.Parse(it.ExecuteResult)
	if res == nil {
		res = &ticketexec.Result{StartedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	res.Success = false
	msg := "由 " + strings.TrimSpace(user) + " 终止执行"
	if p := it.ExecProgress; p != nil && p.SessionID > 0 {
		kind := p.Engine
		if kind == "" {
			kind = "数据库"
		}
		msg += fmt.Sprintf("，%s 会话 %d", kind, p.SessionID)
	}
	if killErr != nil {
		msg += "；终止会话失败: " + killErr.Error()
	}
	res.Message = msg
	res.Summary = msg
	res.EndedAt = time.Now().UTC().Format(time.RFC3339)
	if p := it.ExecProgress; p != nil && strings.TrimSpace(p.CurrentDB) != "" && !execHasDB(res, p.InstanceID, p.CurrentDB) {
		res.Records = append(res.Records, ticketexec.Record{
			InstanceID: p.InstanceID, InstanceName: p.CurrentInstance, Host: p.Host, DB: p.CurrentDB,
			Status: "failed", ErrorMessage: "执行已终止", SQL: "当前库已终止",
			Order: len(res.Records) + 1,
		})
	}
	if err := h.Store.UpdateIssueExec(it.ID, store.StatusStopped, ticketexec.ToJSON(res)); err != nil {
		return err
	}
	return h.Store.ClearIssueProgress(it.ID)
}

func execHasDB(res *ticketexec.Result, instanceID, dbName string) bool {
	if res == nil {
		return false
	}
	for _, rec := range res.Records {
		if strings.EqualFold(strings.TrimSpace(rec.DB), strings.TrimSpace(dbName)) &&
			(instanceID == "" || strings.TrimSpace(rec.InstanceID) == "" || rec.InstanceID == instanceID) {
			return true
		}
	}
	return false
}

func (h *TicketHandler) stopLinkedRunner(it *store.Issue) {
	if h == nil || it == nil {
		return
	}
	switch it.IssueType {
	case store.IssueTypeArchive:
		if h.Archive == nil {
			return
		}
		t, err := h.Store.GetArchiveByIssueID(it.ID)
		if err != nil || t == nil {
			return
		}
		h.Archive.Stop(t.ID)
		_ = h.Store.UpdateArchiveState(t.ID, store.StatusStopped, t.ApprovalStatus)
	case store.IssueTypeMigration:
		if h.Migration == nil {
			return
		}
		t, err := h.Store.GetMigrationByIssueID(it.ID)
		if err != nil || t == nil {
			return
		}
		h.Migration.Cancel(t.ID)
		t.Status = store.StatusStopped
		t.ErrorMessage = "执行已终止"
		t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = h.Store.UpdateMigration(t)
	case store.IssueTypeCDC:
		list, err := h.Store.ListCDCByIssueID(it.ID)
		if err != nil {
			return
		}
		for i := range list {
			if h.CDC != nil {
				h.CDC.Stop(list[i].ID)
			}
			_ = h.Store.UpdateCDCStatus(list[i].ID, store.StatusStopped, list[i].ApprovalStatus)
		}
	case store.IssueTypeFlashback:
		t, err := h.Store.GetFlashbackByIssueID(it.ID)
		if err != nil || t == nil {
			return
		}
		h.fbEngine().Stop(t.ID)
	default:
	}
}
