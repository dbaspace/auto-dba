package cdc

import "testing"

func TestBuildConfigMySQLAfterOnly(t *testing.T) {
	cfg := BuildConfigMySQLAfterOnly(DebeziumBuildParams{
		PGHost: "mysql.example.com", PGPort: 3306, PGUser: "repl", PGPassword: "secret",
		PGDbName: "appdb", Tables: []string{"orders", "items"}, TopicPrefix: "cdc_mysql_app",
		MySQLServerID: 5401,
	})
	if cfg["connector.class"] != "io.debezium.connector.mysql.MySqlConnector" {
		t.Fatalf("connector.class=%s", cfg["connector.class"])
	}
	if cfg["database.server.id"] != "5401" {
		t.Fatalf("server.id=%s", cfg["database.server.id"])
	}
	if cfg["table.include.list"] != "appdb.orders,appdb.items" {
		t.Fatalf("include=%s", cfg["table.include.list"])
	}
	if cfg["database.include.list"] != "appdb" {
		t.Fatalf("database.include.list=%s", cfg["database.include.list"])
	}
}

func TestBuildConfigDispatchesMySQL(t *testing.T) {
	cfg := BuildConfig(ModeAfterOnly, DebeziumBuildParams{
		SourceEngine: "mysql",
		PGHost:       "h", PGPort: 3306, PGUser: "u", PGPassword: "p",
		PGDbName: "db", Tables: []string{"t"}, TopicPrefix: "pfx",
	})
	if cfg["connector.class"] != "io.debezium.connector.mysql.MySqlConnector" {
		t.Fatalf("want mysql connector, got %s", cfg["connector.class"])
	}
}

func TestNormalizeCDCSourceEngine(t *testing.T) {
	if NormalizeCDCSourceEngine("tidb") != SourceEngineMySQL {
		t.Fatal("tidb")
	}
	if NormalizeCDCSourceEngine("postgres") != SourceEnginePostgres {
		t.Fatal("postgres")
	}
}
