package dbprobe

import "testing"

func TestParsePGNextvalSeq(t *testing.T) {
	sch, name := parsePGNextvalSeq(`nextval('tbl_campaign_other_record_0_id_seq'::regclass)`, "public")
	if sch != "public" || name != "tbl_campaign_other_record_0_id_seq" {
		t.Fatalf("got %q.%q", sch, name)
	}
	sch, name = parsePGNextvalSeq(`nextval('myschema.my_seq'::regclass)`, "public")
	if sch != "myschema" || name != "my_seq" {
		t.Fatalf("got %q.%q", sch, name)
	}
	sch, name = parsePGNextvalSeq(`now()`, "public")
	if sch != "" || name != "" {
		t.Fatalf("expected empty, got %q.%q", sch, name)
	}
}

func TestFirstTableFromSQL(t *testing.T) {
	cases := []struct {
		sql, schema, table string
		wantErr            bool
	}{
		{`SELECT * FROM tbl_a LIMIT 10`, "", "tbl_a", false},
		{`select a.* from "public"."project_dbs" join other o on 1=1`, "public", "project_dbs", false},
		{`SELECT * FROM db1.t1, db2.t2`, "db1", "t1", false},
		{`SELECT * FROM ` + "`users`" + ` u JOIN orders o`, "", "users", false},
		{"-- from fake\nSELECT 1 FROM real_table", "", "real_table", false},
		{`SELECT 1`, "", "", true},
		{``, "", "", true},
	}
	for _, tc := range cases {
		schema, table, err := FirstTableFromSQL(tc.sql)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("sql=%q expect error", tc.sql)
			}
			continue
		}
		if err != nil {
			t.Fatalf("sql=%q unexpected err: %v", tc.sql, err)
		}
		if schema != tc.schema || table != tc.table {
			t.Fatalf("sql=%q got %q.%q want %q.%q", tc.sql, schema, table, tc.schema, tc.table)
		}
	}
}
