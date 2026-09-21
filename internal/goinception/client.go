package goinception

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

type AuditError struct {
	RuleID  string
	Message string
	SQL     string
}

type AuditResult struct {
	OK     bool
	Errors []AuditError
}

var variableNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ParseAddr(addr string) (Config, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return Config{}, fmt.Errorf("未配置 goInception 地址")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return Config{}, fmt.Errorf("goInception 地址无效，应为 host:port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return Config{}, fmt.Errorf("goInception 端口无效")
	}
	return Config{Host: host, Port: port}, nil
}

func BuildMagicComment(user, password, host string, port int, dbName string) string {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 3306
	}
	if user == "" {
		user = "root"
	}
	s := fmt.Sprintf("/*--user=%s;--password=%s;--host=%s;--port=%d;--check=1;",
		user, password, host, port)
	if db := strings.TrimSpace(dbName); db != "" {
		s += fmt.Sprintf("--db=%s;", db)
	}
	return s + "*/"
}

func WrapAuditSQL(sqlContent, comment string) string {
	return comment + "\ninception_magic_start;\n" + strings.TrimSpace(sqlContent) + "\ninception_magic_commit;"
}

func Audit(ctx context.Context, cfg Config, sqlContent, targetHost string, targetPort int, targetUser, targetPassword, targetDB string) (AuditResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Host == "" || cfg.Port <= 0 {
		return AuditResult{}, fmt.Errorf("goInception 地址未配置")
	}
	if strings.TrimSpace(targetHost) == "" {
		return AuditResult{}, fmt.Errorf("goInception 目标库 host 为空")
	}
	if targetPort <= 0 {
		targetPort = 3306
	}
	db, err := openInception(cfg)
	if err != nil {
		return AuditResult{}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return AuditResult{}, fmt.Errorf("goinception ping: %w", err)
	}
	comment := BuildMagicComment(targetUser, targetPassword, targetHost, targetPort, targetDB)
	wrapped := WrapAuditSQL(sqlContent, comment)
	rows, err := db.QueryContext(ctx, wrapped)
	if err != nil {
		return AuditResult{}, fmt.Errorf("goinception query: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return AuditResult{}, err
	}
	colIndex := map[string]int{}
	for i, c := range cols {
		colIndex[strings.ToLower(c)] = i
	}
	stageIdx, hasStage := colIndex["stage_status"]
	levelIdx, hasLevel := colIndex["error_level"]
	msgIdx, hasMsg := colIndex["error_message"]
	sqlIdx, hasSQL := colIndex["sql"]
	if !hasSQL {
		sqlIdx, hasSQL = colIndex["sqlstatement"]
	}
	if !hasStage || !hasLevel || !hasMsg {
		return AuditResult{OK: true}, nil
	}
	vals := make([]any, len(cols))
	ok := true
	var errs []AuditError
	for rows.Next() {
		for i := range vals {
			var v sql.NullString
			vals[i] = &v
		}
		if err := rows.Scan(vals...); err != nil {
			return AuditResult{}, err
		}
		stage := nullString(vals[stageIdx])
		level := 0
		if s := nullString(vals[levelIdx]); s != "" {
			if n, convErr := strconv.Atoi(strings.TrimSpace(s)); convErr == nil {
				level = n
			}
		}
		msg := nullString(vals[msgIdx])
		stmt := ""
		if hasSQL {
			stmt = nullString(vals[sqlIdx])
		}
		if !strings.EqualFold(strings.TrimSpace(stage), "Audit completed") || level != 0 {
			ok = false
			if msg != "" {
				errs = append(errs, AuditError{RuleID: "goInception", Message: msg, SQL: stmt})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return AuditResult{}, err
	}
	return AuditResult{OK: ok, Errors: errs}, nil
}

func Ping(ctx context.Context, cfg Config) error {
	db, err := openInception(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("goinception ping: %w", err)
	}
	return nil
}

func SetVariable(ctx context.Context, cfg Config, name, value string) error {
	stmt, err := FormatSetSQL(name, value)
	if err != nil {
		return err
	}
	db, err := openInception(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("goinception ping: %w", err)
	}
	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("goinception set %s: %w", name, err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func FormatSetSQL(name, value string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !variableNameRe.MatchString(name) {
		return "", fmt.Errorf("变量名无效")
	}
	return fmt.Sprintf("inception set %s = %s;", name, formatSetLiteral(value)), nil
}

func formatSetLiteral(value string) string {
	v := strings.TrimSpace(value)
	if strings.EqualFold(v, "true") || strings.EqualFold(v, "false") {
		return strings.ToLower(v)
	}
	if v != "" {
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			return v
		}
	}
	return "'" + strings.ReplaceAll(v, `'`, `''`) + "'"
}

func openInception(cfg Config) (*sql.DB, error) {
	if cfg.Host == "" || cfg.Port <= 0 {
		return nil, fmt.Errorf("goInception 地址未配置")
	}
	db, err := sql.Open("mysql", inceptionDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("goinception open: %w", err)
	}
	db.SetConnMaxLifetime(20 * time.Second)
	db.SetMaxOpenConns(1)
	return db, nil
}

func inceptionDSN(cfg Config) string {
	user, pass := cfg.User, cfg.Password
	if strings.TrimSpace(user) == "" {
		user, pass = randomSessionLogin()
	}
	mc := mysql.NewConfig()
	mc.User = user
	mc.Passwd = pass
	mc.Net = "tcp"
	mc.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mc.Timeout = 8 * time.Second
	mc.Params = map[string]string{"charset": "utf8mb4"}
	return mc.FormatDSN()
}

func randomSessionLogin() (string, string) {
	return "gi" + randAlphaNum(10), randAlphaNum(16)
}

const sessionLoginAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randAlphaNum(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = sessionLoginAlphabet[i%len(sessionLoginAlphabet)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = sessionLoginAlphabet[int(b[i])%len(sessionLoginAlphabet)]
	}
	return string(b)
}

func nullString(v any) string {
	if n, ok := v.(*sql.NullString); ok && n.Valid {
		return n.String
	}
	return ""
}
