package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func batchACLTestStore(t *testing.T) *store.DB {
	t.Helper()
	cfgPath := filepath.Join("..", "..", "config", "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Skip("no config/config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil || cfg == nil || cfg.DB.DSN == "" {
		t.Skip("cannot load DSN via config/Nacos")
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDatabaseBatchACL(t *testing.T) {
	db := batchACLTestStore(t)
	h := &DatabaseHandler{Store: db}
	gin.SetMode(gin.TestMode)

	admin, err := db.GetUserByUsername("admin")
	if err != nil || admin == nil {
		t.Skip("admin user missing")
	}
	var plainRole *store.Role
	roles, _ := db.ListRoles()
	for i := range roles {
		if roles[i].Code != "admin" {
			plainRole = &roles[i]
			break
		}
	}
	if plainRole == nil {
		t.Skip("no non-admin role")
	}
	outsider := &store.User{Username: "bacl_out_" + uuid.NewString()[:8], DisplayName: "out", Role: plainRole.Code, RoleID: plainRole.ID}
	if err := db.CreateUser(outsider, "x"); err != nil {
		t.Fatal(err)
	}
	_ = db.RemoveTenantMember(store.DefaultTenantID, outsider.ID)
	if ok, _ := db.IsActiveTenantMember(store.DefaultTenantID, outsider.ID); ok {
		t.Fatal("outsider should not be a default tenant member")
	}

	other := &store.Tenant{Code: "bacl-" + uuid.NewString()[:8], Name: "bacl-other", Status: store.TenantStatusActive}
	if err := db.CreateTenant(other); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{Name: "bacl-" + uuid.NewString()[:8], DBType: "mysql", Host: "127.0.0.1", Port: 3306, TenantID: store.DefaultTenantID}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	mk := func(name, vis, tenant string, roles []string) *store.Database {
		it := &store.Database{InstanceID: inst.ID, DBName: name, Visibility: vis, TenantID: tenant, RoleIDs: roles}
		if vis == "private" {
			it.AccessUsers = []string{"admin"}
		}
		if err := db.CreateDatabase(it); err != nil {
			t.Fatal(err)
		}
		return it
	}
	sfx := uuid.NewString()[:6]
	privA := mk("bacl_a_"+sfx, "private", store.DefaultTenantID, nil)
	privB := mk("bacl_b_"+sfx, "private", store.DefaultTenantID, nil)
	glob := mk("bacl_g_"+sfx, "global", store.DefaultTenantID, nil)
	foreign := mk("bacl_f_"+sfx, "global", other.ID, nil)

	t.Cleanup(func() {
		for _, d := range []*store.Database{privA, privB, glob, foreign} {
			_ = db.DeleteDatabase(d.ID)
		}
		_ = db.DeleteInstance(inst.ID)
		_ = db.DeleteTenant(other.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, outsider.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, outsider.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, outsider.ID)
	})

	call := func(fn gin.HandlerFunc, body map[string]any, auditing bool) (int, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		raw, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(middleware.CtxUsername, "admin")
		c.Set(middleware.CtxUserID, admin.ID)
		c.Set(middleware.CtxRole, "admin")
		c.Set(middleware.CtxTenantID, store.DefaultTenantID)
		if auditing {
			c.Set(middleware.CtxImpersonate, true)
		}
		fn(c)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	// 审核人：同租户追加成功
	code, out := call(h.BatchApprovers, map[string]any{"ids": []string{privA.ID, glob.ID}, "approvers": []string{"admin"}}, false)
	if code != http.StatusOK || out["ok_count"].(float64) != 2 {
		t.Fatalf("approvers ok: code=%d out=%v", code, out)
	}
	if got, _ := db.GetDatabase(glob.ID); got == nil || len(got.Approvers) != 1 || got.Approvers[0] != "admin" || got.Visibility != "global" {
		t.Fatalf("glob approvers=%v", got)
	}
	if got, _ := db.GetDatabase(privA.ID); got == nil || len(got.AccessUsers) != 1 || got.Visibility != "private" {
		t.Fatalf("privA other fields changed: %v", got)
	}

	// 审核人：非租户成员忽略，不整批失败；已有合法名单保留
	if _, err := db.Exec(`UPDATE tbl_databases SET approvers=? WHERE id=?`, `["admin"]`, glob.ID); err != nil {
		t.Fatal(err)
	}
	if code, out = call(h.BatchApprovers, map[string]any{"ids": []string{glob.ID}, "approvers": []string{outsider.Username}}, false); code != http.StatusOK {
		t.Fatalf("outsider approver should soft-drop: code=%d out=%v", code, out)
	}
	if got, _ := db.GetDatabase(glob.ID); got == nil || len(got.Approvers) != 1 || got.Approvers[0] != "admin" {
		t.Fatalf("should keep existing member approver: %v", got)
	}
	if dropped, _ := out["dropped"].([]any); len(dropped) == 0 {
		t.Fatalf("expected dropped outsider, out=%v", out)
	}

	// 审核人：空名单不追加，保留已有合法成员
	if code, out = call(h.BatchApprovers, map[string]any{"ids": []string{glob.ID}, "approvers": []string{}}, false); code != http.StatusOK {
		t.Fatalf("empty append: code=%d out=%v", code, out)
	}
	if got, _ := db.GetDatabase(glob.ID); got == nil || len(got.Approvers) != 1 || got.Approvers[0] != "admin" {
		t.Fatalf("empty append should keep members: %v", got)
	}

	// 可访问用户：含全局库拒绝
	if code, out = call(h.BatchAccessUsers, map[string]any{"ids": []string{privA.ID, glob.ID}, "access_users": []string{"admin"}}, false); code != http.StatusBadRequest {
		t.Fatalf("access with global: code=%d out=%v", code, out)
	}

	// 可访问用户：非租户成员拒绝
	if code, out = call(h.BatchAccessUsers, map[string]any{"ids": []string{privA.ID, privB.ID}, "access_users": []string{outsider.Username}}, false); code != http.StatusBadRequest {
		t.Fatalf("outsider access: code=%d out=%v", code, out)
	}

	// 可访问用户：私有库覆盖成功
	if code, out = call(h.BatchAccessUsers, map[string]any{"ids": []string{privA.ID, privB.ID}, "access_users": []string{"admin"}}, false); code != http.StatusOK || out["ok_count"].(float64) != 2 {
		t.Fatalf("access ok: code=%d out=%v", code, out)
	}

	// 可访问用户：空名单且无角色 → 逐条失败，不写入
	if code, out = call(h.BatchAccessUsers, map[string]any{"ids": []string{privA.ID}, "access_users": []string{}}, false); code != http.StatusOK || out["fail_count"].(float64) != 1 {
		t.Fatalf("empty access: code=%d out=%v", code, out)
	}
	if got, _ := db.GetDatabase(privA.ID); got == nil || len(got.AccessUsers) != 1 {
		t.Fatalf("privA access cleared unexpectedly: %v", got)
	}

	// 非审计：他租户库不可见
	if code, out = call(h.BatchApprovers, map[string]any{"ids": []string{privA.ID, foreign.ID}, "approvers": []string{"admin"}}, false); code == http.StatusOK {
		t.Fatalf("foreign db should be rejected: %v", out)
	}

	// 审计：跨租户勾选拒绝
	if ok, _ := db.UserIsPlatformAdmin(admin.ID); ok {
		if code, out = call(h.BatchApprovers, map[string]any{"ids": []string{privA.ID, foreign.ID}, "approvers": []string{"admin"}}, true); code != http.StatusBadRequest {
			t.Fatalf("cross tenant auditing: code=%d out=%v", code, out)
		}
	}
}
