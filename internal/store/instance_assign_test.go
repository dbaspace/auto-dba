package store

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAssignInstanceTenant(t *testing.T) {
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
		Code: "assign-" + uuid.NewString()[:8], Name: "划拨测试", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	inst := &Instance{
		TenantID: DefaultTenantID, Name: "pg-" + suf, DomainName: "h-" + suf + ".local", Port: 5432,
		DBType: "postgres", Host: "127.0.0.1", Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	dbRow := &Database{TenantID: DefaultTenantID, InstanceID: inst.ID, DBName: "appdb_" + suf}
	if err := d.CreateDatabase(dbRow); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_databases WHERE id=?`, dbRow.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})

	// conflict: same domain+port already in target
	clash := &Instance{
		TenantID: other.ID, Name: "clash-" + suf, DomainName: inst.DomainName, Port: inst.Port,
		DBType: "postgres", Host: "127.0.0.1", Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(clash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, clash.ID) })
	if err := d.AssignInstanceTenant(inst.ID, other.ID); err == nil {
		t.Fatal("expected unique conflict")
	}
	_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, clash.ID)

	if err := d.AssignInstanceTenant(inst.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetInstance(inst.ID)
	if err != nil || got == nil || got.TenantID != other.ID {
		t.Fatalf("instance tenant=%v err=%v", got, err)
	}
	tid, err := d.lookupRowTenantID("tbl_databases", dbRow.ID)
	if err != nil || tid != other.ID {
		t.Fatalf("database tenant=%q err=%v", tid, err)
	}
}

func TestAssignInstanceTenantSkipsForeignDatabase(t *testing.T) {
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
	keep := &Tenant{
		Code: "keep-" + uuid.NewString()[:8], Name: "异租库", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	dest := &Tenant{
		Code: "dest-" + uuid.NewString()[:8], Name: "实例目标", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(keep); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateTenant(dest); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	inst := &Instance{
		TenantID: DefaultTenantID, Name: "pg-keep-" + suf, DomainName: "k-" + suf + ".local", Port: 5432,
		DBType: "postgres", Host: "127.0.0.1", Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	own := &Database{TenantID: DefaultTenantID, InstanceID: inst.ID, DBName: "own_" + suf}
	foreign := &Database{TenantID: keep.ID, InstanceID: inst.ID, DBName: "foreign_" + suf}
	if err := d.CreateDatabase(own); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateDatabase(foreign); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_databases WHERE instance_id=?`, inst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, keep.ID, dest.ID)
	})
	if err := d.AssignInstanceTenant(inst.ID, dest.ID); err != nil {
		t.Fatal(err)
	}
	ownGot, _ := d.GetDatabase(own.ID)
	if ownGot == nil || ownGot.TenantID != dest.ID {
		t.Fatalf("owned db should move, got %+v", ownGot)
	}
	forGot, _ := d.GetDatabase(foreign.ID)
	if forGot == nil || forGot.TenantID != keep.ID {
		t.Fatalf("foreign db should stay, got %+v", forGot)
	}
}
