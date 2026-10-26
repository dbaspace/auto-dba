package ticketexec

import (
	"context"
	"fmt"
	"time"

	"auto-dba/internal/redisquery"
	"auto-dba/internal/store"
)

// KillSession 按数据库类型终止正在执行的会话。
func KillSession(ctx context.Context, t Target, sessionID int64) error {
	if sessionID <= 0 {
		return nil
	}
	switch EngineOf(t.DBType) {
	case "postgres":
		db, err := openPostgres(t, 15*time.Second)
		if err != nil {
			return err
		}
		defer db.Close()
		if _, err := db.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, sessionID); err != nil {
			return err
		}
		return nil
	case "mysql", "starrocks":
		db, err := openMySQL(t)
		if err != nil {
			return err
		}
		defer db.Close()
		if _, err := db.ExecContext(ctx, fmt.Sprintf("KILL %d", sessionID)); err != nil {
			return err
		}
		return nil
	case "redis":
		inst := &store.Instance{
			Host: t.Host, Port: t.Port, Username: t.Username, Password: t.Password, DBType: t.DBType,
		}
		return redisquery.KillClientID(ctx, inst, redisquery.Options{Username: t.Username, Password: t.Password, Host: t.Host}, t.DBName, sessionID)
	default:
		return fmt.Errorf("不支持终止该数据库类型的会话")
	}
}
