package cdc

import "testing"

func TestSplitSchemaTable(t *testing.T) {
	s, n := SplitSchemaTable("public", "orders")
	if s != "public" || n != "orders" {
		t.Fatalf("got %s %s", s, n)
	}
	s, n = SplitSchemaTable("public", "app.orders")
	if s != "app" || n != "orders" {
		t.Fatalf("qualified got %s %s", s, n)
	}
}

func TestIntersectColumns(t *testing.T) {
	a := []ColumnInfo{{Name: "id"}, {Name: "name"}, {Name: "only_a"}}
	b := []ColumnInfo{{Name: "id"}, {Name: "name"}, {Name: "only_b"}}
	got := IntersectColumns([][]ColumnInfo{a, b})
	if len(got) != 2 || got[0].Name != "id" || got[1].Name != "name" {
		t.Fatalf("%+v", got)
	}
}
