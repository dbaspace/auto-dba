package handler

import (
	"encoding/json"
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

func TestGetTargetApprovers_GrantedDBShowsResourceApproversNotSubmitter(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config dsn")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	suf := strings.ReplaceAll(time.Now().Format("150405.000"), ".", "") + uuid.NewString()[:6]
	now := time.Now().UTC().Format(time.RFC3339)
	fromID := "tenant-tap-from-" + suf
	viewerTenID := "tenant-tap-view-" + suf
	for _, row := range []struct{ id, code, name string }{
		{fromID, "tf" + suf[:10], "from-" + suf},
		{viewerTenID, "tv" + suf[:10], "view-" + suf},
	} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT (id) DO NOTHING`, row.id, row.code, row.name, store.TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}

	role := mustTestRole(t, db)
	approver := &store.User{Username: "tap-admin-" + suf, DisplayName: "tap-admin", Role: "developer", RoleID: role.ID}
	if err := db.CreateUser(approver, "x"); err != nil {
		t.Fatal(err)
	}
	grantee := &store.User{Username: "tap-user-" + suf, DisplayName: "tap-user", Role: "developer", RoleID: role.ID}
	if err := db.CreateUser(grantee, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: fromID, UserID: approver.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: viewerTenID, UserID: grantee.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{
		TenantID: fromID, Name: "tap-i-" + suf, DomainName: "tap-" + suf + ".local",
		Port: 5432, DBType: "postgres", Host: "127.0.0.1", IDC: "default",
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	rec := &store.Database{
		TenantID: fromID, InstanceID: inst.ID, DBName: "db_tap_" + suf[:8],
		Visibility: "private", Approvers: []string{approver.Username},
	}
	if err := db.CreateDatabase(rec); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTenantGrant(&store.TenantGrant{
		FromTenantID: fromID, ToUserID: grantee.ID, ResourceType: "database", ResourceID: rec.ID,
		Permission: "read", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE resource_id=? OR to_user_id IN (?,?)`, rec.ID, grantee.ID, approver.ID)
		_, _ = db.Exec(`DELETE FROM tbl_databases WHERE id=?`, rec.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id IN (?,?)`, grantee.ID, approver.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id IN (?,?)`, grantee.ID, approver.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id IN (?,?)`, grantee.ID, approver.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, fromID, viewerTenID)
	})

	h := &TicketHandler{Store: db}
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/tickets/target-approvers?instance_id="+inst.ID+"&db_name="+rec.DBName, nil)
	c.Set(middleware.CtxUserID, grantee.ID)
	c.Set(middleware.CtxUsername, grantee.Username)
	c.Set(middleware.CtxTenantID, viewerTenID)
	h.GetTargetApprovers(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
		ResourceTenantID string `json:"resource_tenant_id"`
		Message          string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ResourceTenantID != fromID {
		t.Fatalf("resource_tenant_id want %s got %s", fromID, out.ResourceTenantID)
	}
	names := map[string]bool{}
	for _, it := range out.Data {
		names[strings.ToLower(it.Name)] = true
	}
	if !names[strings.ToLower(approver.Username)] {
		t.Fatalf("want resource approver %s in list: %#v msg=%s", approver.Username, out.Data, out.Message)
	}
	if names[strings.ToLower(grantee.Username)] {
		t.Fatalf("submitter/grantee %s must not appear as approver: %#v", grantee.Username, out.Data)
	}

	// Submit 侧：resolveTenantAssignee 按资源租户接受审批人
	got, err := resolveTenantAssignee(db, fromID, approver.Username)
	if err != nil || !strings.EqualFold(got, approver.Username) {
		t.Fatalf("resolveTenantAssignee resource tenant: got=%q err=%v", got, err)
	}
	if _, err := resolveTenantAssignee(db, viewerTenID, approver.Username); err == nil {
		t.Fatal("approver is not viewer-tenant member; resolve on viewer tenant should fail")
	}
}
