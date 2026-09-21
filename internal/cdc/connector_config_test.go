package cdc

import "testing"

func TestConfigMapsEqualAndMask(t *testing.T) {
	a := map[string]string{"database.password": "p1", "slot.name": "s"}
	b := map[string]string{"database.password": "p1", "slot.name": "s"}
	if !ConfigMapsEqual(a, b) {
		t.Fatal("equal maps")
	}
	masked := MaskConfigMap(a)
	if masked["database.password"] != connectorMaskValue {
		t.Fatalf("mask: %+v", masked)
	}
	merged := MergeConfigForUpdate(map[string]string{"database.password": connectorMaskValue, "slot.name": "s2"}, a)
	if merged["database.password"] != "p1" || merged["slot.name"] != "s2" {
		t.Fatalf("merge: %+v", merged)
	}
}

func TestTableIncludeDelta(t *testing.T) {
	cur := map[string]string{"table.include.list": "public.a,public.b"}
	hist := map[string]string{"table.include.list": "public.a,public.c"}
	add, drop := TableIncludeDelta(cur, hist)
	if len(add) != 1 || add[0] != "public.c" {
		t.Fatalf("add=%v", add)
	}
	if len(drop) != 1 || drop[0] != "public.b" {
		t.Fatalf("drop=%v", drop)
	}
}

func TestDiffConfigMaps(t *testing.T) {
	cur := map[string]string{"a": "1", "b": "2"}
	hist := map[string]string{"b": "3", "c": "4"}
	d := DiffConfigMaps(cur, hist)
	if len(d.Added) != 1 || d.Added[0] != "a" {
		t.Fatalf("added=%v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "c" {
		t.Fatalf("removed=%v", d.Removed)
	}
	if len(d.Changed) != 1 || d.Changed[0].Key != "b" {
		t.Fatalf("changed=%v", d.Changed)
	}
}

func TestMergeAndRemoveTableIncludeList(t *testing.T) {
	merged := MergeTableIncludeList("public.a", "public", []string{"b", "public.a"})
	if merged != "public.a,public.b" {
		t.Fatal(merged)
	}
	dropped := RemoveFromTableIncludeList("public.a,public.b", "public", []string{"b"})
	if dropped != "public.a" {
		t.Fatal(dropped)
	}
}

func TestParseConfigMap(t *testing.T) {
	got := ParseConfigMap(`{"connector.class":"io.debezium.connector.postgresql.PostgresConnector","slot.name":"s1"}`)
	if got["slot.name"] != "s1" {
		t.Fatalf("%+v", got)
	}
}
