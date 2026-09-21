package store

import "testing"

func TestIsActionURN(t *testing.T) {
	if !IsActionURN("um.action.instances.create") {
		t.Fatal("expected action urn")
	}
	if IsActionURN("um.sidebar.config-instances") {
		t.Fatal("sidebar should not be action")
	}
	if !IsActionMenu(Menu{URN: "um.action.instances.delete"}) {
		t.Fatal("expected action menu")
	}
}
