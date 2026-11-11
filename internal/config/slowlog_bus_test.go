package config

import "testing"

func TestSlowlogBusAllowsAndLogLine(t *testing.T) {
	var off SlowlogBusConfig
	if off.Allows("pg-1") {
		t.Fatal("empty brokers must stay on the direct path")
	}
	on := SlowlogBusConfig{Brokers: "10.0.0.1:9092", Password: "secret-value", Username: "slow"}
	if !on.Allows("pg-1") {
		t.Fatal("brokers without an allow list should include every instance")
	}
	gray := SlowlogBusConfig{Brokers: "10.0.0.1:9092", Instances: "pg-a, pg-b"}
	if !gray.Allows("pg-a") || gray.Allows("pg-c") {
		t.Fatal("allow list")
	}
	line := on.LogLine()
	if line == "" || contains(line, "secret-value") {
		t.Fatalf("log line leaked or empty: %s", line)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
