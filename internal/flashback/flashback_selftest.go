package flashback

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FlashbackSelftestReq 在目标库跑 DEFAULT 堆页补齐自测（不 ALTER FULL）。
type FlashbackSelftestReq struct {
	InstanceID string `json:"instance_id" binding:"required"`
	Database   string `json:"database" binding:"required"`
}

// FlashbackSelftestCheck 单项断言。
type FlashbackSelftestCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// FlashbackSelftestResult 自测报告。
type FlashbackSelftestResult struct {
	OK      bool                     `json:"ok"`
	Table   string                   `json:"table,omitempty"`
	Checks  []FlashbackSelftestCheck `json:"checks"`
	UndoSQL []string                 `json:"undo_sql,omitempty"`
}

func flashbackSelftestAdd(checks *[]FlashbackSelftestCheck, name string, ok bool, detail string) {
	*checks = append(*checks, FlashbackSelftestCheck{Name: name, OK: ok, Detail: detail})
}

func flashbackSelftestOK(checks []FlashbackSelftestCheck) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return len(checks) > 0
}

func flashbackUndoHas(stmts []string, substr string) bool {
	want := strings.ToLower(substr)
	for _, s := range stmts {
		if strings.Contains(strings.ToLower(s), want) {
			return true
		}
	}
	return false
}

// Selftest 连接已登记实例，验证 DEFAULT+PK 预检查为 warning，并跑不 ALTER FULL 的堆页补齐路径。
func (s *FlashbackImpl) Selftest(ctx context.Context, req *FlashbackSelftestReq) (*FlashbackSelftestResult, error) {
	out := &FlashbackSelftestResult{Checks: []FlashbackSelftestCheck{}}
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.Database) == "" {
		return nil, fmt.Errorf("instance_id 与 database 必填")
	}
	if err := s.ensureReady(ctx); err != nil {
		return nil, err
	}
	db, dom, res, err := flashbackConnectTarget(ctx, strings.TrimSpace(req.InstanceID), strings.TrimSpace(req.Database))
	if err != nil {
		flashbackSelftestAdd(&out.Checks, "connect", false, err.Error())
		out.OK = false
		return out, nil
	}
	defer db.Close()
	flashbackSelftestAdd(&out.Checks, "connect", true, fmt.Sprintf("%s:%d/%s", dom.MainIP, dom.Port, req.Database))
	if flashbackIsMySQL(dom.DbType) {
		flashbackSelftestAdd(&out.Checks, "engine", false, "DEFAULT 堆页补齐自测仅支持 PostgreSQL")
		out.OK = false
		return out, nil
	}
	walSrc := flashbackResolveWALSourceFromConn(res, dom, "")
	if walSrc.Kind != flashbackWALSourceSelf {
		flashbackSelftestAdd(&out.Checks, "wal_source", false,
			fmt.Sprintf("需要自建 WAL（pg_ls_waldir/pg_read_binary_file），当前=%s：%s", walSrc.Kind, walSrc.Reason))
		out.OK = false
		return out, nil
	}
	flashbackSelftestAdd(&out.Checks, "wal_source", true, "selfhosted")

	// 纯逻辑：DEFAULT+PK 预检查必须是 warning
	st, msg := flashbackReplicaIdentityCheck(map[string]*flashbackRelation{
		"public.t": {Schema: "public", Name: "t", ReplIdent: "d", PKCols: []string{"id"}},
	})
	flashbackSelftestAdd(&out.Checks, "precheck_default_pk",
		st == flashbackCheckWarning && !strings.Contains(msg, "可用于行还原"),
		fmt.Sprintf("status=%s msg_has_pk_hint=%v", st, strings.Contains(msg, "只保证主键")))

	flashbackVerifyDefaultHeapFill(ctx, db, out)
	out.OK = flashbackSelftestOK(out.Checks)
	return out, nil
}

