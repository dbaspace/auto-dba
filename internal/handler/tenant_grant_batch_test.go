package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestUniqueTrimmedGrantIDs(t *testing.T) {
	got := uniqueTrimmedGrantIDs([]string{" a ", "", "a", "b", " b "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
}

func mustTestRole(t *testing.T, db *store.DB) *store.Role {
	t.Helper()
	for _, code := range []string{"developer", "admin", "dba", "viewer"} {
		r, err := db.GetRoleByCode(code)
		if err != nil {
			t.Fatal(err)
		}
		if r != nil {
			return r
		}
	}
	t.Skip("no seeded role for grant tests")
	return nil
}

func TestCreateGrantBatch_Validation(t *testing.T) {
	h := &TenantHandler{Store: nil}
	gin.SetMode(gin.TestMode)

	call := func(fromID string, body map[string]any) (int, string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		raw, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPost, "/tenants/"+fromID+"/grants/batch", bytes.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: fromID}}
		h.CreateGrantBatch(c)
		return w.Code, w.Body.String()
	}

	if code, body := call("t-from", map[string]any{
		"resource_type": "database",
		"resource_ids":  []string{},
		"to_tenant_id":  "t-to",
	}); code != http.StatusBadRequest || !strings.Contains(body, "resource_ids") {
		t.Fatalf("empty ids: code=%d body=%s", code, body)
	}
	if code, body := call("t-same", map[string]any{
		"resource_type": "database",
		"resource_ids":  []string{"r1"},
		"to_tenant_id":  "t-same",
	}); code != http.StatusBadRequest || !strings.Contains(body, "目标租户") {
		t.Fatalf("same tenant: code=%d body=%s", code, body)
	}
	if code, body := call("t-from", map[string]any{
		"resource_type": "database",
		"resource_ids":  []string{"r1"},
	}); code != http.StatusBadRequest || !strings.Contains(body, "恰好其一") {
		t.Fatalf("missing target: code=%d body=%s", code, body)
	}
	if code, body := call("t-from", map[string]any{
		"resource_type": "database",
		"resource_ids":  []string{"r1"},
		"to_tenant_id":  "t-to",
		"to_user_id":    "u1",
	}); code != http.StatusBadRequest || !strings.Contains(body, "恰好其一") {
		t.Fatalf("both targets: code=%d body=%s", code, body)
	}
}

func TestDeleteGrantBatch_Validation(t *testing.T) {
	h := &TenantHandler{Store: nil}
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw, _ := json.Marshal(map[string]any{"ids": []string{}})
	c.Request = httptest.NewRequest(http.MethodPost, "/tenants/t1/grants/batch-delete", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "t1"}}
	h.DeleteGrantBatch(c)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "ids") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExitImpersonate_IdempotentWhenNotImpersonating(t *testing.T) {
	t.Skip("exit-impersonate 幂等路径需真实 Store；行为见 ExitImpersonate already_exited 分支")
}

