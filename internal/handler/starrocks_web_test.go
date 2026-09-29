package handler

import "testing"

func TestNormalizeStarrocksWebBase(t *testing.T) {
	got, err := normalizeStarrocksWebBase("10.1.2.3:8030", 8030)
	if err != nil || got != "http://10.1.2.3:8030" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = normalizeStarrocksWebBase("fe.example.com", 8030)
	if err != nil || got != "http://fe.example.com:8030" {
		t.Fatalf("host only: %q err=%v", got, err)
	}
	got, err = normalizeStarrocksWebBase("https://fe.example.com:443/", 8030)
	if err != nil || got != "https://fe.example.com:443" {
		t.Fatalf("https: %q err=%v", got, err)
	}
}

func TestFirstFeNode(t *testing.T) {
	if firstFeNode("a:8030,b:8030") != "a:8030" {
		t.Fatal(firstFeNode("a:8030,b:8030"))
	}
}

func TestBuildFePageURL(t *testing.T) {
	base := "http://10.0.0.1:8030"
	got := buildFePageURL(base, "/system", "//routine_loads", true)
	want := "http://10.0.0.1:8030/system?path=%2F%2Froutine_loads"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = buildFePageURL(base, "/system", "", true)
	if got != base+"/system" {
		t.Fatalf("empty path: %q", got)
	}
	got = buildFePageURL(base, "/system", "?path=//dbs", true)
	if got != base+"/system?path=//dbs" {
		t.Fatalf("qpath: %q", got)
	}
}