// flashbackVerifyDefaultHeapFill 不 ALTER FULL：DEFAULT 只带主键，靠堆页死行补齐非主键列。
func flashbackVerifyDefaultHeapFill(ctx context.Context, db *sql.DB, out *FlashbackSelftestResult) {
	stamp := time.Now().UnixNano() % 1000000000
	tbl := fmt.Sprintf("tbl_fb_def_%d", stamp)
	ident := `public.` + flashbackQuoteIdent(tbl)
	out.Table = "public." + tbl
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+ident)
	}()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (id integer PRIMARY KEY, note text)`, ident)); err != nil {
		flashbackSelftestAdd(&out.Checks, "def_create", false, err.Error())
		return
	}
	var ri string
	_ = db.QueryRowContext(ctx, `SELECT relreplident::text FROM pg_class WHERE oid = $1::regclass`, ident).Scan(&ri)
	if ri != "d" {
		flashbackSelftestAdd(&out.Checks, "def_ri", false, "want DEFAULT(d), got "+ri)
		return
	}
	flashbackSelftestAdd(&out.Checks, "def_ri", true, "REPLICA IDENTITY DEFAULT（未改 FULL）")

	time.Sleep(300 * time.Millisecond)
	target := time.Now().UTC().Add(-5 * time.Second)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s(id, note) VALUES (7, 'default-heap-old')`, ident)); err != nil {
		flashbackSelftestAdd(&out.Checks, "def_insert", false, err.Error())
		return
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id=7`, ident)); err != nil {
		flashbackSelftestAdd(&out.Checks, "def_delete", false, err.Error())
		return
	}
	flashbackSelftestAdd(&out.Checks, "def_dml", true, "INSERT+DELETE id=7，禁止 vacuum")
	if _, err := db.ExecContext(ctx, `CHECKPOINT`); err != nil {
		flashbackSelftestAdd(&out.Checks, "def_checkpoint", true, "skipped: "+err.Error())
	} else {
		flashbackSelftestAdd(&out.Checks, "def_checkpoint", true, "wal flushed")
	}
	time.Sleep(800 * time.Millisecond)
	end := time.Now().UTC().Add(5 * time.Second)

	dict, err := flashbackLoadDictionary(ctx, db, "", []string{"public." + tbl})
	if err != nil {
		flashbackSelftestAdd(&out.Checks, "def_dict", false, err.Error())
		return
	}
	live, err := flashbackListLiveWAL(ctx, db)
	if err != nil || len(live) == 0 {
		flashbackSelftestAdd(&out.Checks, "def_wal", false, fmt.Sprintf("ls_wal err=%v n=%d", err, len(live)))
		return
	}
	workDir, err := os.MkdirTemp("", "fb-def-*")
	if err != nil {
		flashbackSelftestAdd(&out.Checks, "def_workdir", false, err.Error())
		return
	}
	defer os.RemoveAll(workDir)
	walDir := filepath.Join(workDir, "wal")
	_ = os.MkdirAll(walDir, 0o700)
	cur := flashbackCurrentWALName(ctx, db)
	cpName, _ := flashbackCheckpointWALName(ctx, db)
	from := target
	picked, _, _, ok := flashbackSelectWALPrecise(live, from, end, flashbackDefaultMaxWALBytes, cur, cpName)
	if !ok || len(picked) == 0 {
		picked, _, _ = flashbackSelectWAL(live, from, end, flashbackDefaultMaxWALBytes, cur)
	}
	if len(picked) == 0 {
		flashbackSelftestAdd(&out.Checks, "def_wal", false, "无 WAL 段")
		return
	}
	flashbackSelftestAdd(&out.Checks, "def_wal", true, fmt.Sprintf("picked=%d current=%s", len(picked), cur))
	var fillStats flashbackOldFillStats
	var undo []string
	var seenDel int
	st, _, err := flashbackStreamWAL(ctx, db, walDir, "", picked, dict, dict.DBOID, flashbackParseOpts{
		MaxChanges:  1000,
		DeleteAfter: true,
		MaxFPWPages: flashbackDefaultFPWPages,
		TimeFrom:    from,
		TimeTo:      end,
	}, nil, nil, func(ch flashbackChange) bool {
		if !dict.matchChange(ch) {
			return true
		}
		if strings.EqualFold(ch.Op, "DELETE") {
			seenDel++
			if flashbackValuesIncomplete(dict.match(ch.Schema, ch.Table), ch.Old) {
				flashbackFillOldFromDB(ctx, db, dict, &ch, flashbackDefaultHeapScanPages, &fillStats)
			}
			if u, _ := flashbackUndoSQL(ch); u != "" {
				undo = append(undo, u)
			}
		}
		return true
	})
	if err != nil {
		flashbackSelftestAdd(&out.Checks, "def_parse", false, err.Error())
		return
	}
	out.UndoSQL = undo
	okFill := flashbackUndoHas(undo, "default-heap-old")
	flashbackSelftestAdd(&out.Checks, "def_heap_fill", okFill,
		fmt.Sprintf("DEFAULT DELETE undo 含非主键列 note；seen_del=%d fill=%+v undo_n=%d parse=%s",
			seenDel, fillStats, len(undo), st.String()))
}
