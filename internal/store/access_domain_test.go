package store

import (
	"os"
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

func TestNormalizeEngineType(t *testing.T) {
	if NormalizeEngineType("PostgreSQL") != "postgres" {
		t.Fatal(NormalizeEngineType("PostgreSQL"))
	}
	if NormalizeEngineType("mysql") != "mysql" {
		t.Fatal()
	}
}
