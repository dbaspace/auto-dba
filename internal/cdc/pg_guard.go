package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
)

const (
	pgOrchLockTimeoutMs      = 5000
	pgOrchStatementTimeoutMs = 60000
	pgBlockerMinAgeSec       = 60
)

// applyPGOrchestrationTimeouts 让被锁堵住的 DDL 在秒级失败，而不是一直挂住。
// 连接池收成 1，避免 SET 落到另一条连接上失效。
func applyPGOrchestrationTimeouts(db *sql.DB) {
	if db == nil {
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, _ = db.Exec(fmt.Sprintf("SET lock_timeout = %d", pgOrchLockTimeoutMs))
	_, _ = db.Exec(fmt.Sprintf("SET statement_timeout = %d", pgOrchStatementTimeoutMs))
}

func slotCreateBlocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "canceling statement") || strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "statement timeout")
}

type pgBlockerSession struct {
	PID    int
	State  string
	AgeSec int
}

func killIdleInTransactionBlockers(ctx context.Context, db *sql.DB, minAgeSec int) ([]pgBlockerSession, error) {
	if db == nil {
		return nil, nil
	}
	if minAgeSec <= 0 {
		minAgeSec = pgBlockerMinAgeSec
	}
	rows, err := db.QueryContext(ctx, `
SELECT pid, state, COALESCE(EXTRACT(EPOCH FROM (now() - xact_start))::int, 0) AS age_sec
FROM pg_stat_activity
WHERE datname = current_database()
  AND pid <> pg_backend_pid()
  AND backend_type = 'client backend'
  AND state IN ('idle in transaction', 'idle in transaction (aborted)')
  AND xact_start IS NOT NULL
  AND now() - xact_start > make_interval(secs => $1)
ORDER BY xact_start`, minAgeSec)
	if err != nil {
		return nil, err
	}
	var candidates []pgBlockerSession
	for rows.Next() {
		var b pgBlockerSession
		if err := rows.Scan(&b.PID, &b.State, &b.AgeSec); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, b)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	var killed []pgBlockerSession
	for _, b := range candidates {
		var ok bool
		if err := db.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, b.PID).Scan(&ok); err != nil || !ok {
			continue
		}
		killed = append(killed, b)
	}
	return killed, nil
}

func createLogicalSlot(ctx context.Context, db *sql.DB, slot string) error {
	slotSQL := fmt.Sprintf(`SELECT pg_create_logical_replication_slot(%s, 'pgoutput')`, quotePGLiteral(slot))
	run := func() error {
		_, err := db.ExecContext(ctx, slotSQL)
		return err
	}
	err := run()
	if err == nil || slotAlreadyExists(err) {
		return nil
	}
	if slotCreateBlocked(err) {
		if killed, _ := killIdleInTransactionBlockers(ctx, db, pgBlockerMinAgeSec); len(killed) > 0 {
			log.Printf("cdc: 建复制槽被空闲事务堵住，已终止 %d 个会话: %+v", len(killed), killed)
			err = run()
			if err == nil || slotAlreadyExists(err) {
				return nil
			}
		}
	}
	return FormatCreateReplicationSlotError(ctx, db, err)
}

func slotAlreadyExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}

func createAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "already exists") || strings.Contains(low, "already exist")
}

func isCreateRoutineLoad(stmt string) bool {
	return strings.Contains(strings.ToUpper(stmt), "CREATE ROUTINE LOAD")
}

func routineLoadDB(stmt string) string {
	db, _ := quotedRoutineLoadPair(stmt)
	return db
}

func routineLoadName(stmt string) string {
	_, name := quotedRoutineLoadPair(stmt)
	return name
}

func quotedRoutineLoadPair(stmt string) (string, string) {
	i := strings.Index(stmt, "`")
	if i < 0 {
		return "", ""
	}
	rest := stmt[i+1:]
	j := strings.Index(rest, "`")
	if j < 0 {
		return "", ""
	}
	db := rest[:j]
	rest = rest[j+1:]
	k := strings.Index(rest, "`")
	if k < 0 {
		return db, ""
	}
	rest = rest[k+1:]
	n := strings.Index(rest, "`")
	if n < 0 {
		return db, ""
	}
	return db, rest[:n]
}
