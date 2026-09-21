package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

const DefaultShardPrecheckConcurrency = 4

// ShardTableMiss 某个非代表库相对期望表的缺失结果。
type ShardTableMiss struct {
	DB      string   `json:"db"`
	Missing []string `json:"missing"`
}

// ShardPrecheckStats 分库表抽检统计。
type ShardPrecheckStats struct {
	DBCount         int
	TableCount      int
	ListTablesCalls int
	Duration        time.Duration
	Misses          []ShardTableMiss
}

// OpenShardDBFunc 按库名打开源端连接（调用方负责在函数返回后关闭）。
type OpenShardDBFunc func(ctx context.Context, dbName string) (*sql.DB, error)

// PrecheckShardTablePresence 对每个非代表库各扫一次表集合，与期望表做内存差集。
// maps 非空时按 TablesForShardDB 取每库期望表，否则用 fallbackTables。
func PrecheckShardTablePresence(
	ctx context.Context,
	openDB OpenShardDBFunc,
	schema string,
	representative string,
	shardDBs []string,
	maps []TableMapping,
	fallbackTables []string,
	maxConcurrent int,
) (ShardPrecheckStats, error) {
	start := time.Now()
	stats := ShardPrecheckStats{
		DBCount:    len(shardDBs),
		TableCount: len(fallbackTables),
	}
	if openDB == nil {
		stats.Duration = time.Since(start)
		return stats, nil
	}
	rep := strings.TrimSpace(representative)
	var nonRep []string
	for _, db := range shardDBs {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}
		if rep != "" && strings.EqualFold(db, rep) {
			continue
		}
		nonRep = append(nonRep, db)
	}
	if len(nonRep) == 0 {
		stats.Duration = time.Since(start)
		return stats, nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultShardPrecheckConcurrency
	}

	var (
		mu        sync.Mutex
		listCalls atomic.Int64
		misses    []ShardTableMiss
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrent)
	for _, dbName := range nonRep {
		dbName := dbName
		g.Go(func() error {
			wantTables := TablesForShardDB(maps, fallbackTables, dbName)
			if len(wantTables) == 0 {
				return nil
			}
			db, err := openDB(gctx, dbName)
			if err != nil {
				return fmt.Errorf("分库 %s 连通失败: %w", dbName, err)
			}
			defer db.Close()

			bySchema := map[string][]string{}
			var schemaOrder []string
			for _, t := range wantTables {
				sch, tb := SplitSchemaTable(schema, t)
				if tb == "" {
					continue
				}
				if _, ok := bySchema[sch]; !ok {
					schemaOrder = append(schemaOrder, sch)
				}
				bySchema[sch] = append(bySchema[sch], tb)
			}
			var missing []string
			for _, sch := range schemaOrder {
				listed, err := ListPGTables(gctx, db, sch)
				listCalls.Add(1)
				if err != nil {
					return fmt.Errorf("分库 %s 列举表失败: %w", dbName, err)
				}
				have := make(map[string]struct{}, len(listed))
				for _, n := range listed {
					have[strings.ToLower(strings.TrimSpace(n))] = struct{}{}
				}
				missing = append(missing, MissingFromTableSet(have, sch, bySchema[sch])...)
			}
			if len(missing) > 0 {
				mu.Lock()
				misses = append(misses, ShardTableMiss{DB: dbName, Missing: missing})
				mu.Unlock()
			}
			return nil
		})
	}
	err := g.Wait()
	stats.ListTablesCalls = int(listCalls.Load())
	stats.Misses = misses
	stats.Duration = time.Since(start)
	log.Printf("cdc shard precheck db_count=%d non_rep=%d table_count=%d list_tables_calls=%d duration=%s miss_dbs=%d",
		stats.DBCount, len(nonRep), stats.TableCount, stats.ListTablesCalls, stats.Duration, len(misses))
	return stats, err
}
