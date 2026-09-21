package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func WaitSRAlterColumn(ctx context.Context, db *sql.DB, targetDB, table string, timeout time.Duration) error {
	if db == nil || strings.TrimSpace(table) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		state, err := latestSRAlterColumnState(ctx, db, targetDB, table)
		if err != nil {
			return err
		}
		switch strings.ToUpper(strings.TrimSpace(state)) {
		case "", "FINISHED", "CANCELLED", "CANCELED":
			if strings.EqualFold(state, "CANCELLED") || strings.EqualFold(state, "CANCELED") {
				return fmt.Errorf("StarRocks schema change cancelled for %s", table)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 %s schema change 超时（state=%s）", table, state)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func latestSRAlterColumnState(ctx context.Context, db *sql.DB, targetDB, table string) (string, error) {
	stmts := []string{
		fmt.Sprintf("SHOW ALTER TABLE COLUMN FROM `%s`", targetDB),
		"SHOW ALTER TABLE COLUMN",
	}
	var last error
	for _, stmt := range stmts {
		rows, err := db.QueryContext(ctx, stmt)
		if err != nil {
			last = err
			continue
		}
		cols, _ := rows.Columns()
		nameIdx := indexFold(cols, "TableName")
		if nameIdx < 0 {
			nameIdx = indexFold(cols, "Table")
		}
		stateIdx := indexFold(cols, "State")
		state := ""
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if rows.Scan(ptrs...) != nil {
				continue
			}
			if nameIdx >= 0 && !strings.EqualFold(asString(vals[nameIdx]), table) {
				continue
			}
			if stateIdx >= 0 {
				state = asString(vals[stateIdx])
			}
		}
		rows.Close()
		return state, nil
	}
	if last != nil && (strings.Contains(strings.ToLower(last.Error()), "unknown") || strings.Contains(strings.ToLower(last.Error()), "not supported")) {
		return "", nil
	}
	return "", last
}
