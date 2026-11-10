package dbprobe

import "testing"

func TestSplitHintTable(t *testing.T) {
	schema, table := SplitHintTable("public.orders")
	if schema != "public" || table != "orders" {
		t.Fatalf("got %s.%s", schema, table)
	}
	schema, table = SplitHintTable("debezium_signal")
	if schema != "" || table != "debezium_signal" {
		t.Fatalf("got %q %q", schema, table)
	}
	schema, table = SplitHintTable("`db`.`t`")
	if schema != "db" || table != "t" {
		t.Fatalf("got %s.%s", schema, table)
	}
}

func TestHintColumnsSupported(t *testing.T) {
	if !HintColumnsSupported("mysql") || !HintColumnsSupported("starrocks") || !HintColumnsSupported("postgres") {
		t.Fatal("mysql family and postgres should support column hints")
	}
	if HintColumnsSupported("redis") || HintColumnsSupported("clickhouse") || HintColumnsSupported("kafka") {
		t.Fatal("other engines must be rejected before dialing")
	}
}
