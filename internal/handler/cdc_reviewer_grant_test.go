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

func TestBatchAddTable_GrantReviewers(t *testing.T) {
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
	prevDefault, _ := db.GetGlobalArg("sync_default_approver")
	prevWL, _ := db.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	_ = db.SetGlobalArg("sync_approval_enabled", "true", "test")
	t.Cleanup(func() {
		_ = db.SetGlobalArg("sync_approval_enabled", prevApproval, "test")
		_ = db.SetGlobalArg("sync_default_approver", prevDefault, "test")
		_ = db.SetGlobalArg(store.ArgIssueGlobalApproverWhitelist, prevWL, "test")
	})

	suf := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	bigID, orderID, thirdID := "tenant-cdcgr-big-"+suf, "tenant-cdcgr-ord-"+suf, "tenant-cdcgr-x-"+suf
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range [][2]string{{bigID, "b"}, {orderID, "o"}, {thirdID, "x"}} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			row[0], "cdcgr"+row[1]+suf, "cdcgr"+row[1]+suf, store.TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}
	role := mustTestRole(t, db)
	mkUser := func(prefix, tenantID string) *store.User {
		u := &store.User{Username: prefix + "-" + suf, DisplayName: prefix, Role: role.Code, RoleID: role.ID}
		if err := db.CreateUser(u, "x"); err != nil {
			t.Fatal(err)
		}
		if tenantID != "" {
			if err := db.UpsertTenantMember(&store.TenantMember{
				TenantID: tenantID, UserID: u.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
			}); err != nil {
				t.Fatal(err)
			}
		}
		return u
	}
	orderUser := mkUser("ord", orderID)
	if ok, _ := db.UserIsPlatformAdmin(orderUser.ID); ok {
		t.Fatal("fixture role is platform admin; reviewer cases would not be distinct")
	}
	thirdUser := mkUser("x", thirdID)
	bigUser := mkUser("big", bigID)
	defUser := mkUser("def", "")
	dbaUser := mkUser("dba", "")
	dbaTenant, _ := db.GetTenantByCode(store.DBATenantCode)
	dbaCreated := false
	if dbaTenant == nil {
		dbaTenant = &store.Tenant{Code: store.DBATenantCode, Name: "dba-" + suf, Status: store.TenantStatusActive}
		if err := db.CreateTenant(dbaTenant); err != nil {
			t.Fatal(err)
		}
		dbaCreated = true
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: dbaTenant.ID, UserID: dbaUser.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{
		TenantID: orderID, Name: "cdcgr-" + suf, DBType: "postgres",
		DomainName: "cdcgr-" + suf + ".example", Host: "cdcgr-" + suf + ".example", Port: 5432,
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	rec := &store.Database{
		TenantID: orderID, InstanceID: inst.ID, DBName: "db_bill_" + suf, Approvers: []string{orderUser.Username},
	}
	if err := db.CreateDatabase(rec); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTenantGrant(&store.TenantGrant{
		FromTenantID: orderID, ToTenantID: bigID, ResourceType: "database", ResourceID: rec.ID, Permission: "read", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	parent := &store.CDCTask{
		TenantID: bigID, Name: "cdcgr-init-" + suf, TaskType: "init", Status: store.StatusRunning,
		SourceID: inst.ID, SourceDB: rec.DBName,
		Description: encodeCDCExtraJSON("", map[string]any{"tables": "tbl_old"}),
	}
	if err := db.CreateCDC(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_issues WHERE instance_id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_cdc_sync_logs WHERE task_id IN (SELECT id FROM tbl_cdc_tasks WHERE id=? OR parent_id=?)`, parent.ID, parent.ID)
		_, _ = db.Exec(`DELETE FROM tbl_cdc_tasks WHERE id=? OR parent_id=?`, parent.ID, parent.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE resource_id=?`, rec.ID)
		_, _ = db.Exec(`DELETE FROM tbl_databases WHERE id=?`, rec.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		for _, u := range []*store.User{orderUser, thirdUser, bigUser, defUser, dbaUser} {
			_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
			_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
			_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
		}
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?,?)`, bigID, orderID, thirdID)
		if dbaCreated {
			_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=?`, dbaTenant.ID)
			_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, dbaTenant.ID)
		}
	})

	h := &CDCHandler{Store: db, Cfg: &config.Config{}}
	gin.SetMode(gin.TestMode)

	call := func(tenantID, userID, username, reviewer string, audit bool, table string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{
			"name": "cdcgr-add-" + table, "parent_ids": []string{parent.ID}, "tables": []string{table},
			"reviewer": reviewer, "source_instance_id": inst.ID, "source_db": rec.DBName,
			"description": encodeCDCExtraJSON("", map[string]any{"tables": table}),
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/cdc-sync/tasks/batch-add-table", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(middleware.CtxTenantID, tenantID)
		c.Set(middleware.CtxUserID, userID)
		c.Set(middleware.CtxUsername, username)
		c.Set(middleware.CtxImpersonate, audit)
		h.BatchAddTable(c)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	assertCreated := func(t *testing.T, code int, out map[string]any, reviewer string) {
		t.Helper()
		if code != http.StatusOK {
			t.Fatalf("code=%d out=%v", code, out)
		}
		data, _ := out["data"].(map[string]any)
		if data == nil || int(data["created"].(float64)) != 1 || int(data["failed"].(float64)) != 0 {
			t.Fatalf("data=%v", data)
		}
		items, _ := data["items"].([]any)
		item, _ := items[0].(map[string]any)
		taskObj, _ := item["task"].(map[string]any)
		taskID, _ := taskObj["id"].(string)
		if db.RowTenantID("tbl_cdc_tasks", taskID) != bigID {
			t.Fatalf("child tenant=%s", db.RowTenantID("tbl_cdc_tasks", taskID))
		}
		got, _ := db.GetCDC(taskID)
		if got == nil || got.IssueID == "" {
			t.Fatalf("issue missing: %+v", got)
		}
		issue, _ := db.GetIssue(got.IssueID)
		if issue == nil || issue.AssignedTo != reviewer || issue.TenantID != bigID || issue.Status != store.StatusPending {
			t.Fatalf("issue=%+v", issue)
		}
		if strings.Contains(issue.Description, "-- auto-dba-cdc:") {
			t.Fatalf("description has cdc config: %s", issue.Description)
		}
		if parent.IssueID != "" && got.IssueID == parent.IssueID {
			t.Fatal("child reused parent issue")
		}
	}
	assertFailed := func(t *testing.T, code int, out map[string]any) {
		t.Helper()
		if code != http.StatusOK {
			t.Fatalf("code=%d out=%v", code, out)
		}
		data, _ := out["data"].(map[string]any)
		if data == nil || int(data["failed"].(float64)) != 1 {
			t.Fatalf("data=%v", data)
		}
		items, _ := data["items"].([]any)
		item, _ := items[0].(map[string]any)
		msg, _ := item["message"].(string)
		if !strings.Contains(msg, "CDC 审核人须为") {
			t.Fatalf("message=%s", msg)
		}
	}

	t.Run("admin skips approver list", func(t *testing.T) {
		code, out := call(thirdID, admin.ID, admin.Username, admin.Username, true, "tbl_admin")
		assertCreated(t, code, out, admin.Username)
	})
	t.Run("db approver is not a cdc reviewer", func(t *testing.T) {
		code, out := call(bigID, admin.ID, admin.Username, orderUser.Username, true, "tbl_order")
		assertFailed(t, code, out)
	})
	t.Run("whitelist user", func(t *testing.T) {
		if err := db.SetGlobalArg(store.ArgIssueGlobalApproverWhitelist, defUser.Username, "test"); err != nil {
			t.Fatal(err)
		}
		code, out := call(bigID, admin.ID, admin.Username, defUser.Username, true, "tbl_def")
		assertCreated(t, code, out, defUser.Username)
	})
	t.Run("dba tenant member is super", func(t *testing.T) {
		code, out := call(bigID, admin.ID, admin.Username, dbaUser.Username, true, "tbl_dba")
		assertCreated(t, code, out, dbaUser.Username)
	})
	t.Run("task tenant member is not enough", func(t *testing.T) {
		code, out := call(bigID, bigUser.ID, bigUser.Username, bigUser.Username, false, "tbl_big")
		assertFailed(t, code, out)
	})
	t.Run("unrelated tenant cannot see parent", func(t *testing.T) {
		code, out := call(thirdID, thirdUser.ID, thirdUser.Username, thirdUser.Username, false, "tbl_x")
		if code != http.StatusBadRequest {
			t.Fatalf("code=%d out=%v", code, out)
		}
	})
}
