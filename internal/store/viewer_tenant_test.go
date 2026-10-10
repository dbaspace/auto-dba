package store

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestViewerOfInTenantUsesMemberRoles(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.ensureTenancy(); err != nil {
		t.Fatal(err)
	}
	adminRole, err := d.GetRoleByCode("admin")
	if err != nil || adminRole == nil {
		adminRole, err = d.ensureRole("admin", "管理员", "admin", now)
		if err != nil {
			t.Fatal(err)
		}
	}
	dbaRole, err := d.GetRoleByCode("dba")
	if err != nil || dbaRole == nil {
		dbaRole, err = d.ensureRole("dba", "DBA", "dba", now)
		if err != nil {
			t.Fatal(err)
		}
	}
	other := &Tenant{Code: "acl-" + uuid.NewString()[:8], Name: "acl", Status: TenantStatusActive, Plan: "standard"}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	u := &User{Username: "vten-" + uuid.NewString()[:8], DisplayName: "V", Role: "dba", RoleID: dbaRole.ID}
	if err := d.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = d.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = d.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})
	_ = d.RemoveTenantMember(DefaultTenantID, u.ID)
	if err := d.UpsertTenantMember(&TenantMember{TenantID: other.ID, UserID: u.ID, RoleIDs: []string{adminRole.ID}, Status: MemberStatusActive}); err != nil {
		t.Fatal(err)
	}

	inOther := d.ViewerOfInTenant(u.Username, "user", other.ID)
	if len(inOther.RoleIDs) != 1 || inOther.RoleIDs[0] != adminRole.ID {
		t.Fatalf("tenant roles=%v want [%s]", inOther.RoleIDs, adminRole.ID)
	}
	inDefault := d.ViewerOfInTenant(u.Username, "user", DefaultTenantID)
	if len(inDefault.RoleIDs) != 0 {
		t.Fatalf("default tenant roles should be empty, got %v", inDefault.RoleIDs)
	}
	dbPriv := Database{Visibility: "private", RoleIDs: []string{adminRole.ID}, TenantID: other.ID}
	if !dbPriv.VisibleTo(inOther) {
		t.Fatal("should see private db via tenant role")
	}
	if dbPriv.VisibleTo(inDefault) {
		t.Fatal("should not see via empty tenant roles")
	}
}
