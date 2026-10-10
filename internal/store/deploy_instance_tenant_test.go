package store

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestSrClusterIDFromTags(t *testing.T) {
	if got := srClusterIDFromTags([]string{"idc=test", "sr-cluster=c-1"}); got != "c-1" {
		t.Fatalf("got %q", got)
	}
	if srClusterIDFromTags(nil) != "" {
		t.Fatal("empty")
	}
}

func TestEnsureInstanceFromHostInTenant(t *testing.T) {
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

	other := &Tenant{
		Code: "dd-inst-" + uuid.NewString()[:8], Name: "部署实例租户", Status: TenantStatusActive, Plan: "standard",
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	host := "h-" + uuid.NewString()[:8] + ".local"
	a, err := d.EnsureInstanceFromHostInTenant(DefaultTenantID, "mysql", "a", host, 3306, "")
	if err != nil || a == nil || a.TenantID != DefaultTenantID {
		t.Fatalf("default ensure: %+v %v", a, err)
	}
	b, err := d.EnsureInstanceFromHostInTenant(other.ID, "mysql", "b", host, 3306, "")
	if err != nil || b == nil || b.TenantID != other.ID {
		t.Fatalf("other ensure: %+v %v", b, err)
	}
	if a.ID == b.ID {
		t.Fatal("同 host:port 不同租户应各有实例")
	}
	again, err := d.EnsureInstanceFromHostInTenant(other.ID, "mysql", "b2", host, 3306, "")
	if err != nil || again == nil || again.ID != b.ID {
		t.Fatalf("同租户应复用: %+v %v", again, err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id IN (?,?)`, a.ID, b.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})
}

func TestRealignDeployInstanceTenantsLive(t *testing.T) {
	if os.Getenv("REALIGN_LIVE") != "1" {
		t.Skip("set REALIGN_LIVE=1 to calibrate linked deploy instances")
	}
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.RealignDeployInstanceTenants(); err != nil {
		t.Fatal(err)
	}
}
