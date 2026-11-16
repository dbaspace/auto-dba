package handler

import "testing"

func TestDCLReviewerBypassesTenantForAdminOnly(t *testing.T) {
	if !dclReviewerBypassesTenant(nil, "admin") {
		t.Fatal("admin should bypass tenant membership")
	}
	if !dclReviewerBypassesTenant(nil, "Admin") {
		t.Fatal("admin match is case-insensitive")
	}
	if dclReviewerBypassesTenant(nil, "chenjiachi") {
		t.Fatal("ordinary user still needs tenant membership")
	}
}

func TestResolveDCLReviewerAcceptsAdminWithoutStore(t *testing.T) {
	name, err := resolveDCLReviewer(nil, "tenant-x", "admin")
	if err != nil || name != "admin" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}
