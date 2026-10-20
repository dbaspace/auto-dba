package mysqldiag

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"auto-dba/internal/store"
)

// Activity 对齐前端会话表字段（pid/query/state 等）。
type Activity struct {
	PID       int64  `json:"pid"`
	Datname   string `json:"datname"`
	Usename   string `json:"usename"`
	State     string `json:"state"`
	Query     string `json:"query"`
	ClientAddr string `json:"client_addr"`
	Time      int64  `json:"time"`
	Duration  string `json:"duration,omitempty"`
	Command   string `json:"command,omitempty"`
}

func IsMySQL(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "mysql", "tidb", "oceanbase", "mariadb":
		return true
	default:
		return false
	}
}

func open(inst *store.Instance, password string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = inst.Username
	cfg.Passwd = password
	cfg.Net = "tcp"
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	cfg.Addr = fmt.Sprintf("%s:%d", host, inst.Port)
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(60 * time.Second)
	return db, nil
}

// QueryProcesslist SHOW FULL PROCESSLIST。
func QueryProcesslist(ctx context.Context, inst *store.Instance, password string) ([]Activity, error) {
	db, err := open(inst, password)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SHOW FULL PROCESSLIST`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []Activity
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]string{}
		for i, c := range cols {
			m[strings.ToLower(c)] = raw[i].String
		}
		var pid int64
		fmt.Sscanf(m["id"], "%d", &pid)
		var tsec int64
		fmt.Sscanf(m["time"], "%d", &tsec)
		out = append(out, Activity{
			PID:        pid,
			Datname:    m["db"],
			Usename:    m["user"],
			State:      firstNonEmpty(m["state"], m["command"]),
			Query:      m["info"],
			ClientAddr: m["host"],
			Time:       tsec,
			Duration:   fmt.Sprintf("%ds", tsec),
			Command:    m["command"],
		})
	}
	return out, rows.Err()
}

func Kill(ctx context.Context, inst *store.Instance, password string, id int64) error {
	db, err := open(inst, password)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, fmt.Sprintf("KILL %d", id))
	return err
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
