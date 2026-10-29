package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/notify"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// dclExtra 权限申请工单扩展（存 need_rollback）。
type dclExtra struct {
	Username    string   `json:"username"`
	Permissions []string `json:"permissions"`
	Databases   []string `json:"databases"`
	DBType      string   `json:"db_type"`
}

func parseDCLExtra(it *store.Issue) *dclExtra {
	if it == nil {
		return nil
	}
	raw := strings.TrimSpace(it.NeedRollback)
	if raw == "" {
		return nil
	}
	var ex dclExtra
	if json.Unmarshal([]byte(raw), &ex) != nil {
		return nil
	}
	ex.Username = strings.TrimSpace(ex.Username)
	ex.DBType = strings.TrimSpace(ex.DBType)
	var dbs []string
	for _, d := range ex.Databases {
		d = strings.TrimSpace(d)
		if d != "" {
			dbs = append(dbs, d)
		}
	}
	ex.Databases = dbs
	if ex.Username == "" && len(ex.Databases) == 0 {
		return nil
	}
	return &ex
}

func encodeDCLExtra(ex *dclExtra) string {
	if ex == nil {
		return ""
	}
	b, _ := json.Marshal(ex)
	return string(b)
}

func splitDBNames(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// buildPGDCLConnectSQL 库级 CONNECT（在维护库会话执行）。
func buildPGDCLConnectSQL(dbName, grantee string) string {
	db := strings.TrimSpace(dbName)
	user := strings.TrimSpace(grantee)
	if db == "" || user == "" {
		return ""
	}
	return fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", pgQuoteIdent(db), pgQuoteIdent(user))
}

// buildPGDCLInDBStatements 已连接到目标库后执行的授权。owner 必须为执行前查到的库拥有者。
func buildPGDCLInDBStatements(grantee, owner string, perms []string) []string {
	user := strings.TrimSpace(grantee)
	own := strings.TrimSpace(owner)
	if user == "" || own == "" {
		return nil
	}
	privs := grantPrivileges(perms)
	u := pgQuoteIdent(user)
	o := pgQuoteIdent(own)
	return []string{
		fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA public TO %s", privs, u),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", u),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT %s ON TABLES TO %s", o, privs, u),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO %s", u),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO %s", o, u),
	}
}

// renderPGDCLScript 工单详情/提交用完整可执行脚本（owners 为每库真实 owner）。
func renderPGDCLScript(databases []string, grantee string, owners map[string]string, perms []string) string {
	var b strings.Builder
	b.WriteString("-- 账号不存在时执行前自动 CREATE USER … ENCRYPTED PASSWORD；FOR ROLE 为提交/执行时查询的库 owner\n")
	written := 0
	for _, db := range databases {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}
		owner := ""
		if owners != nil {
			owner = strings.TrimSpace(owners[db])
		}
		if owner == "" {
			continue
		}
		if written > 0 {
			b.WriteString("\n")
		}
		b.WriteString("-- \\c ")
		b.WriteString(db)
		b.WriteString("\n")
		if s := buildPGDCLConnectSQL(db, grantee); s != "" {
			b.WriteString(s)
			b.WriteString(";\n")
		}
		for _, s := range buildPGDCLInDBStatements(grantee, owner, perms) {
			b.WriteString(s)
			b.WriteString(";\n")
		}
		written++
	}
	return strings.TrimSpace(b.String())
}

func renderMySQLDCLScript(databases []string, grantee string, perms []string) string {
	privs := grantPrivileges(perms)
	user := strings.TrimSpace(grantee)
	if user == "" {
		return ""
	}
	var b strings.Builder
	for _, db := range databases {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("GRANT %s ON `%s`.* TO '%s'@'%%';\n", privs, strings.ReplaceAll(db, "`", "``"), strings.ReplaceAll(user, "'", "''")))
	}
	return strings.TrimSpace(b.String())
}

func dclResolveTargets(it *store.Issue) (grantee string, dbs, perms []string) {
	extra := parseDCLExtra(it)
	if extra != nil {
		grantee = extra.Username
		perms = append([]string{}, extra.Permissions...)
		dbs = append([]string{}, extra.Databases...)
	}
	if len(dbs) == 0 && it != nil {
		dbs = splitDBNames(it.DBName)
	}
	return grantee, dbs, perms
}

type dclUserProvision struct {
	Created  bool
	Password string // 新建时明文，仅用于通知
}

