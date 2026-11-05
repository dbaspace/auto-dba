package dbprobe

import (
	"testing"
	"time"
)

func TestFormatQueryTime_microsecondsUTC(t *testing.T) {
	tm := time.Date(2024, 3, 8, 9, 33, 41, 997559000, time.UTC)
	want := "2024-03-08T09:33:41.997559Z"
	for _, kind := range []string{"TIMESTAMPTZ", "TIMESTAMP", "DATETIME", "timestamptz", ""} {
		if got := FormatQueryTime(tm, kind); got != want {
			t.Fatalf("%s: %s", kind, got)
		}
	}
	local := time.Date(2024, 3, 8, 17, 33, 41, 997559000, time.FixedZone("CST", 8*3600))
	if got := FormatQueryTime(local, "TIMESTAMPTZ"); got != want {
		t.Fatalf("local: %s", got)
	}
}

func TestFormatQueryTime_dateAndClock(t *testing.T) {
	tm := time.Date(2024, 3, 8, 9, 33, 41, 997559000, time.UTC)
	if got := FormatQueryTime(tm, "DATE"); got != "2024-03-08" {
		t.Fatalf("date: %s", got)
	}
	if got := FormatQueryTime(tm, "TIME"); got != "09:33:41.997559" {
		t.Fatalf("time: %s", got)
	}
}
