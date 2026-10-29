package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/notify"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func createDBInstanceIDs(it *store.Issue) []string {
	extra := parseCreateDBExtra(it)
	if extra != nil && len(extra.InstanceIDs) > 0 {
		return extra.InstanceIDs
	}
	if it != nil && strings.TrimSpace(it.InstanceID) != "" {
		return []string{it.InstanceID}
	}
	return nil
}

func grantPrivileges(perms []string) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range perms {
		priv := ""
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "r", "select":
			priv = "SELECT"
		case "c", "insert":
			priv = "INSERT"
		case "u", "update":
			priv = "UPDATE"
		case "d", "delete":
			priv = "DELETE"
		}
		if priv == "" || seen[priv] {
			continue
		}
		seen[priv] = true
		out = append(out, priv)
	}
	if len(out) == 0 {
		return "SELECT"
	}
	return strings.Join(out, ", ")
}

func sqlQuoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func pgQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(name), `"`, `""`) + `"`
}

func buildCreateUserSQL(engine, username, password string) string {
	user := strings.TrimSpace(username)
	if user == "" {
		return ""
	}
	if engine == "postgres" {
		return fmt.Sprintf("CREATE USER %s ENCRYPTED PASSWORD %s", pgQuoteIdent(user), sqlQuoteString(password))
	}
	return fmt.Sprintf("CREATE USER %s IDENTIFIED BY %s", user, sqlQuoteString(password))
}

func buildConnectGrantSQL(engine, username, dbName string) string {
	user := strings.TrimSpace(username)
	db := strings.TrimSpace(dbName)
	if user == "" || db == "" {
		return ""
	}
	if engine == "postgres" {
		return fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", pgQuoteIdent(db), pgQuoteIdent(user))
	}
	return ""
}

// buildSchemaGrantSQL 库内授权。PG 与 DCL 对齐（复用 buildPGDCLInDBStatements：USAGE 无 CREATE + FOR ROLE owner）。
func buildSchemaGrantSQL(engine, username, dbName, owner string, perms []string) string {
	user := strings.TrimSpace(username)
	db := strings.TrimSpace(dbName)
	if user == "" || db == "" {
		return ""
	}
	if engine == "postgres" {
		stmts := buildPGDCLInDBStatements(user, owner, perms)
		if len(stmts) == 0 {
			return ""
		}
		return strings.Join(stmts, ";\n")
	}
	privs := grantPrivileges(perms)
	return fmt.Sprintf("GRANT %s ON DATABASE %s TO %s", privs, db, user)
}

func alreadyExistsErr(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "already exists") ||
		strings.Contains(m, "duplicate") ||
		strings.Contains(m, "already exist")
}

func execOKOrExists(res *ticketexec.Result) error {
	if res == nil {
		return fmt.Errorf("空执行结果")
	}
	if res.Success {
		return nil
	}
	if alreadyExistsErr(res.Message) {
		return nil
	}
	for _, rec := range res.Records {
		if alreadyExistsErr(rec.ErrorMessage) {
			return nil
		}
	}
	if res.Message != "" {
		return fmt.Errorf("%s", res.Message)
	}
	return fmt.Errorf("执行失败")
}

func normalizeDomainKey(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), "."))
}

func instanceContainsDomain(inst *store.Instance, domain string) bool {
	want := normalizeDomainKey(domain)
	if want == "" || inst == nil {
		return false
	}
	for _, d := range inst.Domains {
		if normalizeDomainKey(d) == want {
			return true
		}
	}
	return normalizeDomainKey(inst.DomainName) == want
}

