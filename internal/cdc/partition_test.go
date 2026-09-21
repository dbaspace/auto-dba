package cdc

import (
	"strings"
	"testing"
)

func TestEffectivePartitionColumn(t *testing.T) {
	if got := EffectivePartitionColumn("created_at", "report_date"); got != "report_date" {
		t.Fatalf("mapping col should win, got %q", got)
	}
	if got := EffectivePartitionColumn("created_at", ""); got != "created_at" {
		t.Fatalf("task col fallback, got %q", got)
	}
}

func TestValidateSRDynamicPartitionProps(t *testing.T) {
	if err := ValidateSRDynamicPartitionProps(map[string]string{"dynamic_partition.time_unit": "DAY"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSRDynamicPartitionProps(map[string]string{"foo": "1"}); err == nil {
		t.Fatal("expected invalid key")
	}
}

func TestIsPartitionEligibleUDT(t *testing.T) {
	if !IsPartitionEligibleUDT("timestamp") || !IsPartitionEligibleUDT("date") {
		t.Fatal("date/timestamp should be eligible")
	}
	if IsPartitionEligibleUDT("time") || IsPartitionEligibleUDT("int4") {
		t.Fatal("time/int should not be eligible")
	}
}

func TestSRKeyClause(t *testing.T) {
	if SRKeyClause("unique_key") != "UNIQUE KEY" {
		t.Fatal(SRKeyClause("unique_key"))
	}
	if SRKeyClause("primary_key") != "PRIMARY KEY" {
		t.Fatal(SRKeyClause("primary_key"))
	}
}

func TestFormatSRTablePropertiesIncludesDynamic(t *testing.T) {
	s := FormatSRTableProperties(1, map[string]string{"dynamic_partition.enable": "true", "dynamic_partition.prefix": "p"})
	if !strings.Contains(s, `"dynamic_partition.enable" = "true"`) || !strings.Contains(s, `"replication_num" = "1"`) {
		t.Fatalf("props=%s", s)
	}
}
