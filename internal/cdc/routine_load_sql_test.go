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

func TestRenderRoutineLoadFourTemplates(t *testing.T) {
	cols := []pgCol{
		{Name: "id", UDT: "int8", DataType: "bigint"},
		{Name: "biz_date", UDT: "date", DataType: "date", IsNullable: true},
		{Name: "created_at", UDT: "timestamp", DataType: "timestamp without time zone"},
		{Name: "updated_at", UDT: "timestamptz", DataType: "timestamp with time zone", IsNullable: true},
		{Name: "extra", UDT: "jsonb", DataType: "jsonb"},
		{Name: "active", UDT: "bool", DataType: "boolean"},
	}
	unwrap := renderRoutineLoadSQL("db", "orders", "rl_orders", cols, 3, "k:9092", []string{"t"}, "after_only", "", "without_schema")
	if strings.Contains(unwrap, "jsonpaths") || strings.Contains(unwrap, "get_json_string") {
		t.Fatalf("unwrap should be bare columns: %s", unwrap)
	}
	if !strings.Contains(unwrap, "`extra`=parse_json(`extra`)") || !strings.Contains(unwrap, "`active`=coalesce(`active`,\"false\")") {
		t.Fatalf("unwrap transforms: %s", unwrap)
	}
	if strings.Contains(unwrap, "`updated_at`=DATE_ADD") || strings.Contains(unwrap, "IF(") {
		t.Fatalf("unwrap timestamptz should stay bare: %s", unwrap)
	}

	withAfter := renderRoutineLoadSQL("db", "orders", "rl_orders", cols, 3, "k:9092", []string{"t"}, "after_only", "", "with_schema")
	if !strings.Contains(withAfter, `"jsonpaths" = "[\"$.payload\"]"`) {
		t.Fatalf("with_schema jsonpaths: %s", withAfter)
	}
	if strings.Contains(withAfter, "parse_json") || !strings.Contains(withAfter, "`extra`=get_json_string(payload,'$.extra')") {
		t.Fatalf("with_schema json stays get_json_string: %s", withAfter)
	}
	if !strings.Contains(withAfter, "`biz_date`=IF(") || !strings.Contains(withAfter, "= '0'") || !strings.Contains(withAfter, ", NULL, ") {
		t.Fatalf("nullable date should be null-safe: %s", withAfter)
	}
	if !strings.Contains(withAfter, "`updated_at`=IF(") || strings.Contains(withAfter, "`updated_at`=DATE_ADD") {
		t.Fatalf("nullable timestamptz should be IF get_json_string: %s", withAfter)
	}
	if !strings.Contains(withAfter, "`created_at`=DATE_ADD") || strings.Contains(withAfter, "`created_at`=IF(") {
		t.Fatalf("not-null timestamp stays DATE_ADD: %s", withAfter)
	}

	withBefore := renderRoutineLoadSQL("db", "orders", "rl_orders", cols, 3, "k:9092", []string{"t"}, "before_after", "", "with_schema")
	if !strings.Contains(withBefore, `"jsonpaths" = "[\"$.payload\"]"`) || !strings.Contains(withBefore, "$.after.id") {
		t.Fatalf("with_schema before_after: %s", withBefore)
	}
	if !strings.Contains(withBefore, "`is_physical_deleted`='false'") || !strings.Contains(withBefore, "$.source.db") {
		t.Fatalf("with_schema before_after meta: %s", withBefore)
	}

	noBefore := renderRoutineLoadSQL("db", "orders", "rl_orders", cols, 3, "k:9092", []string{"t"}, "before_after", "", "without_schema")
	if !strings.Contains(noBefore, `"jsonpaths" = "[\"$.after\", \"$.source\"]"`) {
		t.Fatalf("noschema before_after jsonpaths: %s", noBefore)
	}
	if strings.Contains(noBefore, "$.after.id") || strings.Contains(noBefore, "__source_db") || strings.Contains(noBefore, "__deleted") {
		t.Fatalf("template B path: %s", noBefore)
	}
	if !strings.Contains(noBefore, "get_json_string(payload,'$.id')") || !strings.Contains(noBefore, "get_json_string(source_data,'$.db')") {
		t.Fatalf("template B columns: %s", noBefore)
	}
	if !strings.Contains(noBefore, "`is_physical_deleted`='false'") {
		t.Fatalf("template B deleted: %s", noBefore)
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