// ensureTicketDomainVisible 校验域名属于实例登记，且在域名管理中对本租户可见（审计会话放宽）。
func ensureTicketDomainVisible(db *store.DB, c *gin.Context, inst *store.Instance, domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" || inst == nil {
		return nil
	}
	if !instanceContainsDomain(inst, domain) {
		return fmt.Errorf("数据源域名不属于所选实例登记的域名")
	}
	ad, err := db.GetAccessDomainByDomainType(domain, inst.DBType)
	if err != nil {
		return err
	}
	if ad == nil {
		return fmt.Errorf("域名未在域名管理中登记，请先同步域名资源")
	}
	if ad.InstanceID != "" && ad.InstanceID != inst.ID {
		return fmt.Errorf("数据源域名与所选实例不匹配")
	}
	if platformAdminAuditing(db, c) {
		return nil
	}
	tid := middleware.TenantID(c)
	if ad.TenantID == tid {
		return nil
	}
	ok, err := db.HasResourceGrant(tid, middleware.UserID(c), "domain", ad.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("数据源域名对本租户不可见")
	}
	return nil
}

func databaseContainsDomain(db *store.Database, domain string) bool {
	want := normalizeDomainKey(domain)
	if want == "" || db == nil {
		return false
	}
	for _, d := range db.Domains {
		if normalizeDomainKey(d) == want {
			return true
		}
	}
	return false
}

func resolveInstanceHost(inst *store.Instance, preferredDomain string) string {
	if d := strings.TrimSpace(preferredDomain); d != "" {
		return d
	}
	if inst == nil {
		return ""
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	return host
}

func (h *TicketHandler) execTarget(inst *store.Instance, dbName, preferredDomain string) (ticketexec.Target, error) {
	var t ticketexec.Target
	if inst == nil {
		return t, fmt.Errorf("工单实例不存在")
	}
	password, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		return t, fmt.Errorf("解密实例密码失败: %w", err)
	}
	host := resolveInstanceHost(inst, preferredDomain)
	if host == "" {
		return t, fmt.Errorf("实例未配置连接地址或域名")
	}
	t = ticketexec.Target{
		Host:     host,
		Port:     inst.Port,
		Username: inst.Username,
		Password: password,
		DBName:   dbName,
		DBType:   inst.DBType,

		LongStmtTimeout: h.Store.TicketDDLLongStmtTimeout(),
	}
	return t, nil
}

func mergeExecResult(dst, src *ticketexec.Result) {
	if dst == nil || src == nil {
		return
	}
	if dst.Engine == "" {
		dst.Engine = src.Engine
	}
	dst.Records = append(dst.Records, src.Records...)
	dst.SuccessSQLs = append(dst.SuccessSQLs, src.SuccessSQLs...)
	dst.SuccessCount += src.SuccessCount
	dst.FailCount += src.FailCount
	// 计划语句数按单库口径，取较大值（多实例不累加）
	if src.StatementCount > dst.StatementCount {
		dst.StatementCount = src.StatementCount
	}
	if src.Compacted {
		dst.Compacted = true
	}
	mergeTargetStats(dst, src.TargetStats)
	if src.Skipped {
		dst.Skipped = true
	}
	if !src.Success {
		dst.Success = false
		if src.Message != "" {
			dst.Message = src.Message
		}
		if src.Summary != "" {
			dst.Summary = src.Summary
		}
		return
	}
	if src.Summary != "" {
		if dst.Summary != "" {
			dst.Summary += "; "
		}
		dst.Summary += src.Summary
	}
}

func mergeTargetStats(dst *ticketexec.Result, src []ticketexec.TargetStat) {
	if dst == nil || len(src) == 0 {
		return
	}
	idx := map[string]int{}
	for i := range dst.TargetStats {
		key := strings.TrimSpace(dst.TargetStats[i].Host) + "\x00" + strings.TrimSpace(dst.TargetStats[i].DB)
		idx[key] = i
	}
	for _, st := range src {
		key := strings.TrimSpace(st.Host) + "\x00" + strings.TrimSpace(st.DB)
		if i, ok := idx[key]; ok {
			dst.TargetStats[i].Success += st.Success
			dst.TargetStats[i].Failed += st.Failed
			continue
		}
		idx[key] = len(dst.TargetStats)
		dst.TargetStats = append(dst.TargetStats, st)
	}
}

