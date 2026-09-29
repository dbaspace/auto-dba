package handler

import "testing"

func TestSkipDDLSchemaCheck(t *testing.T) {
	cases := []struct {
		name                string
		shardDB, shardTable bool
		want                bool
	}{
		{name: "none", want: false},
		{name: "shard_db", shardDB: true, want: true},
		{name: "shard_table", shardTable: true, want: true},
		{name: "both", shardDB: true, shardTable: true, want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := skipDDLSchemaCheck(c.shardDB, c.shardTable); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestDDLShardSkipSchemaFromNeedRollback(t *testing.T) {
	if ddlShardSkipSchema("") {
		t.Fatal("empty should not skip")
	}
	if ddlShardSkipSchema(`{"env":"prod"}`) {
		t.Fatal("no shard flags should not skip")
	}
	if !ddlShardSkipSchema(`{"shard_db":true,"shard_db_prefix":"app_"}`) {
		t.Fatal("shard_db should skip")
	}
	if !ddlShardSkipSchema(`{"shard_table":true,"shard_table_prefix":"t_"}`) {
		t.Fatal("shard_table should skip")
	}
	if !ddlShardSkipSchema(`{"shard_db":true,"shard_table":true}`) {
		t.Fatal("both should skip")
	}
	if ddlShardSkipSchema(`not-json`) {
		t.Fatal("invalid json should not skip")
	}
}
