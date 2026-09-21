package main

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/secret"
)

func main() {
	meta := "postgres://autosql_user:xV2vkHQk5jdY@10.100.112.14:5432/db_autosql?sslmode=disable"
	db, err := sql.Open("pgx", meta)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, name, host, port, db_type, username, password FROM tbl_instances
WHERE lower(db_type) IN ('mysql','tidb','oceanbase') ORDER BY updated_at DESC NULLS LAST LIMIT 20`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	key := "change-me-in-production"
	for rows.Next() {
		var id, name, host, dbType, user, pass string
		var port int
		if err := rows.Scan(&id, &name, &host, &port, &dbType, &user, &pass); err != nil {
			panic(err)
		}
		plain, err := secret.Decrypt(key, pass)
		if err != nil {
			plain = ""
			fmt.Printf("FAIL decrypt %s: %v enc_prefix=%v\n", name, err, strings.HasPrefix(pass, "enc:"))
			continue
		}
		fmt.Printf("OK name=%s type=%s dsn=%s:%s@tcp(%s:%d)/\n", name, dbType, user, plain, host, port)
	}
}
