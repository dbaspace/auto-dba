package dns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 设置 DNS_TEST_DSN 后运行：在临时 schema 中验证 ApplyOps、变更日志、回滚冲突与 TTL 预降，结束后删除 schema。
func openTestStore(t *testing.T) (*Store, func()) {
	dsn := strings.TrimSpace(os.Getenv("DNS_TEST_DSN"))
	if dsn == "" {
		t.Skip("DNS_TEST_DSN 未设置")
	}
	schema := fmt.Sprintf("dns_it_%d", time.Now().UnixNano())
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	sep := " "
	if strings.Contains(dsn, "://") {
		sep = "&"
		if !strings.Contains(dsn, "?") {
			sep = "?"
		}
		dsn += sep + "search_path=" + schema
	} else {
		dsn += sep + "search_path=" + schema
	}
	st, err := Open(dsn, "", "", "")
	if err != nil {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		t.Fatal(err)
	}
	return st, func() {
		_ = st.Close()
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = admin.Close()
	}
}

func TestApplyOpsIntegration(t *testing.T) {
	st, done := openTestStore(t)
	defer done()
	ctx := context.Background()
	ui := OpMeta{Actor: "tester", Source: SourceUI}

	res, err := st.ApplyOps(ctx, []Op{ZoneOp(ActionCreate, "", ZoneSnap{Origin: "it.example.com", Status: StatusActive})}, ui)
	if err != nil {
		t.Fatal(err)
	}
	zoneID := res.Items[0].ID
	res, err = st.ApplyOps(ctx, []Op{RangeOp(ActionCreate, "", RangeSnap{Name: "office", NetworkList: []string{"10.1.0.0/16"}, Status: StatusActive})}, ui)
	if err != nil {
		t.Fatal(err)
	}
	rangeID := res.Items[0].ID
	if _, err := st.ApplyOps(ctx, []Op{RangeOp(ActionCreate, "", RangeSnap{Name: "overlap", NetworkList: []string{"10.1.2.0/24"}, Status: StatusActive})}, ui); err == nil {
		t.Fatal("overlapping range should be rejected")
	}

	rec := RecordSnap{Name: "db", Type: "A", Data: "10.1.0.10", TTL: 600, ZoneID: zoneID, RangeID: rangeID, Status: StatusActive}
	res, err = st.ApplyOps(ctx, []Op{RecordOp(ActionCreate, "", rec)}, ui)
	if err != nil {
		t.Fatal(err)
	}
	recID := res.Items[0].ID
	createBatch := res.BatchID

	cname := RecordSnap{Name: "db", Type: "CNAME", Data: "x.example.org", TTL: 600, ZoneID: zoneID, RangeID: rangeID, Status: StatusActive}
	_, err = st.ApplyOps(ctx, []Op{RecordOp(ActionCreate, "", cname)}, ui)
	var le *LintError
	if !errors.As(err, &le) {
		t.Fatalf("cname conflict should be lint error, got %v", err)
	}

	// 修改后回滚到原值
	upd := rec
	upd.ID, upd.Data = recID, "10.1.0.20"
	res, err = st.ApplyOps(ctx, []Op{RecordOp(ActionUpdate, recID, upd)}, ui)
	if err != nil {
		t.Fatal(err)
	}
	updBatch := res.BatchID
	logs, err := st.GetChangeBatch(ctx, updBatch)
	if err != nil || len(logs) != 1 || logs[0].Before == nil || logs[0].After == nil {
		t.Fatalf("change log %+v %v", logs, err)
	}
	if _, err := st.Rollback(ctx, updBatch, OpMeta{Actor: "tester"}); err != nil {
		t.Fatal(err)
	}
	cur, _ := getRecordSnap(ctx, st.db, recID)
	if cur.Data != "10.1.0.10" {
		t.Fatalf("rollback data %s", cur.Data)
	}
	if _, err := st.Rollback(ctx, updBatch, OpMeta{Actor: "tester"}); err == nil {
		t.Fatal("second rollback should be refused")
	}

	// 创建后被改过，回滚创建批次应冲突
	upd.Data = "10.1.0.30"
	if _, err := st.ApplyOps(ctx, []Op{RecordOp(ActionUpdate, recID, upd)}, ui); err != nil {
		t.Fatal(err)
	}
	_, err = st.Rollback(ctx, createBatch, OpMeta{Actor: "tester"})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected conflict, got %v", err)
	}

	// 试执行不落库
	dry := ui
	dry.DryRun = true
	if _, err := st.ApplyOps(ctx, []Op{RecordOp(ActionDelete, recID, RecordSnap{})}, dry); err != nil {
		t.Fatal(err)
	}
	if cur, _ := getRecordSnap(ctx, st.db, recID); cur == nil {
		t.Fatal("dry run deleted the record")
	}

	// TTL 预降与恢复
	if _, err := st.LowerTTL(ctx, []string{recID}, 30, 10, "tester"); err != nil {
		t.Fatal(err)
	}
	holds, _ := st.ListTTLHolds(ctx, TTLHoldHolding)
	if len(holds) != 1 || holds[0].OrigTTL != 600 {
		t.Fatalf("holds %+v", holds)
	}
	if n, err := st.RestoreDueTTL(ctx, time.Now().Add(11*time.Minute).Unix()); err != nil || n != 1 {
		t.Fatalf("restore due %d %v", n, err)
	}
	cur, _ = getRecordSnap(ctx, st.db, recID)
	if cur.TTL != 600 {
		t.Fatalf("ttl after restore %d", cur.TTL)
	}

	// 预降后被人工改 TTL：恢复时跳过
	if _, err := st.LowerTTL(ctx, []string{recID}, 30, 10, "tester"); err != nil {
		t.Fatal(err)
	}
	manual := *cur
	manual.TTL = 120
	if _, err := st.ApplyOps(ctx, []Op{RecordOp(ActionUpdate, recID, manual)}, ui); err != nil {
		t.Fatal(err)
	}
	holds, _ = st.ListTTLHolds(ctx, TTLHoldHolding)
	status, err := st.RestoreTTL(ctx, holds[0].ID, "tester")
	if err != nil || status != TTLHoldSkipped {
		t.Fatalf("restore after manual edit: %s %v", status, err)
	}

	batches, total, err := st.ListChangeBatches(ctx, ChangeFilter{ObjectID: recID})
	if err != nil || total < 5 || len(batches) == 0 {
		t.Fatalf("batches %d %v", total, err)
	}

	// 删除记录后 serial 递增，节点据此重载
	var before int64
	_ = st.db.QueryRowContext(ctx, `SELECT serial FROM serials WHERE name=$1`, SerialUpdateZones).Scan(&before)
	if _, err := st.ApplyOps(ctx, []Op{RecordOp(ActionDelete, recID, RecordSnap{})}, ui); err != nil {
		t.Fatal(err)
	}
	var after int64
	_ = st.db.QueryRowContext(ctx, `SELECT serial FROM serials WHERE name=$1`, SerialUpdateZones).Scan(&after)
	if after != before+1 {
		t.Fatalf("serial %d -> %d", before, after)
	}
}
