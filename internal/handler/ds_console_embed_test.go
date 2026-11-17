package handler

import (
	"strings"
	"testing"
)

func TestDSConsoleListTenant(t *testing.T) {
	if got := dsConsoleListTenant("default", true, true); got != "" {
		t.Fatalf("platform console should list every env, got %q", got)
	}
	if got := dsConsoleListTenant("default", true, false); got != "default" {
		t.Fatalf("member console stays in tenant, got %q", got)
	}
	if got := dsConsoleListTenant("default", false, true); got != "default" {
		t.Fatalf("config page stays in tenant, got %q", got)
	}
}

func TestDSEmbedShimKeepsPlatformUser(t *testing.T) {
	if strings.Contains(dsEmbedUserShim, "setItem('user'") || strings.Contains(dsEmbedUserShim, `setItem("user"`) {
		t.Fatal("shim must not write the platform user key")
	}
	if !strings.Contains(dsEmbedUserShim, "ds-embed-user") {
		t.Fatal("shim should use ds-embed-user")
	}
	raw := []byte(`<html><head><script>localStorage.getItem('user')</script></head><body></body></html>`)
	out, ok := rewriteDSBody(raw, "text/html", "http://ds.example/dolphinscheduler", "/api/v1/ds-proxy/test")
	if !ok {
		t.Fatal("expected rewrite")
	}
	s := string(out)
	shimAt := strings.Index(s, "auto-dba-ds-user-shim")
	appAt := strings.Index(s, "localStorage.getItem('user')")
	if shimAt < 0 || appAt < 0 || shimAt > appAt {
		t.Fatalf("shim must run before dolphin scripts\n%s", s)
	}
	if strings.Contains(s, "localStorage.setItem('user'") {
		t.Fatal("rewritten page still writes platform user")
	}
}
