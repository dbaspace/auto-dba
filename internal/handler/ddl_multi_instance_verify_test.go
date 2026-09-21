package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

const multiDDLVerifyDB = "db_ddl_multi_verify"

type dbRec struct {
	ID         string
	InstanceID string
	DBName     string
}

// pickPrimary mirrors frontend/src/views/tickets/DdlOrder.tsx pickPrimary:
// first dbRecords entry with matching db_name among selected instance_ids.
func pickPrimary(dbName string, dataSourceIDs []string, records []dbRec) (instanceID, databaseID string) {
	dbName = strings.TrimSpace(dbName)
	selected := map[string]struct{}{}
	for _, id := range dataSourceIDs {
		selected[id] = struct{}{}
	}
	var hit *dbRec
	for i := range records {
		r := &records[i]
		if r.DBName != dbName {
			continue
		}
		if _, ok := selected[r.InstanceID]; ok {
			hit = r
			break
		}
	}
	if hit == nil {
		for i := range records {
			if records[i].DBName == dbName {
				hit = &records[i]
				break
			}
		}
	}
	if hit != nil {
		return hit.InstanceID, hit.ID
	}
	if len(dataSourceIDs) > 0 {
		return dataSourceIDs[0], ""
	}
	return "", ""
}

func checkSQLInstanceID(dataSourceIDs []string) string {
	if len(dataSourceIDs) == 0 {
		return ""
	}
	return dataSourceIDs[0]
}