func (h *TicketHandler) executeCreateDBTicket(ctx context.Context, it *store.Issue) error {
	ids := createDBInstanceIDs(it)
	if len(ids) == 0 {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: "工单未指定实例"})
	}
	merged := &ticketexec.Result{StartedAt: time.Now().UTC().Format(time.RFC3339), Success: true}
	for _, id := range ids {
		inst, err := h.Store.GetInstance(id)
		if err != nil {
			return err
		}
		if inst == nil {
			merged.Success = false
			merged.Message = "实例不存在: " + id
			break
		}
		check := applyCreateDBReview(h, it.SQLContent, inst.DBType, id)
		if !check.OK {
			return h.persistExec(it, reviewGateResult(check))
		}
		domain := ddlDomainForInstance(it.NeedRollback, id, it.Domain)
		t, err := h.execTarget(inst, it.DBName, domain)
		if err != nil {
			merged.Success = false
			merged.Message = err.Error()
			break
		}
		t.ConnectDB = ticketexec.AdminConnectDB(inst.DBType)
		h.watchSession(it, inst, t.Host, it.DBName, &t)
		res := ticketexec.Run(ctx, store.IssueTypeCreateDB, it.SQLContent, t)
		mergeExecResult(merged, res)
		if res == nil || !res.Success {
			if merged.Message == "" && res != nil {
				merged.Message = res.Message
			}
			break
		}
	}
	merged.EndedAt = time.Now().UTC().Format(time.RFC3339)
	if merged.Success && merged.Summary == "" {
		merged.Summary = "建库 SQL 已执行"
	}
	return h.persistExec(it, merged)
}

func (h *TicketHandler) finishCreateDBAfterExec(ctx context.Context, it *store.Issue) error {
	if it == nil || it.IssueType != store.IssueTypeCreateDB {
		return nil
	}
	if it.Status != store.StatusExecuted {
		return nil
	}
	if err := materializeCreateDBAccounts(h.Store, h.Secret, it); err != nil {
		return err
	}
	warn := provisionCreateDBAccounts(ctx, h, it)
	if warn != "" {
		appendCreateDBWarn(it, h, warn)
	}
	if items := h.createDBNotifyItems(it); len(items) > 0 {
		cp := *it
		go notify.CreateDBDone(h.Store, &cp, items, warn)
	}
	return nil
}

