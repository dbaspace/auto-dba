package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./cmd/apply-sql <sql-file> [config.yaml]")
	}
	sqlPath := os.Args[1]
	cfgPath := "config/config.yaml"
	if len(os.Args) > 2 {
		cfgPath = os.Args[2]
	}
	raw, err := os.ReadFile(sqlPath)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	d, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	// 按分号拆语句；跳过纯注释块
	parts := strings.Split(string(raw), ";")
	n := 0
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		lines := strings.Split(stmt, "\n")
		var keep []string
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "--") {
				continue
			}
			keep = append(keep, ln)
		}
		stmt = strings.TrimSpace(strings.Join(keep, "\n"))
		if stmt == "" {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(stmt), "SELECT") {
			rows, err := d.Query(stmt)
			if err != nil {
				log.Fatalf("query: %v\nSQL: %s", err, stmt)
			}
			cols, _ := rows.Columns()
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			fmt.Println("--", stmt)
			for rows.Next() {
				_ = rows.Scan(ptrs...)
				out := make([]string, len(cols))
				for i, v := range vals {
					out[i] = fmt.Sprint(asString(v))
				}
				fmt.Println(strings.Join(out, " | "))
			}
			_ = rows.Close()
			continue
		}
		if _, err := d.Exec(stmt); err != nil {
			log.Fatalf("exec: %v\nSQL: %s", err, stmt)
		}
		n++
	}
	fmt.Printf("ok: applied %d statements from %s\n", n, sqlPath)
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
