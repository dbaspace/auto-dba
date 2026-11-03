package store

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// 域名与实例已划到本租，库仍在外租：viaAccess 应展开这些库（工单选库不能空）。
func TestListDatabasesViaInstanceAccess_OwnedDomainForeignDBs(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AUTO_DBA_TEST_DSN"))
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN")
	}
	db, err := Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	suf := strings.ReplaceAll(time.Now().Format("150405.000"), ".", "") + uuid.NewString()[:6]
	now := time.Now().UTC().Format(time.RFC3339)
	fromID := "tenant-ova-from-" + suf
	viewerID := "tenant-ova-view-" + suf
	for _, row := range []struct{ id, code, name string }{
		{fromID, "of" + suf[:10], "from-" + suf},
		{viewerID, "ov" + suf[:10], "view-" + suf},
	} {
		if _, err := db.Exec(`INSERT INTO tbl_tenants(id, code, name, status, created_at, updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT (id) DO NOTHING`, row.id, row.code, row.name, TenantStatusActive, now, now); err != nil {
			t.Fatal(err)
		}
	}

	// 实例已属本租，库仍属外租（常见划拨不对称）
	inst := &Instance{
		TenantID: viewerID, Name: "ova-i-" + suf, DomainName: "ova-" + suf + ".local",
		Port: 5432, DBType: "postgres", Host: "127.0.0.1", IDC: "default",
	}
	if err := db.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SyncAccessDomainsFromInstance(inst); err != nil {
		t.Fatal(err)
	}
	ad, err := db.GetAccessDomainByDomainType(inst.DomainName, "postgres")
	if err != nil || ad == nil {
		t.Fatalf("access domain: %v %#v", err, ad)
	}
	if err := db.AssignAccessDomainTenant(ad.ID, viewerID); err != nil {
		t.Fatal(err)
	}
	rec := &Database{TenantID: fromID, InstanceID: inst.ID, DBName: "db_ova_" + suf[:8], Visibility: "global"}
	if err := db.CreateDatabase(rec); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_access_domains WHERE id=?`, ad.ID)
		_, _ = db.Exec(`DELETE FROM tbl_databases WHERE id=?`, rec.ID)
		_, _ = db.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = db.Exec(`DELETE FROM tbl_tenants WHERE id IN (?,?)`, fromID, viewerID)
	})

	own, err := db.ListDatabasesInTenant(viewerID, inst.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != 0 {
		t.Fatalf("own tenant should have 0 dbs, got %d", len(own))
	}

	via, err := db.ListDatabasesViaInstanceAccess(viewerID, "", inst.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range via {
		if d.ID == rec.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("owned domain should expand foreign db %s; got %d rows", rec.ID, len(via))
	}

	ok, err := db.DatabaseVisibleViaInstanceAccess(viewerID, "", rec)
	if err != nil || !ok {
		t.Fatalf("DatabaseVisibleViaInstanceAccess: ok=%v err=%v", ok, err)
	}
}
