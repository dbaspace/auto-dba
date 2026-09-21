package dbmig

import (
	"database/sql"
	"strings"
	"testing"
)

func TestDiffMySQLTableSchema_columnsAndExtras(t *testing.T) {
	left := &mysqlTableInfo{
		Columns: []mysqlColInfo{
			{Name: "id", Type: "bigint", Nullable: false, Extra: "auto_increment"},
			{Name: "name", Type: "varchar(64)", Nullable: false, Comment: "名称"},
			{Name: "score", Type: "decimal(10,2)", Nullable: true, Default: sql.NullString{String: "0", Valid: true}},
		},
		Constraints: []mysqlConstraintInfo{
			{Name: "PRIMARY", Type: "PRIMARY KEY", Def: "id"},
			{Name: "uq_name", Type: "UNIQUE", Def: "name"},
		},
		Indexes: []mysqlIndexInfo{
			{Name: "idx_score", Unique: false, Columns: "score", Type: "BTREE", Def: "INDEX (score) USING BTREE"},
		},
		TableComment: "demo",
		Triggers: []mysqlTriggerInfo{
			{Name: "trg_bi", Timing: "BEFORE", Event: "INSERT", Statement: "SET NEW.score = 0", CreateSQL: "CREATE TRIGGER trg_bi ..."},
		},
	}
	right := &mysqlTableInfo{
		Columns: []mysqlColInfo{
			{Name: "id", Type: "bigint", Nullable: false},
			{Name: "name", Type: "varchar(64)", Nullable: false},
		},
		Constraints: []mysqlConstraintInfo{
			{Name: "PRIMARY", Type: "PRIMARY KEY", Def: "id"},
		},
	}
	diffs := diffMySQLTableSchema(left, right)
	kinds := map[string]bool{}
	for _, d := range diffs {
		kinds[d.Kind] = true
	}
	for _, k := range []string{"column", "comment", "constraint", "index", "trigger"} {
		if !kinds[k] {
			t.Fatalf("expected kind %s, got %+v", k, diffs)
		}
	}
}

func TestBuildMySQLAddConstraintAndIndex(t *testing.T) {
	ref := "`t`"
	pk := buildMySQLAddConstraint(ref, mysqlConstraintInfo{Name: "PRIMARY", Type: "PRIMARY KEY", Def: "id"})
	if !strings.Contains(pk, "ADD PRIMARY KEY") {
		t.Fatal(pk)
	}
	uq := buildMySQLAddConstraint(ref, mysqlConstraintInfo{Name: "uq_x", Type: "UNIQUE", Def: "a,b"})
	if !strings.Contains(uq, "ADD UNIQUE") || !strings.Contains(uq, "`a`") {
		t.Fatal(uq)
	}
	idx := buildMySQLAddIndex(ref, mysqlIndexInfo{Name: "idx_a", Columns: "a", Unique: false})
	if !strings.Contains(idx, "ADD INDEX") {
		t.Fatal(idx)
	}
	fk := buildMySQLAddFK(ref, mysqlFKInfo{
		Name: "fk_t", Columns: "tid", RefTable: "parent", RefColumns: "id", OnUpdate: "CASCADE", OnDelete: "RESTRICT",
	})
	if !strings.Contains(fk, "FOREIGN KEY") || !strings.Contains(fk, "REFERENCES") {
		t.Fatal(fk)
	}
}

func TestMySQLColDDLPartIncludesComment(t *testing.T) {
	part := mysqlColDDLPart(mysqlColInfo{
		Name: "name", Type: "varchar(32)", Nullable: false, Comment: "名称字段",
	})
	if !strings.Contains(part, "COMMENT") || !strings.Contains(part, "名称字段") {
		t.Fatal(part)
	}
}

func TestSelectSchemaEngine(t *testing.T) {
	pg, err := selectSchemaEngine("postgres")
	if err != nil || !pg.IsRoutineVirtualTable(schemaFunctionsVirtualTable) {
		t.Fatal(err, pg)
	}
	my, err := selectSchemaEngine("mysql")
	if err != nil || !my.IsRoutineVirtualTable(schemaRoutinesVirtualTable) {
		t.Fatal(err, my)
	}
	if _, err := selectSchemaEngine("oracle"); err == nil {
		t.Fatal("expected error")
	}
}
