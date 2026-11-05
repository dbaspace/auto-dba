package dbmig

import (
	"testing"

	"auto-dba/internal/store"
)

func TestWithDialHostUsesDomain(t *testing.T) {
	orig := &store.Instance{Host: "10.0.0.1", DomainName: "pg.legacy.example", Port: 5432}
	got := WithDialHost(orig, " pg.bj.example ")
	if got == orig {
		t.Fatal("dial host should copy the instance")
	}
	if got.Host != "pg.bj.example" || orig.Host != "10.0.0.1" {
		t.Fatalf("copy host=%q orig=%q", got.Host, orig.Host)
	}
	if instHost(got) != "pg.bj.example" {
		t.Fatalf("dial address %q", instHost(got))
	}
	if WithDialHost(orig, "  ") != orig {
		t.Fatal("empty host should keep the instance address")
	}
}
