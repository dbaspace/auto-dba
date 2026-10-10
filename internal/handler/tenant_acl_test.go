package handler

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/store"
)

func TestRequireTenantMemberUsernames(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	now := time.Now().UTC().Format(time.RFC3339)
	adminRole, err := db.GetRoleByCode("admin")
	if err != nil || adminRole == nil {
		t.Fatalf("admin role: %v", err)
	}
	_ = now
	outsider := &store.User{Username: "acl_out_" + uuid.NewString()[:8], DisplayName: "out", Role: "admin", RoleID: adminRole.ID}
	if err := db.CreateUser(outsider, "x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, outsider.ID)
		_, _ = db.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, outsider.ID)
		_, _ = db.Exec(`DELETE FROM tbl_users WHERE id=?`, outsider.ID)
	})
	_ = db.RemoveTenantMember(store.DefaultTenantID, outsider.ID)

	if err := requireTenantMemberUsernames(db, store.DefaultTenantID, []string{"admin"}); err != nil {
		t.Fatalf("admin should be member: %v", err)
	}
	if err := requireTenantMemberUsernames(db, store.DefaultTenantID, []string{outsider.Username}); err == nil {
		t.Fatal("outsider should fail")
	}
	roles, err := db.ListRoles()
	if err != nil || len(roles) == 0 {
		t.Fatalf("roles: %v", err)
	}
	if err := validateResourceACL(db, store.DefaultTenantID, "private", []string{"admin"}, []string{roles[0].ID}, []string{"admin"}); err != nil {
		t.Fatalf("validate ok: %v", err)
	}
	if err := validateResourceACL(db, store.DefaultTenantID, "private", nil, []string{"no-such-role"}, nil); err == nil {
		t.Fatal("bad role should fail")
	}
}
