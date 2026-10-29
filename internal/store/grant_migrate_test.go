package store

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMigrateResourceGrantsOnAssign_Database(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := OpenWithoutMigrate("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	suf := strings.ReplaceAll(time.Now().Format("150405.000"), ".", "")
	now := time.Now().UTC().Format(time.RFC3339)
	fromID := "tenant-gm-a-" + suf
	toBID := "tenant-gm-b-" + suf
	toCID := "tenant-gm-c-" + suf
	for _, row := range []struct{ id, code, name string }{
		{fromID, "gma" + suf, "from-a"},
		{toBID, "gmb" + suf, "to-b"},
		{toCID, "gmc" + suf, "to-c"},
	} {
		if _, err := d.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT (id) DO NOTHING`, row.id, row.code, row.name, TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}
	inst := &Instance{
		TenantID: fromID, Name: "i-" + suf, DomainName: "h-" + suf + ".test", Port: 5432,
		DBType: "postgres", Host: "h-" + suf + ".test", Username: "u", Password: "p", IDC: "default",
	}
	if err := d.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	row := &Database{TenantID: fromID, InstanceID: inst.ID, DBName: "app_gm_" + suf}
	if err := d.CreateDatabase(row); err != nil {
		t.Fatal(err)
	}
	dbID := row.ID
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_tenant_grants WHERE resource_id=?`, dbID)
		_, _ = d.Exec(`DELETE FROM tbl_databases WHERE id=?`, dbID)
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?,?)`, fromID, toBID, toCID)
	})

	if err := d.CreateTenantGrant(&TenantGrant{
		FromTenantID: fromID, ToTenantID: toBID, ResourceType: "database", ResourceID: dbID,
		Permission: "read", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}

	ok, err := d.HasResourceGrant(toBID, "", "database", dbID)
	if err != nil || !ok {
		t.Fatalf("before assign B should have grant: ok=%v err=%v", ok, err)
	}

	if err := d.AssignDatabaseTenant(dbID, toCID); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetDatabase(dbID)
	if got == nil || got.TenantID != toCID {
		t.Fatalf("owner want %s got %#v", toCID, got)
	}

	ok, err = d.HasResourceGrant(toBID, "", "database", dbID)
	if err != nil || !ok {
		t.Fatalf("after assign to C, B should still have grant: ok=%v err=%v", ok, err)
	}
	g, err := d.FindActiveTenantGrant(toBID, "", "database", dbID)
	if err != nil || g == nil {
		t.Fatalf("grant missing: %v %#v", err, g)
	}
	if g.FromTenantID != toCID {
		t.Fatalf("from should migrate to C, got %q", g.FromTenantID)
	}

	// 脏数据：from 仍为 A 时应无效（模拟未迁移）
	_, _ = d.Exec(`UPDATE tbl_tenant_grants SET from_tenant_id=? WHERE id=?`, fromID, g.ID)
	ok, err = d.HasResourceGrant(toBID, "", "database", dbID)
	if err != nil || ok {
		t.Fatalf("stale from should deny: ok=%v err=%v", ok, err)
	}
	// 恢复并划到 B 自身 → grant 删除
	_, _ = d.Exec(`UPDATE tbl_tenant_grants SET from_tenant_id=? WHERE id=?`, toCID, g.ID)
	if err := d.AssignDatabaseTenant(dbID, toBID); err != nil {
		t.Fatal(err)
	}
	ok, err = d.HasResourceGrant(toBID, "", "database", dbID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("assign to grantee should delete grant")
	}
	vis, err := d.GetDatabaseVisible(dbID, toBID, "")
	if err != nil || vis == nil {
		t.Fatalf("B as owner should see db: %v %#v", err, vis)
	}
}

func TestGrantMigrateResultAdd(t *testing.T) {
	a := GrantMigrateResult{Migrated: 1, Deleted: 2}.Add(GrantMigrateResult{Migrated: 3, Deleted: 0})
	if a.Migrated != 4 || a.Deleted != 2 {
		t.Fatalf("%+v", a)
	}
	_ = uuid.NewString()
}
