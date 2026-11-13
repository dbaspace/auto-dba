package redisquery

import (
	"context"
	"strings"
	"testing"
)

func TestCheckQueryCommand(t *testing.T) {
	ok := []string{
		"GET mykey",
		"INFO server",
		"SCAN 0 MATCH * COUNT 100",
		"LLEN mylist",
		"HSCAN h 0 COUNT 100",
		"DBSIZE",
		"PTTL k",
		"OBJECT ENCODING k",
		"MEMORY USAGE k",
		"SLOWLOG GET 10",
		"XLEN s",
		"XINFO STREAM s",
	}
	for _, line := range ok {
		if err := CheckQueryCommand(line); err != nil {
			t.Errorf("want OK %q: %v", line, err)
		}
	}
	denied := []struct {
		line string
		want string
	}{
		{"KEYS *", "禁止执行危险命令: KEYS"},
		{"KEYS prefix*", "禁止执行危险命令: KEYS"},
		{"CONFIG GET maxmemory", "禁止执行危险命令: CONFIG"},
		{"FLUSHALL", "禁止执行危险命令: FLUSHALL"},
		{"SET k v", "查询仅允许只读命令"},
		{"DEL k", "查询仅允许只读命令"},
		{"EVAL x", "查询仅允许只读命令"},
		{"SORT k STORE out", "查询仅允许只读命令"},
		{"XREAD BLOCK 0 STREAMS s $", "查询仅允许只读命令"},
		{"MEMORY PURGE", "MEMORY 查询只允许 USAGE"},
		{"OBJECT HELP", "OBJECT 查询只允许 ENCODING、IDLETIME、REFCOUNT、FREQ"},
		{"SLOWLOG RESET", "SLOWLOG 查询只允许 GET"},
		{"LLEN", "LLEN 至少需要 1 个参数"},
	}
	for _, tc := range denied {
		err := CheckQueryCommand(tc.line)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v want %q", tc.line, err, tc.want)
		}
	}
}

func TestExecuteRejectsKeysBeforeDial(t *testing.T) {
	_, err := Execute(context.Background(), nil, Options{}, "0", "KEYS *")
	if err == nil || !strings.Contains(err.Error(), "禁止执行危险命令: KEYS") {
		t.Fatalf("got %v", err)
	}
}

func TestCapScanCount(t *testing.T) {
	got := capScanCount([]string{"HSCAN", "h", "0", "COUNT", "5000", "MATCH", "a*"})
	if got[4] != "1000" {
		t.Fatalf("count=%s", got[4])
	}
	kept := capScanCount([]string{"SSCAN", "s", "0", "COUNT", "20"})
	if kept[4] != "20" {
		t.Fatalf("count=%s", kept[4])
	}
}
