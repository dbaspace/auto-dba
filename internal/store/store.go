package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/jackc/pgx/v4/stdlib"
)

type DB struct {
	db     *sql.DB
	driver string
}

func Open(driver, dsn string) (*DB, error) {
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
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	d := &DB{db: db, driver: driver}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := d.seed(); err != nil {
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
	if err := adb.Ping(); err != nil {
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
