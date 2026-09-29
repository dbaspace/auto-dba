package store

import "testing"

func TestHostRefWhere(t *testing.T) {
	where, args := hostRefWhere("h17", []string{"10.100.112.17", "Node17"})
	if where != "(host_instance_id=? OR LOWER(host) IN (?,?))" || len(args) != 3 || args[2] != "node17" {
		t.Fatalf("where=%q args=%v", where, args)
	}
	where, args = hostRefWhere("", []string{"10.0.0.1"})
	if where != "(LOWER(host) IN (?))" || len(args) != 1 {
		t.Fatalf("空 ID 不应匹配 host_instance_id='': where=%q args=%v", where, args)
	}
	if where, args = hostRefWhere(" ", nil); where != "1=0" || len(args) != 0 {
		t.Fatalf("where=%q args=%v", where, args)
	}
}

func TestNodeNeedsAgentRebind(t *testing.T) {
	live := map[string]bool{"a-live": true}
	exists := func(id string) bool { return live[id] }
	cases := []struct {
		cur  string
		want bool
	}{
		{"", true},
		{"a-deleted", true},
		{"a-live", false},
		{"a-new", false},
		{" a-new ", false},
	}
	for _, c := range cases {
		if got := nodeNeedsAgentRebind(c.cur, "a-new", exists); got != c.want {
			t.Fatalf("cur=%q got %v want %v", c.cur, got, c.want)
		}
	}
}
