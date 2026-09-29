package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
)

type DB struct {
	db     *sql.DB
	driver string
}

// PoolSettings 元库连接池参数；零值字段在 Open 时使用默认。
type PoolSettings struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime  time.Duration
	ConnMaxIdleTime time.Duration
}

const (
	defaultMaxOpenConns    = 50
	defaultMaxIdleConns    = 10
	defaultConnMaxLifetime  = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
	defaultPingTimeout     = 10 * time.Second
)

func DefaultPoolSettings() PoolSettings {
	return PoolSettings{
		MaxOpenConns:    defaultMaxOpenConns,
		MaxIdleConns:    defaultMaxIdleConns,
		ConnMaxLifetime:  defaultConnMaxLifetime,
		ConnMaxIdleTime: defaultConnMaxIdleTime,
	}
}

func applyPool(db *sql.DB, p PoolSettings) {
	def := DefaultPoolSettings()
	maxOpen := p.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = def.MaxOpenConns
	}
	maxIdle := p.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = def.MaxIdleConns
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	lifetime := p.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = def.ConnMaxLifetime
	}
	idleTime := p.ConnMaxIdleTime
	if idleTime <= 0 {
		idleTime = def.ConnMaxIdleTime
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)
	db.SetConnMaxIdleTime(idleTime)
}

func (d *DB) Ping() error {
	if d == nil || d.db == nil {
		return fmt.Errorf("db not open")
	}
	return d.db.Ping()
}

// Open 打开元库。可选传入 PoolSettings；省略时使用默认池上限。
func Open(driver, dsn string, pool ...PoolSettings) (*DB, error) {
	if driver == "" {
		driver = "postgres"
	}
	if driver != "postgres" {
		return nil, fmt.Errorf("unsupported db driver %q, only postgres is supported", driver)
	}
	if err := ensurePostgresDatabase(dsn); err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	var p PoolSettings
	if len(pool) > 0 {
		p = pool[0]
	}
	applyPool(db, p)

	pingCtx, cancel := context.WithTimeout(context.Background(), defaultPingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}
	d := &DB{db: db, driver: driver}
	if err := d.migrateFast(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := d.seedFast(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed: %w", err)
	}
	return d, nil
}

func ensurePostgresDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || dbName == "postgres" {
		return nil
	}
	admin := *u
	admin.Path = "/postgres"
	adb, err := sql.Open("pgx", admin.String())
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer adb.Close()
	adb.SetMaxOpenConns(1)
	adb.SetMaxIdleConns(0)
	adb.SetConnMaxLifetime(30 * time.Second)

	pingCtx, cancel := context.WithTimeout(context.Background(), defaultPingTimeout)
	defer cancel()
	if err := adb.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	var exists int
	if err := adb.QueryRow(`SELECT COUNT(1) FROM pg_database WHERE datname = $1`, dbName).Scan(&exists); err != nil {
		return fmt.Errorf("check database: %w", err)
	}
	if exists > 0 {
		return nil
	}
	rows, qerr := adb.Query(`SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY 1`)
	avail := []string{}
	if qerr == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				avail = append(avail, n)
			}
		}
	}
	return fmt.Errorf("database %s does not exist (account has no CREATEDB privilege); existing databases: %s", dbName, strings.Join(avail, ", "))
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Driver() string { return d.driver }

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.db.Exec(d.rewrite(query), args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.db.Query(d.rewrite(query), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.db.QueryRow(d.rewrite(query), args...)
}

func (d *DB) rewrite(q string) string {
	if d.driver != "postgres" {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteString(fmt.Sprintf("$%d", n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (d *DB) nowExpr() string {
	return "CURRENT_TIMESTAMP"
}
