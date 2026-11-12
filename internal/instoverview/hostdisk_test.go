package instoverview

import (
	"testing"

	"auto-dba/internal/store"
)

func TestCloudHostInfoSplitsDisks(t *testing.T) {
	it := store.CloudInstance{
		CPU: 16, MemoryMB: 65536, DiskGB: 1100, Status: "running",
		Extras: map[string]any{
			"SystemDisk": map[string]any{"DiskSize": float64(100), "DiskType": "CLOUD_SSD"},
			"DataDisks":  []any{map[string]any{"DiskSize": float64(500)}, map[string]any{"DiskSize": float64(500)}},
		},
	}
	got := CloudHostInfo(it)
	if got.SystemDiskGB != 100 || got.DataDiskGB != 1000 || got.CPU != 16 || got.CloudStatus != "running" {
		t.Fatalf("info = %+v", got)
	}
	if len(got.DataDisks) != 2 || got.DataDisks[0].GB != 500 || got.DataDisks[1].GB != 500 {
		t.Fatalf("disks = %+v", got.DataDisks)
	}
}

func TestHostDisplayDomain(t *testing.T) {
	if got := HostDisplayDomain([]string{"idc=beijing", "domain=", "domain=doris.example.cn"}, []string{"10.0.0.1"}, "10.0.0.1"); got != "doris.example.cn" {
		t.Fatalf("tag domain = %q", got)
	}
	if got := HostDisplayDomain(nil, []string{"a.example.cn", " b.example.cn "}, "old"); got != "a.example.cn，b.example.cn" {
		t.Fatalf("domains = %q", got)
	}
	if got := HostDisplayDomain(nil, nil, " legacy.example.cn "); got != "legacy.example.cn" {
		t.Fatalf("domain_name = %q", got)
	}
	if got := HostDisplayDomain([]string{"app=fe"}, nil, ""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestHostAppName(t *testing.T) {
	if got := HostAppName([]string{"idc=beijing", "App= fe-StarRocks ", "app=be-StarRocks"}); got != "fe-StarRocks" {
		t.Fatalf("app = %q", got)
	}
	if got := HostAppName([]string{"app=", "domain=x"}); got != "" {
		t.Fatalf("empty app = %q", got)
	}
}

func TestMatchCloudHosts(t *testing.T) {
	rows := []store.CloudInstance{
		{CloudID: "ins-a", SyncedAt: "1", Extras: map[string]any{"PrivateIpAddresses": []any{"10.0.0.1"}}},
		{CloudID: "ins-b", InstanceID: "h2", SyncedAt: "1"},
		{CloudID: "ins-c", SyncedAt: "1", Extras: map[string]any{"PrivateIpAddresses": []any{"10.0.0.3"}}},
	}
	got := MatchCloudHosts([]HostCloudKey{
		{ID: "h1", CloudID: "INS-A"},
		{ID: "h2"},
		{ID: "h3", Host: "10.0.0.3:22"},
		{ID: "h4", Host: "10.9.9.9"},
	}, rows)
	if got["h1"].CloudID != "ins-a" || got["h2"].CloudID != "ins-b" || got["h3"].CloudID != "ins-c" {
		t.Fatalf("match = %+v", got)
	}
	if _, ok := got["h4"]; ok {
		t.Fatal("unknown host must not match")
	}
}

func TestHostFSExprsScopedToHosts(t *testing.T) {
	addrs := HostAddrs([]string{"10.0.0.2:22", " 10.0.0.1 ", "10.0.0.2", ""})
	if len(addrs) != 2 || addrs[0] != "10.0.0.1" || addrs[1] != "10.0.0.2" {
		t.Fatalf("addrs = %v", addrs)
	}
	size, avail := HostFSExprs(addrs)
	want := `{fstype=~"ext4|xfs",instance=~"(10\\.0\\.0\\.1|10\\.0\\.0\\.2)(:[0-9]+)?"}`
	if size != "node_filesystem_size_bytes"+want || avail != "node_filesystem_avail_bytes"+want {
		t.Fatalf("size=%s avail=%s", size, avail)
	}
	if s, a := HostFSExprs(nil); s != "" || a != "" {
		t.Fatal("no hosts must not query")
	}
}

func TestHostDataUsageSkipsSystemMounts(t *testing.T) {
	gb := float64(1 << 30)
	got := HostDataUsage([]FSSample{
		{Instance: "10.0.0.1:9100", Mountpoint: "/", Device: "/dev/vda1", Size: 100 * gb, Avail: 10 * gb},
		{Instance: "10.0.0.1:9100", Mountpoint: "/boot", Device: "/dev/vda2", Size: gb, Avail: 0},
		{Instance: "10.0.0.1:9100", Mountpoint: "/data", Device: "/dev/vdb", Size: 500 * gb, Avail: 400 * gb},
		{Instance: "10.0.0.1:9100", Mountpoint: "/data/bind", Device: "/dev/vdb", Size: 500 * gb, Avail: 400 * gb},
		{Instance: "10.0.0.1:9100", Mountpoint: "/data2", Device: "/dev/vdc", Size: 500 * gb, Avail: 100 * gb},
		{Instance: "10.0.0.2:9100", Mountpoint: "/", Device: "/dev/vda1", Size: 50 * gb, Avail: 5 * gb},
	})
	u := got["10.0.0.1"]
	if u.Size != 1000*gb || u.Used != 500*gb || u.Rate() != 50 {
		t.Fatalf("usage = %+v rate=%v", u, u.Rate())
	}
	if _, ok := got["10.0.0.2"]; ok {
		t.Fatal("root-only host has no data disk usage")
	}
}
