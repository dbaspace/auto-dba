package dbmig

import (
	"strings"
	"testing"
)

func TestNormalizePGConstraintDef_arrayCastVariants(t *testing.T) {
	a := `CHECK (topic_kind::text = ANY (ARRAY['BASE'::character varying, 'MESSAGE'::character varying]::text[]))`
	b := `CHECK (topic_kind::text = ANY (ARRAY['BASE'::character varying::text, 'MESSAGE'::character varying::text]))`
	na, nb := normalizePGConstraintDef(a), normalizePGConstraintDef(b)
	if na != nb {
		t.Fatalf("expected equivalent CHECK defs to normalize equal:\n  a=%q\n  b=%q", na, nb)
	}
}

func TestComparePGTableSchemaInfo_checkDefCastVariantsEqual(t *testing.T) {
	cols := []pgColInfo{
		{Name: "topic", DataType: "character varying(255)", NotNull: true},
		{Name: "topic_kind", DataType: "character varying(20)", NotNull: true},
	}
	src := &pgTableSchemaInfo{
		Columns: cols,
		Constraints: []pgConstraintInfo{
			{Name: "tbl_sync_topic_config_pkey", Type: "p", Def: "PRIMARY KEY (topic)"},
			{Name: "chk_sync_topic_config_kind", Type: "c", Def: `CHECK (topic_kind::text = ANY (ARRAY['BASE'::character varying, 'MESSAGE'::character varying]::text[]))`},
			{Name: "chk_sync_topic_config_message_retention", Type: "c", Def: "CHECK (message_retention_hours > 0)"},
		},
	}
	tgt := &pgTableSchemaInfo{
		Columns: cols,
		Constraints: []pgConstraintInfo{
			{Name: "tbl_sync_topic_config_pkey", Type: "p", Def: "PRIMARY KEY (topic)"},
			{Name: "chk_sync_topic_config_kind", Type: "c", Def: `CHECK (topic_kind::text = ANY (ARRAY['BASE'::character varying::text, 'MESSAGE'::character varying::text]))`},
			{Name: "chk_sync_topic_config_message_retention", Type: "c", Def: "CHECK (message_retention_hours > 0)"},
		},
	}
	if err := comparePGTableSchemaInfo(src, tgt, "tbl_sync_topic_config"); err != nil {
		t.Fatalf("semantically equal CHECK cast variants should pass: %v", err)
	}
}

func TestDiffPGTableSchema_checkDefCastVariantsNoDiff(t *testing.T) {
	cols := []pgColInfo{
		{Name: "topic", DataType: "character varying(255)", NotNull: true},
		{Name: "topic_kind", DataType: "character varying(20)", NotNull: true},
	}
	left := &pgTableSchemaInfo{
		Columns: cols,
		Constraints: []pgConstraintInfo{
			{Name: "chk_sync_topic_config_kind", Type: "c", Def: `CHECK (topic_kind::text = ANY (ARRAY['BASE'::character varying, 'MESSAGE'::character varying]::text[]))`},
		},
	}
	right := &pgTableSchemaInfo{
		Columns: cols,
		Constraints: []pgConstraintInfo{
			{Name: "chk_sync_topic_config_kind", Type: "c", Def: `CHECK (topic_kind::text = ANY (ARRAY['BASE'::character varying::text, 'MESSAGE'::character varying::text]))`},
		},
	}
	diffs := diffPGTableSchema(left, right)
	if len(diffs) != 0 {
		t.Fatalf("expected no diffs for equivalent CHECK casts, got %#v", diffs)
	}
}

func TestDiffPGTableSchema_realConstraintDiff(t *testing.T) {
	cols := []pgColInfo{{Name: "id", DataType: "integer", NotNull: true}}
	left := &pgTableSchemaInfo{
		Columns:     cols,
		Constraints: []pgConstraintInfo{{Name: "chk_x", Type: "c", Def: "CHECK (id > 0)"}},
	}
	right := &pgTableSchemaInfo{
		Columns:     cols,
		Constraints: []pgConstraintInfo{{Name: "chk_x", Type: "c", Def: "CHECK (id > 1)"}},
	}
	diffs := diffPGTableSchema(left, right)
	if len(diffs) != 1 || diffs[0].Kind != "constraint" {
		t.Fatalf("expected one constraint diff, got %#v", diffs)
	}
}

func TestSameEngineFamily(t *testing.T) {
	if _, ok := SameEngineFamily("postgres", "postgresql"); !ok {
		t.Fatal("postgres family")
	}
	if _, ok := SameEngineFamily("mysql", "tidb"); !ok {
		t.Fatal("mysql family")
	}
	if _, ok := SameEngineFamily("postgres", "mysql"); ok {
		t.Fatal("hetero should fail")
	}
}

func TestComparePGTableSchemaInfo_constraintDiffMessage(t *testing.T) {
	src := &pgTableSchemaInfo{
		Columns: []pgColInfo{{Name: "id", DataType: "integer", NotNull: true}},
		Constraints: []pgConstraintInfo{
			{Name: "chk_x", Type: "c", Def: "CHECK (id > 0)"},
		},
	}
	tgt := &pgTableSchemaInfo{
		Columns: []pgColInfo{{Name: "id", DataType: "integer", NotNull: true}},
		Constraints: []pgConstraintInfo{
			{Name: "chk_x", Type: "c", Def: "CHECK (id > 1)"},
		},
	}
	err := comparePGTableSchemaInfo(src, tgt, "t")
	if err == nil {
		t.Fatal("expected constraint mismatch")
	}
	msg := err.Error()
	for _, want := range []string{"chk_x", "源=", "目标=", "id > 0", "id > 1"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error should contain %q, got %q", want, msg)
		}
	}
}
