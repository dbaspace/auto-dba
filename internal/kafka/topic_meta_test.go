package kafka

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	if got := formatBytes(304 * 1024); got != "304 KB" {
		t.Fatalf("got %q", got)
	}
	if got := formatBytes(10); got != "10 B" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatPartitionSize(t *testing.T) {
	if got := formatPartitionSize(2718, 304*1024); got != "2718 - 304 KB" {
		t.Fatalf("got %q", got)
	}
	if got := formatPartitionSize(12, 0); got != "12" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigSourceName(t *testing.T) {
	if got := configSourceName(1, false); got != "DYNAMIC_TOPIC_CONFIG" {
		t.Fatalf("got %q", got)
	}
	if got := configSourceName(5, false); got != "DEFAULT_CONFIG" {
		t.Fatalf("got %q", got)
	}
	if got := configSourceName(0, true); got != "DEFAULT_CONFIG" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigValueHint(t *testing.T) {
	if got := configValueHint("delete.retention.ms", "86400000"); got != "1 days" {
		t.Fatalf("got %q", got)
	}
	if got := configValueHint("file.delete.delay.ms", "60000"); got != "1 minutes" {
		t.Fatalf("got %q", got)
	}
	if got := configValueHint("compression.type", "producer"); got != "" {
		t.Fatalf("got %q", got)
	}
}
