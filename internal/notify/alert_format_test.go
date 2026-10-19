package notify

import (
	"strings"
	"testing"
)

func TestFormatAlertLocation(t *testing.T) {
	p := AlertPayload{
		Severity: "P2", Event: "trigger", Title: "磁盘使用率过高", CurValue: "0.92", Threshold: "0.9", Handle: "unclaimed",
		Labels: map[string]string{"instance": "10.0.0.8:9100", "mountpoint": "/data", "device": "/dev/vdb"},
	}
	text := FormatAlertText(p)
	if !strings.Contains(text, "host=10.0.0.8:9100") || !strings.Contains(text, "mount=/data") {
		t.Fatalf("text missing location: %s", text)
	}
	md := FormatAlertMarkdown(p)
	if !strings.Contains(md, "> 主机: 10.0.0.8:9100") || !strings.Contains(md, "> 挂载: /data") {
		t.Fatalf("markdown missing location: %s", md)
	}

	p.Event = "resolve"
	if text := FormatAlertText(p); !strings.Contains(text, "告警已恢复") || !strings.Contains(text, "mount=/data") {
		t.Fatalf("resolve text missing location: %s", text)
	}

	p.Labels = map[string]string{"hostname": "db-01", "instance": "10.0.0.8:9100"}
	if text := FormatAlertText(p); !strings.Contains(text, "host=db-01") || strings.Contains(text, "mount=") {
		t.Fatalf("hostname priority / empty mount: %s", text)
	}

	p.Labels = map[string]string{"instance": "10.20.0.5:9187", "registered_host": "10.20.0.5", "registered_name": "pg-order"}
	if text := FormatAlertText(p); !strings.Contains(text, "host=pg-order(10.20.0.5)") || strings.Contains(text, "9187") {
		t.Fatalf("registered host priority: %s", text)
	}

	p.Labels = nil
	if text := FormatAlertText(p); strings.Contains(text, "host=") || strings.Contains(text, "mount=") {
		t.Fatalf("no labels should omit location: %s", text)
	}
}
