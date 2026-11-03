package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/store"
)

func TestCDCAssignTenantRequiresAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CDCHandler{}
	for _, call := range []func(*gin.Context){h.AssignTenant, h.AssignTenantBatch} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/cdc-sync/tasks/x/assign-tenant", bytes.NewReader([]byte(`{"tenant_id":"t","ids":["x"]}`)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: "x"}}
		call(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestAssignCDCTaskTenantMovesTree(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config/config.yaml db.dsn to run")
	}
	db, err := store.OpenWithoutMigrate("postgres", dsn)
	if err != nil {
		t.Skip(err.Error())
	}
	t.Cleanup(func() { db.Close() })

	suf := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	fromID, toID, offID := "tenant-cdcasg-a-"+suf, "tenant-cdcasg-b-"+suf, "tenant-cdcasg-off-"+suf
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range [][3]string{{fromID, "a", store.TenantStatusActive}, {toID, "b", store.TenantStatusActive}, {offID, "o", store.TenantStatusDisabled}} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			row[0], "cdcasg"+row[1]+suf, "cdc划拨"+row[1]+suf, row[2], now, now); err != nil {
			t.Fatal(err)
		}
	}
	inst := &store.Instance{TenantID: fromID, Name: "cdcasg-" + suf, DBType: "postgres", DomainName: "cdcasg-" + suf + ".example", Host: "cdcasg-" + suf + ".example", Port: 5432}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	root := &store.CDCTask{TenantID: fromID, Name: "cdcasg-root-" + suf, TaskType: "init", Status: store.StatusRunning, SourceID: inst.ID, SourceDB: "db1"}
	if err := db.CreateCDC(root); err != nil {
		t.Fatal(err)
	}
	child := &store.CDCTask{TenantID: fromID, Name: "cdcasg-add-" + suf, TaskType: "add_table", Status: store.StatusRunning, SourceID: inst.ID, SourceDB: "db1", ParentID: root.ID}
	if err := db.CreateCDC(child); err != nil {
		t.Fatal(err)
	}
	grand := &store.CDCTask{TenantID: fromID, Name: "cdcasg-col-" + suf, TaskType: "alter_schema", Status: store.StatusRunning, SourceID: inst.ID, SourceDB: "db1", ParentID: child.ID}
	if err := db.CreateCDC(grand); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_cdc_tasks WHERE id IN (?,?,?)`, root.ID, child.ID, grand.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?,?)`, fromID, toID, offID)
	})

	if _, err := db.AssignCDCTaskTenant(root.ID, offID); err == nil {
		t.Fatal("disabled tenant should fail")
	}

	res, err := db.AssignCDCTaskTenant(child.ID, toID)
	if err != nil {
		t.Fatal(err)
	}
	if res.RootID != root.ID || res.Moved != 3 {
		t.Fatalf("res = %+v", res)
	}
	for _, id := range []string{root.ID, child.ID, grand.ID} {
		if got, _ := db.GetCDCInTenant(id, toID); got == nil {
			t.Fatalf("%s not visible in target tenant", id)
		}
		if got, _ := db.GetCDCInTenant(id, fromID); got != nil {
			t.Fatalf("%s still visible in source tenant", id)
		}
	}
	tids, err := db.CDCTaskTenantIDs([]string{root.ID, grand.ID})
	if err != nil || tids[root.ID] != toID || tids[grand.ID] != toID {
		t.Fatalf("tids=%v err=%v", tids, err)
	}
	again, err := db.AssignCDCTaskTenant(root.ID, toID)
	if err != nil || again.Moved != 0 {
		t.Fatalf("again=%+v err=%v", again, err)
	}

	h := &CDCHandler{Store: db}
	if w := h.cdcAssignVisibilityWarning(root.ID, toID); !strings.Contains(w, "源端实例") {
		t.Fatalf("warning = %q", w)
	}
	if w := h.cdcAssignVisibilityWarning(root.ID, fromID); w != "" {
		t.Fatalf("owner tenant should see instance, warning = %q", w)
	}
}
