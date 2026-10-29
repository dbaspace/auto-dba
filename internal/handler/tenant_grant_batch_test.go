package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func TestUniqueTrimmedGrantIDs(t *testing.T) {
	got := uniqueTrimmedGrantIDs([]string{" a ", "", "a", "b", " b "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
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
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run grant batch tests")
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
