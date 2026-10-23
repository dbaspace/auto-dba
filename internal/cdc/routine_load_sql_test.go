package cdc

import (
	"strings"
	"testing"
)

func TestRoutineLoadCoversColumns(t *testing.T) {
	props := `{"columnToColumnExpr":"id,name,is_physical_deleted"}`
	if !routineLoadCoversColumns(props, []string{"id", "name"}) {
		t.Fatal("expected cover")
	}
	if routineLoadCoversColumns(props, []string{"id", "extra"}) {
		t.Fatal("missing extra")
	}
}

func TestRenderRoutineLoadWithoutSchemaMapsDeleted(t *testing.T) {
	sql := renderRoutineLoadSQL("db", "orders", "rl_orders", []pgCol{{Name: "id", UDT: "int8"}}, 3, "k:9092", []string{"t"}, "after_only", "", "without_schema")
	if strings.Contains(sql, "jsonpaths") {
		t.Fatalf("unwrap should not set jsonpaths: %s", sql)
	}
	if !strings.Contains(sql, "`is_physical_deleted`=`__deleted`") {
		t.Fatalf("missing deleted mapping: %s", sql)
	}
}

func TestRenderRoutineLoadWithSchemaPayload(t *testing.T) {
	sql := renderRoutineLoadSQL("db", "orders", "rl_orders", []pgCol{{Name: "id", UDT: "int8"}, {Name: "created_at", UDT: "timestamp"}}, 3, "k:9092", []string{"t"}, "after_only", "", "with_schema")
	for _, part := range []string{`$.payload`, "is_physical_deleted", "__deleted", "created_at"} {
		if !strings.Contains(sql, part) {
			t.Fatalf("with_schema missing %s: %s", part, sql)
		}
	}
}

func TestRenderRoutineLoadBeforeAfterFalseDeleted(t *testing.T) {
	sql := renderRoutineLoadSQL("db", "orders", "rl_orders", []pgCol{{Name: "id", UDT: "int8"}}, 3, "k:9092", []string{"t"}, "before_after", "", "without_schema")
	for _, part := range []string{`$.after`, `$.source`, "`is_physical_deleted`='false'"} {
		if !strings.Contains(sql, part) {
			t.Fatalf("before_after missing %s: %s", part, sql)
		}
	}
}
