package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func TestTenantDeleteHandlerLive(t *testing.T) {
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

	h := &TenantHandler{Store: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/tenants/:id", h.Delete)

	doDel := func(id string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/tenants/"+id, nil)
		r.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	code, body := doDel(store.DefaultTenantID)
	if code != http.StatusBadRequest || body["message"] != "默认租户不可删除" {
		t.Fatalf("default: code=%d body=%v", code, body)
	}

	empty := &store.Tenant{Code: "hdl-e-" + uuid.NewString()[:8], Name: "hdl-empty", Status: store.TenantStatusActive}
	if err := db.CreateTenant(empty); err != nil {
		t.Fatal(err)
	}
	code, body = doDel(empty.ID)
	if code != http.StatusOK {
		t.Fatalf("empty delete: code=%d body=%v", code, body)
	}

	withM := &store.Tenant{Code: "hdl-m-" + uuid.NewString()[:8], Name: "hdl-mem", Status: store.TenantStatusActive}
	if err := db.CreateTenant(withM); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=?`, withM.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, withM.ID)
	})
	role, err := db.GetRoleByCode("admin")
	if err != nil || role == nil {
		t.Fatal(err)
	}
	u := &store.User{Username: "hdl-u-" + uuid.NewString()[:8], DisplayName: "H", Role: "admin", RoleID: role.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
	})
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: withM.ID, UserID: u.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	code, body = doDel(withM.ID)
	if code != http.StatusConflict || body["message"] != "请先移除全部成员" {
		t.Fatalf("members: code=%d body=%v", code, body)
	}

	withR := &store.Tenant{Code: "hdl-r-" + uuid.NewString()[:8], Name: "hdl-res", Status: store.TenantStatusActive}
	if err := db.CreateTenant(withR); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE tenant_id=?`, withR.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id=?`, withR.ID)
	})
	suf := uuid.NewString()[:6]
	if err := db.CreateInstance(&store.Instance{
		TenantID: withR.ID, Name: "hdl-" + suf, DomainName: "hdl-" + suf + ".local", Port: 3306,
		DBType: "mysql", Host: "127.0.0.1", IDC: "default",
	}); err != nil {
		t.Fatal(err)
	}
	code, body = doDel(withR.ID)
	if code != http.StatusConflict || body["message"] != "需要先释放资源" {
		t.Fatalf("resources: code=%d body=%v", code, body)
	}
	res, _ := body["resources"].(map[string]any)
	if res == nil || res["tbl_instances"] == nil {
		t.Fatalf("resources payload missing: %v", body)
	}
}
