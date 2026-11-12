package cloudsvc

import (
	"testing"

	"auto-dba/internal/cloud"
	"auto-dba/internal/store"
)

func TestMatchRegisteredHostIP(t *testing.T) {
	keys := registeredHostKeys([]store.Instance{
		{DBType: "host", Host: "10.0.0.2:22"},
		{DBType: "postgres", Host: "10.0.0.9"},
	})
	if keys["10.0.0.9"] {
		t.Fatal("database instance must not count as registered host")
	}
	inst := cloud.Instance{Host: "10.0.0.1", Extras: map[string]any{"PrivateIpAddresses": []any{"10.0.0.1", "10.0.0.2"}}}
	if got := matchRegisteredHostIP(inst, keys); got != "10.0.0.2" {
		t.Fatalf("got %q want 10.0.0.2", got)
	}
	if got := matchRegisteredHostIP(cloud.Instance{Host: "10.0.0.9"}, keys); got != "" {
		t.Fatalf("unregistered host matched %q", got)
	}
}
