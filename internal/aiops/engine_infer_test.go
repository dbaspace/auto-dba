package aiops

import "testing"

func TestInferCaseEngine(t *testing.T) {
	if g := InferCaseEngine("conn_usage_ratio|warning|above|mysql", "MySQL 连接使用率偏高", "MySQLConn", "conn_usage_ratio"); g != "mysql" {
		t.Fatalf("got %s", g)
	}
	if g := InferCaseEngine("max_xact_duration|warning|above", "PG 存在长事务", "PGLongTransaction", "max_xact_duration"); g != "postgres" {
		t.Fatalf("got %s", g)
	}
	if g := InferCaseEngine("learn|abc", "学习补充：PG WAL 体积偏大", "PGStorage", "wal_retain_bytes"); g != "postgres" {
		t.Fatalf("learn pg got %s", g)
	}
	if g := InferCaseEngine("mem_usage|warning|above|redis", "Redis 内存使用率偏高", "RedisMemHigh", "mem_usage"); g != "redis" {
		t.Fatalf("got %s", g)
	}
	if !CaseMatchesEngine("mysql", "conn_usage_ratio|warning|above|mysql", "MySQL 连接", "MySQLConn", "") {
		t.Fatal("mysql should match")
	}
	if CaseMatchesEngine("mysql", "max_xact_duration|warning|above", "PG 存在长事务", "PGLongTransaction", "") {
		t.Fatal("pg must not match mysql filter")
	}
}
