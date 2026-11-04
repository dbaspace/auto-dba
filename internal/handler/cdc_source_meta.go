package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlexec"
	"auto-dba/internal/store"
)

type cdcSourceMeta struct {
	Tables   []string
	Live     map[string]struct{}
	LiveOK   bool
	Error    string
	ViaAgent bool
}

type cdcMetaJob struct {
	Init *store.CDCTask
	Inst *store.Instance
}

func (h *CDCHandler) cdcExecutionCatalog() (local []string, execs []sqlexec.Executor, err error) {
	if h == nil || h.Store == nil {
		return nil, nil, nil
	}
	raw, err := h.Store.GetGlobalArg(store.ArgTicketLocalIDC)
	if err != nil {
		return nil, nil, err
	}
	local = splitCodes(raw)
	rows, err := h.Store.ListSQLExecutors()
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		pass, decErr := secret.Decrypt(h.Secret, row.PasswordEnc)
		if decErr != nil {
			pass = ""
		}
		execs = append(execs, sqlexec.Executor{
			ID: row.ID, Name: row.Name, BaseURL: row.BaseURL,
			Username: row.Username, Password: pass, EnvCodes: row.EnvCodes, Enabled: row.Enabled,
		})
	}
	return local, execs, nil
}

func bareLiveSet(names []string) map[string]struct{} {
	live := map[string]struct{}{}
	for _, n := range names {
		k := bareTableKey(n)
		if k != "" {
			live[k] = struct{}{}
		}
	}
	return live
}

func listBaseTablesOnDB(ctx context.Context, db *sql.DB, schema string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n != "" && !strings.EqualFold(n, "debezium_signal") {
			all = append(all, n)
		}
	}
	return all, rows.Err()
}

// loadSourceMetaLocal 单连接拉 schema 表 + publication 成员。
func (h *CDCHandler) loadSourceMetaLocal(ctx context.Context, inst *store.Instance, init *store.CDCTask) cdcSourceMeta {
	out := cdcSourceMeta{Tables: []string{}, Live: map[string]struct{}{}}
	if inst == nil || init == nil {
		out.Error = "源端实例不存在"
		return out
	}
	extra := cdc.ParseTaskExtra(init.Description)
	src := cdc.TaskSourceInstance(inst, extra)
	schema := firstNonEmpty(extra["source_schema"], "public")
	pub := strings.TrimSpace(extra["publication_name"])
	db, err := dbmig.OpenInstance(ctx, src, h.Secret, init.SourceDB)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer db.Close()
	tables, err := listBaseTablesOnDB(ctx, db, schema)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Tables = tables
	if pub == "" {
		return out
	}
	live := map[string]struct{}{}
	liveOK := true
	addPub := func(conn *sql.DB, pubName string) bool {
		names, err := cdc.ListPublicationTables(ctx, conn, pubName)
		if err != nil {
			return false
		}
		for k := range bareLiveSet(names) {
			live[k] = struct{}{}
		}
		return true
	}
	if !addPub(db, pub) {
		liveOK = false
	}
	if liveOK {
		bindings := cdc.ResolveOrBuildShardBindings(extra, init.SourceDB, pub, extra["slot_name"], extra["connector_name"])
		for _, b := range bindings {
			dbName := firstNonEmpty(b.DbName, init.SourceDB)
			if strings.EqualFold(dbName, init.SourceDB) {
				continue
			}
			bp := firstNonEmpty(b.PublicationName, pub)
			cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			other, oerr := dbmig.OpenInstance(cctx, src, h.Secret, dbName)
			if oerr != nil {
				cancel()
				liveOK = false
				break
			}
			ok := addPub(other, bp)
			other.Close()
			cancel()
			if !ok {
				liveOK = false
				break
			}
		}
	}
	out.Live = live
	out.LiveOK = liveOK
	return out
}

// cdcMetaRemoteGroupKey 同执行器 + 同 PG 地址（host/port/user）合并为一次 FetchMeta，避免 64 实例拆成 64 次。
func cdcMetaRemoteGroupKey(execID string, init *store.CDCTask, inst *store.Instance) string {
	extra := cdc.ParseTaskExtra(init.Description)
	src := cdc.TaskSourceInstance(inst, extra)
	host := strings.TrimSpace(src.Host)
	if host == "" {
		host = strings.TrimSpace(src.DomainName)
	}
	port := src.Port
	if port <= 0 {
		port = 5432
	}
	return execID + "|" + strings.ToLower(host) + "|" + strconv.Itoa(port) + "|" + strings.TrimSpace(src.Username)
}

