package pgdiag

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"auto-dba/internal/store"
)

const (
	sessionActivitySQL = `SELECT pid,
       COALESCE(datname, '') AS datname,
       COALESCE(usename, '') AS usename,
       COALESCE(state, '') AS state,
       COALESCE(query, '') AS query,
       COALESCE(application_name, '') AS application_name,
       COALESCE(client_addr::text, '') AS client_addr,
       backend_start,
       COALESCE(wait_event_type, '') AS wait_event_type,
       COALESCE(wait_event, '') AS wait_event
FROM pg_stat_activity
ORDER BY pid`

	sessionLocksSQL = `SELECT locktype,
       COALESCE(database::text, '') AS database,
       pid,
       mode,
       COALESCE(relation::text, '') AS relation
FROM pg_locks`
)

func formatDuration(start time.Time) string {
	if start.IsZero() {
		return "-"
	}
	sec := int64(time.Since(start).Seconds())
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func QueryActivities(ctx context.Context, inst *store.Instance, password string) ([]store.PGSession, error) {
	db, qctx, cancel, err := pingQuery(ctx, inst, password)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer db.Close()

	rows, err := db.QueryContext(qctx, sessionActivitySQL)
	if err != nil {
		return nil, fmt.Errorf("查询 pg_stat_activity: %w", err)
	}
	defer rows.Close()

	var out []store.PGSession
	for rows.Next() {
		var (
			pid           sql.NullInt64
			datname       string
			usename       string
			state         string
			query         string
			app           string
			client        string
			backendStart  sql.NullTime
			waitEventType string
			waitEvent     string
		)
		if err := rows.Scan(&pid, &datname, &usename, &state, &query, &app, &client, &backendStart, &waitEventType, &waitEvent); err != nil {
			return nil, err
		}
		item := store.PGSession{
			PID:             pid.Int64,
			Datname:         datname,
			Usename:         usename,
			State:           state,
			Query:           query,
			ApplicationName: app,
			ClientAddr:      client,
			WaitEventType:   waitEventType,
			WaitEvent:       waitEvent,
			Duration:        "-",
		}
		if backendStart.Valid {
			item.BackendStart = backendStart.Time.Local().Format("2006-01-02 15:04:05")
			item.Duration = formatDuration(backendStart.Time)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []store.PGSession{}
	}
	return out, nil
}

func QueryLocks(ctx context.Context, inst *store.Instance, password string) ([]store.PGLock, error) {
	db, qctx, cancel, err := pingQuery(ctx, inst, password)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer db.Close()

	rows, err := db.QueryContext(qctx, sessionLocksSQL)
	if err != nil {
		return nil, fmt.Errorf("查询 pg_locks: %w", err)
	}
	defer rows.Close()

	var out []store.PGLock
	for rows.Next() {
		var (
			locktype string
			database string
			pid      sql.NullInt64
			mode     string
			relation string
		)
		if err := rows.Scan(&locktype, &database, &pid, &mode, &relation); err != nil {
			return nil, err
		}
		out = append(out, store.PGLock{
			LockType: locktype,
			Database: database,
			PID:      pid.Int64,
			Mode:     mode,
			Relation: relation,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []store.PGLock{}
	}
	return out, nil
}

func CancelBackend(ctx context.Context, inst *store.Instance, password string, pid int64) error {
	return execBackendFn(ctx, inst, password, "pg_cancel_backend", pid)
}

func TerminateBackend(ctx context.Context, inst *store.Instance, password string, pid int64) error {
	return execBackendFn(ctx, inst, password, "pg_terminate_backend", pid)
}

func execBackendFn(ctx context.Context, inst *store.Instance, password, fn string, pid int64) error {
	if pid <= 0 {
		return fmt.Errorf("pid 无效")
	}
	db, qctx, cancel, err := pingQuery(ctx, inst, password)
	if err != nil {
		return err
	}
	defer cancel()
	defer db.Close()

	var ok bool
	q := "SELECT " + fn + "($1)"
	if err := db.QueryRowContext(qctx, q, pid).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s(%d) 未生效（会话可能已结束）", fn, pid)
	}
	return nil
}

func ParsePID(s string) (int64, error) {
	pid, err := strconv.ParseInt(s, 10, 64)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("pid 无效")
	}
	return pid, nil
}
