package store

import "testing"

func TestSameTenant(t *testing.T) {
	if !sameTenant("t1", "") {
		t.Fatal("empty want should match any row")
	}
	if sameTenant("t1", "t2") {
		t.Fatal("different tenants must not match")
	}
	if !sameTenant("t1", "t1") {
		t.Fatal("same tenant should match")
	}
}

func TestAppendTenantFilter(t *testing.T) {
	q, args := appendTenantFilter("SELECT 1 WHERE 1=1", nil, "")
	if q != "SELECT 1 WHERE 1=1" || len(args) != 0 {
		t.Fatalf("empty tenant should not add clause: %s %v", q, args)
	}
	q, args = appendTenantFilter("SELECT 1 WHERE 1=1", nil, "tid-a")
	if q != "SELECT 1 WHERE 1=1 AND COALESCE(tenant_id,'')=?" || len(args) != 1 || args[0] != "tid-a" {
		t.Fatalf("got %s %v", q, args)
	}
}

func TestAssignTenantIDRequiresValue(t *testing.T) {
	d := &DB{}
	if err := d.AssignTenantID("tbl_issues", "x", ""); err == nil {
		t.Fatal("empty tenant_id must error")
	}
}
