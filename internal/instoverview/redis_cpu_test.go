package instoverview

import "testing"

func TestRedisCPUCandidates(t *testing.T) {
	got := RedisCPUCandidates("10.0.0.1", 6379, []string{"redis.auth.bj.example.cn", "10.0.0.1"})
	want := []string{
		"10.0.0.1:6379",
		"10.0.0.1",
		"redis.auth.bj.example.cn:6379",
		"redis.auth.bj.example.cn",
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("i=%d got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestPickRedisCPU(t *testing.T) {
	hits := []CPUSeriesHit{
		{Instance: "10.0.0.1:6379", Points: []CpuPoint{{T: 1, V: 12}, {T: 2, V: 15}}},
		{Instance: "redis.other:6379", Points: []CpuPoint{{T: 1, V: 90}}},
	}
	pts, ok := PickRedisCPU("10.0.0.1", 6379, nil, hits)
	if !ok || len(pts) != 2 || pts[1].V != 15 {
		t.Fatalf("host:port match: ok=%v pts=%v", ok, pts)
	}
	pts, ok = PickRedisCPU("redis.auth.bj.example.cn", 6379, []string{"10.0.0.1"}, hits)
	if !ok || len(pts) != 2 {
		t.Fatalf("domain falls back to host:port via domains: ok=%v pts=%v", ok, pts)
	}
	if _, ok := PickRedisCPU("missing", 6379, nil, hits); ok {
		t.Fatal("missing should miss")
	}
}
