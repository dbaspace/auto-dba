package slowbus

import (
	"strings"
	"testing"
)

func TestClipAndRoundTrip(t *testing.T) {
	raw := strings.Repeat("select 1;", 40*1024)
	m := Prepare(Message{
		CloudID: "pg-1", Region: "ap-guangzhou", AccountID: "a", TenantID: "t", Vendor: "tencent",
		SQL: raw, Raw: raw, Dedupe: DedupeKey(raw, raw), LogTime: "2026-11-10T00:00:00Z",
	})
	if !m.SQLTruncated || !m.RawTruncated || len(m.SQL) > MaxFieldBytes {
		t.Fatalf("truncated=%v len=%d", m.SQLTruncated, len(m.SQL))
	}
	b, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.CloudID != "pg-1" || got.Dedupe != m.Dedupe || got.V != Version {
		t.Fatalf("%+v", got)
	}
}

func TestDecodeRejectsBadMessage(t *testing.T) {
	if _, err := Decode([]byte(`{"v":1}`)); err == nil {
		t.Fatal("missing cloud id should fail")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Fatal("invalid json should fail")
	}
}

func TestDedupeUsesOriginal(t *testing.T) {
	raw := strings.Repeat("a", MaxFieldBytes+10)
	key := DedupeKey(raw, raw)
	clipped, _ := clip(raw, MaxFieldBytes)
	if key == DedupeKey(clipped, clipped) {
		t.Fatal("truncated text must not share the original dedupe key")
	}
}
