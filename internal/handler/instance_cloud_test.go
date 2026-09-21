package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestNormalizeInstanceCloud(t *testing.T) {
	it := &store.Instance{DBType: "postgres", CloudVendor: "TENCENT", CloudInstanceID: " postgres-abc ", CloudRegion: " ap-guangzhou "}
	if err := normalizeInstanceCloud(it); err != nil {
		t.Fatal(err)
	}
	if it.CloudVendor != "tencent" || it.CloudInstanceID != "postgres-abc" || it.CloudRegion != "ap-guangzhou" {
		t.Fatalf("normalized: %+v", it)
	}
	bad := &store.Instance{DBType: "postgres", CloudVendor: "tencent"}
	if err := normalizeInstanceCloud(bad); err == nil {
		t.Fatal("expected missing cloud instance id")
	}
	self := &store.Instance{DBType: "postgres", CloudVendor: "self"}
	if err := normalizeInstanceCloud(self); err != nil {
		t.Fatal(err)
	}
	if self.CloudVendor != "" {
		t.Fatalf("self should clear vendor, got %q", self.CloudVendor)
	}
	if err := normalizeInstanceCloud(&store.Instance{CloudVendor: "foo"}); err == nil {
		t.Fatal("expected invalid vendor")
	}
}
