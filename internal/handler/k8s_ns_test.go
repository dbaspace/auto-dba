package handler

import "testing"

func TestNsAllowed(t *testing.T) {
	h := &K8sHandler{}
	if !h.nsAllowed(nil, "any") {
		t.Fatal("nil allow = all")
	}
	if h.nsAllowed([]string{"a"}, "") {
		t.Fatal("cluster-scoped denied for non-owner")
	}
	if !h.nsAllowed([]string{"a", "b"}, "b") {
		t.Fatal("bound ns allowed")
	}
	if h.nsAllowed([]string{"a"}, "x") {
		t.Fatal("unbound ns denied")
	}
}
