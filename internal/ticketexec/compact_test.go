package ticketexec

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCompactSmallUnchanged(t *testing.T) {
	r := &Result{StartedAt: nowRFC3339(), EndedAt: nowRFC3339()}
	tg := Target{Host: "h", DBName: "db", Port: 5432, Username: "u"}
	for i := 0; i < 10; i++ {
		r.appendSuccess(tg, fmt.Sprintf("SELECT %d", i), time.Now(), time.Now(), 0)
	}
	Compact(r)
	if r.Compacted {
		t.Fatal("small result should not compact")
	}
	if len(r.Records) != 10 || len(r.SuccessSQLs) != 10 {
		t.Fatalf("want full detail, got records=%d sqls=%d", len(r.Records), len(r.SuccessSQLs))
	}
	if r.SuccessCount != 10 {
		t.Fatalf("success_count=%d", r.SuccessCount)
	}
}

func TestCompactLargeBounded(t *testing.T) {
	r := &Result{StartedAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), EndedAt: nowRFC3339()}
	r.enableCompactMode()
	tg := Target{Host: "h", DBName: "db_bill_0", Port: 5432, Username: "u"}
	for i := 0; i < 2500; i++ {
		r.appendSuccess(tg, fmt.Sprintf("CREATE TABLE t_%d(id int)", i), time.Now(), time.Now(), 0)
	}
	// 模拟失败仍全保留
	r.appendFailure(tg, "CREATE TABLE bad(id int)", time.Now(), fmt.Errorf("syntax error"))
	Compact(r)
	if !r.Compacted {
		t.Fatal("large result should compact")
	}
	if r.SuccessCount != 2500 || r.FailCount != 1 {
		t.Fatalf("counts success=%d fail=%d", r.SuccessCount, r.FailCount)
	}
	if len(r.SuccessSQLs) != 0 {
		t.Fatalf("success_sqls should clear, got %d", len(r.SuccessSQLs))
	}
	successDetail := 0
	failDetail := 0
	for _, rec := range r.Records {
		switch strings.ToLower(rec.Status) {
		case "success":
			successDetail++
		case "failed":
			failDetail++
		}
	}
	if successDetail > MaxSuccessSamplesPerTarget {
		t.Fatalf("success samples too many: %d", successDetail)
	}
	if failDetail != 1 {
		t.Fatalf("failures should keep all, got %d", failDetail)
	}
	raw := ToJSON(r)
	// 2500×双写原本可达 MB 级；收敛后应远小于 500KB
	if len(raw) > 500_000 {
		t.Fatalf("JSON too large after compact: %d bytes", len(raw))
	}
	if !strings.Contains(r.Summary, "大批量结果已收敛") {
		t.Fatalf("summary=%q", r.Summary)
	}
	if len(r.TargetStats) != 1 || r.TargetStats[0].Success != 2500 {
		t.Fatalf("target_stats=%+v", r.TargetStats)
	}
}

func TestCompactMultiDBSamplesBounded(t *testing.T) {
	r := &Result{StartedAt: nowRFC3339(), EndedAt: nowRFC3339()}
	for d := 0; d < 64; d++ {
		tg := Target{Host: "h", DBName: fmt.Sprintf("db_bill_%d", d), Port: 5432, Username: "u"}
		// 每库超过阈值的语句数：进入收敛后每库最多 N 条抽样
		r.enableCompactMode()
		for i := 0; i < 50; i++ {
			r.appendSuccess(tg, fmt.Sprintf("ALTER TABLE t ADD c%d int", i), time.Now(), time.Now(), 0)
		}
	}
	Compact(r)
	if r.SuccessCount != 64*50 {
		t.Fatalf("success_count=%d", r.SuccessCount)
	}
	if countSuccessRecords(r.Records) > 64*MaxSuccessSamplesPerTarget {
		t.Fatalf("too many success records: %d", countSuccessRecords(r.Records))
	}
	raw := ToJSON(r)
	if len(raw) > 2_000_000 {
		t.Fatalf("multi-db JSON too large: %d", len(raw))
	}
}

func TestAppendSuccessAutoEntersCompactAfterThreshold(t *testing.T) {
	r := &Result{}
	tg := Target{Host: "h", DBName: "db", Port: 1, Username: "u"}
	n := DetailCompactThreshold + 200
	for i := 0; i < n; i++ {
		r.appendSuccess(tg, "SELECT 1", time.Now(), time.Now(), 0)
	}
	if r.SuccessCount != n {
		t.Fatalf("count=%d", r.SuccessCount)
	}
	// 阈值内全量 + 之后每库最多 N 条
	if len(r.Records) > DetailCompactThreshold+MaxSuccessSamplesPerTarget {
		t.Fatalf("records not bounded during append: %d", len(r.Records))
	}
	Compact(r)
	if !r.Compacted || len(r.SuccessSQLs) != 0 {
		t.Fatalf("compacted=%v sqls=%d", r.Compacted, len(r.SuccessSQLs))
	}
}
