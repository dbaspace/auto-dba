package dbmig

import (
	"strings"
	"testing"
)

func TestBuildPGMigrationPlanSerialIdentityAndSequence(t *testing.T) {
	info := &pgMigTable{
		Columns: []migCol{
			{
				Name: "id", DataType: "bigint", NotNull: true,
				Default: "nextval('public.questions_id_seq'::regclass)", SerialKind: "bigserial",
				SerialSequence: "public.questions_id_seq",
			},
			{Name: "name", DataType: "text", NotNull: true},
			{Name: "n", DataType: "integer", Identity: "a", NotNull: true},
		},
		Constraints: []pgConstraintInfo{{Name: "questions_pkey", Type: "p", Def: "PRIMARY KEY (id)"}},
		Indexes:     []pgIndexInfo{{Name: "questions_name_idx", Def: `CREATE INDEX questions_name_idx ON public.questions USING btree (name)`}},
		ForeignKeys: []pgConstraintInfo{{Name: "questions_user_fk", Type: "f", Def: `FOREIGN KEY (name) REFERENCES public.users(name)`}},
		SeqSyncs:    []pgSeqSync{{Column: "id", LastValue: 40, HasLast: true, Fallback: "public.questions_id_seq"}},
	}
	plan, err := buildPGMigrationPlan("public", "questions", "public", "questions", info, []pgTablePair{
		{SrcSchema: "public", SrcTable: "users", TgtSchema: "public", TgtTable: "users"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join(plan.Body, "\n")
	if !strings.Contains(body, `"id" BIGSERIAL`) {
		t.Fatalf("serial should use BIGSERIAL, got %s", body)
	}
	if strings.Contains(body, "CREATE SEQUENCE") {
		t.Fatalf("SERIAL must not CREATE SEQUENCE first: %s", body)
	}
	if !strings.Contains(body, "GENERATED ALWAYS AS IDENTITY") {
		t.Fatalf("missing identity: %s", body)
	}
	if !strings.Contains(body, "PRIMARY KEY") || !strings.Contains(body, "questions_name_idx") {
		t.Fatalf("missing pk or index: %s", body)
	}
	if strings.Contains(body, "FOREIGN KEY") {
		t.Fatalf("foreign key must be deferred: %s", body)
	}
	if len(plan.FKs) != 1 || !strings.Contains(plan.FKs[0], "questions_user_fk") {
		t.Fatalf("fk: %#v", plan.FKs)
	}
	if len(plan.SeqSyncs) != 1 || !plan.SeqSyncs[0].HasLast || plan.SeqSyncs[0].LastValue != 40 {
		t.Fatalf("seq sync: %#v", plan.SeqSyncs)
	}
	srcSQL := pgSetvalFromSourceSQL(plan.SeqSyncs[0].Fallback, plan.SeqSyncs[0].LastValue)
	if !strings.Contains(srcSQL, "setval") || !strings.Contains(srcSQL, "40") {
		t.Fatalf("schema_only setval: %s", srcSQL)
	}
	after := pgSetvalAfterDataSQL("public", "questions", "id", "public.questions_id_seq")
	if !strings.Contains(after, "COALESCE((SELECT MAX") || !strings.Contains(after, "pg_get_serial_sequence") && !strings.Contains(after, "questions_id_seq") {
		t.Fatalf("after data setval: %s", after)
	}
}

func TestExternalSequenceSharedSkipsOwnedBy(t *testing.T) {
	info := &pgMigTable{
		Columns: []migCol{
			{Name: "a", DataType: "bigint", Default: "nextval('public.shared_seq'::regclass)", ExternalSeqSchema: "public", ExternalSeqName: "shared_seq"},
			{Name: "b", DataType: "bigint", Default: "nextval('public.shared_seq'::regclass)", ExternalSeqSchema: "public", ExternalSeqName: "shared_seq"},
		},
		Sequences: []migSeq{{
			Schema: "public", Name: "shared_seq", DataType: "bigint",
			StartValue: 1, MinValue: 1, MaxValue: 100, Increment: 1, Cache: 1,
			SetOwnedBy: false, OwnedByColumn: "a",
		}},
	}
	plan, err := buildPGMigrationPlan("public", "t", "public", "t", info, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join(plan.Body, "\n")
	if strings.Count(body, "CREATE SEQUENCE") != 1 {
		t.Fatalf("shared sequence created once: %s", body)
	}
	if strings.Contains(body, "OWNED BY") {
		t.Fatalf("shared sequence must not OWNED BY: %s", body)
	}
	owned := buildOwnedBySQL(migSeq{SetOwnedBy: true, OwnedByColumn: "a", Name: "solo"}, "public", "t")
	if !strings.Contains(owned, "OWNED BY") {
		t.Fatal(owned)
	}
}

func TestUnparsedExternalSequenceFails(t *testing.T) {
	info := &pgMigTable{Columns: []migCol{{
		Name: "id", DataType: "bigint", Default: "nextval()",
	}}}
	_, err := buildPGMigrationPlan("public", "t", "public", "t", info, nil)
	if err == nil || !strings.Contains(err.Error(), "不支持自动迁移外部序列") {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultReferencingOtherColumnBecomesGenerated(t *testing.T) {
	cols := normalizeGeneratedDefaults([]migCol{
		{Name: "name", DataType: "text"},
		{Name: "name_lower", DataType: "text", Default: "lower(name)"},
	})
	def := buildMigColumnDef(cols[1], "public", "public")
	if !strings.Contains(def, "GENERATED ALWAYS AS (lower(name)) STORED") {
		t.Fatal(def)
	}
	if strings.Contains(def, "DEFAULT") {
		t.Fatal(def)
	}
}

func TestRewriteTriggerAvoidsOnDelete(t *testing.T) {
	def := `CREATE TRIGGER t AFTER INSERT ON orders FOR EACH ROW WHEN (NEW.note = 'ON DELETE CASCADE') EXECUTE FUNCTION public.fn()`
	out, err := rewriteTriggerDef(def, "public", "orders", "dest", "orders")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.ToUpper(out), "ON DELETE") != 1 {
		t.Fatalf("ON DELETE changed: %s", out)
	}
	if !strings.Contains(out, `"dest"."orders"`) {
		t.Fatalf("table not rewritten: %s", out)
	}
}

func TestIndexRewriteFailure(t *testing.T) {
	info := &pgMigTable{
		Columns: []migCol{{Name: "id", DataType: "integer"}},
		Indexes: []pgIndexInfo{{Name: "idx_odd", Def: "CREATE INDEX idx_odd USING btree (id)"}},
	}
	_, err := buildPGMigrationPlan("public", "t", "public", "t", info, nil)
	if err == nil || !strings.Contains(err.Error(), "无法改写索引") {
		t.Fatalf("got %v", err)
	}
}

func TestSourceRelationErrors(t *testing.T) {
	if err := pgSourceRelationError(false, "", "public", "t"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatal(err)
	}
	if err := pgSourceRelationError(true, "v", "public", "t"); err == nil || !strings.Contains(err.Error(), "不是表") {
		t.Fatal(err)
	}
	if err := pgSourceRelationError(true, "p", "public", "t"); err != nil {
		t.Fatal(err)
	}
}

func TestFilterExtensionsAndInstallError(t *testing.T) {
	got := filterPGExtensions([]string{"plpgsql", "postgis", "PostGIS", "uuid-ossp"})
	if len(got) != 2 || got[0] != "postgis" && got[1] != "postgis" {
		if strings.Join(got, ",") != "postgis,uuid-ossp" && strings.Join(got, ",") != "uuid-ossp,postgis" {
			t.Fatal(got)
		}
	}
	err := extensionInstallError("postgis", `CREATE EXTENSION IF NOT EXISTS "postgis"`, errTest("missing"))
	if !strings.Contains(err.Error(), "postgis") || !strings.Contains(err.Error(), "请在目标实例安装对应扩展包") {
		t.Fatal(err)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestPartitionAndTriggerDeferred(t *testing.T) {
	info := &pgMigTable{
		Columns:     []migCol{{Name: "id", DataType: "integer"}},
		PartitionBy: "RANGE (id)",
		Triggers: []migTrigger{{
			Name: "tr", Def: `CREATE TRIGGER tr BEFORE INSERT ON public.p FOR EACH ROW EXECUTE FUNCTION public.fn()`,
			FunctionSQL: "CREATE FUNCTION public.fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END; $$",
		}},
	}
	plan, err := buildPGMigrationPlan("public", "p", "public", "p", info, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join(plan.Body, "\n")
	if !strings.Contains(body, "PARTITION BY RANGE (id)") {
		t.Fatal(body)
	}
	if strings.Contains(body, "CREATE TRIGGER") || strings.Contains(body, "CREATE FUNCTION") {
		t.Fatalf("trigger must be deferred: %s", body)
	}
	joined := strings.Join(plan.Triggers, "\n")
	if !strings.Contains(joined, "CREATE FUNCTION") || !strings.Contains(joined, "CREATE TRIGGER") {
		t.Fatal(joined)
	}
	if pgCheckFunctionBodiesSQL != `SET check_function_bodies = false` {
		t.Fatal(pgCheckFunctionBodiesSQL)
	}
}

func TestFilterTriggersOutsideTask(t *testing.T) {
	allowed := map[string]struct{}{"public.orders": {}}
	kept, skipped := filterDeferredByTables([]deferredStmt{
		{SQL: "a", Table: "public.orders"},
		{SQL: "b", Table: "public.other"},
	}, allowed)
	if len(kept) != 1 || len(skipped) != 1 || skipped[0] != "public.other" {
		t.Fatalf("kept=%d skipped=%v", len(kept), skipped)
	}
}

func TestInsertOverridingAndMySQLFixes(t *testing.T) {
	q := pgInsertSQL(`"public"."t"`, `"id"`, "($1)", true)
	if !strings.Contains(q, "OVERRIDING SYSTEM VALUE") {
		t.Fatal(q)
	}
	plain := pgInsertSQL(`"public"."t"`, `"id"`, "($1)", false)
	if strings.Contains(plain, "OVERRIDING") {
		t.Fatal(plain)
	}
	ddl := "CREATE TABLE `orders` (\n  `id` int NOT NULL,\n  `uid` int DEFAULT NULL,\n  PRIMARY KEY (`id`),\n  CONSTRAINT `fk_u` FOREIGN KEY (`uid`) REFERENCES `users` (`id`)\n) ENGINE=InnoDB"
	body, fks := splitMySQLForeignKeys(ddl, "orders", []pgTablePair{{SrcTable: "users", TgtTable: "app_users"}})
	if strings.Contains(body, "FOREIGN KEY") {
		t.Fatal(body)
	}
	if len(fks) != 1 || !strings.Contains(fks[0], "ALTER TABLE `orders` ADD") || !strings.Contains(fks[0], "`app_users`") {
		t.Fatalf("%s", fks)
	}
	ai := mysqlAlterAutoIncrement("orders", 0)
	if ai != "ALTER TABLE `orders` AUTO_INCREMENT = 1" {
		t.Fatal(ai)
	}
	qualified := "CREATE TABLE `orders` (\n  `id` int NOT NULL,\n  CONSTRAINT `fk_u` FOREIGN KEY (`uid`) REFERENCES `srcdb`.`users` (`id`) ON DELETE CASCADE\n) ENGINE=InnoDB"
	body2, fks2 := splitMySQLForeignKeys(qualified, "orders", nil)
	if strings.Contains(body2, "FOREIGN KEY") || len(fks2) != 1 || !strings.Contains(fks2[0], "ON DELETE CASCADE") {
		t.Fatalf("body=%s fks=%v", body2, fks2)
	}
}
