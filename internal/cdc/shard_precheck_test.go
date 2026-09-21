package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
)

func TestPrecheckShardTablePresenceNoNonRep(t *testing.T) {
	opened := 0
	open := func(ctx context.Context, dbName string) (*sql.DB, error) {
		opened++
		return nil, nil
	}
	stats, err := PrecheckShardTablePresence(context.Background(), open, "public", "db0", []string{"db0"}, nil, []string{"t1", "t2"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if opened != 0 {
		t.Fatalf("opened=%d want 0", opened)
	}
	if stats.ListTablesCalls != 0 || len(stats.Misses) != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestPrecheckShardTablePresenceEmptyTables(t *testing.T) {
	opened := 0
	open := func(ctx context.Context, dbName string) (*sql.DB, error) {
		opened++
		return nil, nil
	}
	stats, err := PrecheckShardTablePresence(context.Background(), open, "public", "db0", []string{"db0", "db1"}, nil, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	if opened != 0 || stats.ListTablesCalls != 0 {
		t.Fatalf("opened=%d stats=%+v", opened, stats)
	}
}

func TestPrecheckShardTablePresenceOpensOncePerNonRep(t *testing.T) {
	var mu sync.Mutex
	var opened []string
	open := func(ctx context.Context, dbName string) (*sql.DB, error) {
		mu.Lock()
		opened = append(opened, dbName)
		mu.Unlock()
		return nil, fmt.Errorf("mock connect fail")
	}
	// 3 库 × 多表：应只对 2 个非代表库各 open 一次，而非按表次数
	_, err := PrecheckShardTablePresence(
		context.Background(), open, "public", "db0",
		[]string{"db0", "db1", "db2"},
		nil,
		[]string{"t1", "t2", "t3", "t4", "t5"},
		4,
	)
	if err == nil {
		t.Fatal("expected connect error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 2 {
		t.Fatalf("opened=%v want 2 non-rep opens", opened)
	}
}