func (h *TicketHandler) ensurePGDCLUser(ctx context.Context, admin ticketexec.Target, inst *store.Instance, it *store.Issue, grantee string, dbs, perms []string) (dclUserProvision, error) {
	var out dclUserProvision
	exists, err := ticketexec.PostgresUserExists(ctx, admin, grantee)
	if err != nil {
		return out, err
	}
	if exists {
		return out, nil
	}
	plain := randomAccountPassword(16)
	sqlText := buildCreateUserSQL("postgres", grantee, plain)
	if sqlText == "" {
		return out, fmt.Errorf("无法生成 CREATE USER")
	}
	res := ticketexec.ExecStatements(ctx, admin, []string{sqlText})
	if err := execOKOrExists(res); err != nil {
		return out, fmt.Errorf("CREATE USER: %w", err)
	}
	out.Created = true
	out.Password = plain
	if err := materializeDCLAccounts(h.Store, h.Secret, inst, it, grantee, plain, dbs, perms); err != nil {
		return out, fmt.Errorf("写入账号管理失败: %w", err)
	}
	return out, nil
}

func materializeDCLAccounts(st *store.DB, secretKey string, inst *store.Instance, it *store.Issue, username, plain string, dbs, perms []string) error {
	if st == nil || inst == nil || it == nil {
		return nil
	}
	username = strings.TrimSpace(username)
	if username == "" || len(dbs) == 0 {
		return nil
	}
	enc, err := secret.Encrypt(secretKey, plain)
	if err != nil {
		return err
	}
	for _, dbName := range dbs {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" {
			continue
		}
		if found, _ := st.FindAccount(inst.ID, username, dbName); found != nil {
			continue
		}
		var dbID string
		if rec, _ := st.FindDatabase(inst.ID, dbName); rec != nil {
			dbID = rec.ID
		}
		acc := &store.DBAccount{
			InstanceID:  inst.ID,
			DatabaseID:  dbID,
			DBName:      dbName,
			DBType:      inst.DBType,
			IDC:         inst.IDC,
			Username:    username,
			Password:    enc,
			Permissions: perms,
			Description: strings.TrimSpace(it.Description),
			TicketID:    it.ID,
			CreatedBy:   it.Submitter,
			UpdatedBy:   it.Submitter,
			TenantID:    it.TenantID,
		}
		if err := st.CreateAccount(acc); err != nil {
			return err
		}
	}
	return nil
}

func dclNotifyAddress(inst *store.Instance, domain string) string {
	host := resolveInstanceHost(inst, domain)
	if host == "" && inst != nil {
		host = inst.Host
	}
	if inst != nil && !strings.Contains(host, ":") && inst.Port > 0 {
		return fmt.Sprintf("%s:%d", host, inst.Port)
	}
	return host
}

func buildDCLNotifyItem(inst *store.Instance, domain, grantee, password string, created bool, dbs, perms []string) notify.DCLNotifyItem {
	return notify.DCLNotifyItem{
		Address:     dclNotifyAddress(inst, domain),
		DBNames:     append([]string{}, dbs...),
		Username:    grantee,
		Password:    password,
		Created:     created,
		Permissions: append([]string{}, perms...),
	}
}

