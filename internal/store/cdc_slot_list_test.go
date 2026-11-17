package store

import (
	"os"
	"strings"
	"testing"
)

func TestCDCListExtraDropsDebeziumBody(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AUTO_DBA_TEST_DSN"))
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	desc := `-- 说明` + "\n" + `-- auto-dba-cdc:{"slot_name":"s1","connector_name":"c1","source_host":"pg.example","source_port":"5432","debezium_config":{"database.hostname":"pg.bill.example","database.port":"5433","table.include.list":"db.t"},"shard_db_bindings":[{"db_name":"db_1","slot_name":"s2","connector_name":"c2"}]}`
	var slim string
	if err := d.QueryRow(`SELECT COALESCE(cdc_list_extra(?)::text,'')`, desc).Scan(&slim); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"slot_name": "s1"`, `"connector_name": "c1"`, `"source_host": "pg.example"`, `"database.hostname": "pg.bill.example"`, `"slot_name": "s2"`} {
		if !strings.Contains(slim, want) {
			t.Fatalf("slim missing %s: %s", want, slim)
		}
	}
	if strings.Contains(slim, "table.include.list") {
		t.Fatalf("debezium body leaked: %s", slim)
	}
	var bad string
	if err := d.QueryRow(`SELECT COALESCE(cdc_list_extra(?)::text,'')`, `-- auto-dba-cdc:{not-json`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != "" && bad != "null" {
		t.Fatalf("bad json should be empty, got %s", bad)
	}
}