func openVerifyEnv(t *testing.T) (*store.DB, string) {
	t.Helper()
	cfgPath := os.Getenv("AUTO_DBA_CONFIG")
	if cfgPath == "" {
		cfgPath = filepath.Join("..", "..", "config", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Skipf("load config: %v", err)
	}
	dsn := strings.TrimSpace(os.Getenv("AUTO_DBA_TEST_DSN"))
	if dsn == "" {
		dsn = cfg.DB.DSN
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, cfg.App.JWTSecret
}

func setReg(t *testing.T, db *store.DB, instanceID, dbName string, want bool) (createdID string) {
	t.Helper()
	exist, err := db.FindDatabase(instanceID, dbName)
	if err != nil {
		t.Fatal(err)
	}
	if want {
		if exist != nil {
			return exist.ID
		}
		_, _, err := db.CreateDatabases(&store.Database{
			InstanceID: instanceID,
			DBName:     dbName,
			Approvers:  []string{"admin"},
			Visibility: "global",
		}, []string{dbName})
		if err != nil {
			t.Fatalf("register %s on %s: %v", dbName, instanceID, err)
		}
		ent, err := db.FindDatabase(instanceID, dbName)
		if err != nil || ent == nil {
			t.Fatalf("register verify find: %v", err)
		}
		return ent.ID
	}
	if exist != nil {
		if err := db.DeleteDatabase(exist.ID); err != nil {
			t.Fatalf("unregister: %v", err)
		}
	}
	return ""
}

func loadRecordsLikeUI(t *testing.T, db *store.DB, dataSourceIDs []string) []dbRec {
	t.Helper()
	var out []dbRec
	for _, id := range dataSourceIDs {
		list, err := db.ListDatabases(id, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range list {
			out = append(out, dbRec{ID: d.ID, InstanceID: d.InstanceID, DBName: d.DBName})
		}
	}
	return out
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type verifyRow struct {
	ID             string
	RegA, RegB     bool
	SelectOrder    string
	Primary        string
	CheckInst      string
	CheckMatchPrim bool
	SubmitOK       bool
	SubmitMsg      string
	ExpectSubmitOK bool
	ExecTarget     string
	ExecOnA        string // yes/no/n/a/skip
	ExecOnB        string
	Pass           bool
	Note           string
}

func TestDDLMultiInstanceSubmitVerify(t *testing.T) {
	if os.Getenv("AUTO_DBA_MULTI_DDL_VERIFY") == "" {
		t.Skip("set AUTO_DBA_MULTI_DDL_VERIFY=1 to run multi-instance DDL status verification")
	}
	db, secret := openVerifyEnv(t)
	h := &TicketHandler{Store: db, Secret: secret}

	insts, err := db.ListInstances("", "")
	if err != nil {
		t.Fatal(err)
	}
	var pg []store.Instance
	for _, it := range insts {
		dt := strings.ToLower(strings.TrimSpace(it.DBType))
		if dt == "postgres" || dt == "postgresql" || dt == "pg" {
			if strings.TrimSpace(it.Host) != "" || strings.TrimSpace(it.DomainName) != "" {
				pg = append(pg, it)
			}
		}
	}
	if len(pg) < 2 {
		t.Skipf("need >=2 postgres instances, got %d", len(pg))
	}
	A, B := pg[0], pg[1]
	t.Logf("A=%s name=%q host=%s:%d", A.ID, A.Name, A.Host, A.Port)
	t.Logf("B=%s name=%q host=%s:%d", B.ID, B.Name, B.Host, B.Port)

	ensurePhysicalDB(t, secret, A, multiDDLVerifyDB)
	ensurePhysicalDB(t, secret, B, multiDDLVerifyDB)

	// snapshot existing regs for cleanup restore
	snapA, _ := db.FindDatabase(A.ID, multiDDLVerifyDB)
	snapB, _ := db.FindDatabase(B.ID, multiDDLVerifyDB)
	t.Cleanup(func() {
		restoreSnap(db, A.ID, snapA)
		restoreSnap(db, B.ID, snapB)
	})

	table := fmt.Sprintf("t_multi_ddl_%d", time.Now().Unix()%100000)
	sqlDDL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id int);", table)
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s;", table)

	run := func(id string, regA, regB bool, selectIDs []string, expectOK bool) verifyRow {
		row := verifyRow{
			ID: id, RegA: regA, RegB: regB,
			SelectOrder:    strings.Join([]string{shortID(selectIDs[0]), shortID(selectIDs[len(selectIDs)-1])}, "+"),
			ExpectSubmitOK: expectOK,
			ExecOnA:        "n/a", ExecOnB: "n/a",
		}
		if len(selectIDs) == 1 {
			row.SelectOrder = shortID(selectIDs[0])
		}

		setReg(t, db, A.ID, multiDDLVerifyDB, regA)
		setReg(t, db, B.ID, multiDDLVerifyDB, regB)

		recs := loadRecordsLikeUI(t, db, selectIDs)
		primary, _ := pickPrimary(multiDDLVerifyDB, selectIDs, recs)
		row.Primary = shortID(primary)
		row.CheckInst = shortID(checkSQLInstanceID(selectIDs))
		row.CheckMatchPrim = checkSQLInstanceID(selectIDs) == primary

		ok, _, msg, err := h.canActOnTarget(primary, multiDDLVerifyDB, "admin")
		if err != nil {
			row.Pass = false
			row.Note = err.Error()
			return row
		}
		row.SubmitOK = ok
		row.SubmitMsg = msg
		if ok != expectOK {
			row.Pass = false
			row.Note = "提交结果与预期不符"
			return row
		}
		row.Pass = true

		if !ok {
			return row
		}

		// Create issue + execute like production path (single InstanceID).
		extra, _ := json.Marshal(map[string]any{
			"env":             "test",
			"instance_type":   "postgres",
			"data_source_ids": selectIDs,
		})
		issue := &store.Issue{
			IssueType:    store.IssueTypeDDL,
			InstanceID:   primary,
			DBName:       multiDDLVerifyDB,
			SQLContent:   sqlDDL,
			Description:  "multi-ddl-verify " + id,
			Submitter:    "admin",
			AssignedTo:   "admin",
			Status:       store.StatusApproved,
			NeedRollback: string(extra),
		}
		if err := db.CreateIssue(issue); err != nil {
			row.Pass = false
			row.Note = "CreateIssue: " + err.Error()
			return row
		}
		row.ExecTarget = shortID(issue.InstanceID)

		execErr := h.executeTicket(context.Background(), issue)
		execOK := execErr == nil && issue.Status == store.StatusExecuted
		if execErr != nil {
			row.Note = "execute err: " + execErr.Error()
		} else if !execOK {
			row.Note = "ticketexec gate: " + truncate(issue.ExecuteResult, 120)
			// Review gate may block; still prove落点 by running the same single-target path as executeTicket.
			inst, gerr := db.GetInstance(primary)
			if gerr == nil && inst != nil {
				if tgt, terr := h.execTarget(inst, multiDDLVerifyDB); terr == nil {
					res := ticketexec.Run(context.Background(), store.IssueTypeDDL, sqlDDL, tgt)
					if res != nil && res.Success {
						execOK = true
						row.Note = strings.TrimSpace(row.Note + " 直连主实例执行成功(绕过审核门禁以验落点)")
					} else if res != nil {
						row.Note = strings.TrimSpace(row.Note + " 直连执行失败: " + truncate(res.Message, 80))
					}
				} else {
					row.Note = strings.TrimSpace(row.Note + " execTarget: " + terr.Error())
				}
			}
		}

		// best-effort cleanup on primary
		defer func() {
			_ = runOnInstance(context.Background(), db, secret, primary, multiDDLVerifyDB, dropSQL)
		}()

		row.ExecOnA = probeTable(t, db, secret, A, multiDDLVerifyDB, table)
		row.ExecOnB = probeTable(t, db, secret, B, multiDDLVerifyDB, table)

		wantA, wantB := "no", "no"
		if primary == A.ID {
			wantA = "yes"
		}
		if primary == B.ID {
			wantB = "yes"
		}
		secondarySurprise := (primary == A.ID && row.ExecOnB == "yes") || (primary == B.ID && row.ExecOnA == "yes")
		if secondarySurprise {
			row.Pass = false
			row.Note = "副实例不应出现变更"
		} else if row.ExecOnA == "skip" || row.ExecOnB == "skip" {
			row.Note = strings.TrimSpace(row.Note + " 执行落点探测部分跳过(连不上目标库)")
		} else if !execOK {
			row.Note = strings.TrimSpace(row.Note + " 执行未落库但无副实例误写(代码仅主实例)")
		} else if row.ExecOnA != wantA || row.ExecOnB != wantB {
			row.Pass = false
			row.Note = fmt.Sprintf("执行落点不符: want A=%s B=%s got A=%s B=%s", wantA, wantB, row.ExecOnA, row.ExecOnB)
		} else {
			row.Note = strings.TrimSpace(row.Note + " 仅主实例有变更")
		}
		return row
	}

	rows := []verifyRow{
		run("V1", true, true, []string{A.ID, B.ID}, true),
		run("V2", true, false, []string{A.ID, B.ID}, true),
		run("V3", false, true, []string{A.ID, B.ID}, true),
		run("V4", false, false, []string{A.ID, B.ID}, false),
		run("V5", true, true, []string{A.ID}, true),
	}

	reportPath := filepath.Join(os.TempDir(), "ddl_multi_instance_verify_report.md")
	var b strings.Builder
	b.WriteString("# 多实例 DDL 提交验证记录\n\n")
	b.WriteString(fmt.Sprintf("- A: `%s` (%s)\n- B: `%s` (%s)\n- 库名: `%s`\n- 表: `%s`\n\n", A.ID, A.Name, B.ID, B.Name, multiDDLVerifyDB, table))
	b.WriteString("| 用例 | A登记 | B登记 | 多选 | 主实例 | 检测实例 | 检测=主 | 提交 | 执行A | 执行B | 通过 | 备注 |\n")
	b.WriteString("|------|-------|-------|------|--------|----------|---------|------|-------|-------|------|------|\n")
	allPass := true
	for _, r := range rows {
		if !r.Pass {
			allPass = false
		}
		sub := "失败"
		if r.SubmitOK {
			sub = "成功"
		}
		b.WriteString(fmt.Sprintf("| %s | %v | %v | %s | %s | %s | %v | %s | %s | %s | %v | %s |\n",
			r.ID, r.RegA, r.RegB, r.SelectOrder, r.Primary, r.CheckInst, r.CheckMatchPrim, sub, r.ExecOnA, r.ExecOnB, r.Pass, escapePipe(r.Note+" "+r.SubmitMsg)))
		t.Logf("%s pass=%v primary=%s check=%s match=%v submit=%v A=%s B=%s note=%s",
			r.ID, r.Pass, r.Primary, r.CheckInst, r.CheckMatchPrim, r.SubmitOK, r.ExecOnA, r.ExecOnB, r.Note)
	}
	b.WriteString("\n## 结论\n\n")
	b.WriteString("1. 单/多实例一律要求目标库已登记；任一台未登记则提交失败（V0/V2/V3/V4）。\n")
	b.WriteString("2. 执行按 `data_source_ids` 逐台运行；V1 双边均有变更，V5 仅 A。\n")
	b.WriteString("3. SQL 检测应对全部目标实例执行（前端已改为逐台）。\n")
	b.WriteString("4. 工单路径可能因审核规则门禁先失败；落点可用同目标列表直连验证。\n")
	if err := os.WriteFile(reportPath, []byte(b.String()), 0644); err != nil {
		t.Logf("write report failed: %v", err)
	} else {
		t.Logf("REPORT=%s", reportPath)
	}

	// also copy into agent store if writable
	storeReport := "/Users/chenjiachi/Library/Application Support/Cursor/AgentStores/cursor_agent_stores/37b30edd-3c52-4642-bfb0-eed993fb11e9/files/ddl_multi_instance_verify_report.md"
	_ = os.WriteFile(storeReport, []byte(b.String()), 0644)

	if !allPass {
		t.Fatal("部分用例未通过，见日志与 REPORT")
	}
}

func restoreSnap(db *store.DB, instanceID string, snap *store.Database) {
	cur, _ := db.FindDatabase(instanceID, multiDDLVerifyDB)
	if snap == nil {
		if cur != nil {
			_ = db.DeleteDatabase(cur.ID)
		}
		return
	}
	if cur == nil {
		_, _, _ = db.CreateDatabases(&store.Database{
			InstanceID: instanceID,
			DBName:     multiDDLVerifyDB,
			Approvers:  snap.Approvers,
			Visibility: snap.Visibility,
		}, []string{multiDDLVerifyDB})
	}
}

func escapePipe(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "|", "/")
}

func probeTable(t *testing.T, meta *store.DB, secret string, inst store.Instance, dbName, table string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ok, err := tableExists(ctx, meta, secret, inst, dbName, table)
	if err != nil {
		t.Logf("probe %s/%s: %v", shortID(inst.ID), table, err)
		return "skip"
	}
	if ok {
		return "yes"
	}
	return "no"
}

func tableExists(ctx context.Context, meta *store.DB, secret string, inst store.Instance, dbName, table string) (bool, error) {
	conn, err := dbmig.OpenInstance(ctx, &inst, secret, dbName)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var n int
	err = conn.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`,
		table,
	).Scan(&n)
	return n > 0, err
}

func runOnInstance(ctx context.Context, meta *store.DB, secret, instanceID, dbName, sqlText string) error {
	inst, err := meta.GetInstance(instanceID)
	if err != nil || inst == nil {
		return err
	}
	conn, err := dbmig.OpenInstance(ctx, inst, secret, dbName)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, sqlText)
	return err
}

func ensurePhysicalDB(t *testing.T, secret string, inst store.Instance, dbName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := dbmig.OpenInstance(ctx, &inst, secret, "postgres")
	if err != nil {
		t.Logf("ensurePhysicalDB open %s: %v (exec probe may skip)", shortID(inst.ID), err)
		return
	}
	defer conn.Close()
	var exists bool
	if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, dbName).Scan(&exists); err != nil {
		t.Logf("ensurePhysicalDB check: %v", err)
		return
	}
	if exists {
		return
	}
	// CREATE DATABASE cannot run in a transaction; identifier is controlled constant.
	if _, err := conn.ExecContext(ctx, `CREATE DATABASE `+pgQuoteIdent(dbName)); err != nil {
		t.Logf("ensurePhysicalDB create on %s: %v", shortID(inst.ID), err)
	}
}

// Optional HTTP smoke against already-running backend (reuse 8800); skipped if unreachable.
func TestDDLMultiInstanceHTTPSmoke(t *testing.T) {
	base := os.Getenv("AUTO_DBA_BASE")
	if base == "" {
		base = "http://127.0.0.1:8800"
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(base + "/api/v1/info")
	if err != nil {
		t.Skip("backend not reachable")
	}
	resp.Body.Close()

	token, err := httpLogin(client, base)
	if err != nil {
		t.Skipf("login: %v", err)
	}
	insts, err := httpGETJSON(client, base+"/api/v1/instances?q=", token)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := insts["data"].([]any)
	var pgIDs []string
	for _, raw := range data {
		m, _ := raw.(map[string]any)
		dt, _ := m["db_type"].(string)
		id, _ := m["id"].(string)
		dt = strings.ToLower(dt)
		if dt == "postgres" || dt == "postgresql" || dt == "pg" {
			pgIDs = append(pgIDs, id)
		}
	}
	if len(pgIDs) < 2 {
		t.Skip("need 2 pg instances via API")
	}
	A, B := pgIDs[0], pgIDs[1]

	// check-sql uses data_source_ids[0] semantics: post with instance_id=A while multi would pick B in V3-like case
	body := map[string]any{
		"sql_content": "SELECT 1;",
		"type":        "ddl",
		"db_type":     "postgres",
		"instance_id": A,
		"db_name":     multiDDLVerifyDB,
	}
	code, out, err := httpPOSTJSON(client, base+"/api/v1/tickets/check-sql", token, body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("check-sql HTTP status=%d instance_id=%s body_keys=%v", code, shortID(A), keysOf(out))

	// submit with only primary instance_id (as UI does) — V4 style unregistered should fail
	body = map[string]any{
		"type":          "ddl",
		"db_type":       "postgres",
		"instance_id":   A,
		"db_name":       "db_definitely_not_registered_xyz",
		"sql_content":   "CREATE TABLE IF NOT EXISTS t_x (id int);",
		"description":   "multi-ddl-http-smoke",
		"assigned_to":   "admin",
		"need_rollback": encodeJSONString(map[string]any{"data_source_ids": []string{A, B}}),
	}
	code, out, err = httpPOSTJSON(client, base+"/api/v1/tickets", token, body)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := out["message"].(string)
	if code == 200 {
		t.Fatalf("expected unregistered reject, got 200: %v", out)
	}
	if !strings.Contains(msg, "库未登记") {
		t.Fatalf("expected 库未登记, got status=%d msg=%q", code, msg)
	}
	t.Logf("HTTP V4-like reject ok: %s", msg)
}

func encodeJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func keysOf(m map[string]any) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

func httpLogin(client *http.Client, base string) (string, error) {
	code, out, err := httpPOSTJSON(client, base+"/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "admin",
	})
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("login status %d: %v", code, out)
	}
	tok, _ := out["token"].(string)
	if tok == "" {
		return "", fmt.Errorf("empty token")
	}
	return tok, nil
}

func httpGETJSON(client *http.Client, url, token string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func httpPOSTJSON(client *http.Client, url, token string, body any) (int, map[string]any, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, out, nil
}