// loadSourceMetaForParents 按实例 idc 分流：本机 / sql-agent / missing。
func (h *CDCHandler) loadSourceMetaForParents(ctx context.Context, jobs []cdcMetaJob) []cdcSourceMeta {
	out := make([]cdcSourceMeta, len(jobs))
	if len(jobs) == 0 {
		return out
	}
	localCodes, execs, _ := h.cdcExecutionCatalog()

	type remoteGroup struct {
		exec *sqlexec.Executor
		idxs []int
	}
	remote := map[string]*remoteGroup{} // execID|host|port|user
	var localIdx []int

	for i, j := range jobs {
		if j.Init == nil || j.Inst == nil {
			out[i] = cdcSourceMeta{Error: "源端实例不存在", Tables: []string{}, Live: map[string]struct{}{}}
			continue
		}
		dec := sqlexec.Decide(j.Inst.IDC, localCodes, execs)
		switch dec.Mode {
		case sqlexec.ModeMissing:
			out[i] = cdcSourceMeta{
				Error:  "请在该环境部署 SQL 执行器后再拉可选表（异地不硬跨网）",
				Tables: []string{}, Live: map[string]struct{}{},
			}
		case sqlexec.ModeRemote:
			key := cdcMetaRemoteGroupKey(dec.Executor.ID, j.Init, j.Inst)
			g := remote[key]
			if g == nil {
				g = &remoteGroup{exec: dec.Executor}
				remote[key] = g
			}
			g.idxs = append(g.idxs, i)
		default:
			localIdx = append(localIdx, i)
		}
	}

	runBounded(len(localIdx), cdcBatchConcurrency, func(k int) {
		i := localIdx[k]
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		out[i] = h.loadSourceMetaLocal(cctx, jobs[i].Inst, jobs[i].Init)
		cancel()
	})

	remoteKeys := make([]string, 0, len(remote))
	for k := range remote {
		remoteKeys = append(remoteKeys, k)
	}
	runBounded(len(remoteKeys), cdcBatchConcurrency, func(k int) {
		g := remote[remoteKeys[k]]
		h.fillSourceMetaRemote(ctx, g.exec, jobs, g.idxs, out)
	})
	return out
}

func (h *CDCHandler) fillSourceMetaRemote(ctx context.Context, exec *sqlexec.Executor, jobs []cdcMetaJob, idxs []int, out []cdcSourceMeta) {
	if exec == nil || len(idxs) == 0 {
		return
	}
	inst := jobs[idxs[0]].Inst
	init0 := jobs[idxs[0]].Init
	extra0 := cdc.ParseTaskExtra(init0.Description)
	src := cdc.TaskSourceInstance(inst, extra0)
	plain, err := secret.Decrypt(h.Secret, src.Password)
	if err != nil {
		for _, i := range idxs {
			out[i] = cdcSourceMeta{Error: "解密实例密码失败: " + err.Error(), Tables: []string{}, Live: map[string]struct{}{}, ViaAgent: true}
		}
		return
	}
	host := strings.TrimSpace(src.Host)
	if host == "" {
		host = strings.TrimSpace(src.DomainName)
	}
	req := sqlexec.MetaRequest{
		Target: sqlexec.Target{
			InstanceID: inst.ID, InstanceName: inst.Name, Host: host, Port: src.Port,
			Username: src.Username, Password: plain, DBType: src.DBType, Env: inst.IDC,
		},
	}
	have := map[string]bool{}
	for _, i := range idxs {
		extra := cdc.ParseTaskExtra(jobs[i].Init.Description)
		dbName := strings.TrimSpace(jobs[i].Init.SourceDB)
		k := strings.ToLower(dbName)
		if dbName == "" || have[k] {
			continue
		}
		have[k] = true
		req.Databases = append(req.Databases, sqlexec.MetaDB{
			DBName:          dbName,
			Schema:          firstNonEmpty(extra["source_schema"], "public"),
			PublicationName: strings.TrimSpace(extra["publication_name"]),
		})
	}
	resp, err := sqlexec.FetchMeta(ctx, exec.BaseURL, exec.Username, exec.Password, req)
	if err != nil {
		execName := strings.TrimSpace(exec.Name)
		if execName == "" {
			execName = exec.ID
		}
		reason := err.Error()
		if errors.Is(err, sqlexec.ErrMetaUnsupported) {
			reason = sqlexec.ErrMetaUnsupported.Error()
		}
		msg := fmt.Sprintf("执行器「%s」元数据查询失败：%s", execName, reason)
		for _, i := range idxs {
			out[i] = cdcSourceMeta{Error: msg, Tables: []string{}, Live: map[string]struct{}{}, ViaAgent: true}
		}
		return
	}
	byDB := map[string]sqlexec.MetaDBResult{}
	for _, r := range resp.Results {
		byDB[strings.ToLower(strings.TrimSpace(r.DBName))] = r
	}
	for _, i := range idxs {
		init := jobs[i].Init
		pub := strings.TrimSpace(cdc.ParseTaskExtra(init.Description)["publication_name"])
		r, ok := byDB[strings.ToLower(strings.TrimSpace(init.SourceDB))]
		if !ok {
			out[i] = cdcSourceMeta{Error: "执行器未返回该库元数据", Tables: []string{}, Live: map[string]struct{}{}, ViaAgent: true}
			continue
		}
		if r.Error != "" {
			out[i] = cdcSourceMeta{Error: r.Error, Tables: []string{}, Live: map[string]struct{}{}, ViaAgent: true}
			continue
		}
		tables := r.Tables
		if tables == nil {
			tables = []string{}
		}
		pubs := r.PublicationTables
		if pubs == nil {
			pubs = []string{}
		}
		out[i] = cdcSourceMeta{
			Tables:   tables,
			Live:     bareLiveSet(pubs),
			LiveOK:   pub != "",
			ViaAgent: true,
		}
	}
}

func (h *CDCHandler) occupancyFromMeta(init *store.CDCTask, meta cdcSourceMeta) cdcOccupancy {
	var children []store.CDCTask
	if h != nil && h.Store != nil && init != nil {
		children, _ = h.Store.ListCDCByParentID(init.ID, "add_table")
	}
	return computeCDCOccupancy(init, children, meta.Live, meta.LiveOK && meta.Error == "")
}
