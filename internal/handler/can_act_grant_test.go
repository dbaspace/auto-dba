package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/store"
)

func TestCanActOnTarget_GrantedDatabaseAcrossTenant(t *testing.T) {
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
	fromID := "tenant-cat-from-" + suf
	viewerTenID := "tenant-cat-view-" + suf
	for _, row := range []struct{ id, code, name string }{
		{fromID, "cf" + suf[:10], "from-" + suf},
		{viewerTenID, "cv" + suf[:10], "view-" + suf},
	} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT (id) DO NOTHING`, row.id, row.code, row.name, store.TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}

	role := mustTestRole(t, db)
	u := &store.User{Username: "cat-" + suf, DisplayName: "cat", Role: "developer", RoleID: role.ID}
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertTenantMember(&store.TenantMember{
		TenantID: viewerTenID, UserID: u.ID, RoleIDs: []string{role.ID}, Status: store.MemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	inst := &store.Instance{
		TenantID: fromID, Name: "cat-i-" + suf, DomainName: "cat-" + suf + ".local",
		Port: 5432, DBType: "postgres", Host: "127.0.0.1", IDC: "default",
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	rec := &store.Database{TenantID: fromID, InstanceID: inst.ID, DBName: "db_cat_" + suf[:8], Visibility: "private"}
	if err := db.CreateDatabase(rec); err != nil {
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

	h := &TicketHandler{Store: db}

	// 无授权：他租实例+库应拒绝
	ok, deny, msg, err := h.canActOnTarget(viewerTenID, inst.ID, rec.DBName, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok || deny != targetActDenyInstanceMissing {
		t.Fatalf("without grant want instance deny: ok=%v deny=%v msg=%s", ok, deny, msg)
	}

	if err := db.CreateTenantGrant(&store.TenantGrant{
		FromTenantID: fromID, ToUserID: u.ID, ResourceType: "database", ResourceID: rec.ID,
		Permission: "read", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	ok, deny, msg, err = h.canActOnTarget(viewerTenID, inst.ID, rec.DBName, u.ID)
	if err != nil || !ok {
		t.Fatalf("with user grant should pass: ok=%v deny=%v msg=%s err=%v", ok, deny, msg, err)
	}

	// 建库类：仅校验实例可达（dbName 空）
	ok, deny, msg, err = h.canActOnTarget(viewerTenID, inst.ID, "", u.ID)
	if err != nil || !ok {
		t.Fatalf("instance-only with db grant should pass: ok=%v deny=%v msg=%s err=%v", ok, deny, msg, err)
	}

	// 错误用户仍拒绝
	ok, deny, msg, err = h.canActOnTarget(viewerTenID, inst.ID, rec.DBName, "other-"+u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("other user should deny, msg=%s deny=%v", msg, deny)
	}
}