// createDBNotifyItems 按实例汇总建库结果（地址/库/账号/密码），供通知申请人。
func (h *TicketHandler) createDBNotifyItems(it *store.Issue) []notify.CreateDBNotifyItem {
	extra := parseCreateDBExtra(it)
	if extra == nil {
		return nil
	}
	ids := createDBInstanceIDs(it)
	accs, _ := h.Store.ListAccountsByTicket(it.ID)
	names := sqlcheck.ParseCreateDBNames(it.SQLContent)
	if len(names) == 0 {
		for _, n := range strings.Split(it.DBName, ",") {
			if s := strings.TrimSpace(n); s != "" {
				names = append(names, s)
			}
		}
	}
	username := strings.TrimSpace(extra.Username)
	var out []notify.CreateDBNotifyItem
	for _, id := range ids {
		inst, _ := h.Store.GetInstance(id)
		if inst == nil {
			continue
		}
		host := ddlDomainForInstance(it.NeedRollback, id, it.Domain)
		if host == "" {
			host = strings.TrimSpace(inst.DomainName)
		}
		if host == "" {
			host = inst.Host
		}
		addr := host
		if !strings.Contains(host, ":") && inst.Port > 0 {
			addr = fmt.Sprintf("%s:%d", host, inst.Port)
		}
		item := notify.CreateDBNotifyItem{
			Address:     addr,
			DBNames:     names,
			Permissions: extra.Permissions,
		}
		if username != "" {
			item.Username = username
			if prev, _ := h.Store.FindAccountByInstanceUser(id, username, it.ID); prev != nil {
				item.Reused = true
			} else {
				for _, acc := range accs {
					if acc.InstanceID != id || acc.Username != username {
						continue
					}
					if pwd, err := secret.Decrypt(h.Secret, acc.Password); err == nil {
						item.Password = pwd
					}
					break
				}
				if item.Password == "" {
					item.Reused = true
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func appendCreateDBWarn(it *store.Issue, h *TicketHandler, warn string) {
	if it == nil || h == nil || strings.TrimSpace(warn) == "" {
		return
	}
	res := ticketexec.Parse(it.ExecuteResult)
	if res == nil {
		res = &ticketexec.Result{Success: true}
	}
	if res.Summary != "" {
		res.Summary += "；"
	}
	res.Summary += "账号授权：" + warn
	_ = h.Store.UpdateIssueExec(it.ID, it.Status, ticketexec.ToJSON(res))
	it.ExecuteResult = ticketexec.ToJSON(res)
	it.Exec = res
}

func (h *TicketHandler) maybeFinishApprovedCreateDB(ctx context.Context, it *store.Issue) *store.Issue {
	if it == nil || it.IssueType != store.IssueTypeCreateDB || it.Status != store.StatusApproved {
		return it
	}
	if scheduledInFuture(it.ScheduledAt) {
		return it
	}
	// 历史卡在「已通过」的建库单：后台异步补执行，避免详情页同步跑被 HTTP 超时掐断
	_ = h.beginAsyncAutoExec(it)
	if latest, err := h.Store.GetIssue(it.ID); err == nil && latest != nil {
		it = latest
	}
	return it
}

// provisionCreateDBAccounts 对齐 Hub：建库成功后在实例上 CREATE USER 并授权。失败不推翻工单。
func provisionCreateDBAccounts(ctx context.Context, h *TicketHandler, it *store.Issue) string {
	if h == nil || it == nil {
		return ""
	}
	extra := parseCreateDBExtra(it)
	if extra == nil || strings.TrimSpace(extra.Username) == "" {
		return ""
	}
	accs, err := h.Store.ListAccountsByTicket(it.ID)
	if err != nil {
		return err.Error()
	}
	if len(accs) == 0 {
		return ""
	}
	var warns []string
	createdUser := map[string]bool{}
	for _, acc := range accs {
		inst, _ := h.Store.GetInstance(acc.InstanceID)
		if inst == nil {
			warns = append(warns, "实例不存在 "+acc.InstanceID)
			continue
		}
		pwd, err := secret.Decrypt(h.Secret, acc.Password)
		if err != nil {
			warns = append(warns, "解密账号密码失败")
			continue
		}
		domain := ddlDomainForInstance(it.NeedRollback, acc.InstanceID, it.Domain)
		admin, err := h.execTarget(inst, acc.DBName, domain)
		if err != nil {
			warns = append(warns, err.Error())
			continue
		}
		admin.ConnectDB = ticketexec.AdminConnectDB(inst.DBType)
		engine := ticketexec.EngineOf(inst.DBType)
		if !createdUser[acc.InstanceID] {
			if sql := buildCreateUserSQL(engine, acc.Username, pwd); sql != "" {
				if err := execOKOrExists(ticketexec.ExecStatements(ctx, admin, []string{sql})); err != nil {
					warns = append(warns, "CREATE USER: "+err.Error())
					continue
				}
			}
			createdUser[acc.InstanceID] = true
		}
		if sql := buildConnectGrantSQL(engine, acc.Username, acc.DBName); sql != "" {
			if err := execOKOrExists(ticketexec.ExecStatements(ctx, admin, []string{sql})); err != nil {
				warns = append(warns, "GRANT CONNECT: "+err.Error())
			}
		}
		owner := ""
		if engine == "postgres" {
			o, oerr := ticketexec.LookupPostgresDatabaseOwner(ctx, admin, acc.DBName)
			if oerr != nil {
				warns = append(warns, "查库 owner("+acc.DBName+"): "+oerr.Error())
				continue
			}
			owner = o
		}
		if sql := buildSchemaGrantSQL(engine, acc.Username, acc.DBName, owner, extra.Permissions); sql != "" {
			target := admin
			if engine == "postgres" {
				target.ConnectDB = acc.DBName
			}
			stmts := sqlcheck.SplitSQL(sql)
			if err := execOKOrExists(ticketexec.ExecStatements(ctx, target, stmts)); err != nil {
				warns = append(warns, "GRANT: "+err.Error())
			}
		}
	}
	return strings.Join(warns, "；")
}
