package handler

import "testing"

func TestNacosSplitConsoleURL(t *testing.T) {
	origin, path, err := nacosSplitConsoleURL("http://10.100.113.151:8080/next")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "http://10.100.113.151:8080" || path != "/next" {
		t.Fatalf("got origin=%q path=%q", origin, path)
	}
	origin, path, err = nacosSplitConsoleURL("http://10.100.113.151:8080/next/")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "http://10.100.113.151:8080" || path != "/next" {
		t.Fatalf("trailing slash: origin=%q path=%q", origin, path)
	}
	origin, path, err = nacosSplitConsoleURL("http://10.100.113.151:8080")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "http://10.100.113.151:8080" || path != "/next" {
		t.Fatalf("default next: origin=%q path=%q", origin, path)
	}
	if _, _, err := nacosSplitConsoleURL("not-a-url"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNacosProxyEnvPrefix(t *testing.T) {
	if got := nacosProxyEnvPrefix("test"); got != "/api/v1/nacos-proxy/test" {
		t.Fatalf("got %q", got)
	}
}
