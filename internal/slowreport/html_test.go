package slowreport

import (
	"strings"
	"testing"
	"time"

	"auto-dba/internal/jobreg"
)

func TestFormatAndTables(t *testing.T) {
	if FormatDuration(1500) != "1.5s" || FormatDuration(80) != "80ms" {
		t.Fatalf("duration %s %s", FormatDuration(1500), FormatDuration(80))
	}
	if ChangePercent(12, 15) != "↓ 20%" {
		t.Fatal(ChangePercent(12, 15))
	}
	tables := TablesFromSQL("SELECT * FROM public.orders o JOIN pay.bill b ON o.id=b.id")
	if !strings.Contains(tables, "public.orders") || !strings.Contains(tables, "pay.bill") {
		t.Fatal(tables)
	}
	if RowsFromRaw(`{"ScanRows":12,"ReturnRows":3}`) != "12 / 3" {
		t.Fatal(RowsFromRaw(`{"ScanRows":12,"ReturnRows":3}`))
	}
	if RowsFromRaw(`{"Duration":1}`) != "—" {
		t.Fatal("missing rows")
	}
}

func TestFireKeyMonday(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// 2026-11-16 是周一。
	now := time.Date(2026, 11, 16, 9, 10, 0, 0, loc)
	key, ok := FireKey("0 9 * * 1", "", now)
	if !ok || key != "2026-11-16T09:00" {
		t.Fatalf("key=%s ok=%v", key, ok)
	}
	if _, again := FireKey("0 9 * * 1", key, now); again {
		t.Fatal("same slot should not fire again")
	}
	if !jobreg.Cron5Matches("0 9 * * 1", time.Date(2026, 11, 16, 9, 0, 0, 0, loc)) {
		t.Fatal("monday 09:00")
	}
}
