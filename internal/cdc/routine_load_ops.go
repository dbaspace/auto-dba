package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

var srRLRebuildLocks sync.Map

func srRLRebuildLock(targetDB, rlName string) *sync.Mutex {
	key := strings.ToLower(strings.TrimSpace(targetDB) + "." + strings.TrimSpace(rlName))
	v, _ := srRLRebuildLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func srAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "already exists") || strings.Contains(low, "duplicate column") || strings.Contains(low, "column") && strings.Contains(low, "exists")
}

func identTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})
}

func routineLoadCoversColumns(jobProps string, want []string) bool {
	if len(want) == 0 {
		return true
	}
	cols := map[string]struct{}{}
	for _, tok := range identTokens(jobProps) {
		cols[strings.ToLower(tok)] = struct{}{}
	}
	if len(cols) == 0 {
		return false
	}
	for _, w := range want {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if _, ok := cols[strings.ToLower(w)]; !ok {
			return false
		}
	}
	return true
}

func showRoutineLoad(ctx context.Context, db *sql.DB, targetDB, rlName string) (state, jobProps string, exists bool, err error) {
	q := fmt.Sprintf("SHOW ROUTINE LOAD FOR `%s`.`%s`", targetDB, rlName)
	rows, qerr := db.QueryContext(ctx, q)
	if qerr != nil {
		low := strings.ToLower(qerr.Error())
		if strings.Contains(low, "not found") || strings.Contains(low, "does not exist") || strings.Contains(low, "not exist") {
			return "", "", false, nil
		}
		return "", "", false, qerr
	}
	defer rows.Close()
	colNames, cerr := rows.Columns()
	if cerr != nil {
		return "", "", false, cerr
	}
	stateIdx, propsIdx := -1, -1
	for i, c := range colNames {
		switch {
		case strings.EqualFold(c, "State"):
			stateIdx = i
		case strings.EqualFold(c, "JobProperties"):
			propsIdx = i
		}
	}
	if !rows.Next() {
		return "", "", false, nil
	}
	vals := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return "", "", true, err
	}
	cell := func(i int) string {
		if i < 0 || vals[i] == nil {
			return ""
		}
		switch v := vals[i].(type) {
		case []byte:
			return string(v)
		case string:
			return v
		default:
			return fmt.Sprint(v)
		}
	}
	return cell(stateIdx), cell(propsIdx), true, nil
}

func execSRIgnoreMissing(ctx context.Context, db *sql.DB, stmt string) error {
	_, err := db.ExecContext(ctx, strings.TrimSuffix(strings.TrimSpace(stmt), ";"))
	return err
}

// ensureRoutineLoad 已覆盖最新列则不重建；CREATE 会把消费位点重置。
func ensureRoutineLoad(ctx context.Context, db *sql.DB, targetDB, rlName, createSQL string, want []string) (string, error) {
	mu := srRLRebuildLock(targetDB, rlName)
	mu.Lock()
	defer mu.Unlock()
	state, props, exists, err := showRoutineLoad(ctx, db, targetDB, rlName)
	if err == nil && exists && routineLoadCoversColumns(props, want) {
		switch strings.ToUpper(strings.TrimSpace(state)) {
		case "RUNNING", "NEED_SCHEDULE":
			return "skip", nil
		case "PAUSED":
			resume := fmt.Sprintf("RESUME ROUTINE LOAD FOR `%s`.`%s`", targetDB, rlName)
			if rerr := execSRIgnoreMissing(ctx, db, resume); rerr != nil {
				return "resume", rerr
			}
			return "resume", nil
		}
	}
	return "rebuild", recreateRoutineLoad(ctx, db, targetDB, rlName, createSQL)
}

func recreateRoutineLoad(ctx context.Context, db *sql.DB, targetDB, rlName, createSQL string) error {
	stop := fmt.Sprintf("STOP ROUTINE LOAD FOR `%s`.`%s`", targetDB, rlName)
	_ = execSRIgnoreMissing(ctx, db, stop)
	err := execSRIgnoreMissing(ctx, db, createSQL)
	if err != nil && srAlreadyExists(err) {
		_ = execSRIgnoreMissing(ctx, db, stop)
		err = execSRIgnoreMissing(ctx, db, createSQL)
	}
	if err != nil && srAlreadyExists(err) {
		return nil
	}
	return err
}
