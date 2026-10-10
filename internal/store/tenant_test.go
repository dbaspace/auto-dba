package store

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTenantIsolationBasics(t *testing.T) {
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
	def, err := d.GetTenant(DefaultTenantID)
	if err != nil || def == nil {
		t.Fatalf("default tenant: %v %#v", err, def)
	}

	other := &Tenant{Code: "bu-b-" + uuid.NewString()[:8], Name: "事业部B", Status: TenantStatusActive, Plan: "standard"}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=?`, other.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenant_grants WHERE from_tenant_id=? OR to_tenant_id=?`, other.ID, other.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE tenant_id=?`, other.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})

	adminRole, err := d.GetRoleByCode("admin")
	if err != nil || adminRole == nil {
		adminRole, err = d.ensureRole("admin", "管理员", "admin", now)
		if err != nil {
			t.Fatal(err)
		}
	}

	u1 := &User{Username: "tuser-a-" + uuid.NewString()[:8], DisplayName: "A", Role: "admin", RoleID: adminRole.ID}
	if err := d.CreateUser(u1, "x"); err != nil {
		t.Fatal(err)
	}
	u2 := &User{Username: "tuser-b-" + uuid.NewString()[:8], DisplayName: "B", Role: "admin", RoleID: adminRole.ID}
	if err := d.CreateUser(u2, "x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_tenant_members WHERE user_id IN (?,?)`, u1.ID, u2.ID)
		_, _ = d.Exec(`DELETE FROM tbl_user_roles WHERE user_id IN (?,?)`, u1.ID, u2.ID)
		_, _ = d.Exec(`DELETE FROM tbl_users WHERE id IN (?,?)`, u1.ID, u2.ID)
	})

	if err := d.UpsertTenantMember(&TenantMember{TenantID: DefaultTenantID, UserID: u1.ID, RoleIDs: []string{adminRole.ID}, Status: MemberStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertTenantMember(&TenantMember{TenantID: other.ID, UserID: u2.ID, RoleIDs: []string{adminRole.ID}, Status: MemberStatusActive}); err != nil {
		t.Fatal(err)
	}

	ok, err := d.IsActiveTenantMember(DefaultTenantID, u1.ID)
	if err != nil || !ok {
		t.Fatalf("u1 should be member of default: %v %v", ok, err)
	}
	ok, err = d.IsActiveTenantMember(other.ID, u1.ID)
	if err != nil || ok {
		t.Fatalf("u1 must not be member of other: %v %v", ok, err)
	}

	instA := &Instance{TenantID: DefaultTenantID, Name: "a-" + uuid.NewString()[:6], DomainName: "a-" + uuid.NewString()[:6] + ".local", Port: 3306, DBType: "mysql", Host: "127.0.0.1", IDC: "default"}
	if err := d.CreateInstance(instA); err != nil {
		t.Fatal(err)
	}
	instB := &Instance{TenantID: other.ID, Name: "b-" + uuid.NewString()[:6], DomainName: "b-" + uuid.NewString()[:6] + ".local", Port: 3307, DBType: "mysql", Host: "127.0.0.1", IDC: "default"}
	if err := d.CreateInstance(instB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id IN (?,?)`, instA.ID, instB.ID)
	})

	got, err := d.GetInstanceInTenant(instB.ID, DefaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("cross-tenant get must return nil, got %#v", got)
	}
	got, err = d.GetInstanceInTenant(instA.ID, DefaultTenantID)
	if err != nil || got == nil {
		t.Fatalf("same-tenant get: %v %#v", err, got)
	}

	list, err := d.ListInstancesInTenant(DefaultTenantID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range list {
		if it.ID == instB.ID {
			t.Fatalf("list leaked instance from other tenant")
		}
	}

	if err := d.CreateTenantGrant(&TenantGrant{
		FromTenantID: other.ID,
		ToTenantID:   DefaultTenantID,
		ResourceType: "instance",
		ResourceID:   instB.ID,
		Permission:   "read",
		CreatedBy:    "test",
	}); err != nil {
		t.Fatal(err)
	}
	has, err := d.HasResourceGrant(DefaultTenantID, u1.ID, "instance", instB.ID)
	if err != nil || !has {
		t.Fatalf("grant should allow: %v %v", has, err)
	}
}

func TestDeleteUserCleansTenantMembers(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureTenancy(); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	devRole, err := d.GetRoleByCode("developer")
	if err != nil || devRole == nil {
		devRole, err = d.ensureRole("developer", "开发者", "developer", now)
		if err != nil {
			t.Fatal(err)
		}
	}

	u := &User{Username: "del-u-" + uuid.NewString()[:8], DisplayName: "待删", Role: "developer", RoleID: devRole.ID}
	if err := d.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_tenant_grants WHERE to_user_id=?`, u.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, u.ID)
		_, _ = d.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, u.ID)
		_, _ = d.Exec(`DELETE FROM tbl_users WHERE id=?`, u.ID)
	})

	inst := &Instance{
		TenantID: DefaultTenantID, Name: "del-i-" + uuid.NewString()[:6],
		DomainName: "del-" + uuid.NewString()[:6] + ".local", Port: 3306, DBType: "mysql", Host: "127.0.0.1", IDC: "default",
	}
	if err := d.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID) })

	grant := &TenantGrant{
		FromTenantID: DefaultTenantID,
		ToUserID:     u.ID,
		ResourceType: "instance",
		ResourceID:   inst.ID,
		Permission:   "read",
		CreatedBy:    "test",
	}
	if err := d.CreateTenantGrant(grant); err != nil {
		t.Fatal(err)
	}

	if err := d.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}

	members, err := d.ListTenantMembers(DefaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.UserID == u.ID {
			t.Fatalf("orphan tenant member still present: %#v", m)
		}
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_tenant_grants WHERE to_user_id=?`, u.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("to_user grant should be deleted, got %d", n)
	}
}

func TestCleanupOrphanTenantMemberships(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureTenancy(); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	devRole, err := d.GetRoleByCode("developer")
	if err != nil || devRole == nil {
		devRole, err = d.ensureRole("developer", "开发者", "developer", now)
		if err != nil {
			t.Fatal(err)
		}
	}
	ghostID := uuid.NewString()
	if _, err := d.Exec(`INSERT INTO tbl_tenant_members(tenant_id, user_id, role_ids, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, DefaultTenantID, ghostID, MemberRoleIDsJSON([]string{devRole.ID}), MemberStatusActive, now, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, ghostID) })

	if err := d.CleanupOrphanTenantMemberships(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_tenant_members WHERE user_id=?`, ghostID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("orphan member should be cleaned, got %d", n)
	}
}

func TestIsPlatformURN(t *testing.T) {
	if !IsPlatformURN(URNTenantsCreate) {
		t.Fatal("tenants create should be platform URN")
	}
	if IsPlatformURN(URNInstancesCreate) {
		t.Fatal("instances create should not be platform URN")
	}
}

func TestNormalizeTenantPlan(t *testing.T) {
	got, err := NormalizeTenantPlan("")
	if err != nil || got != TenantPlanStandard {
		t.Fatalf("empty -> standard, got %q %v", got, err)
	}
	got, err = NormalizeTenantPlan(" Enterprise ")
	if err != nil || got != TenantPlanEnterprise {
		t.Fatalf("enterprise, got %q %v", got, err)
	}
	got, err = NormalizeTenantPlan("bu-finance")
	if err != nil || got != "bu-finance" {
		t.Fatalf("custom plan, got %q %v", got, err)
	}
	if _, err := NormalizeTenantPlan("Bad Plan!"); err == nil {
		t.Fatal("invalid chars should be rejected")
	}
}
