package handler

import "testing"

func TestReviewerAllowedByUnion(t *testing.T) {
	if msg := reviewerAllowedByUnion("songxincong", [][]string{
		{"songxincong", "zongjianhui"},
		{"admin"},
	}); msg != "" {
		t.Fatalf("union should allow name on one list: %s", msg)
	}
	if msg := reviewerAllowedByUnion("nobody", [][]string{
		{"songxincong"},
		{"admin"},
	}); msg == "" {
		t.Fatal("expected reject when not on any list")
	}
	if msg := reviewerAllowedByUnion("admin", [][]string{nil, {}}); msg != "" {
		t.Fatalf("all empty should pass: %s", msg)
	}
}