// executeDCLTicket 按库执行授权：PG 对齐运维模板；MySQL/SR 执行库级 GRANT。
func (h *TicketHandler) executeDCLTicket(ctx context.Context, it *store.Issue) error {
	if it == nil {
		return fmt.Errorf("工单不存在")
	}
	inst, err := h.instanceUsableOrRegistered(it.InstanceID, it.TenantID)
	if err != nil {
		return err
	}
	if inst == nil {
		return h.persistExec(it, ticketexec.FailBeforeExec("工单实例不存在: "+it.InstanceID, ticketexec.Record{
			InstanceID: it.InstanceID, DB: it.DBName,
		}))
	}
	grantee, dbs, perms := dclResolveTargets(it)
	if grantee == "" {
		return h.persistExec(it, ticketexec.FailBeforeExec("工单缺少被授权账号", ticketexec.Record{
			InstanceID: inst.ID, InstanceName: inst.Name, DB: it.DBName,
		}))
	}
	if len(dbs) == 0 {
		return h.persistExec(it, ticketexec.FailBeforeExec("工单未指定数据库", ticketexec.Record{
			InstanceID: inst.ID, InstanceName: inst.Name,
		}))
	}
	if len(perms) == 0 {
		perms = []string{"r"}
	}
	engine := ticketexec.EngineOf(inst.DBType)
	domain := strings.TrimSpace(it.Domain)
	merged := &ticketexec.Result{
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Success:   true,
		Engine:    engine,
	}
	var provision dclUserProvision

	if engine == "postgres" {
		admin, terr := h.execTarget(inst, dbs[0], domain)
		if terr != nil {
			return h.persistExec(it, ticketexec.FailBeforeExec(terr.Error(), ticketexec.Record{
				InstanceID: inst.ID, InstanceName: inst.Name, Host: resolveInstanceHost(inst, domain), DB: dbs[0],
			}))
		}
		admin.ConnectDB = ticketexec.AdminConnectDB(inst.DBType)
		h.watchSession(it, inst, admin.Host, dbs[0], &admin)

		provision, err = h.ensurePGDCLUser(ctx, admin, inst, it, grantee, dbs, perms)
		if err != nil {
			merged.Success = false
			merged.Message = err.Error()
			merged.EndedAt = time.Now().UTC().Format(time.RFC3339)
			return h.persistExec(it, merged)
		}

		for _, db := range dbs {
			if err := ctx.Err(); err != nil {
				merged.Success = false
				if merged.Message == "" {
					merged.Message = err.Error()
				}
				break
			}
			owner, oerr := ticketexec.LookupPostgresDatabaseOwner(ctx, admin, db)
			if oerr != nil {
				merged.Success = false
				merged.Message = oerr.Error()
				rec := ticketexec.Record{
					InstanceID: inst.ID, InstanceName: inst.Name, Host: admin.Host,
					DB: db, Port: inst.Port, Username: inst.Username, SQL: ticketexec.PrecheckSQL,
					Status: "failed", ErrorMessage: oerr.Error(),
				}
				merged.FailCount++
				merged.Records = append(merged.Records, rec)
				break
			}
			connectSQL := buildPGDCLConnectSQL(db, grantee)
			cres := ticketexec.ExecStatements(ctx, admin, []string{connectSQL})
			mergeExecResult(merged, cres)
			if cres == nil || !cres.Success {
				if merged.Message == "" && cres != nil {
					merged.Message = cres.Message
				}
				if provision.Created && merged.Message != "" {
					merged.Message = "账号已创建，授权未完成: " + merged.Message
				}
				break
			}
			target := admin
			target.ConnectDB = db
			target.DBName = db
			stmts := buildPGDCLInDBStatements(grantee, owner, perms)
			if len(stmts) == 0 {
				merged.Success = false
				merged.Message = fmt.Sprintf("库 %s 缺少 owner，无法生成授权 SQL", db)
				break
			}
			gres := ticketexec.ExecStatements(ctx, target, stmts)
			mergeExecResult(merged, gres)
			if gres == nil || !gres.Success {
				if merged.Message == "" && gres != nil {
					merged.Message = gres.Message
				}
				if provision.Created && merged.Message != "" {
					merged.Message = "账号已创建，授权未完成: " + merged.Message
				}
				break
			}
		}
	} else {
		script := renderMySQLDCLScript(dbs, grantee, perms)
		stmts := sqlcheck.SplitSQL(script)
		if len(stmts) == 0 {
			return h.persistExec(it, ticketexec.FailBeforeExec("无法生成授权 SQL", ticketexec.Record{
				InstanceID: inst.ID, InstanceName: inst.Name, DB: it.DBName,
			}))
		}
		t, terr := h.execTarget(inst, dbs[0], domain)
		if terr != nil {
			return h.persistExec(it, ticketexec.FailBeforeExec(terr.Error(), ticketexec.Record{
				InstanceID: inst.ID, InstanceName: inst.Name, Host: resolveInstanceHost(inst, domain), DB: dbs[0],
			}))
		}
		h.watchSession(it, inst, t.Host, dbs[0], &t)
		res := ticketexec.ExecStatements(ctx, t, stmts)
		mergeExecResult(merged, res)
		if res == nil || !res.Success {
			if merged.Message == "" && res != nil {
				merged.Message = res.Message
			}
		}
	}

	merged.EndedAt = time.Now().UTC().Format(time.RFC3339)
	if merged.Success && merged.Summary == "" {
		merged.Summary = "权限授权 SQL 已执行"
	}
	if err := h.persistExec(it, merged); err != nil {
		return err
	}
	if merged.Success && engine == "postgres" {
		item := buildDCLNotifyItem(inst, domain, grantee, provision.Password, provision.Created, dbs, perms)
		cp := *it
		go notify.DCLDone(h.Store, &cp, item)
	}
	return nil
}
