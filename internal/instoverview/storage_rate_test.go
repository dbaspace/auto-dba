package instoverview

import "testing"

func TestPickStorageRate(t *testing.T) {
	hits := []RateHit{
		{Name: "pg-a", IDC: "test", Rate: 40.19},
		{Name: "pg-a", IDC: "prod", Rate: 80},
		{Name: "test-pg", IDC: "other", Rate: 78.1},
	}
	if v, ok := PickStorageRate("pg-a", []string{"test"}, hits); !ok || v != 40.19 {
		t.Fatalf("idc match: %v %v", v, ok)
	}
	if _, ok := PickStorageRate("pg-a", []string{"beijing"}, hits); ok {
		t.Fatal("two idcs and neither matches")
	}
	if v, ok := PickStorageRate("test-pg", []string{"test"}, hits); !ok || v != 78.1 {
		t.Fatalf("single series ignores idc mismatch: %v %v", v, ok)
	}
	if v, ok := PickStorageRate("Test-PG", nil, hits); !ok || v != 78.1 {
		t.Fatalf("name fold: %v %v", v, ok)
	}
}
