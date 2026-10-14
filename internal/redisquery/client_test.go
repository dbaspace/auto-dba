package redisquery

import (
	"testing"

	"auto-dba/internal/store"
)

func TestParseDBIndex(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"db0", 0, false},
		{"DB3", 3, false},
		{"15", 15, false},
		{"db16", 0, true},
		{"foo", 0, true},
	}
	for _, c := range cases {
		got, err := ParseDBIndex(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("%q should fail", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("%q got %d err=%v want %d", c.in, got, err, c.want)
		}
	}
}

func TestAddr(t *testing.T) {
	if got := Addr(nil); got != "localhost:6379" {
		t.Fatalf("nil addr %s", got)
	}
	inst := &store.Instance{Host: "10.0.0.1", Port: 6380, Domains: []string{"redis.a.example"}}
	if got := Addr(inst); got != "10.0.0.1:6380" {
		t.Fatalf("host prefer %s", got)
	}
}

func TestResolveHost(t *testing.T) {
	inst := &store.Instance{
		Host:       "10.0.0.1",
		DomainName: "redis.primary.example",
		Domains:    []string{"redis.a.example", "redis.b.example"},
		Port:       6379,
	}
	h, err := ResolveHost(inst, "")
	if err != nil || h != "10.0.0.1" {
		t.Fatalf("default: %q %v", h, err)
	}
	h, err = ResolveHost(inst, "redis.a.example")
	if err != nil || h != "redis.a.example" {
		t.Fatalf("domain: %q %v", h, err)
	}
	if _, err := ResolveHost(inst, "evil.example"); err == nil {
		t.Fatal("expected reject foreign domain")
	}
	onlyDom := &store.Instance{Domains: []string{"redis.only.example"}, Port: 6379}
	h, err = ResolveHost(onlyDom, "")
	if err != nil || h != "redis.only.example" {
		t.Fatalf("domains-only default: %q %v", h, err)
	}
}

func TestInstanceHostsDedup(t *testing.T) {
	inst := &store.Instance{
		Host:       "Redis.A.Example",
		DomainName: "redis.a.example",
		Domains:    []string{"redis.a.example", "redis.b.example"},
	}
	got := InstanceHosts(inst)
	if len(got) != 2 {
		t.Fatalf("want 2 unique hosts, got %#v", got)
	}
}
