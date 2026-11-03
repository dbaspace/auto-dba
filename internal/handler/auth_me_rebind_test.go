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

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func TestMe_RebindsStaleDefaultTenantJWT(t *testing.T) {
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
	bizID := "tenant-me-biz-" + suf
	if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT (id) DO NOTHING`, bizID, "mb"+suf[:10], "biz-"+suf, store.TenantStatusActive, now, now); err != nil {
		t.Fatal(err)
	}

	role := mustTestRole(t, db)
	u := &store.User{Username: "me-rb-" + suf, DisplayName: "me-rb", Role: "developer", RoleID: role.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	// 仅业务租户成员（模拟已从 default 迁出）
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: bizID, UserID: u.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=? AND user_id=?`, store.DefaultTenantID, u.ID)

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, bizID)
	})

	cfg := &config.Config{}
	cfg.App.JWTSecret = "test-me-rebind-secret"
	cfg.App.JWTTTLHours = 24
	h := &AuthHandler{Store: db, Cfg: cfg}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	c.Set(middleware.CtxUserID, u.ID)
	c.Set(middleware.CtxUsername, u.Username)
	c.Set(middleware.CtxTenantID, store.DefaultTenantID) // 僵死：仍停在 default
	c.Set(middleware.CtxRole, "developer")

	h.Me(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
		Data  struct {
			CurrentTenant         *store.Tenant `json:"current_tenant"`
			TenantSessionRebound  bool          `json:"tenant_session_rebound"`
			NeedTenantSelect      bool          `json:"need_tenant_select"`
			Menus                 []any         `json:"menus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Token == "" {
		t.Fatalf("want rebound token, body=%s", w.Body.String())
	}
	if !resp.Data.TenantSessionRebound {
		t.Fatalf("want tenant_session_rebound, body=%s", w.Body.String())
	}
	if resp.Data.NeedTenantSelect {
		t.Fatal("should not need_tenant_select when single biz tenant")
	}
	if resp.Data.CurrentTenant == nil || resp.Data.CurrentTenant.ID != bizID {
		t.Fatalf("current_tenant=%#v want %s", resp.Data.CurrentTenant, bizID)
	}
}

func TestMe_NoRebindWhenStillDefaultMember(t *testing.T) {
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
	role := mustTestRole(t, db)
	u := &store.User{Username: "me-ok-" + suf, DisplayName: "me-ok", Role: "developer", RoleID: role.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: store.DefaultTenantID, UserID: u.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
	})

	cfg := &config.Config{}
	cfg.App.JWTSecret = "test-me-rebind-secret"
	cfg.App.JWTTTLHours = 24
	h := &AuthHandler{Store: db, Cfg: cfg}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	c.Set(middleware.CtxUserID, u.ID)
	c.Set(middleware.CtxUsername, u.Username)
	c.Set(middleware.CtxTenantID, store.DefaultTenantID)
	c.Set(middleware.CtxRole, "developer")

	h.Me(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["token"]; ok {
		t.Fatalf("active default member should not rebound: %v", resp)
	}
	data, _ := resp["data"].(map[string]any)
	if data["tenant_session_rebound"] == true {
		t.Fatal("should not set tenant_session_rebound")
	}
}
