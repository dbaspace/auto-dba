package mysqldiag

import (
	"context"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

// DigestRow performance_schema 语句摘要。
type DigestRow struct {
	Schema           string  `json:"schema"`
	Digest           string  `json:"digest"`
	DigestText       string  `json:"digest_text"`
	CountStar        int64   `json:"count_star"`
	SumTimerWaitSec  float64 `json:"sum_timer_wait_sec"`
	AvgTimerWaitSec  float64 `json:"avg_timer_wait_sec"`
	SumRowsExamined  int64   `json:"sum_rows_examined"`
	SumRowsSent      int64   `json:"sum_rows_sent"`
	FirstSeen        string  `json:"first_seen,omitempty"`
	LastSeen         string  `json:"last_seen,omitempty"`
}

// QueryDigests 读取 events_statements_summary_by_digest（需 performance_schema）。
func QueryDigests(ctx context.Context, inst *store.Instance, password string, limit int) ([]DigestRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	db, err := open(inst, password)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := `
SELECT
  COALESCE(SCHEMA_NAME, '') AS schema_name,
  DIGEST,
  LEFT(DIGEST_TEXT, 500) AS digest_text,
  COUNT_STAR,
  ROUND(SUM_TIMER_WAIT/1e12, 6) AS sum_sec,
  ROUND(AVG_TIMER_WAIT/1e12, 6) AS avg_sec,
  SUM_ROWS_EXAMINED,
  SUM_ROWS_SENT,
  COALESCE(CAST(FIRST_SEEN AS CHAR), ''),
  COALESCE(CAST(LAST_SEEN AS CHAR), '')
FROM performance_schema.events_statements_summary_by_digest
WHERE DIGEST_TEXT IS NOT NULL
  AND DIGEST_TEXT NOT LIKE 'SHOW %'
  AND DIGEST_TEXT NOT LIKE 'SET %'
ORDER BY SUM_TIMER_WAIT DESC
LIMIT ?`
	rows, err := db.QueryContext(ctx, q, limit)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "performance_schema") ||
			strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
			return nil, fmt.Errorf("无法读取 performance_schema.events_statements_summary_by_digest（请确认已启用 performance_schema）: %w", err)
		}
		return nil, err
	}
	defer rows.Close()
	var out []DigestRow
	for rows.Next() {
		var r DigestRow
		if err := rows.Scan(
			&r.Schema, &r.Digest, &r.DigestText, &r.CountStar,
			&r.SumTimerWaitSec, &r.AvgTimerWaitSec,
			&r.SumRowsExamined, &r.SumRowsSent,
			&r.FirstSeen, &r.LastSeen,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
