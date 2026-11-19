package store

import (
	"testing"
	"time"
)

func TestSummarizeArchiveRunsScheduledZeroRows(t *testing.T) {
	now := time.Date(2026, 11, 19, 8, 40, 0, 0, time.UTC) // 上海 16:40
	finished := time.Date(2026, 11, 19, 8, 32, 0, 0, time.UTC).Format(time.RFC3339)
	rows := []archiveRunOutcome{
		{Status: StatusExecuted, At: finished},
		{Status: "success", At: finished},
		{Status: StatusCanceled, At: finished},
		{Status: StatusRunning, At: finished},
	}
	rate, done, failed := summarizeArchiveRuns(rows, now)
	if rate != "100%" || done != 2 || failed != 0 {
		t.Fatalf("got rate=%s done=%d failed=%d", rate, done, failed)
	}
}

func TestSummarizeArchiveRunsEmpty(t *testing.T) {
	rate, done, failed := summarizeArchiveRuns(nil, time.Now())
	if rate != "-" || done != 0 || failed != 0 {
		t.Fatalf("got rate=%s done=%d failed=%d", rate, done, failed)
	}
}

func TestSummarizeArchiveRunsYesterdayFailure(t *testing.T) {
	now := time.Date(2026, 11, 19, 8, 40, 0, 0, time.UTC)
	today := time.Date(2026, 11, 19, 8, 32, 0, 0, time.UTC).Format(time.RFC3339)
	yesterday := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rows := []archiveRunOutcome{
		{Status: StatusExecuted, At: today},
		{Status: StatusFailed, At: yesterday},
	}
	rate, done, failedToday := summarizeArchiveRuns(rows, now)
	if rate != "50%" || done != 1 || failedToday != 0 {
		t.Fatalf("got rate=%s done=%d failedToday=%d", rate, done, failedToday)
	}
}
