package pgspace

import "testing"

func TestParseDailyCronHMS(t *testing.T) {
	sec, min, hour, ok := parseDailyCronHMS("0 0 3 * * *")
	if !ok || sec != 0 || min != 0 || hour != 3 {
		t.Fatalf("got %d %d %d ok=%v", sec, min, hour, ok)
	}
	if _, _, _, ok := parseDailyCronHMS("0 */2 * * * *"); ok {
		t.Fatal("interval cron should be rejected")
	}
	if _, _, _, ok := parseDailyCronHMS("0 0 3 * * 1"); ok {
		t.Fatal("weekday-specific cron should be rejected")
	}
}
