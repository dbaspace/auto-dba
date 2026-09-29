package store

import (
	"os"
	"testing"
)

func TestApplyProxyConfVersion(t *testing.T) {
	s := &ProxyService{ConfContent: "cur", ConfPath: "/a", VIP: "1.1.1.1", ListenPort: 1, BackendsJSON: "[]"}
	ApplyProxyConfVersion(s, &ProxyConfVersion{
		ConfContent: "old", ConfPath: "/b", VIP: "2.2.2.2", ListenPort: 3306, BackendsJSON: `[{"host":"h"}]`,
	})
	if s.ConfContent != "old" || s.ConfPath != "/b" || s.VIP != "2.2.2.2" || s.ListenPort != 3306 {
		t.Fatalf("apply mismatch: %+v", s)
	}
}

func TestProxyConfVersion_SaveAndRestore(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	s := &ProxyService{
		Name: "proxy-ver-selftest", Kind: "haproxy", ConfPath: "/etc/haproxy/haproxy.cfg",
		ConfContent: "v1\n", ListenPort: 3306, Status: "draft", CreatedBy: "u", UpdatedBy: "u",
	}
	if err := d.UpsertProxyService(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteProxyService(s.ID) })

	v1, err := d.SaveProxyConfVersionIfChanged(s, "create", "u", "127.0.0.1", "")
	if err != nil || v1 == nil || v1.VersionNo != 1 {
		t.Fatalf("v1=%v err=%v", v1, err)
	}
	again, err := d.SaveProxyConfVersionIfChanged(s, "save", "u", "", "")
	if err != nil || again != nil {
		t.Fatalf("expected nil version, got %v err=%v", again, err)
	}
	s.ConfContent = "v2\n"
	v2, err := d.SaveProxyConfVersionIfChanged(s, "save", "u", "", "edit")
	if err != nil || v2 == nil || v2.VersionNo != 2 {
		t.Fatalf("v2=%v err=%v", v2, err)
	}
	list, total, err := d.ListProxyConfVersions(s.ID, 10, 0)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("list total=%d len=%d err=%v", total, len(list), err)
	}
	got, err := d.GetProxyConfVersion(s.ID, v1.ID)
	if err != nil || got == nil || got.ConfContent != "v1\n" {
		t.Fatalf("get v1=%v err=%v", got, err)
	}
	byNo, err := d.GetProxyConfVersion(s.ID, "1")
	if err != nil || byNo == nil || byNo.ID != v1.ID {
		t.Fatalf("by no=%v", byNo)
	}
	ApplyProxyConfVersion(s, got)
	if err := d.UpsertProxyService(s); err != nil {
		t.Fatal(err)
	}
	restored, err := d.SaveProxyConfVersionIfChanged(s, "restore", "u", "", "from v1")
	if err != nil || restored == nil || restored.VersionNo != 3 {
		t.Fatalf("restore ver=%v err=%v", restored, err)
	}
	if err := d.PruneProxyConfVersions(s.ID, 2); err != nil {
		t.Fatal(err)
	}
	_, total, err = d.ListProxyConfVersions(s.ID, 20, 0)
	if err != nil || total != 2 {
		t.Fatalf("after prune total=%d err=%v", total, err)
	}
}
