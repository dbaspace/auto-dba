package dbmig

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

type WholeDBTarget struct {
	SourceDB string `json:"source_db_name"`
	TargetDB string `json:"target_db_name"`
}

// ExpandWholeDBs 将整库选择在服务端展开为表级对象，避免前端传上万行。
func ExpandWholeDBs(ctx context.Context, st *store.DB, secretKey, engine string, srcID string, whole []string, targets []WholeDBTarget, exclude []string, extra []store.MigrationObject) ([]store.MigrationObject, error) {
	engine = store.NormalizeDBType(engine)
	tgtMap := map[string]string{}
	for _, t := range targets {
		src := strings.TrimSpace(t.SourceDB)
		if src == "" {
			continue
		}
		td := strings.TrimSpace(t.TargetDB)
		if td == "" {
			td = src
		}
		tgtMap[src] = td
	}
	exMatchers := compileExclude(exclude)

	inst, err := st.GetInstance(srcID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("源实例不存在")
	}
	plain, err := secret.Decrypt(secretKey, inst.Password)
	if err != nil {
		return nil, fmt.Errorf("解密实例密码失败: %w", err)
	}

	type dbResult struct {
		db   string
		objs []store.MigrationObject
		err  error
	}
	ch := make(chan dbResult, len(whole))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, dbName := range uniqueNonEmpty(whole) {
		wg.Add(1)
		go func(dbName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			names, err := dbprobe.ListTables(ctx, inst, plain, dbName)
			if err != nil {
				ch <- dbResult{db: dbName, err: err}
				return
			}
			tgtDB := tgtMap[dbName]
			if tgtDB == "" {
				tgtDB = dbName
			}
			var objs []store.MigrationObject
			for _, name := range names {
				if matchExclude(exMatchers, dbName, name) {
					continue
				}
				schema, table := splitTable(engine, name)
				if table == "" {
					table = name
				}
				objs = append(objs, store.MigrationObject{
					SourceDB: dbName, SourceSchema: schema, SourceTable: table,
					TargetDB: tgtDB, TargetSchema: schema, TargetTable: table,
					ObjectType: "table",
				})
			}
			ch <- dbResult{db: dbName, objs: objs}
		}(dbName)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	seen := map[string]struct{}{}
	var out []store.MigrationObject
	for _, o := range extra {
		k := o.SourceDB + "\x00" + o.SourceTable
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, o)
	}
	for r := range ch {
		if r.err != nil {
			return nil, fmt.Errorf("展开库 %s: %w", r.db, r.err)
		}
		for _, o := range r.objs {
			k := o.SourceDB + "\x00" + o.SourceTable
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, o)
		}
	}
	return out, nil
}

func uniqueNonEmpty(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func compileExclude(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func matchExclude(patterns []string, db, table string) bool {
	full := db + "." + table
	for _, p := range patterns {
		if matchWild(p, table) || matchWild(p, full) {
			return true
		}
	}
	return false
}

func matchWild(pattern, s string) bool {
	ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(s))
	return err == nil && ok
}
