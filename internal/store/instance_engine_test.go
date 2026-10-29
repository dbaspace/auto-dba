package store

import "testing"

func TestIsLogicalDatabaseEngine(t *testing.T) {
	yes := []string{
		"mysql", "MySQL", "tidb", "oceanbase",
		"postgres", "postgresql", "PG", "cockroachdb",
		"starrocks", "sr", "clickhouse",
		"oracle", "sqlserver", "mongodb",
	}
	for _, in := range yes {
		if !IsLogicalDatabaseEngine(in) {
			t.Fatalf("%q should be logical database engine", in)
		}
	}
	no := []string{"", "kafka", "redis", "elasticsearch", "cassandra", "host", "os", "主机", "unknown"}
	for _, in := range no {
		if IsLogicalDatabaseEngine(in) {
			t.Fatalf("%q must not be logical database engine", in)
		}
	}
}