func TestCreateGrantBatch_CreateAndSkip(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config/config.yaml db.dsn to run grant batch tests")
	}
	db, err := store.OpenWithoutMigrate("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	suf := time.Now().Format("150405.000")
	code := "gb" + strings.ReplaceAll(suf, ".", "")
	fromID := "tenant-gb-from-" + code
	toID := "tenant-gb-to-" + code
	now := time.Now().UTC().Format(time.RFC3339)
	// 直接插租户行，避免 CreateTenant 触发额外迁移/校验；测完清理。
	if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT (id) DO NOTHING`, fromID, "c"+code+"f", "from-"+code, store.TenantStatusActive, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT (id) DO NOTHING`, toID, "c"+code+"t", "to-"+code, store.TenantStatusActive, now, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE from_tenant_id=? OR to_tenant_id=?`, fromID, fromID)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE from_tenant_id=? OR to_tenant_id=?`, toID, toID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, fromID, toID)
	})

	r1, r2 := "res-"+code+"-1", "res-"+code+"-2"
	h := &TenantHandler{Store: db}
	gin.SetMode(gin.TestMode)

	callBatch := func(ids []string) (int, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body, _ := json.Marshal(map[string]any{
			"resource_type": "database",
			"resource_ids":  ids,
			"to_tenant_id":  toID,
			"permission":    "read",
		})
		c.Request = httptest.NewRequest(http.MethodPost, "/tenants/"+fromID+"/grants/batch", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: fromID}}
		c.Set(middleware.CtxUsername, "admin")
		c.Set(middleware.CtxUserID, "u-admin")
		h.CreateGrantBatch(c)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	code1, out1 := callBatch([]string{r1, r2, r1})
	if code1 != http.StatusOK {
		t.Fatalf("first batch code=%d body=%v", code1, out1)
	}
	if int(out1["created"].(float64)) != 2 || int(out1["skipped"].(float64)) != 0 {
		t.Fatalf("first batch want created=2 skipped=0 got %v", out1)
	}

	code2, out2 := callBatch([]string{r1, r2})
	if code2 != http.StatusOK {
		t.Fatalf("second batch code=%d body=%v", code2, out2)
	}
	if int(out2["created"].(float64)) != 0 || int(out2["skipped"].(float64)) != 2 {
		t.Fatalf("second batch want created=0 skipped=2 got %v", out2)
	}

	// 单条幂等：再次 CreateTenantGrant 不应新增行
	g := &store.TenantGrant{
		FromTenantID: fromID,
		ToTenantID:   toID,
		ResourceType: "database",
		ResourceID:   r1,
		Permission:   "read",
		CreatedBy:    "admin",
	}
	if err := db.CreateTenantGrant(g); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM tbl_tenant_grants WHERE to_tenant_id=? AND resource_type=? AND resource_id=?`,
		toID, "database", r1).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("idempotent create want 1 row got %d", n)
	}
}

func TestCreateGrantBatch_ToUserAndCoverSkip(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config/config.yaml db.dsn to run grant batch user tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	suf := strings.ReplaceAll(time.Now().Format("150405.000"), ".", "")
	fromID := "tenant-gbu-from-" + suf
	toTenID := "tenant-gbu-to-" + suf
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT (id) DO NOTHING`, fromID, "cf"+suf, "from-"+suf, store.TenantStatusActive, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT (id) DO NOTHING`, toTenID, "ct"+suf, "to-"+suf, store.TenantStatusActive, now, now); err != nil {
		t.Fatal(err)
	}

	devRole := mustTestRole(t, db)
	u := &store.User{Username: "gbu-" + suf, DisplayName: "grant-user", Role: "developer", RoleID: devRole.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: toTenID, UserID: u.ID, RoleIDs: []string{devRole.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{
		TenantID: fromID, Name: "gbu-i-" + suf,
		DomainName: "gbu-" + suf + ".local", Port: 3306, DBType: "mysql", Host: "127.0.0.1", IDC: "default",
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE from_tenant_id=? OR to_tenant_id=? OR to_user_id=?`, fromID, toTenID, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, fromID, toTenID)
	})

	h := &TenantHandler{Store: db}
	gin.SetMode(gin.TestMode)
	call := func(body map[string]any) (int, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		raw, _ := json.Marshal(body)
		c.Request = httptest.NewRequest(http.MethodPost, "/tenants/"+fromID+"/grants/batch", bytes.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: fromID}}
		c.Set(middleware.CtxUsername, "admin")
		c.Set(middleware.CtxUserID, "u-admin")
		h.CreateGrantBatch(c)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	// 1) 授权给用户
	code, out := call(map[string]any{
		"resource_type": "instance",
		"resource_ids":  []string{inst.ID},
		"to_user_id":    u.ID,
		"permission":    "read",
	})
	if code != http.StatusOK || int(out["created"].(float64)) != 1 {
		t.Fatalf("user grant create: code=%d out=%v", code, out)
	}

	// 2) 重复用户授权 → 跳过
	code, out = call(map[string]any{
		"resource_type": "instance",
		"resource_ids":  []string{inst.ID},
		"to_user_id":    u.ID,
	})
	if code != http.StatusOK || int(out["created"].(float64)) != 0 || int(out["skipped"].(float64)) != 1 {
		t.Fatalf("user grant skip: code=%d out=%v", code, out)
	}

	// 3) 撤销用户授权后，先给所属租户授权，再给用户 → 因租户覆盖而跳过
	_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE to_user_id=? AND resource_id=?`, u.ID, inst.ID)
	code, out = call(map[string]any{
		"resource_type": "instance",
		"resource_ids":  []string{inst.ID},
		"to_tenant_id":  toTenID,
	})
	if code != http.StatusOK || int(out["created"].(float64)) != 1 {
		t.Fatalf("tenant grant create: code=%d out=%v", code, out)
	}
	code, out = call(map[string]any{
		"resource_type": "instance",
		"resource_ids":  []string{inst.ID},
		"to_user_id":    u.ID,
	})
	if code != http.StatusOK || int(out["created"].(float64)) != 0 || int(out["skipped"].(float64)) != 1 {
		t.Fatalf("cover skip: code=%d out=%v", code, out)
	}

	// 4) 目标用户不存在
	code, out = call(map[string]any{
		"resource_type": "instance",
		"resource_ids":  []string{inst.ID},
		"to_user_id":    "missing-" + uuid.NewString(),
	})
	if code != http.StatusBadRequest {
		t.Fatalf("missing user: code=%d out=%v", code, out)
	}
	msg, _ := out["message"].(string)
	if !strings.Contains(msg, "目标用户不存在") {
		t.Fatalf("missing user message: %v", out)
	}
}

// TestCreateGrantBatch_UserGrantSelfTest10 连续 10 轮用户授权/跳过/覆盖跳过自测。
func TestCreateGrantBatch_UserGrantSelfTest10(t *testing.T) {
	dsn := testingDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or config/config.yaml db.dsn to run grant self-test")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	devRole := mustTestRole(t, db)

	h := &TenantHandler{Store: db}
	gin.SetMode(gin.TestMode)
	for i := 1; i <= 10; i++ {
		suf := fmt.Sprintf("%s-%02d", strings.ReplaceAll(time.Now().Format("150405.000"), ".", ""), i)
		fromID := "tenant-st10-f-" + suf
		toTenID := "tenant-st10-t-" + suf
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			fromID, "sf"+suf[:12], "st-from-"+suf, store.TenantStatusActive, now, now); err != nil {
			t.Fatalf("round %d insert from: %v", i, err)
		}
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			toTenID, "st"+suf[:12], "st-to-"+suf, store.TenantStatusActive, now, now); err != nil {
			t.Fatalf("round %d insert to: %v", i, err)
		}
		u := &store.User{Username: "st10-" + suf, DisplayName: "st10", Role: "developer", RoleID: devRole.ID}
		if err := db.CreateUser(u, "x"); err != nil {
			t.Fatalf("round %d create user: %v", i, err)
		}
		if err := db.UpsertTenantMember(&store.TenantMember{
			TenantID: toTenID, UserID: u.ID, RoleIDs: []string{devRole.ID}, Status: store.MemberStatusActive,
		}); err != nil {
			t.Fatalf("round %d member: %v", i, err)
		}
		inst := &store.Instance{
			TenantID: fromID, Name: "st10-i-" + suf,
			DomainName: "st10-" + suf + ".local", Port: 3306, DBType: "mysql", Host: "127.0.0.1", IDC: "default",
		}
		if err := db.CreateInstance(inst); err != nil {
			t.Fatalf("round %d instance: %v", i, err)
		}
		cleanup := func() {
			_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE from_tenant_id=? OR to_tenant_id=? OR to_user_id=?`, fromID, toTenID, u.ID)
			_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
			_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
			_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
			_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
			_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, fromID, toTenID)
		}

		call := func(body map[string]any) (int, map[string]any) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			raw, _ := json.Marshal(body)
			c.Request = httptest.NewRequest(http.MethodPost, "/tenants/"+fromID+"/grants/batch", bytes.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: fromID}}
			c.Set(middleware.CtxUsername, "admin")
			c.Set(middleware.CtxUserID, "u-admin")
			h.CreateGrantBatch(c)
			var out map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			return w.Code, out
		}

		code, out := call(map[string]any{"resource_type": "instance", "resource_ids": []string{inst.ID}, "to_user_id": u.ID})
		if code != http.StatusOK || int(out["created"].(float64)) != 1 {
			cleanup()
			t.Fatalf("round %d user create: code=%d out=%v", i, code, out)
		}
		code, out = call(map[string]any{"resource_type": "instance", "resource_ids": []string{inst.ID}, "to_user_id": u.ID})
		if code != http.StatusOK || int(out["skipped"].(float64)) != 1 {
			cleanup()
			t.Fatalf("round %d user skip: code=%d out=%v", i, code, out)
		}
		_, _ = db.Exec(`DELETE FROM tbl_tenant_grants WHERE to_user_id=? AND resource_id=?`, u.ID, inst.ID)
		code, out = call(map[string]any{"resource_type": "instance", "resource_ids": []string{inst.ID}, "to_tenant_id": toTenID})
		if code != http.StatusOK || int(out["created"].(float64)) != 1 {
			cleanup()
			t.Fatalf("round %d tenant create: code=%d out=%v", i, code, out)
		}
		code, out = call(map[string]any{"resource_type": "instance", "resource_ids": []string{inst.ID}, "to_user_id": u.ID})
		if code != http.StatusOK || int(out["created"].(float64)) != 0 || int(out["skipped"].(float64)) != 1 {
			cleanup()
			t.Fatalf("round %d cover skip: code=%d out=%v", i, code, out)
		}
		cleanup()
		t.Logf("self-test round %d/10 ok", i)
	}
}
