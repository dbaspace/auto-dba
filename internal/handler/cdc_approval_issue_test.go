package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func TestReconcileCDCApprovalIssueRebindsPendingTicket(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config dsn")
	}
	db, err := store.OpenWithoutMigrate("postgres", dsn)
	if err != nil {
		t.Skip(err.Error())
	}
	t.Cleanup(func() { db.Close() })

	admin, err := db.GetUserByUsername("admin")
	if err != nil || admin == nil {
		t.Skip("admin user missing")
	}

	suf := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	tid := "tenant-cdcap-" + suf
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		tid, "cdcap"+suf, "cdcap"+suf, store.TenantStatusActive, now, now); err != nil {
		t.Fatal(err)
	}
	inst := &store.Instance{
		TenantID: tid, Name: "cdcap-" + suf, DBType: "postgres",
		DomainName: "cdcap-" + suf + ".example", Host: "cdcap-" + suf + ".example", Port: 5432,
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	parent := &store.CDCTask{
		TenantID: tid, Name: "cdcap-init-" + suf, TaskType: "init", Status: store.StatusRunning,
		SourceID: inst.ID, SourceDB: "db_bill",
	}
	if err := db.CreateCDC(parent); err != nil {
		t.Fatal(err)
	}
	oldIssue := &store.Issue{
		IssueType: store.IssueTypeCDC, Type: store.IssueTypeCDC, Status: store.StatusApproved,
		InstanceID: inst.ID, DBName: "db_bill", SQLContent: "cdc_sync init " + parent.ID,
		Submitter: "someone", AssignedTo: admin.Username, TenantID: tid,
	}
	if err := db.CreateIssue(oldIssue); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateCDCIssueID(parent.ID, oldIssue.ID); err != nil {
		t.Fatal(err)
	}
	child := &store.CDCTask{
		TenantID: tid, Name: "cdcap-add-" + suf, TaskType: "add_table",
		Status: store.StatusPending, ApprovalStatus: store.StatusPending,
		SourceID: inst.ID, SourceDB: "db_bill", ParentID: parent.ID, IssueID: oldIssue.ID,
		Description: encodeCDCExtraJSON("", map[string]any{"assigned_to": admin.Username, "created_by": "wangxiaotai"}),
	}
	if err := db.CreateCDC(child); err != nil {
		t.Fatal(err)
	}
	pending := &store.Issue{
		IssueType: store.IssueTypeCDC, Type: store.IssueTypeCDC, Status: store.StatusPending,
		InstanceID: inst.ID, DBName: "db_bill",
		SQLContent: "cdc_sync add_table " + child.ID + " " + child.Name,
		Submitter:  "wangxiaotai", AssignedTo: admin.Username, TenantID: tid,
	}
	if err := db.CreateIssue(pending); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE id IN (?,?)`, oldIssue.ID, pending.ID)
		_, _ = db.Exec(`DELETE FROM tbl_cdc_tasks WHERE id=? OR parent_id=? OR id=?`, parent.ID, parent.ID, child.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, tid)
	})

	h := &CDCHandler{Store: db}
	th := &TicketHandler{Store: db}
	gin.SetMode(gin.TestMode)

	rejectOld := func() {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/tickets/"+oldIssue.ID+"/approve", nil)
		c.Params = gin.Params{{Key: "id", Value: oldIssue.ID}}
		c.Set(middleware.CtxTenantID, tid)
		c.Set(middleware.CtxUsername, admin.Username)
		c.Set(middleware.CtxUserID, admin.ID)
		th.Approve(c)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "仅待审批工单可批准") {
			t.Fatalf("old issue approve code=%d body=%s", w.Code, w.Body.String())
		}
	}
	rejectOld()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/cdc-sync/tasks/"+child.ID+"/resolve-approval-issue", nil)
	c.Params = gin.Params{{Key: "id", Value: child.ID}}
	c.Set(middleware.CtxTenantID, tid)
	c.Set(middleware.CtxUsername, admin.Username)
	c.Set(middleware.CtxUserID, admin.ID)
	h.ResolveApprovalIssue(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), pending.ID) {
		t.Fatalf("resolve code=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := db.GetCDC(child.ID)
	if got == nil || got.IssueID != pending.ID {
		t.Fatalf("rebind issue=%v want %s", got, pending.ID)
	}
	if got.IssueID == oldIssue.ID {
		t.Fatal("still bound to finished parent issue")
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/tickets/"+pending.ID+"/approve", nil)
	c.Params = gin.Params{{Key: "id", Value: pending.ID}}
	c.Set(middleware.CtxTenantID, tid)
	c.Set(middleware.CtxUsername, admin.Username)
	c.Set(middleware.CtxUserID, admin.ID)
	th.Approve(c)
	if w.Code != http.StatusOK {
		t.Fatalf("pending approve code=%d body=%s", w.Code, w.Body.String())
	}
	after, _ := db.GetIssue(pending.ID)
	if after == nil || after.Status != store.StatusApproved {
		t.Fatalf("issue after=%+v", after)
	}
}
