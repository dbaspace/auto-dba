package alert

import (
	"testing"

	"auto-dba/internal/store"
)

func testRegistry() *registryIndex {
	return newRegistryIndex([]store.Instance{
		{ID: "h1", Name: "pg-server-37", DBType: "host", Host: "10.128.16.37"},
		{ID: "p1", Name: "pg-order", DBType: "postgres", Host: "10.20.0.5", Port: 5432},
		{ID: "m1", Name: "mysql-a", DBType: "mysql", Host: "10.30.0.6,10.30.0.7"},
		{ID: "c1", Name: "cloud-pg", DBType: "postgres", CloudInstanceID: "postgres-abc123"},
	})
}

func TestRegistryUnregisteredDropped(t *testing.T) {
	ix := testRegistry()
	if _, ok := ix.match(map[string]string{"instance": "10.99.99.99:9187"}, "postgres"); ok {
		t.Fatal("unregistered address should not match")
	}
}

func TestRegistryHostNotMatchedByPGRule(t *testing.T) {
	ix := testRegistry()
	if _, ok := ix.match(map[string]string{"instance": "10.128.16.37:9211"}, "postgres"); ok {
		t.Fatal("host-only address must not trigger postgres rule")
	}
	it, ok := ix.match(map[string]string{"instance": "10.128.16.37:9100"}, "host")
	if !ok || it.ID != "h1" {
		t.Fatalf("host rule should match registered host, got %+v ok=%v", it, ok)
	}
}

func TestRegistryTypeMatched(t *testing.T) {
	ix := testRegistry()
	if it, ok := ix.match(map[string]string{"instance": "10.20.0.5:9187"}, "postgres"); !ok || it.ID != "p1" {
		t.Fatalf("registered postgres should match, got %+v ok=%v", it, ok)
	}
	if it, ok := ix.match(map[string]string{"instance": "10.30.0.7:9104"}, "mysql"); !ok || it.ID != "m1" {
		t.Fatalf("multi-node mysql host should match, got %+v ok=%v", it, ok)
	}
	if it, ok := ix.match(map[string]string{"d_b_instance_name": "postgres-abc123"}, "postgres"); !ok || it.ID != "c1" {
		t.Fatalf("cloud instance name should match, got %+v ok=%v", it, ok)
	}
}

func TestRegistryUnknownServiceOnlyNeedsRegistered(t *testing.T) {
	ix := testRegistry()
	if _, ok := ix.match(map[string]string{"instance": "10.20.0.5:9999"}, "custom-exporter"); !ok {
		t.Fatal("unknown service should only require registration")
	}
}

func TestRegisteredLabelsKeepOriginal(t *testing.T) {
	in := map[string]string{"instance": "10.20.0.5:9187", "service": "postgres"}
	out := registeredLabels(in, &store.Instance{Name: "pg-order", DBType: "postgres", Host: "10.20.0.5"})
	if out[LabelRegisteredHost] != "10.20.0.5" || out[LabelRegisteredName] != "pg-order" || out[LabelRegisteredType] != "postgres" {
		t.Fatalf("registered labels: %+v", out)
	}
	if _, ok := in[LabelRegisteredHost]; ok {
		t.Fatal("input labels must not be mutated (fingerprint uses them)")
	}
}
