package store

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInstanceSameAddressViaDBDomains(t *testing.T) {
	src := &Instance{ID: "a", DBType: "postgres", DomainName: "10.100.112.14", Host: "10.100.112.14", Port: 5432}
	dst := &Instance{ID: "b", DBType: "postgres", DomainName: "pg.test.restosuife.ai", Port: 5432}
	dbRow := &Database{Domains: []string{"pg.test.restosuife.ai", "10.100.112.14"}}
	if !instanceSameAddress(src, dbRow, dst) {
		t.Fatal("expected match via db domains")
	}
	if instanceSameAddress(src, dbRow, &Instance{ID: "c", DBType: "mysql", DomainName: "pg.test.restosuife.ai", Port: 5432}) {
		t.Fatal("engine mismatch")
	}
	shop := &Instance{ID: "d", DBType: "postgres", DomainName: "pg.test.restosuife.shop", Port: 5432}
	if instanceSameAddress(src, &Database{Domains: []string{"10.100.112.14"}}, shop) {
		t.Fatal("shop should not match ip-only db")
	}
}

func TestPickSameAddressPrefersName(t *testing.T) {
	src := &Instance{Name: "test-pg"}
	got, err := pickSameAddressInstance(src, []Instance{
		{ID: "1", Name: "other"},
		{ID: "2", Name: "test-pg"},
	})
	if err != nil || got == nil || got.ID != "2" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestAssignDatabaseTenantKeepsInstance(t *testing.T) {
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
	other := &Tenant{
		Code: "db-asg-" + uuid.NewString()[:8], Name: "库划拨", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	host := "h-" + suf + ".example"
	ip := "10.66." + suf[:2] + ".9"
	dst := &Instance{
		TenantID: other.ID, Name: "pg-" + suf, DomainName: host, Port: 5432,
		DBType: "postgres", Host: host, Username: "u", Password: "p", IDC: "default",
	}
	src := &Instance{
		TenantID: DefaultTenantID, Name: "pg-" + suf, DomainName: ip, Port: 5432,
		DBType: "postgres", Host: ip, Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(dst); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateInstance(src); err != nil {
		t.Fatal(err)
	}
	row := &Database{TenantID: DefaultTenantID, InstanceID: src.ID, DBName: "app_" + suf, Domains: []string{host, ip}}
	if err := d.CreateDatabase(row); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_databases WHERE id=?`, row.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id IN (?,?)`, src.ID, dst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})
	if err := d.AssignDatabaseTenant(row.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetDatabase(row.ID)
	if err != nil || got == nil || got.TenantID != other.ID || got.InstanceID != src.ID {
		t.Fatalf("db=%+v err=%v", got, err)
	}
	srcGot, _ := d.GetInstance(src.ID)
	if srcGot == nil || srcGot.TenantID != DefaultTenantID {
		t.Fatalf("source instance should stay, got %+v", srcGot)
	}
	dstGot, _ := d.GetInstance(dst.ID)
	if dstGot == nil || dstGot.TenantID != other.ID {
		t.Fatalf("peer instance should stay in target, got %+v", dstGot)
	}
}

func TestAssignDatabaseTenantSiblingStays(t *testing.T) {
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
	other := &Tenant{
		Code: "db-asg2-" + uuid.NewString()[:8], Name: "库划拨整实例", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	src := &Instance{
		TenantID: DefaultTenantID, Name: "shop-" + suf, DomainName: "shop-" + suf + ".local", Port: 5432,
		DBType: "postgres", Host: "shop-" + suf + ".local", Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(src); err != nil {
		t.Fatal(err)
	}
	row := &Database{TenantID: DefaultTenantID, InstanceID: src.ID, DBName: "only_" + suf}
	if err := d.CreateDatabase(row); err != nil {
		t.Fatal(err)
	}
	sib := &Database{TenantID: DefaultTenantID, InstanceID: src.ID, DBName: "sib_" + suf}
	if err := d.CreateDatabase(sib); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_databases WHERE instance_id=?`, src.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, src.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})
	if err := d.AssignDatabaseTenant(row.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetInstance(src.ID)
	if got == nil || got.TenantID != DefaultTenantID {
		t.Fatalf("instance should stay, tenant=%v", got)
	}
	moved, _ := d.GetDatabase(row.ID)
	if moved == nil || moved.TenantID != other.ID || moved.InstanceID != src.ID {
		t.Fatalf("moved db=%+v", moved)
	}
	s2, _ := d.GetDatabase(sib.ID)
	if s2 == nil || s2.TenantID != DefaultTenantID {
		t.Fatalf("sibling should stay, tenant=%v", s2)
	}
}

func TestAssignDatabaseTenantResyncsAccountsWhenAlreadyAssigned(t *testing.T) {
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
	other := &Tenant{
		Code: "db-resync-" + uuid.NewString()[:8], Name: "库划拨补账号", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	inst := &Instance{
		TenantID: DefaultTenantID, Name: "resync-" + suf, DomainName: "resync-" + suf + ".local", Port: 5432,
		DBType: "postgres", Host: "resync-" + suf + ".local", Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	row := &Database{TenantID: other.ID, InstanceID: inst.ID, DBName: "resync_" + suf}
	if err := d.CreateDatabase(row); err != nil {
		t.Fatal(err)
	}
	acc := &DBAccount{
		TenantID: DefaultTenantID, InstanceID: inst.ID, DatabaseID: row.ID, DBName: row.DBName,
		DBType: "postgres", Username: "u_" + suf, Password: "p",
	}
	if err := d.CreateAccount(acc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_db_accounts WHERE id=?`, acc.ID)
		_, _ = d.Exec(`DELETE FROM tbl_databases WHERE id=?`, row.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})
	if err := d.AssignDatabaseTenant(row.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetAccount(acc.ID)
	if err != nil || got == nil || got.TenantID != other.ID {
		t.Fatalf("account should resync to target tenant, got=%+v err=%v", got, err)
	}
}
