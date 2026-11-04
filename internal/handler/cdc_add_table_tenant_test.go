package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func TestPrepareCDCCreate_AuditSeesCrossTenantParent(t *testing.T) {
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
	if ok, _ := db.UserIsPlatformAdmin(admin.ID); !ok {
		t.Skip("admin is not platform admin")
	}

	suf := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	ownerID, jwtID := "tenant-cdcat-own-"+suf, "tenant-cdcat-jwt-"+suf
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range [][2]string{{ownerID, "o"}, {jwtID, "j"}} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			row[0], "cdcat"+row[1]+suf, "cdc加表租户"+row[1]+suf, store.TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}
	inst := &store.Instance{
		TenantID: ownerID, Name: "cdcat-" + suf, DBType: "postgres",
		DomainName: "cdcat-" + suf + ".example", Host: "cdcat-" + suf + ".example", Port: 5432,
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	parent := &store.CDCTask{
		TenantID: ownerID, Name: "cdcat-init-" + suf, TaskType: "init", Status: store.StatusRunning,
		SourceID: inst.ID, SourceDB: "db_bill_0",
		Description: encodeCDCExtraJSON("", map[string]any{"tables": "tbl_old"}),
	}
	if err := db.CreateCDC(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_cdc_tasks WHERE id=? OR parent_id=?`, parent.ID, parent.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, ownerID, jwtID)
	})

	h := &CDCHandler{Store: db}
	gin.SetMode(gin.TestMode)

	newChild := func() *store.CDCTask {
		return &store.CDCTask{
			Name: "cdcat-add-" + suf, TaskType: "add_table", ParentID: parent.ID,
			Description: encodeCDCExtraJSON("", map[string]any{"tables": "tbl_new"}),
		}
	}

	// 非审计：JWT 在另一租户 → 父任务不可见
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Set(middleware.CtxTenantID, jwtID)
		c.Set(middleware.CtxUserID, admin.ID)
		c.Set(middleware.CtxUsername, admin.Username)
		c.Set(middleware.CtxImpersonate, false)
		child := newChild()
		err := h.prepareCDCCreate(c, child)
		if err == nil || !strings.Contains(err.Error(), "父任务不存在") {
			t.Fatalf("non-audit want 父任务不存在, got %v", err)
		}
	}

	// 平台审计：列表全平台可见，父任务可用，子任务继承父租户
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Set(middleware.CtxTenantID, jwtID)
		c.Set(middleware.CtxUserID, admin.ID)
		c.Set(middleware.CtxUsername, admin.Username)
		c.Set(middleware.CtxImpersonate, true)
		c.Set(ctxCDCAddTablesPrefiltered, true)
		child := newChild()
		if err := h.prepareCDCCreate(c, child); err != nil {
			t.Fatalf("audit prepare: %v", err)
		}
		if child.TenantID != ownerID {
			t.Fatalf("child.TenantID=%q want %q", child.TenantID, ownerID)
		}
		if child.SourceID != inst.ID || child.SourceDB != parent.SourceDB {
			t.Fatalf("child source not copied from parent: %+v", child)
		}
	}
}

func TestBatchAddTable_AuditInheritsParentTenant(t *testing.T) {
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
	if ok, _ := db.UserIsPlatformAdmin(admin.ID); !ok {
		t.Skip("admin is not platform admin")
	}

	prevApproval, _ := db.GetGlobalArg("sync_approval_enabled")
	_ = db.SetGlobalArg("sync_approval_enabled", "false", "test")
	t.Cleanup(func() { _ = db.SetGlobalArg("sync_approval_enabled", prevApproval, "test") })

	suf := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	ownerID, jwtID := "tenant-cdbat-own-"+suf, "tenant-cdbat-jwt-"+suf
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range [][2]string{{ownerID, "o"}, {jwtID, "j"}} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			row[0], "cdbat"+row[1]+suf, "cdc批量加表"+row[1]+suf, store.TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}
	inst := &store.Instance{
		TenantID: ownerID, Name: "cdbat-" + suf, DBType: "postgres",
		DomainName: "cdbat-" + suf + ".example", Host: "cdbat-" + suf + ".example", Port: 5432,
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	parent := &store.CDCTask{
		TenantID: ownerID, Name: "cdbat-init-" + suf, TaskType: "init", Status: store.StatusRunning,
		SourceID: inst.ID, SourceDB: "db_bill_1",
		Description: encodeCDCExtraJSON("", map[string]any{"tables": "tbl_old"}),
	}
	if err := db.CreateCDC(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_cdc_sync_logs WHERE task_id IN (SELECT id FROM tbl_cdc_tasks WHERE id=? OR parent_id=?)`, parent.ID, parent.ID)
		_, _ = db.Exec(`DELETE FROM tbl_cdc_tasks WHERE id=? OR parent_id=?`, parent.ID, parent.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, ownerID, jwtID)
	})

	h := &CDCHandler{Store: db, Cfg: &config.Config{}}
	gin.SetMode(gin.TestMode)

	body, _ := json.Marshal(map[string]any{
		"name":               "cdbat-add-" + suf,
		"parent_ids":         []string{parent.ID},
		"tables":             []string{"tbl_order_tax"},
		"source_instance_id": inst.ID,
		"source_db":          parent.SourceDB,
		"description":        encodeCDCExtraJSON("", map[string]any{"tables": "tbl_order_tax"}),
	})

	call := func(audit bool) (int, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/cdc-sync/tasks/batch-add-table", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(middleware.CtxTenantID, jwtID)
		c.Set(middleware.CtxUserID, admin.ID)
		c.Set(middleware.CtxUsername, admin.Username)
		c.Set(middleware.CtxImpersonate, audit)
		h.BatchAddTable(c)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	if code, out := call(false); code != http.StatusBadRequest {
		t.Fatalf("non-audit: code=%d out=%v", code, out)
	}

	code, out := call(true)
	if code != http.StatusOK {
		t.Fatalf("audit batch: code=%d out=%v", code, out)
	}
	data, _ := out["data"].(map[string]any)
	if data == nil || int(data["created"].(float64)) != 1 || int(data["failed"].(float64)) != 0 {
		t.Fatalf("audit batch data=%v", data)
	}
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", items)
	}
	item, _ := items[0].(map[string]any)
	taskObj, _ := item["task"].(map[string]any)
	taskID, _ := taskObj["id"].(string)
	if taskID == "" {
		t.Fatalf("no task in item: %v", item)
	}
	got, err := db.GetCDC(taskID)
	if err != nil || got == nil {
		t.Fatalf("get child: %v", err)
	}
	if tid := db.RowTenantID("tbl_cdc_tasks", taskID); tid != ownerID {
		t.Fatalf("child tenant=%q want %q", tid, ownerID)
	}
	if got.ParentID != parent.ID || got.TaskType != "add_table" {
		t.Fatalf("child=%+v", got)
	}
	if visible, _ := db.GetCDCInTenant(taskID, jwtID); visible != nil {
		t.Fatal("child must not be visible under JWT audit tenant")
	}
	if visible, _ := db.GetCDCInTenant(taskID, ownerID); visible == nil {
		t.Fatal("child must be visible under parent tenant")
	}
}
