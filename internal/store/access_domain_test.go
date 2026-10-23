package store

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSyncAccessDomainsKeepsEngineTypesSeparate(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureAccessDomainsTable(); err != nil {
		t.Fatal(err)
	}

	suf := uuid.NewString()[:8]
	host := "shared-" + suf + ".example.com"

	mysql := &Instance{
		TenantID: DefaultTenantID, Name: "mysql-" + suf, DomainName: host, Port: 3306,
		DBType: "mysql", IDC: "test", Host: "127.0.0.1", Domains: []string{host},
	}
	pg := &Instance{
		TenantID: DefaultTenantID, Name: "pg-" + suf, DomainName: host, Port: 5432,
		DBType: "postgresql", IDC: "test", Host: "127.0.0.1", Domains: []string{host},
	}
	if err := d.CreateInstance(mysql); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateInstance(pg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.DeleteAccessDomainsByInstance(mysql.ID)
		_ = d.DeleteAccessDomainsByInstance(pg.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id IN (?,?)`, mysql.ID, pg.ID)
	})

	if _, err := d.SyncAccessDomainsFromInstance(mysql); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SyncAccessDomainsFromInstance(pg); err != nil {
		t.Fatal(err)
	}

	mRow, err := d.GetAccessDomainByDomainType(host, "mysql")
	if err != nil || mRow == nil {
		t.Fatalf("mysql domain: %v %#v", err, mRow)
	}
	pRow, err := d.GetAccessDomainByDomainType(host, "postgres")
	if err != nil || pRow == nil {
		t.Fatalf("postgres domain: %v %#v", err, pRow)
	}
	if mRow.ID == pRow.ID {
		t.Fatal("mysql and postgres domain rows must be distinct")
	}
	if mRow.InstanceID != mysql.ID || pRow.InstanceID != pg.ID {
		t.Fatalf("instance binding mismatch mysql=%s pg=%s", mRow.InstanceID, pRow.InstanceID)
	}
	if mRow.TenantID != DefaultTenantID || pRow.TenantID != DefaultTenantID {
		t.Fatal("new rows should belong to default tenant")
	}

	if _, err := d.SyncAllAccessDomains("mysql"); err != nil {
		t.Fatal(err)
	}
	p2, _ := d.GetAccessDomainByDomainType(host, "postgres")
	if p2 == nil || p2.ID != pRow.ID || p2.InstanceID != pg.ID {
		t.Fatal("postgres domain must not be overwritten by mysql-only sync")
	}
}

func TestRemoveInstanceDomainHost(t *testing.T) {
	next, name, changed := removeInstanceDomainHost([]string{"a.example", "b.example"}, "a.example", "a.example")
	if !changed || name != "" || len(next) != 1 || next[0] != "b.example" {
		t.Fatalf("domains=%v name=%q changed=%v", next, name, changed)
	}
	next, name, changed = removeInstanceDomainHost([]string{"A.Example"}, "A.Example", "a.example")
	if !changed || name != "" || len(next) != 0 {
		t.Fatalf("domains=%v name=%q changed=%v", next, name, changed)
	}
	next, name, changed = removeInstanceDomainHost([]string{"keep.example"}, "keep.example", "other.example")
	if changed || name != "keep.example" || len(next) != 1 {
		t.Fatalf("domains=%v name=%q changed=%v", next, name, changed)
	}
}

func TestDeleteAccessDomainStripsInstance(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureAccessDomainsTable(); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	keep := "keep-" + suf + ".example.com"
	drop := "drop-" + suf + ".example.com"
	pg := &Instance{
		TenantID: DefaultTenantID, Name: "pg-del-" + suf, DomainName: drop, Port: 5432,
		DBType: "postgresql", IDC: "test", Host: "127.0.0.1", Domains: []string{drop, keep},
	}
	if err := d.CreateInstance(pg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.DeleteAccessDomainsByInstance(pg.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, pg.ID)
	})
	if _, err := d.SyncAccessDomainsFromInstance(pg); err != nil {
		t.Fatal(err)
	}
	row, err := d.GetAccessDomainByDomainType(drop, "postgres")
	if err != nil || row == nil {
		t.Fatalf("domain: %v %#v", err, row)
	}
	if err := d.DeleteAccessDomain(row.ID); err != nil {
		t.Fatal(err)
	}
	gone, err := d.GetAccessDomain(row.ID)
	if err != nil || gone != nil {
		t.Fatalf("row still present: %v %#v", err, gone)
	}
	inst, err := d.GetInstance(pg.ID)
	if err != nil || inst == nil {
		t.Fatal(err)
	}
	if inst.DomainName == drop {
		t.Fatalf("domain_name still %s", inst.DomainName)
	}
	for _, h := range inst.Domains {
		if strings.EqualFold(h, drop) {
			t.Fatalf("domains still contain %s: %v", drop, inst.Domains)
		}
	}
	if _, err := d.SyncAccessDomainsFromInstance(inst); err != nil {
		t.Fatal(err)
	}
	again, _ := d.GetAccessDomainByDomainType(drop, "postgres")
	if again != nil {
		t.Fatal("sync recreated deleted domain")
	}
	kept, _ := d.GetAccessDomainByDomainType(keep, "postgres")
	if kept == nil {
		t.Fatal("other domain should remain")
	}
}

func TestNormalizeEngineType(t *testing.T) {
	if NormalizeEngineType("PostgreSQL") != "postgres" {
		t.Fatal(NormalizeEngineType("PostgreSQL"))
	}
	if NormalizeEngineType("mysql") != "mysql" {
		t.Fatal()
	}
}
