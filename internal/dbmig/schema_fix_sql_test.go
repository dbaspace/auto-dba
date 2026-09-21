package dbmig

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizePGDefault(t *testing.T) {
	a := `nextval('tbl_anomaly_detection_result_id_seq'::regclass)`
	b := `nextval( 'tbl_anomaly_detection_result_id_seq' :: regclass )`
	if normalizePGDefault(a) != normalizePGDefault(b) {
		t.Fatalf("expected equal: %q vs %q", normalizePGDefault(a), normalizePGDefault(b))
	}
}

func TestRewritePGIndexDef(t *testing.T) {
	def := `CREATE INDEX idx_x ON public.t_a USING btree (id)`
	got := rewritePGIndexDef(def, "public", "t_a", "app", "t_b")
	want := "ON " + tableRef("postgres", "app", "t_b")
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in %q", want, got)
	}
}

func TestBuildSchemaFixSQL_rightOnlyAndMatch(t *testing.T) {
	items := []TableSchemaDiff{
		{Table: "a", Status: "match", Message: "一致"},
		{Table: "b", Status: "right_only", Message: "仅目标端存在"},
	}
	script, err := BuildSchemaFixSQL(context.Background(), nil, "postgres", "public", "public", items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(items[1].FixSQL, "跳过") {
		t.Fatalf("right_only should skip: %q", items[1].FixSQL)
	}
	if !strings.Contains(script, "跳过") {
		t.Fatalf("script should mention skip: %s", script)
	}
}

func TestPGSequenceSetDefaultSQL(t *testing.T) {
	def := `nextval('tbl_anomaly_detection_result_id_seq'::regclass)`
	stmts := pgSequenceSetDefaultSQL("public", "public", "tbl_anomaly_detection_result", "id", def)
	joined := strings.Join(stmts, "")
	for _, want := range []string{
		"CREATE SEQUENCE IF NOT EXISTS",
		"tbl_anomaly_detection_result_id_seq",
		"SET DEFAULT nextval",
		"OWNED BY",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestBuildPGCreatePolicySQL(t *testing.T) {
	sql := buildPGCreatePolicySQL(`"public"."t"`, pgPolicyInfo{
		Name:       "tenant_anomaly_isolation",
		Permissive: true,
		Cmd:        "ALL",
		Roles:      "PUBLIC",
		Qual:       `((tenant_id = (current_setting('app.current_tenant_id'::text, true))::bigint))`,
	})
	for _, want := range []string{
		"CREATE POLICY",
		"tenant_anomaly_isolation",
		"ENABLE", // should NOT be here - policy only
		"USING",
	} {
		if want == "ENABLE" {
			if strings.Contains(sql, "ENABLE ROW") {
				t.Fatalf("policy SQL should not enable RLS: %s", sql)
			}
			continue
		}
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in %s", want, sql)
		}
	}
}

func TestRewriteFunctionDefSchema(t *testing.T) {
	def := `CREATE OR REPLACE FUNCTION public.fn_x() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END; $$`
	got := rewriteFunctionDefSchema(def, "public", "app")
	if !strings.Contains(got, "app") || strings.Contains(got, "FUNCTION public.") {
		t.Fatalf("schema rewrite failed: %s", got)
	}
}
