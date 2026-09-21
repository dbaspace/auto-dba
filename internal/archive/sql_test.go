package archive

import "testing"

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
	if err != nil || !containsAll(q, "WITH cte1", "DELETE FROM", `"t"`) {
		t.Fatalf("%v %s", err, q)
	}
	q, err = BuildPGCTESQL("t", "id>0", nil)
	if err != nil || !containsAll(q, "ctid", "DELETE FROM") {
		t.Fatalf("ctid fallback: %v %s", err, q)
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
