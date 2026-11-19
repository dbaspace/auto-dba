package archive

import (
	"testing"
	"time"
)

func TestValidateDeleteCondition(t *testing.T) {
	if err := ValidateDeleteCondition("create_time < now()"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDeleteCondition(""); err == nil {
		t.Fatal("empty should fail")
	}
	if err := ValidateDeleteCondition("id=1; drop table t"); err == nil {
		t.Fatal("semicolon should fail")
	}
	if err := ValidateDeleteCondition("1=1 LIMIT 10"); err == nil {
		t.Fatal("limit should fail")
	}
	if err := ValidateDeleteCondition("WHERE id>0"); err == nil {
		t.Fatal("where prefix should fail")
	}
}

func TestBuildDeleteSQL(t *testing.T) {
	q, err := BuildMySQLDeleteSQL("t", "id>0")
	if err != nil || q != "DELETE FROM `t` WHERE id>0 LIMIT 10000" {
		t.Fatalf("%v %s", err, q)
	}
	q, err = BuildPGCTESQL("t", "id>0", []string{"id"})
	if err != nil || !containsAll(q, "WITH cte1", "DELETE FROM", `"t"`, "LIMIT 10000") {
		t.Fatalf("%v %s", err, q)
	}
	q, err = BuildPGCTESQL("t", "id>0", nil)
	if err != nil || !containsAll(q, "ctid", "DELETE FROM", "SELECT ctid FROM cte1", "LIMIT 10000") {
		t.Fatalf("ctid fallback: %v %s", err, q)
	}
	q, err = BuildPGDeleteByPKsSQL("t", []string{"id"}, [][]string{{"1"}, {"2"}})
	if err != nil || q != `DELETE FROM "t" WHERE "id" IN ('1', '2')` {
		t.Fatalf("%v %s", err, q)
	}
}

func TestSplitShardBounds(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bounds := SplitShardBounds([]HourCount{
		{Hour: base, Count: 60000},
		{Hour: base.Add(time.Hour), Count: 60000},
		{Hour: base.Add(2 * time.Hour), Count: 10000},
	}, 100000)
	if len(bounds) != 2 {
		t.Fatalf("bounds=%v", bounds)
	}
	if !bounds[0].Equal(base.Add(2*time.Hour)) || !bounds[1].Equal(base.Add(3*time.Hour)) {
		t.Fatalf("bounds=%v", bounds)
	}
}

func TestNormalizeStrategyAndPGMode(t *testing.T) {
	if NormalizeStrategy("direct_delete") != "delete_only" {
		t.Fatal(NormalizeStrategy("direct_delete"))
	}
	mode, err := ResolvePGMode("auto", false, 10)
	if err != nil || mode != "cte" {
		t.Fatalf("%v %s", err, mode)
	}
	if _, err := ResolvePGMode("shard", false, 1); err == nil {
		t.Fatal("shard without create_time")
	}
	mode, err = ResolvePGMode("auto", true, shardThreshold)
	if err != nil || mode != "shard" {
		t.Fatalf("auto shard: %v %s", err, mode)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, p string) bool {
	return len(s) >= len(p) && (s == p || len(p) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(p) <= len(s); i++ {
			if s[i:i+len(p)] == p {
				return true
			}
		}
		return false
	})()))
}
