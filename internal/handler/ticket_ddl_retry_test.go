package handler

import (
	"testing"

	"auto-dba/internal/ticketexec"
)

func TestDDLShardDBPrefix(t *testing.T) {
	if ddlShardDBPrefix("") != "" {
		t.Fatal("empty")
	}
	if ddlShardDBPrefix(`{"shard_db":false,"shard_db_prefix":"db_order_"}`) != "" {
		t.Fatal("unchecked shard should not expand")
	}
	if got := ddlShardDBPrefix(`{"shard_db":true,"shard_db_prefix":" db_order_ "}`); got != "db_order_" {
		t.Fatalf("prefix=%q", got)
	}
}

func TestDDLDoneSource(t *testing.T) {
	rec := func(inst, status, sql string) ticketexec.Record {
		return ticketexec.Record{InstanceID: inst, Host: "h-" + inst, DB: "app", Status: status, SQL: sql}
	}
	prev := &ticketexec.Result{Records: []ticketexec.Record{
		rec("sea", "success", "ALTER TABLE t ADD COLUMN IF NOT EXISTS c int"),
		rec("sea", "success", ticketexec.CleanupRecordPrefix+"DROP INDEX CONCURRENTLY IF EXISTS i"),
		rec("sea", "success", "CREATE INDEX CONCURRENTLY IF NOT EXISTS i ON t(c)"),
		rec("bj", "success", "ALTER TABLE t ADD COLUMN IF NOT EXISTS c int"),
		rec("bj", "failed", "CREATE INDEX CONCURRENTLY IF NOT EXISTS i ON t(c)"),
		rec("gz", "success", "ALTER TABLE t ADD COLUMN IF NOT EXISTS c int"),
	}}

	done := ddlDoneSource(prev, "sea", "app", 2)
	if done == nil || !done.Success || done.SuccessCount != 2 || len(done.Records) != 3 {
		t.Fatalf("sea 两条都成功应跳过并带入原明细（清理明细不计数）: %+v", done)
	}
	if ddlDoneSource(prev, "bj", "app", 2) != nil {
		t.Fatal("bj 有失败记录，不应跳过")
	}
	if ddlDoneSource(prev, "gz", "app", 2) != nil {
		t.Fatal("gz 只成功 1 条（长语句前中断），不应跳过")
	}
	if ddlDoneSource(prev, "sh", "app", 2) != nil {
		t.Fatal("上次未执行到的数据源不应跳过")
	}
	if ddlDoneSource(nil, "sea", "app", 2) != nil {
		t.Fatal("首次执行无历史结果")
	}

	compacted := &ticketexec.Result{
		Compacted: true,
		Records:   []ticketexec.Record{rec("sea", "success", "s1"), rec("bj", "success", "s1")},
		TargetStats: []ticketexec.TargetStat{
			{Host: "h-sea", DB: "app", Success: 1500},
			{Host: "h-bj", DB: "app", Success: 900, Failed: 1},
		},
	}
	if d := ddlDoneSource(compacted, "sea", "app", 1500); d == nil || len(d.TargetStats) != 1 {
		t.Fatalf("收敛结果按 TargetStats 判断全部成功: %+v", d)
	}
	if ddlDoneSource(compacted, "bj", "app", 1500) != nil {
		t.Fatal("收敛结果中有失败计数不应跳过")
	}
	if ddlDoneSource(compacted, "sea", "app", 1600) != nil {
		t.Fatal("成功数与语句数不一致不应跳过")
	}

	sharded := &ticketexec.Result{Records: []ticketexec.Record{
		{InstanceID: "bj", Host: "h-bj", DB: "db_order_0", Status: "success", SQL: "s1"},
		{InstanceID: "bj", Host: "h-bj", DB: "db_order_0", Status: "success", SQL: "s2"},
		{InstanceID: "bj", Host: "h-bj", DB: "db_order_1", Status: "failed", SQL: "s1"},
	}}
	if d := ddlDoneSource(sharded, "bj", "db_order_0", 2); d == nil || !d.Success {
		t.Fatal("同一实例已成功的分库应跳过")
	}
	if ddlDoneSource(sharded, "bj", "db_order_1", 2) != nil {
		t.Fatal("同一实例失败的分库不应跳过")
	}
}
