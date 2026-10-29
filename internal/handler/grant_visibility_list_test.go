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

// TestGrantedDatabaseVisibleInInstanceAndTicketLists
// 仅 to_user_id 授库时：/instances 应含父实例；/tickets/instances/:id/databases 应含该库。
func TestGrantedDatabaseVisibleInInstanceAndTicketLists(t *testing.T) {
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
	fromID := "tenant-gvl-from-" + suf
	viewerTenID := "tenant-gvl-view-" + suf
	for _, row := range []struct{ id, code, name string }{
		{fromID, "vf" + suf[:10], "from-" + suf},
		{viewerTenID, "vv" + suf[:10], "view-" + suf},
	} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT (id) DO NOTHING`, row.id, row.code, row.name, store.TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}

	role := mustTestRole(t, db)
	u := &store.User{Username: "gvl-" + suf, DisplayName: "gvl", Role: "developer", RoleID: role.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: viewerTenID, UserID: u.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{
		TenantID: fromID, Name: "gvl-i-" + suf, DomainName: "gvl-" + suf + ".local",
		Port: 5432, DBType: "postgres", Host: "127.0.0.1", IDC: "idc-gvl-" + suf[:6],
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	rec := &store.Database{TenantID: fromID, InstanceID: inst.ID, DBName: "db_gvl_" + suf[:8], Visibility: "private"}
	if err := db.CreateDatabase(rec); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTenantGrant(&store.TenantGrant{
		FromTenantID: fromID, ToUserID: u.ID, ResourceType: "database", ResourceID: rec.ID,
		Permission: "read", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE resource_id=? OR to_user_id=?`, rec.ID, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_databases WHERE id=?`, rec.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, fromID, viewerTenID)
	})

	gin.SetMode(gin.TestMode)
	withUser := func(c *gin.Context) {
		c.Set(middleware.CtxUserID, u.ID)
		c.Set(middleware.CtxUsername, u.Username)
		c.Set(middleware.CtxTenantID, viewerTenID)
		c.Set(middleware.CtxRole, "developer")
	}

	// 1) 实例列表应含父实例
	{
		h := &InstanceHandler{Store: db}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/instances?scope=tenant", nil)
		withUser(c)
		h.List(c)
		if w.Code != http.StatusOK {
			t.Fatalf("instances list code=%d body=%s", w.Code, w.Body.String())
		}
		var out struct {
			Data []store.Instance `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, it := range out.Data {
			if it.ID == inst.ID {
				found = true
				if strings.TrimSpace(it.DBType) == "" {
					t.Fatalf("merged instance missing db_type: %#v", it)
				}
				break
			}
		}
		if !found {
			t.Fatalf("instances list missing granted db parent %s; got %d rows", inst.ID, len(out.Data))
		}
	}

	// 2) 工单选库应含授权库
	{
		h := &TicketHandler{Store: db}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/tickets/instances/"+inst.ID+"/databases", nil)
		c.Params = gin.Params{{Key: "instance_id", Value: inst.ID}}
		withUser(c)
		h.ListInstanceDatabases(c)
		if w.Code != http.StatusOK {
			t.Fatalf("ticket dbs code=%d body=%s", w.Code, w.Body.String())
		}
		var out struct {
			Data []store.Database `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range out.Data {
			if d.ID == rec.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ticket databases missing granted db %s; got %d rows", rec.ID, len(out.Data))
		}
	}

	// 3) tenantInstanceIDSet 应含父实例
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		withUser(c)
		set := tenantInstanceIDSet(db, c)
		if set == nil || !set[inst.ID] {
			t.Fatalf("tenantInstanceIDSet missing parent instance %s: %#v", inst.ID, set)
		}
	}
}
