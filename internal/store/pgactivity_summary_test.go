package store

import "testing"

func TestSummaryDurationBucketsShape(t *testing.T) {
	// 纯结构自检：确认分桶标签与空汇总零值可用（不依赖真实 DB）
	f0, f1, f10, f60 := float64(0), float64(1), float64(10), float64(60)
	buckets := []PGActivityDurationBucket{
		{Label: "<1s", Min: &f0, Max: &f1, Count: 0},
		{Label: "1-10s", Min: &f1, Max: &f10, Count: 0},
		{Label: "10-60s", Min: &f10, Max: &f60, Count: 0},
		{Label: ">60s", Min: &f60, Max: nil, Count: 0},
	}
	if len(buckets) != 4 {
		t.Fatalf("want 4 buckets, got %d", len(buckets))
	}
	sum := &PGActivitySummary{
		StateDistribution: []PGActivityKV{},
		DurationBuckets:   buckets,
		TopDatabases:      []PGActivityKV{},
		TopUsers:          []PGActivityKV{},
		TopInstances:      []PGActivityKV{},
		Trend:             []PGActivityTrendPoint{},
	}
	if sum.DurationBuckets[3].Max != nil {
		t.Fatal(">60s bucket max should be nil")
	}
}
