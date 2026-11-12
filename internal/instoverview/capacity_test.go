package instoverview

import (
	"testing"

	"auto-dba/internal/store"
)

func TestStarRocksIDCAliasesUsesDefinedSuffix(t *testing.T) {
	defined := []string{"beijing", "beijing-t", "singapore", "singapore-t"}
	got := StarRocksIDCAliases("beijing", "", nil, defined)
	if !containsFold(got, "beijing") || !containsFold(got, "beijing-t") {
		t.Fatalf("aliases=%v", got)
	}
	if containsFold(got, "singapore") || containsFold(got, "singapore-t") {
		t.Fatalf("should not take other env: %v", got)
	}
}

func TestStarRocksIDCAliasesWithoutDictionary(t *testing.T) {
	got := StarRocksIDCAliases("beijing", "beijing", []string{"idc=beijing-t"}, nil)
	if !containsFold(got, "beijing") || !containsFold(got, "beijing-t") {
		t.Fatalf("aliases=%v", got)
	}
	if len(got) != 2 {
		t.Fatalf("aliases=%v", got)
	}
}

func TestSumCapacityCNameAndMiss(t *testing.T) {
	const gb = float64(1024 * 1024 * 1024)
	hits := []CapacityHit{
		{IDC: "beijing-t", Domain: "other.example", CName: "dorisdb.report.bj.restosuite.cn", Bytes: 100 * gb},
		{IDC: "singapore-t", Domain: "dorisdb.report.bj.restosuite.cn", Bytes: 999 * gb},
	}
	hosts := StarRocksHostCandidates(&store.Instance{Host: "dorisdb.report.bj.restosuite.cn"})
	idcs := StarRocksIDCAliases("beijing", "", nil, []string{"beijing", "beijing-t", "singapore-t"})
	sum, ok := SumCapacity(idcs, hosts, true, hits)
	if !ok || sum != 100*gb {
		t.Fatalf("sum=%v ok=%v", sum, ok)
	}
	if _, ok := SumCapacity(idcs, hosts, false, hits); ok {
		t.Fatal("cname 未定义时不应命中")
	}
	if _, ok := SumCapacity(idcs, []string{"no-such-host"}, true, hits); ok {
		t.Fatal("主机对不上时应保留 SHOW BACKENDS")
	}
}

func TestApplyPromCapacityKeepsShowBackendsWhenDiskMissing(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	dataGB := 118.60
	s := Sample{DataBytes: int64(dataGB * float64(gb)), DataKnown: true, DiskBytes: 7774 * gb, DiskKnown: true, StorageRate: 2}
	ApplyPromCapacity(&s, 0, 0)
	if s.DataBytes != int64(dataGB*float64(gb)) || s.DiskBytes != 7774*gb || s.StorageRate != 2 {
		t.Fatalf("show backends changed data=%d disk=%d rate=%v", s.DataBytes, s.DiskBytes, s.StorageRate)
	}
	data, disk := 12.2*1024*float64(gb), 18.2*1024*float64(gb)
	ApplyPromCapacity(&s, data, disk)
	if !s.DataKnown || !s.DiskKnown || !s.StorageRateKnown {
		t.Fatal("should overwrite both columns")
	}
	if s.StorageRate != data/disk*100 {
		t.Fatalf("rate=%v", s.StorageRate)
	}
}
