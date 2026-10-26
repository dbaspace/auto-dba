package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/archive"
	"auto-dba/internal/backup"
	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/flashback"
	"auto-dba/internal/goinception"
	"auto-dba/internal/middleware"
	"auto-dba/internal/rediscmd"
	"auto-dba/internal/redisquery"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

type TicketHandler struct {
	Store     *store.DB
	Secret    string
	Flashback *flashback.FlashbackImpl
	Migration *dbmig.Engine
	Archive   *archive.Engine
	CDC       *cdc.Orchestrator
	Backup    *backup.Engine
	DNS       *DNSHandler
}

type ticketCheckReq struct {
	SQLContent string `json:"sql_content"`
	Type       string `json:"type"`
	DBType     string `json:"db_type"`
	InstanceID string `json:"instance_id"`
	DBName     string `json:"db_name"`
	// 分库/分表：SQL 为待前缀展开的模板，字面表名通常不存在，跳过库表存在性校验。
	ShardDB    bool `json:"shard_db"`
	ShardTable bool `json:"shard_table"`
}

type ticketSubmitReq struct {
	InstanceID    string `json:"instance_id"`
	Domain        string `json:"domain"`
	DBName        string `json:"db_name"`
	DatabaseID    string `json:"database_id"`
	Type          string `json:"type"`
	SQLContent    string `json:"sql_content"`
	Description   string `json:"description"`
	DBType        string `json:"db_type"`
	AssignedTo    string `json:"assigned_to"`
	AssignedToA   string `json:"AssignedTo"`
	Reviewer      string `json:"reviewer"`
	Approver      string `json:"approver"`
	NeedRollback  string `json:"need_rollback"`
	ScheduledAt   string `json:"scheduled_at"`
	NeedSensitive bool   `json:"need_sensitive_data"`
	DBAAssigned   string `json:"dba_assigned_to"`
	SecAssigned   string `json:"security_assigned_to"`
	RowCount      int    `json:"row_count"`
}

func (r ticketSubmitReq) reviewer() string {
	for _, v := range []string{r.AssignedTo, r.AssignedToA, r.Reviewer, r.Approver} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

type targetActDeny int

const (
	targetActAllow targetActDeny = iota
	targetActDenyNotApprover
	targetActDenyDBNotRegistered
	targetActDenyInstanceMissing
)

func usernameInList(username string, list []string) bool {
	username = strings.TrimSpace(username)
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), username) {
			return true
		}
	}
	return false
}

// canActOnTarget 校验提交目标：实例/库已登记且归属当前租户。
// 提交人不必在审批人名单中；所选审核人由 Submit 路径另行校验。
func (h *TicketHandler) canActOnTarget(tenantID, instanceID, dbName, _ string) (bool, targetActDeny, string, error) {
	inst, err := h.Store.GetInstance(instanceID)
	if err != nil {
		return false, targetActAllow, "", err
	}
	if inst == nil {
		return false, targetActDenyInstanceMissing, "实例未登记，请先在「配置 → 实例」中登记", nil
	}
	ownsInst := strings.TrimSpace(inst.TenantID) == strings.TrimSpace(tenantID)
	if !ownsInst {
		n, err := h.Store.CountDatabasesOnInstanceInTenant(instanceID, tenantID)
		if err != nil {
			return false, targetActAllow, "", err
		}
		if n == 0 {
			return false, targetActDenyInstanceMissing, "实例未登记，请先在「配置 → 实例」中登记", nil
		}
	}
	if strings.TrimSpace(dbName) == "" {
		return true, targetActAllow, "", nil
	}
	dbEnt, err := h.Store.FindDatabase(instanceID, dbName)
	if err != nil {
		return false, targetActAllow, "", err
	}
	if dbEnt == nil {
		return false, targetActDenyDBNotRegistered, "库未登记，请先在「配置 → 已登记库」中登记 " + dbName, nil
	}
	if tid := strings.TrimSpace(tenantID); tid != "" && strings.TrimSpace(dbEnt.TenantID) != tid {
		return false, targetActDenyDBNotRegistered, "库未登记，请先在「配置 → 已登记库」中登记 " + dbName, nil
	}
	return true, targetActAllow, "", nil
}

// submitInstanceIDs 提交时要逐条校验审核人的实例。DDL 取全部数据源，其余用主实例。
func submitInstanceIDs(req ticketSubmitReq) []string {
	if req.Type == store.IssueTypeDDL {
		return ddlDataSourceIDs(req.NeedRollback, req.InstanceID)
	}
	id := strings.TrimSpace(req.InstanceID)
	if id == "" {
		return nil
	}
	return []string{id}
}

// reviewerCoversInstances 所选审核人必须在每一条已配置审核人的数据源名单里。
func (h *TicketHandler) reviewerCoversInstances(reviewer, dbName string, ids []string) string {
	reviewer = strings.TrimSpace(reviewer)
	if reviewer == "" || h == nil {
		return ""
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		list := h.resolveApprovers(id, dbName)
		if len(list) == 0 || usernameInList(reviewer, list) {
			continue
		}
		label := id
		if h.Store != nil {
			if inst, _ := h.Store.GetInstance(id); inst != nil && strings.TrimSpace(inst.Name) != "" {
				label = strings.TrimSpace(inst.Name)
			}
		}
		return "审核人须同时在全部数据源的库/实例审核人中（" + label + " 不包含该审核人）"
	}
	return ""
}

func (h *TicketHandler) resolveApprovers(instanceID, dbName string) []string {
	if dbName != "" {
		if dbEnt, _ := h.Store.FindDatabase(instanceID, dbName); dbEnt != nil && len(dbEnt.Approvers) > 0 {
			return store.ExpandApproverNames(dbEnt.Approvers)
		}
	}
	if inst, _ := h.Store.GetInstance(instanceID); inst != nil {
		return store.ExpandApproverNames(inst.Approvers)
	}
	return nil
}

// resolveApproversForIssue 优先用工单已绑定的 database_id 取审批人，避免仅按提交时 instance_id+库名重查错绑。
func (h *TicketHandler) resolveApproversForIssue(it *store.Issue) []string {
	if it == nil {
		return nil
	}
	if dbID := strings.TrimSpace(it.DatabaseID); dbID != "" {
		dbEnt, _ := h.Store.GetDatabase(dbID)
		if dbEnt != nil {
			if len(dbEnt.Approvers) > 0 {
				return store.ExpandApproverNames(dbEnt.Approvers)
			}
			return h.resolveApprovers(dbEnt.InstanceID, dbEnt.DBName)
		}
	}
	return h.resolveApprovers(it.InstanceID, it.DBName)
}

func (h *TicketHandler) ListDBTypes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": []string{"mysql", "postgres", "tidb", "starrocks", "clickhouse"}})
}

func (h *TicketHandler) GetTargets(c *gin.Context) {
	list, err := h.Store.ListInstancesInTenant(effectiveListTenantID(h.Store, c), c.Query("db_type"), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TicketHandler) ListInstances(c *gin.Context) {
	h.GetTargets(c)
}

func (h *TicketHandler) ListInstanceDatabases(c *gin.Context) {
	list, err := h.Store.ListDatabasesInTenant(effectiveListTenantID(h.Store, c), c.Param("instance_id"), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	list = store.FilterVisibleDatabasesByTenant(list, viewerForRequest(h.Store, c))
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TicketHandler) GetTargetApprovers(c *gin.Context) {
	tid := middleware.TenantID(c)
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	memberSet := map[string]struct{}{}
	if members, err := listTenantMemberUsernames(h.Store, tid); err == nil {
		for _, n := range members {
			memberSet[strings.ToLower(n)] = struct{}{}
		}
	}
	list := h.resolveApprovers(c.Query("instance_id"), c.Query("db_name"))
	type item struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	seen := map[string]bool{}
	out := make([]item, 0)
	add := func(name string) {
		for _, n := range store.ExpandApproverNames([]string{name}) {
			key := strings.ToLower(n)
			if _, ok := memberSet[key]; !ok {
				continue
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, item{Type: "user", Name: n})
		}
	}
	if strings.EqualFold(strings.TrimSpace(c.Query("issue_type")), store.IssueTypeMigration) {
		if allowed := h.migrationApproverWhitelist(); len(allowed) > 0 {
			for _, name := range allowed {
				if len(list) == 0 || usernameInList(name, list) {
					add(name)
				}
			}
			c.JSON(http.StatusOK, gin.H{"data": out})
			return
		}
	}
	for _, name := range list {
		add(name)
	}
	if len(out) == 0 {
		if def, _ := h.Store.GetGlobalArg("sync_default_approver"); def != "" {
			add(def)
		}
		if members, err := listTenantMemberUsernames(h.Store, tid); err == nil {
			for _, name := range members {
				add(name)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *TicketHandler) CheckSQL(c *gin.Context) {
	var req ticketCheckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	skipSchema := skipDDLSchemaCheck(req.ShardDB, req.ShardTable)
	c.JSON(http.StatusOK, h.checkTicketSQL(req.SQLContent, req.Type, req.DBType, req.InstanceID, req.DBName, skipSchema))
}

// skipDDLSchemaCheck 分库/分表模式下仅做语法与规范校验，不连库校验对象是否存在。
func skipDDLSchemaCheck(shardDB, shardTable bool) bool {
	return shardDB || shardTable
}

// ddlShardSkipSchema 从 need_rollback JSON 解析分库/分表标记。
func ddlShardSkipSchema(needRollback string) bool {
	raw := strings.TrimSpace(needRollback)
	if raw == "" {
		return false
	}
	var extra struct {
		ShardDB    bool `json:"shard_db"`
		ShardTable bool `json:"shard_table"`
	}
	if json.Unmarshal([]byte(raw), &extra) != nil {
		return false
	}
	return skipDDLSchemaCheck(extra.ShardDB, extra.ShardTable)
}

func (h *TicketHandler) resolveReviewEngine(dbType, instanceID string) string {
	if e := store.NormalizeReviewEngine(dbType); e != "" {
		return e
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return ""
	}
	inst, err := h.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return ""
	}
	return store.NormalizeReviewEngine(inst.DBType)
}

func toCheckRules(list []store.ReviewRule) []sqlcheck.Rule {
	out := make([]sqlcheck.Rule, 0, len(list))
	for _, it := range list {
		out = append(out, sqlcheck.Rule{Name: it.Name, Value: it.Value, Label: it.Label})
	}
	return out
}

// reviewRulesForCheck 仅测试注入审核规则，生产为 nil。
var reviewRulesForCheck func(engine string) []sqlcheck.Rule

// ddlConstraintCheckEnabled 读取总开关。测试可替换，避免依赖元库。
var ddlConstraintCheckEnabled = func(h *TicketHandler) bool {
	if h == nil || h.Store == nil {
		return true
	}
	return h.Store.TicketDDLConstraintCheckEnabled()
}

func (h *TicketHandler) loadReviewRules(engine string) []sqlcheck.Rule {
	if reviewRulesForCheck != nil {
		return reviewRulesForCheck(engine)
	}
	if engine == "" || h == nil || h.Store == nil {
		return nil
	}
	list, err := h.Store.ListReviewRules(engine)
	if err != nil {
		return nil
	}
	return toCheckRules(list)
}

func (h *TicketHandler) checkTicketSQL(sqlContent, issueType, dbType, instanceID, dbName string, skipSchemaCheck bool) sqlcheck.Result {
	engine := h.resolveReviewEngine(dbType, instanceID)
	issueType = strings.ToLower(strings.TrimSpace(issueType))
	if issueType == store.IssueTypeDDL && !ddlConstraintCheckEnabled(h) {
		return skippedDDLConstraintResult(sqlContent)
	}
	var res sqlcheck.Result
	// 分库/分表：模板表名通常不存在于代表库，跳过 goInception 库表存在性审核，仅静态规则。
	if !skipSchemaCheck && engine == "mysql" && (issueType == store.IssueTypeDDL || issueType == store.IssueTypeDML) {
		if out, used := h.checkViaGoInception(sqlContent, instanceID, dbName); used {
			res = out
			sqlcheck.AttachStatementCount(&res, sqlContent)
			return res
		}
	}
	rules := h.loadReviewRules(engine)
	if len(rules) == 0 {
		rules = sqlcheck.LegacyRules(
			h.Store.BoolArg("ddl_forbid_drop_database", true),
			h.Store.BoolArg("dml_require_where", true),
		)
	}
	res = sqlcheck.CheckTicketWithRules(sqlContent, issueType, engine, rules)
	sqlcheck.AttachStatementCount(&res, sqlContent)
	return res
}

func skippedDDLConstraintResult(sqlContent string) sqlcheck.Result {
	if strings.TrimSpace(sqlContent) == "" {
		return sqlcheck.CheckTicketWithRules("", store.IssueTypeDDL, "postgres", nil)
	}
	log.Printf("ddl_constraint_check_skipped sql_len=%d", len(sqlContent))
	res := sqlcheck.Result{
		OK:                     true,
		ConstraintCheckSkipped: true,
		Statements: []sqlcheck.StatementRow{{
			Stage:      "check",
			ErrorLevel: "WARN",
			Message:    "平台已关闭 DDL 约束检查，仅做提交门禁",
			SQL:        sqlContent,
		}},
	}
	sqlcheck.AttachStatementCount(&res, sqlContent)
	return res
}

func stampConstraintCheckSkipped(raw string) string {
	m := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
			log.Printf("ddl_constraint_check_skipped_stamp_failed: %v", err)
			return raw
		}
	}
	m["constraint_check_skipped"] = true
	b, err := json.Marshal(m)
	if err != nil {
		log.Printf("ddl_constraint_check_skipped_stamp_failed: %v", err)
		return raw
	}
	return string(b)
}

func applyCreateDBReview(h *TicketHandler, sqlContent, dbType, instanceID string) sqlcheck.Result {
	engine := ""
	if h != nil {
		engine = h.resolveReviewEngine(dbType, instanceID)
	} else {
		engine = store.NormalizeReviewEngine(dbType)
	}
	var rules []sqlcheck.Rule
	if h != nil {
		rules = h.loadReviewRules(engine)
	}
	res := sqlcheck.CheckCreateDBWithRules(sqlContent, engine, rules)
	sqlcheck.AttachStatementCount(&res, sqlContent)
	return res
}

func (h *TicketHandler) checkViaGoInception(sqlContent, instanceID, dbName string) (sqlcheck.Result, bool) {
	cfg, err := h.Store.GetGoInceptionSettings(h.Secret)
	if err != nil || cfg == nil || !cfg.Ready() {
		return sqlcheck.Result{}, false
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return giCheckFail("goInception 需要先选择实例"), true
	}
	inst, err := h.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return giCheckFail("实例不存在"), true
	}
	host := resolveInstanceHost(inst, "")
	if host == "" {
		return giCheckFail("实例 host 为空"), true
	}
	password := ""
	if strings.TrimSpace(inst.Password) != "" {
		plain, decErr := secret.Decrypt(h.Secret, inst.Password)
		if decErr != nil {
			return giCheckFail("解密实例密码失败: " + decErr.Error()), true
		}
		password = plain
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := goinception.Audit(ctx, goinception.Config{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
	}, sqlContent, host, inst.Port, inst.Username, password, dbName)
	if err != nil {
		return giCheckFail(err.Error()), true
	}
	if res.OK {
		return sqlcheck.Result{
			OK: true,
			Statements: []sqlcheck.StatementRow{{
				Stage: "Audit completed", ErrorLevel: "INFO", Message: "goInception 审核通过", SQL: sqlContent, RuleID: "goInception",
			}},
		}, true
	}
	out := sqlcheck.Result{OK: false}
	for _, e := range res.Errors {
		msg := strings.TrimSpace(e.Message)
		if msg == "" {
			msg = "审核未通过"
		}
		out.Statements = append(out.Statements, sqlcheck.StatementRow{
			Stage: "goInception", ErrorLevel: "ERROR", Message: msg, SQL: e.SQL, RuleID: e.RuleID,
		})
		out.Errors = append(out.Errors, map[string]any{"rule_id": e.RuleID, "message": msg, "sql": e.SQL})
	}
	if len(out.Errors) == 0 {
		out.Statements = append(out.Statements, sqlcheck.StatementRow{
			Stage: "goInception", ErrorLevel: "ERROR", Message: "审核未通过", SQL: sqlContent, RuleID: "goInception",
		})
		out.Errors = append(out.Errors, map[string]any{"rule_id": "goInception", "message": "审核未通过"})
	}
	return out, true
}

func giCheckFail(msg string) sqlcheck.Result {
	return sqlcheck.Result{
		OK: false,
		Statements: []sqlcheck.StatementRow{{
			Stage: "goInception", ErrorLevel: "ERROR", Message: msg, RuleID: "goInception",
		}},
		Errors: []map[string]any{{"rule_id": "goInception", "message": msg}},
	}
}

func (h *TicketHandler) Submit(c *gin.Context) {
	var req ticketSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if !store.ValidIssueType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的工单类型: " + req.Type})
		return
	}
	if req.Type == store.IssueTypeResourceApply {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请使用 POST /resource-apply 提交平台资源申请"})
		return
	}
	username := middleware.Username(c)
	if req.Type == store.IssueTypeDQL {
		if err := h.prepareDQLSubmit(c, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	} else if req.Type == store.IssueTypeDDL {
		ids := ddlDataSourceIDs(req.NeedRollback, req.InstanceID)
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请选择数据源实例"})
			return
		}
		for _, id := range ids {
			ok, _, msg, err := h.canActOnTarget(middleware.TenantID(c), id, req.DBName, username)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"message": h.decorateTargetDeny(id, msg)})
				return
			}
		}
	} else {
		ok, _, msg, err := h.canActOnTarget(middleware.TenantID(c), req.InstanceID, req.DBName, username)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}
	}
	reviewer, err := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.reviewer())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if msg := h.reviewerCoversInstances(reviewer, req.DBName, submitInstanceIDs(req)); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	skipSchema := req.Type == store.IssueTypeDDL && ddlShardSkipSchema(req.NeedRollback)
	res := h.checkTicketSQL(req.SQLContent, req.Type, req.DBType, req.InstanceID, req.DBName, skipSchema)
	if !res.OK {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SQL 校验未通过", "details": res})
		return
	}
	if req.Type == store.IssueTypeDDL && res.ConstraintCheckSkipped {
		req.NeedRollback = stampConstraintCheckSkipped(req.NeedRollback)
		log.Printf("ddl_constraint_check_skipped_submit instance_id=%s db=%s", req.InstanceID, req.DBName)
	}
	if req.Type == store.IssueTypeDQL && req.NeedRollback == "" {
		extra, err := buildDQLSubmitExtra(&req, reviewer)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		req.NeedRollback = extra
	}
	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		// 兼容 need_rollback 内嵌 domain
		var extra struct {
			Domain string `json:"domain"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(req.NeedRollback)), &extra) == nil {
			domain = strings.TrimSpace(extra.Domain)
		}
	}
	if req.Type == store.IssueTypeDDL {
		ids := ddlDataSourceIDs(req.NeedRollback, req.InstanceID)
		for _, id := range ids {
			dom := ddlDomainForInstance(req.NeedRollback, id, domain)
			if strings.TrimSpace(dom) == "" {
				continue
			}
			inst, ierr := h.Store.GetInstance(id)
			if ierr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": ierr.Error()})
				return
			}
			if inst == nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "数据源域名不属于所选实例登记的域名"})
				return
			}
			if err := ensureTicketDomainVisible(h.Store, c, inst, dom); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
				return
			}
		}
	} else if domain != "" && strings.TrimSpace(req.InstanceID) != "" {
		inst, ierr := h.Store.GetInstance(req.InstanceID)
		if ierr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": ierr.Error()})
			return
		}
		if inst == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "数据源域名不属于所选实例登记的域名"})
			return
		}
		if err := ensureTicketDomainVisible(h.Store, c, inst, domain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if strings.TrimSpace(req.DBName) != "" {
			dbEnt, derr := h.Store.FindDatabase(req.InstanceID, req.DBName)
			if derr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": derr.Error()})
				return
			}
			if dbEnt != nil && len(dbEnt.Domains) > 0 && !databaseContainsDomain(dbEnt, domain) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "所选数据库未绑定该域名"})
				return
			}
		}
	}
	issue := &store.Issue{
		IssueType:    req.Type,
		Type:         req.Type,
		InstanceID:   req.InstanceID,
		Domain:       domain,
		DatabaseID:   req.DatabaseID,
		DBName:       req.DBName,
		SQLContent:   req.SQLContent,
		Description:  req.Description,
		Status:       store.StatusPending,
		Submitter:    username,
		AssignedTo:   reviewer,
		NeedRollback: req.NeedRollback,
		ScheduledAt:  req.ScheduledAt,
	}
	stampTenantID(c, func(tid string) { issue.TenantID = tid })
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": issue})
}

func (h *TicketHandler) prepareDQLSubmit(c *gin.Context, req *ticketSubmitReq) error {
	if strings.TrimSpace(req.SQLContent) == "" {
		return errors.New("SQL语句不能为空")
	}
	dba := strings.TrimSpace(req.DBAAssigned)
	sec := strings.TrimSpace(req.SecAssigned)
	if (dba == "") != (sec == "") {
		return errors.New("请同步设置 DBA审批人 和 安全审批人")
	}
	if req.NeedSensitive && (dba == "" || sec == "") {
		return errors.New("开启原文导出敏感字段需设置DBA审批人和安全审批人")
	}
	if req.RowCount > 500 && (dba == "" || sec == "") {
		return errors.New("导出数据超过500条需设置DBA审批人和安全审批人")
	}
	var rec *store.Database
	if strings.TrimSpace(req.DatabaseID) != "" {
		it, err := h.Store.GetDatabaseInTenant(req.DatabaseID, effectiveListTenantID(h.Store, c))
		if err != nil {
			return err
		}
		rec = it
	}
	if rec == nil && req.InstanceID != "" && req.DBName != "" {
		it, err := h.Store.FindDatabase(req.InstanceID, req.DBName)
		if err != nil {
			return err
		}
		rec = it
	}
	if rec == nil {
		return errors.New("库未登记，请先在「配置 → 已登记库」中登记")
	}
	viewer := h.Store.ViewerOfInTenant(middleware.Username(c), middleware.Role(c), rec.TenantID)
	if !rec.VisibleTo(viewer) {
		return errors.New("无权限访问该库")
	}
	req.DatabaseID = rec.ID
	req.InstanceID = rec.InstanceID
	req.DBName = rec.DBName
	return nil
}

// fillIssueListMeta 补列表展示用的环境、数据库类型：优先实例，其次 sql_content。
func fillIssueListMeta(st *store.DB, it *store.Issue, instCache map[string]*store.Instance) {
	if it == nil {
		return
	}
	if id := strings.TrimSpace(it.InstanceID); id != "" && st != nil {
		inst, ok := instCache[id]
		if !ok {
			inst, _ = st.GetInstance(id)
			instCache[id] = inst
		}
		if inst != nil {
			it.Env = strings.TrimSpace(inst.IDC)
			it.DBType = strings.TrimSpace(inst.DBType)
		}
	}
	raw := strings.TrimSpace(it.SQLContent)
	if raw == "" || raw[0] != '{' {
		return
	}
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return
	}
	if it.Env == "" {
		for _, key := range []string{"idc_scope", "env", "idc"} {
			if s, ok := obj[key].(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					it.Env = s
					break
				}
			}
		}
	}
	if it.DBType == "" {
		for _, key := range []string{"db_type", "dbType", "engine"} {
			if s, ok := obj[key].(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					it.DBType = s
					break
				}
			}
		}
	}
}

func (h *TicketHandler) List(c *gin.Context) {
	role := middleware.Role(c)
	user := middleware.Username(c)
	tid := effectiveListTenantID(h.Store, c)
	list, err := h.Store.ListIssuesInTenant(tid, c.Query("type"), c.Query("status"), c.Query("q"), user, middleware.IsPrivileged(role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Issue{}
	}
	// 平台资源申请：平台管理员/白名单可看全平台该类工单（不影响其它类型）
	typeQ := c.Query("type")
	if h.isPlatformResourceApplyReviewer(c) && (typeQ == "" || typeQ == store.IssueTypeResourceApply) {
		extra, e2 := h.Store.ListResourceApplyAcrossTenants(c.Query("status"), c.Query("q"))
		if e2 == nil {
			seen := map[string]struct{}{}
			for _, it := range list {
				seen[it.ID] = struct{}{}
			}
			for _, it := range extra {
				if tid != "" && strings.TrimSpace(it.TenantID) == tid {
					continue
				}
				if _, ok := seen[it.ID]; ok {
					continue
				}
				list = append(list, it)
			}
		}
	}
	names := tenantNameMap(h.Store)
	instCache := map[string]*store.Instance{}
	for i := range list {
		if n := names[list[i].TenantID]; n != "" {
			list[i].TenantName = n
		}
		fillIssueListMeta(h.Store, &list[i], instCache)
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *TicketHandler) GetDetail(c *gin.Context) {
	it, err := h.loadIssueForActor(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	it = h.maybeFinishApprovedCreateDB(c.Request.Context(), it)
	if n := tenantNameMap(h.Store)[it.TenantID]; n != "" {
		it.TenantName = n
	}
	canApprove := it.Status == store.StatusPending && h.actorCanApproveIssue(c, it)
	canTransfer := it.Status == store.StatusPending && strings.EqualFold(it.Submitter, middleware.Username(c))
	c.JSON(http.StatusOK, gin.H{
		"data":         it,
		"can_approve":  canApprove,
		"can_transfer": canTransfer,
	})
}

func (h *TicketHandler) Approve(c *gin.Context) {
	it, err := h.loadIssueForActor(c, c.Param("id"))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.Status != store.StatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅待审批工单可批准"})
		return
	}
	user := middleware.Username(c)
	priv := middleware.IsPrivileged(middleware.Role(c))
	if !h.actorCanApproveIssue(c, it) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅指定审批人、超级审核白名单或管理员可批准"})
		return
	}
	viaWhitelist := !priv && !strings.EqualFold(it.AssignedTo, user) && h.Store.IsGlobalApprover(user)
	if viaWhitelist {
		hist := store.AppendAssigneeTransferHistory(it.AssigneeTransferHistory, store.FormatWhitelistApprovalLine(user, "approved"))
		_ = h.Store.UpdateIssueAssignee(it.ID, it.AssignedTo, hist)
		it.AssigneeTransferHistory = hist
	}
	// DQL 多级审批：指定审批人 → DBA → 安全，全部通过后才 approved
	if it.IssueType == store.IssueTypeDQL {
		exBefore, _ := parseDQLApprovalExtra(it.NeedRollback)
		if exBefore != nil {
			exBefore.normalizePhase(it.AssignedTo, it.Status)
		}
		prevPhase := ""
		if exBefore != nil {
			prevPhase = exBefore.Phase
		}
		advanced, aerr := advanceDQLApproval(it, user)
		if aerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
			return
		}
		if advanced {
			ex, _ := parseDQLApprovalExtra(it.NeedRollback)
			nextPhase := ""
			if ex != nil {
				nextPhase = ex.Phase
			}
			hist := store.AppendAssigneeTransferHistory(
				it.AssigneeTransferHistory,
				fmt.Sprintf("dql_%s_approved by=%s next=%s(%s) at=%s",
					prevPhase, user, nextPhase, it.AssignedTo, time.Now().UTC().Format(time.RFC3339)),
			)
			if err := h.Store.UpdateIssueAssigneeAndRollback(it.ID, it.AssignedTo, hist, it.NeedRollback); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			it.AssigneeTransferHistory = hist
			NotifyIssueAssigneeChangedAsync(h.Store, h.Secret, it)
			c.JSON(http.StatusOK, gin.H{
				"data":    it,
				"message": fmt.Sprintf("%s已通过，等待%s（%s）", dqlPhaseLabel(prevPhase), dqlPhaseLabel(nextPhase), it.AssignedTo),
			})
			return
		}
		if strings.TrimSpace(it.NeedRollback) != "" {
			_ = h.Store.UpdateIssueNeedRollback(it.ID, it.NeedRollback)
		}
	}
	if err := h.Store.UpdateIssueStatus(it.ID, store.StatusApproved, ""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it.Status = store.StatusApproved
	if err := h.syncArchiveAfterIssue(it, store.StatusApproved); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "工单已通过，但同步归档任务失败: " + err.Error()})
		return
	}
	if err := h.syncFlashbackAfterIssue(c.Request.Context(), it, store.StatusApproved); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "工单已通过，但启动闪回任务失败: " + err.Error()})
		return
	}
	if err := h.syncCDCAfterIssue(it, store.StatusApproved); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "工单已通过，但启动 CDC 任务失败: " + err.Error()})
		return
	}
	if err := h.syncMigrationAfterIssue(it, store.StatusApproved); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "工单已通过，但启动迁移任务失败: " + err.Error()})
		return
	}
	if ticketexec.ShouldAutoRun(it.IssueType) && !scheduledInFuture(it.ScheduledAt) {
		if err := h.beginAsyncAutoExec(it); err != nil {
			NotifyIssueAwaitExecAsync(h.Store, h.Secret, it)
			c.JSON(http.StatusOK, gin.H{"data": it, "message": "已通过，启动后台执行失败: " + err.Error()})
			return
		}
		if latest, err := h.Store.GetIssue(it.ID); err == nil && latest != nil {
			it = latest
		}
		c.JSON(http.StatusOK, gin.H{"data": it, "message": "已通过，正在后台执行"})
		return
	}
	if latest, err := h.Store.GetIssue(it.ID); err == nil && latest != nil {
		it = latest
	}
	if it.Status == store.StatusApproved {
		NotifyIssueAwaitExecAsync(h.Store, h.Secret, it)
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *TicketHandler) Reject(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	it, err := h.loadIssueForActor(c, c.Param("id"))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.Status != store.StatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅待审批工单可驳回"})
		return
	}
	user := middleware.Username(c)
	priv := middleware.IsPrivileged(middleware.Role(c))
	if !h.actorCanApproveIssue(c, it) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅指定审批人、超级审核白名单或管理员可驳回"})
		return
	}
	viaWhitelist := !priv && !strings.EqualFold(it.AssignedTo, user) && h.Store.IsGlobalApprover(user)
	if viaWhitelist {
		hist := store.AppendAssigneeTransferHistory(it.AssigneeTransferHistory, store.FormatWhitelistApprovalLine(user, "rejected"))
		_ = h.Store.UpdateIssueAssignee(it.ID, it.AssignedTo, hist)
	}
	if it.IssueType == store.IssueTypeDQL {
		if err := markDQLRejected(it, user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if strings.TrimSpace(it.NeedRollback) != "" {
			if err := h.Store.UpdateIssueNeedRollback(it.ID, it.NeedRollback); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
		}
	}
	if err := h.Store.UpdateIssueStatus(it.ID, store.StatusRejected, req.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.syncArchiveAfterIssue(it, store.StatusRejected)
	_ = h.syncFlashbackAfterIssue(c.Request.Context(), it, store.StatusRejected)
	_ = h.syncCDCAfterIssue(it, store.StatusRejected)
	_ = h.syncMigrationAfterIssue(it, store.StatusRejected)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TransferAssignee PUT /tickets/:id/assignee — 仅提交者在 pending 时可更换审核人。
func (h *TicketHandler) TransferAssignee(c *gin.Context) {
	var req struct {
		AssignedTo string `json:"assigned_to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	newAssignee := strings.TrimSpace(req.AssignedTo)
	if newAssignee == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "assigned_to 必填"})
		return
	}
	it, err := h.Store.GetIssueInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.Status != store.StatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅待审批状态可转移审核人"})
		return
	}
	user := middleware.Username(c)
	if !strings.EqualFold(it.Submitter, user) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅提交者可转移审核人"})
		return
	}
	if strings.EqualFold(it.AssignedTo, newAssignee) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新审核人与当前审核人相同"})
		return
	}
	if errMsg := h.validateTransferAssignee(it, newAssignee); errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": errMsg})
		return
	}
	hist := store.AppendAssigneeTransferHistory(
		it.AssigneeTransferHistory,
		store.FormatAssigneeTransferLine(it.AssignedTo, newAssignee, user),
	)
	if err := syncDQLExtraOnTransfer(it, newAssignee); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.AssignedTo = newAssignee
	if it.IssueType == store.IssueTypeDQL && strings.TrimSpace(it.NeedRollback) != "" {
		if err := h.Store.UpdateIssueAssigneeAndRollback(it.ID, newAssignee, hist, it.NeedRollback); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	} else if err := h.Store.UpdateIssueAssignee(it.ID, newAssignee, hist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it.AssigneeTransferHistory = hist
	h.syncMigrationAssigneeAfterTransfer(it, newAssignee)
	NotifyIssueAssigneeChangedAsync(h.Store, h.Secret, it)
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *TicketHandler) validateTransferAssignee(it *store.Issue, newAssignee string) string {
	if it == nil {
		return "工单不存在"
	}
	if strings.EqualFold(strings.TrimSpace(it.Submitter), newAssignee) {
		return "不能将审核人转给提交人本人"
	}
	tid := strings.TrimSpace(it.TenantID)
	if tid == "" {
		tid = store.DefaultTenantID
	}
	if err := requireTenantMemberUsernames(h.Store, tid, []string{newAssignee}); err != nil {
		return err.Error()
	}
	switch it.IssueType {
	case store.IssueTypeCreateDB:
		return ""
	case store.IssueTypeMigration:
		if !h.migrationApproverAllowed(newAssignee) {
			return "审核人不在迁移审批人列表"
		}
		approvers := h.resolveApproversForIssue(it)
		if len(approvers) > 0 && !usernameInList(newAssignee, approvers) {
			return "审核人不在审批人列表"
		}
		return ""
	default:
		approvers := h.resolveApproversForIssue(it)
		if len(approvers) > 0 && !usernameInList(newAssignee, approvers) {
			return "审核人不在审批人列表"
		}
		return ""
	}
}

// migrationApproverWhitelist 返回 migration_approver_user_ids；未配置时为 nil（不限制）。
func (h *TicketHandler) migrationApproverWhitelist() []string {
	raw, _ := h.Store.GetGlobalArg("migration_approver_user_ids")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return store.ParseApproverWhitelist(raw)
}

func (h *TicketHandler) migrationApproverAllowed(name string) bool {
	allowed := h.migrationApproverWhitelist()
	return len(allowed) == 0 || usernameInList(name, allowed)
}

func (h *TicketHandler) syncMigrationAssigneeAfterTransfer(it *store.Issue, newAssignee string) {
	if h == nil || h.Store == nil || it == nil || it.IssueType != store.IssueTypeMigration {
		return
	}
	m := regexp.MustCompile(`(?i)^migration\s+([0-9a-fA-F-]{36})`).FindStringSubmatch(strings.TrimSpace(it.SQLContent))
	if len(m) < 2 {
		return
	}
	t, err := h.Store.GetMigration(m[1])
	if err != nil || t == nil {
		return
	}
	t.AssignedApprover = newAssignee
	t.AssignedApproverLogin = newAssignee
	_ = h.Store.UpdateMigration(t)
}

func (h *TicketHandler) Cancel(c *gin.Context) {
	it, err := h.Store.GetIssueInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if it.Submitter != middleware.Username(c) && !middleware.IsPrivileged(middleware.Role(c)) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅提交人或管理员可撤销"})
		return
	}
	if err := h.cancelIssue(c.Request.Context(), it, ""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *TicketHandler) cancelIssue(ctx context.Context, it *store.Issue, reason string) error {
	if h == nil || h.Store == nil || it == nil {
		return nil
	}
	if err := h.Store.UpdateIssueStatus(it.ID, store.StatusCanceled, reason); err != nil {
		return err
	}
	_ = h.syncArchiveAfterIssue(it, store.StatusCanceled)
	_ = h.syncFlashbackAfterIssue(ctx, it, store.StatusCanceled)
	_ = h.syncCDCAfterIssue(it, store.StatusCanceled)
	_ = h.syncMigrationAfterIssue(it, store.StatusCanceled)
	return nil
}

func (h *TicketHandler) Execute(c *gin.Context) {
	it, err := h.Store.GetIssueInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "工单不存在"})
		return
	}
	if ticketexec.IsExecuting(it.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "工单正在后台执行中，请稍后刷新"})
		return
	}
	if !ticketexec.CanRetry(it.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅已通过或执行失败的工单可执行"})
		return
	}
	if it.IssueType == store.IssueTypeFlashback {
		if err := h.syncFlashbackAfterIssue(c.Request.Context(), it, store.StatusApproved); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已触发闪回执行"})
		return
	}
	if it.IssueType == store.IssueTypeCDC {
		if err := h.syncCDCAfterIssue(it, store.StatusApproved); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已触发 CDC 执行"})
		return
	}
	if it.IssueType == store.IssueTypeMigration {
		if err := h.syncMigrationAfterIssue(it, store.StatusApproved); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已触发数据迁移执行"})
		return
	}
	if !ticketexec.ShouldAutoRun(it.IssueType) {
		if err := h.Store.UpdateIssueStatus(it.ID, store.StatusExecuted, ""); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已标记执行"})
		return
	}
	if err := h.beginAsyncAutoExec(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error(), "data": it})
		return
	}
	if latest, err := h.Store.GetIssue(it.ID); err == nil && latest != nil {
		it = latest
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": it, "message": "已开始后台执行"})
}

// beginAsyncAutoExec 将 DDL/DML/建库执行放到后台，避免 HTTP WriteTimeout 切断大 SQL。
func (h *TicketHandler) beginAsyncAutoExec(it *store.Issue) error {
	if h == nil || h.Store == nil || it == nil {
		return errors.New("工单无效")
	}
	if ticketexec.IsExecuting(it.Status) {
		return errors.New("工单正在执行中")
	}
	if err := h.Store.UpdateIssueStatus(it.ID, store.StatusRunning, ""); err != nil {
		return err
	}
	it.Status = store.StatusRunning
	id := it.ID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("ticket async exec panic %s: %v", id, r)
				h.failAsyncIssue(id, fmt.Sprintf("执行异常: %v", r))
			}
		}()
		latest, err := h.Store.GetIssue(id)
		if err != nil || latest == nil {
			log.Printf("ticket async exec load %s: %v", id, err)
			msg := "加载工单失败"
			if err != nil {
				msg = "加载工单失败: " + err.Error()
			}
			h.failAsyncIssue(id, msg)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), asyncExecMaxWait(h.Store, latest))
		defer cancel()
		if err := h.executeTicket(ctx, latest); err != nil {
			log.Printf("ticket async exec %s: %v", id, err)
			h.failAsyncIssue(id, err.Error())
			return
		}
		if cur, e := h.Store.GetIssue(id); e == nil && cur != nil {
			if err := h.finishCreateDBAfterExec(ctx, cur); err != nil {
				log.Printf("ticket async createdb account %s: %v", id, err)
			}
		}
	}()
	return nil
}

// asyncExecMaxWait 后台执行总墙钟；DDL 多数据源串行，含长耗时语句时按数据源数放宽。
func asyncExecMaxWait(st *store.DB, it *store.Issue) time.Duration {
	targets := 1
	if it.IssueType == store.IssueTypeDDL {
		targets = len(ddlDataSourceIDs(it.NeedRollback, it.InstanceID))
	}
	return ticketexec.AsyncMaxWait(it.SQLContent, targets, st.TicketDDLLongStmtTimeout())
}

// failAsyncIssue 后台失败时写入 failed + exec_result，避免长期卡在 running 且无结果。
func (h *TicketHandler) failAsyncIssue(id, msg string) {
	if h == nil || h.Store == nil || strings.TrimSpace(id) == "" {
		return
	}
	cur, err := h.Store.GetIssue(id)
	if err != nil || cur == nil {
		_ = h.Store.UpdateIssueStatus(id, store.StatusFailed, msg)
		return
	}
	// 已由 persistExec 落成终态则不再覆盖
	if !ticketexec.IsExecuting(cur.Status) {
		return
	}
	res := ticketexec.FailBeforeExec(msg, ticketexec.Record{
		InstanceID: cur.InstanceID, DB: cur.DBName, SQL: "(后台执行)",
	})
	res.Summary = "后台执行异常终止"
	res.ChangeID = prevChangeID(cur.ExecuteResult)
	if err := h.Store.UpdateIssueExec(id, store.StatusFailed, ticketexec.ToJSON(res)); err != nil {
		log.Printf("ticket async fail persist %s: %v", id, err)
		_ = h.Store.UpdateIssueStatus(id, store.StatusFailed, msg)
		return
	}
	cur.Status = store.StatusFailed
	cur.ExecuteResult = ticketexec.ToJSON(res)
	NotifyIssueExecFailedAsync(h.Store, h.Secret, cur, msg)
}

func (h *TicketHandler) syncArchiveAfterIssue(it *store.Issue, issueStatus string) error {
	if it == nil || it.IssueType != store.IssueTypeArchive {
		return nil
	}
	t, err := h.Store.GetArchiveByIssueID(it.ID)
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	switch issueStatus {
	case store.StatusRejected:
		return h.Store.UpdateArchiveState(t.ID, store.StatusRejected, store.StatusRejected)
	case store.StatusCanceled:
		return h.Store.UpdateArchiveState(t.ID, store.StatusCanceled, store.StatusCanceled)
	case store.StatusApproved:
		if store.ArchiveRunMode(t) == "scheduled" {
			return h.Store.UpdateArchiveState(t.ID, "scheduled", store.StatusApproved)
		}
		if err := h.Store.UpdateArchiveState(t.ID, store.StatusApproved, store.StatusApproved); err != nil {
			return err
		}
		if h.Archive != nil {
			return h.Archive.Start(t.ID)
		}
		return nil
	default:
		return nil
	}
}

func (h *TicketHandler) fbEngine() *flashback.FlashbackImpl {
	if h != nil && h.Flashback != nil {
		return h.Flashback
	}
	return flashback.New()
}

func (h *TicketHandler) syncFlashbackAfterIssue(ctx context.Context, it *store.Issue, issueStatus string) error {
	if it == nil || it.IssueType != store.IssueTypeFlashback {
		return nil
	}
	return h.fbEngine().SyncAfterIssue(ctx, it.ID, issueStatus)
}

func (h *TicketHandler) syncCDCAfterIssue(it *store.Issue, issueStatus string) error {
	if it == nil || it.IssueType != store.IssueTypeCDC {
		return nil
	}
	list, err := h.Store.ListCDCByIssueID(it.ID)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}
	var last error
	for i := range list {
		t := &list[i]
		switch issueStatus {
		case store.StatusRejected:
			if err := h.Store.UpdateCDCStatus(t.ID, store.StatusRejected, store.StatusRejected); err != nil {
				last = err
			}
		case store.StatusCanceled:
			if err := h.Store.UpdateCDCStatus(t.ID, store.StatusCanceled, store.StatusCanceled); err != nil {
				last = err
			}
		case store.StatusApproved:
			next := t.Status
			if next == store.StatusPending || next == store.StatusRejected || next == store.StatusCanceled {
				next = store.StatusInit
				if t.TaskType == "add_table" || t.TaskType == "alter_schema" || t.TaskType == "remove_table" || t.TaskType == "delete_task" {
					next = store.StatusRunning
				}
			}
			if err := h.Store.UpdateCDCStatus(t.ID, next, store.StatusApproved); err != nil {
				last = err
				continue
			}
			if h.CDC != nil && (t.TaskType == "init" || t.TaskType == "add_table" || t.TaskType == "alter_schema" || t.TaskType == "remove_table" || t.TaskType == "delete_task") {
				_ = h.Store.InsertCDCLog(&store.CDCSyncLog{
					TaskID: t.ID, Level: "INFO", Step: "orchestrate",
					Message: "approval passed, start execution",
				})
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				_ = h.CDC.Apply(ctx, t.ID, "")
				cancel()
			}
		}
	}
	return last
}

func (h *TicketHandler) migEngine() *dbmig.Engine {
	if h != nil && h.Migration != nil {
		return h.Migration
	}
	if h == nil {
		return nil
	}
	return dbmig.New(h.Store, h.Secret)
}

func (h *TicketHandler) syncMigrationAfterIssue(it *store.Issue, issueStatus string) error {
	if it == nil || it.IssueType != store.IssueTypeMigration {
		return nil
	}
	t, err := h.Store.GetMigrationByIssueID(it.ID)
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	switch issueStatus {
	case store.StatusRejected:
		t.ApprovalStatus = store.StatusRejected
		t.Status = store.StatusCanceled
		t.ErrorMessage = "审批已驳回"
		_ = h.Store.AddMigrationLog(t.ID, "WARN", "审批已驳回")
		return h.Store.UpdateMigration(t)
	case store.StatusCanceled:
		t.ApprovalStatus = store.StatusCanceled
		t.Status = store.StatusCanceled
		t.ErrorMessage = "审批已撤销"
		_ = h.Store.AddMigrationLog(t.ID, "WARN", "审批已撤销")
		return h.Store.UpdateMigration(t)
	case store.StatusApproved:
		t.ApprovalStatus = store.StatusApproved
		t.ApprovedBy = it.AssignedTo
		if t.StartMode == "scheduled" {
			t.Status = store.StatusScheduled
			if err := h.Store.UpdateMigration(t); err != nil {
				return err
			}
			_ = h.Store.AddMigrationLog(t.ID, "INFO", "审批已通过，等待定时启动")
			return nil
		}
		if err := h.Store.UpdateMigration(t); err != nil {
			return err
		}
		_ = h.Store.AddMigrationLog(t.ID, "INFO", "审批已通过，开始执行")
		eng := h.migEngine()
		if eng == nil {
			return nil
		}
		return eng.Start(t.ID)
	default:
		return nil
	}
}

func (h *TicketHandler) executeTicket(ctx context.Context, it *store.Issue) error {
	if it == nil {
		return errors.New("工单不存在")
	}
	if it.IssueType == store.IssueTypeCreateDB {
		return h.executeCreateDBTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeDDL {
		return h.executeDDLTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeRedis {
		return h.executeRedisTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeHA {
		return h.executeHATicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeK8sChange {
		return h.executeK8sChangeTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeDnsChange {
		if h.DNS == nil {
			return h.persistExec(it, &ticketexec.Result{Success: false, Message: "DNS 管理未启用"})
		}
		return h.persistExec(it, h.DNS.ExecuteDnsChangeTicket(ctx, it))
	}
	if it.IssueType == store.IssueTypeSrCluster {
		return h.executeSrClusterTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeDbDeploy {
		return h.executeDbDeployTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeResourceApply {
		return h.executeResourceApplyTicket(ctx, it)
	}
	if it.IssueType == store.IssueTypeBackupRestore {
		return h.executeBackupRestoreTicket(ctx, it)
	}
	inst, err := h.Store.GetInstanceUsableInTenant(it.InstanceID, it.TenantID)
	if err != nil {
		return err
	}
	if inst == nil {
		res := ticketexec.FailBeforeExec("工单实例不存在: "+it.InstanceID, ticketexec.Record{
			InstanceID: it.InstanceID, DB: it.DBName,
		})
		return h.persistExec(it, res)
	}
	check := h.checkTicketSQL(it.SQLContent, it.IssueType, inst.DBType, it.InstanceID, it.DBName, false)
	if !check.OK {
		return h.persistExec(it, reviewGateResult(check))
	}
	t, err := h.execTarget(inst, it.DBName, it.Domain)
	if err != nil {
		res := ticketexec.FailBeforeExec(err.Error(), ticketexec.Record{
			InstanceID: inst.ID, InstanceName: inst.Name, Host: resolveInstanceHost(inst, it.Domain), DB: it.DBName, Port: inst.Port,
		})
		return h.persistExec(it, res)
	}
	res := ticketexec.Run(ctx, it.IssueType, it.SQLContent, t)
	return h.persistExec(it, res)
}

// ddlDataSourceIDs 从 need_rollback JSON 解析多选实例；空则回退到主 instance_id。
func ddlDataSourceIDs(needRollback, fallbackInstanceID string) []string {
	var ids []string
	raw := strings.TrimSpace(needRollback)
	if raw != "" {
		var extra struct {
			DataSourceIDs []string `json:"data_source_ids"`
			DataSources   []struct {
				InstanceID string `json:"instance_id"`
				Domain     string `json:"domain"`
			} `json:"data_sources"`
		}
		if json.Unmarshal([]byte(raw), &extra) == nil {
			ids = append(ids, extra.DataSourceIDs...)
			for _, ds := range extra.DataSources {
				ids = append(ids, ds.InstanceID)
			}
		}
	}
	out := uniqueTrimmedIDs(ids)
	if len(out) == 0 {
		fb := strings.TrimSpace(fallbackInstanceID)
		if fb != "" {
			return []string{fb}
		}
		return nil
	}
	return out
}

// ddlDomainForInstance 从 need_rollback.data_sources 取该实例对应域名；无则回退 issue.Domain。
func ddlDomainForInstance(needRollback, instanceID, fallbackDomain string) string {
	raw := strings.TrimSpace(needRollback)
	if raw != "" {
		var extra struct {
			Domain      string `json:"domain"`
			DataSources []struct {
				InstanceID string `json:"instance_id"`
				Domain     string `json:"domain"`
			} `json:"data_sources"`
		}
		if json.Unmarshal([]byte(raw), &extra) == nil {
			id := strings.TrimSpace(instanceID)
			for _, ds := range extra.DataSources {
				if strings.TrimSpace(ds.InstanceID) == id && strings.TrimSpace(ds.Domain) != "" {
					return strings.TrimSpace(ds.Domain)
				}
			}
			if d := strings.TrimSpace(extra.Domain); d != "" {
				return d
			}
		}
	}
	return strings.TrimSpace(fallbackDomain)
}

func uniqueTrimmedIDs(ids []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (h *TicketHandler) decorateTargetDeny(instanceID, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "目标校验失败"
	}
	inst, err := h.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return msg + "（实例 " + instanceID + "）"
	}
	name := strings.TrimSpace(inst.Name)
	if name == "" {
		name = instanceID
	}
	return msg + "（实例 " + name + "）"
}

func (h *TicketHandler) executeDDLTicket(ctx context.Context, it *store.Issue) error {
	ids := ddlDataSourceIDs(it.NeedRollback, it.InstanceID)
	if len(ids) == 0 {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: "工单未指定实例"})
	}
	stmtN := len(sqlcheck.SplitSQL(it.SQLContent))
	merged := &ticketexec.Result{
		StartedAt:      time.Now().UTC().Format(time.RFC3339),
		Success:        true,
		StatementCount: stmtN,
	}
	var prev *ticketexec.Result
	if len(ids) > 1 {
		prev = ticketexec.Parse(it.ExecuteResult)
	}
	var skipped []string
	for _, id := range ids {
		inst, err := h.Store.GetInstanceUsableInTenant(id, it.TenantID)
		if err != nil {
			return err
		}
		if inst == nil {
			merged.Success = false
			merged.Message = "实例不存在: " + id
			mergeExecResult(merged, ticketexec.FailBeforeExec(merged.Message, ticketexec.Record{
				InstanceID: id, DB: it.DBName,
			}))
			break
		}
		// 多数据源重试：上次已全部成功的数据源不再执行
		if done := ddlDoneSource(prev, id, it.DBName, stmtN); done != nil {
			mergeExecResult(merged, done)
			skipped = append(skipped, inst.Name)
			continue
		}
		// 执行前再确认库仍已登记（与提交硬约束一致）
		dbEnt, err := h.Store.FindDatabase(id, it.DBName)
		if err != nil {
			return err
		}
		if dbEnt == nil {
			merged.Success = false
			merged.Message = h.decorateTargetDeny(id, "库未登记，请先在「配置 → 已登记库」中登记 "+it.DBName)
			merged.Records = append(merged.Records, ticketexec.Record{
				InstanceID: id, InstanceName: inst.Name, Host: inst.Host, DB: it.DBName,
				Status: "failed", ErrorMessage: merged.Message, Order: len(merged.Records) + 1,
			})
			break
		}
		skipSchema := ddlShardSkipSchema(it.NeedRollback)
		check := h.checkTicketSQL(it.SQLContent, it.IssueType, inst.DBType, id, it.DBName, skipSchema)
		if !check.OK {
			gate := reviewGateResult(check)
			tagExecRecords(gate, id, inst.Name)
			mergeExecResult(merged, gate)
			merged.Success = false
			if merged.Message == "" && gate != nil {
				merged.Message = gate.Message
			}
			break
		}
		domain := ddlDomainForInstance(it.NeedRollback, id, it.Domain)
		t, err := h.execTarget(inst, it.DBName, domain)
		if err != nil {
			merged.Success = false
			merged.Message = err.Error()
			merged.Records = append(merged.Records, ticketexec.Record{
				InstanceID: id, InstanceName: inst.Name, Host: resolveInstanceHost(inst, domain), DB: it.DBName,
				Status: "failed", ErrorMessage: err.Error(), Order: len(merged.Records) + 1,
			})
			break
		}
		res := ticketexec.Run(ctx, it.IssueType, it.SQLContent, t)
		tagExecRecords(res, id, inst.Name)
		mergeExecResult(merged, res)
		if res == nil || !res.Success {
			if merged.Message == "" && res != nil {
				merged.Message = res.Message
			}
			merged.Success = false
			break
		}
	}
	merged.EndedAt = time.Now().UTC().Format(time.RFC3339)
	if merged.Success && merged.Summary == "" {
		merged.Summary = "DDL 已在全部目标实例执行"
	}
	if len(skipped) > 0 {
		note := "重试跳过此前已全部成功的数据源：" + strings.Join(skipped, "、")
		if merged.Summary != "" {
			note = merged.Summary + "；" + note
		}
		merged.Summary = note
	}
	return h.persistExec(it, merged)
}

func (h *TicketHandler) executeHATicket(ctx context.Context, it *store.Issue) error {
	haH := &HAHandler{Store: h.Store, Secret: h.Secret}
	res, err := haH.ExecuteHATicket(ctx, it)
	if err != nil {
		return err
	}
	return h.persistExec(it, res)
}

func (h *TicketHandler) executeK8sChangeTicket(ctx context.Context, it *store.Issue) error {
	k8sH := NewK8sHandler(h.Store, h.Secret)
	res, err := k8sH.ExecuteK8sChangeTicket(ctx, it)
	if err != nil {
		return err
	}
	return h.persistExec(it, res)
}

func (h *TicketHandler) executeSrClusterTicket(ctx context.Context, it *store.Issue) error {
	srH := &SrClusterHandler{Store: h.Store, Secret: h.Secret}
	res, err := srH.ExecuteSrClusterTicket(ctx, it)
	if err != nil {
		return err
	}
	return h.persistExec(it, res)
}

func (h *TicketHandler) executeDbDeployTicket(ctx context.Context, it *store.Issue) error {
	dbH := &DbDeployHandler{Store: h.Store, Secret: h.Secret}
	res, err := dbH.ExecuteDbDeployTicket(ctx, it)
	if err != nil {
		return err
	}
	return h.persistExec(it, res)
}

func (h *TicketHandler) executeResourceApplyTicket(ctx context.Context, it *store.Issue) error {
	raH := &ResourceApplyHandler{Store: h.Store, Secret: h.Secret}
	res, err := raH.ExecuteResourceApplyTicket(ctx, it)
	if err != nil {
		return err
	}
	return h.persistExec(it, res)
}

func (h *TicketHandler) executeBackupRestoreTicket(ctx context.Context, it *store.Issue) error {
	payload, err := backup.ParseLogicalRestorePayload(it.SQLContent)
	if err != nil {
		return h.persistExec(it, ticketexec.FailBeforeExec(err.Error(), ticketexec.Record{
			InstanceID: it.InstanceID, DB: it.DBName,
		}))
	}
	eng := h.Backup
	if eng == nil {
		eng = backup.New(h.Store, h.Secret, backup.Config{AllowEmbedded: true})
	}
	msg, err := eng.ExecuteLogicalRestore(ctx, payload)
	if err != nil {
		res := ticketexec.FailBeforeExec(err.Error(), ticketexec.Record{
			InstanceID: payload.TargetInstanceID, DB: payload.TargetDB,
			SQL: "logical_restore", Status: "failed",
		})
		res.Message = err.Error()
		return h.persistExec(it, res)
	}
	res := &ticketexec.Result{
		Success:   true,
		Summary:   msg,
		Message:   msg,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		EndedAt:   time.Now().UTC().Format(time.RFC3339),
		Engine:    payload.Engine,
	}
	res.Records = []ticketexec.Record{{
		InstanceID: payload.TargetInstanceID, DB: payload.TargetDB,
		SQL: "logical_restore", Status: "success", StartedAt: res.StartedAt, EndedAt: res.EndedAt,
	}}
	return h.persistExec(it, res)
}

func (h *TicketHandler) isPlatformResourceApplyReviewer(c *gin.Context) bool {
	if h == nil || h.Store == nil || c == nil {
		return false
	}
	return h.Store.IsPlatformResourceApplyReviewer(middleware.UserID(c), middleware.Username(c))
}

func (h *TicketHandler) actorCanApproveIssue(c *gin.Context, it *store.Issue) bool {
	if it == nil {
		return false
	}
	user := middleware.Username(c)
	priv := middleware.IsPrivileged(middleware.Role(c))
	if it.IssueType == store.IssueTypeResourceApply {
		return h.Store.CanApproveResourceApply(it, user, middleware.UserID(c), priv)
	}
	if it.IssueType == store.IssueTypeDnsChange && h.DNS != nil && h.DNS.IsDNSApprover(user) {
		return true
	}
	return h.Store.CanApproveIssue(it, user, priv)
}

// loadIssueForActor 本租可见；平台资源申请额外允许：提交人/指定审批人，或平台管理员/白名单跨租打开。
func (h *TicketHandler) loadIssueForActor(c *gin.Context, id string) (*store.Issue, error) {
	tid := effectiveListTenantID(h.Store, c)
	it, err := h.Store.GetIssueInTenant(id, tid)
	if err != nil {
		return nil, err
	}
	if it != nil {
		return it, nil
	}
	raw, err := h.Store.GetIssue(id)
	if err != nil || raw == nil {
		return raw, err
	}
	if raw.IssueType != store.IssueTypeResourceApply {
		return nil, nil
	}
	user := middleware.Username(c)
	if strings.EqualFold(raw.Submitter, user) || strings.EqualFold(raw.AssignedTo, user) || h.isPlatformResourceApplyReviewer(c) {
		return raw, nil
	}
	return nil, nil
}

func (h *TicketHandler) executeRedisTicket(ctx context.Context, it *store.Issue) error {
	inst, err := h.Store.GetInstanceUsableInTenant(it.InstanceID, it.TenantID)
	if err != nil {
		return err
	}
	if inst == nil {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: "工单实例不存在"})
	}
	if !strings.EqualFold(strings.TrimSpace(inst.DBType), "redis") {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: "实例类型不是 redis"})
	}
	rules, err := h.Store.GetRedisCommandRules()
	if err != nil {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: err.Error()})
	}
	cmds, err := rediscmd.ValidCommandsWith(rules, it.SQLContent)
	if err != nil {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: err.Error()})
	}
	password := ""
	if strings.TrimSpace(inst.Password) != "" {
		plain, decErr := secret.Decrypt(h.Secret, inst.Password)
		if decErr != nil {
			return h.persistExec(it, &ticketexec.Result{Success: false, Message: "解密实例密码失败: " + decErr.Error()})
		}
		password = plain
	}
	host, err := redisquery.ResolveHost(inst, it.Domain)
	if err != nil {
		return h.persistExec(it, &ticketexec.Result{Success: false, Message: err.Error()})
	}
	opt := redisquery.Options{Username: inst.Username, Password: password, Host: host}
	started := time.Now().UTC().Format(time.RFC3339)
	res := &ticketexec.Result{StartedAt: started, Engine: "redis", Success: true}
	for i, cmd := range cmds {
		cmdStart := time.Now().UTC().Format(time.RFC3339)
		out, execErr := redisquery.ExecuteAllowed(ctx, inst, opt, it.DBName, cmd, rules)
		cmdEnd := time.Now().UTC().Format(time.RFC3339)
		rec := ticketexec.Record{
			InstanceID: inst.ID, InstanceName: inst.Name, Host: host, DB: it.DBName,
			Port: inst.Port, Username: inst.Username, SQL: cmd,
			StartedAt: cmdStart, EndedAt: cmdEnd, Order: i + 1,
		}
		if execErr != nil {
			rec.Status = "failed"
			rec.ErrorMessage = execErr.Error()
			res.Records = append(res.Records, rec)
			res.Success = false
			res.Message = execErr.Error()
			if len(res.SuccessSQLs) > 0 {
				res.Summary = fmt.Sprintf("部分执行后失败，已成功 %d/%d 条", len(res.SuccessSQLs), len(cmds))
			}
			res.EndedAt = cmdEnd
			return h.persistExec(it, res)
		}
		rec.Status = "success"
		if out != nil {
			rec.AffectedRow = int64(out.RowCount)
		}
		res.Records = append(res.Records, rec)
		res.SuccessSQLs = append(res.SuccessSQLs, ticketexec.SuccSQL{
			Host: host, DB: it.DBName, SQL: cmd, Order: i + 1,
		})
	}
	res.Summary = fmt.Sprintf("成功执行 %d 条 Redis 命令", len(cmds))
	res.EndedAt = time.Now().UTC().Format(time.RFC3339)
	return h.persistExec(it, res)
}

func tagExecRecords(res *ticketexec.Result, instanceID, instanceName string) {
	if res == nil {
		return
	}
	for i := range res.Records {
		if strings.TrimSpace(res.Records[i].InstanceID) == "" {
			res.Records[i].InstanceID = instanceID
		}
		if strings.TrimSpace(res.Records[i].InstanceName) == "" {
			res.Records[i].InstanceName = instanceName
		}
	}
}

func (h *TicketHandler) persistExec(it *store.Issue, res *ticketexec.Result) error {
	status := store.StatusFailed
	if res != nil && res.Success {
		status = store.StatusExecuted
	}
	payload := ticketexec.ToJSON(res)
	if err := h.Store.UpdateIssueExec(it.ID, status, payload); err != nil {
		return err
	}
	it.Status = status
	it.ExecuteResult = payload
	if res != nil {
		it.Exec = res
	}
	if status == store.StatusFailed {
		msg := ""
		if res != nil {
			msg = strings.TrimSpace(res.Message)
			if msg == "" {
				msg = strings.TrimSpace(res.Summary)
			}
		}
		NotifyIssueExecFailedAsync(h.Store, h.Secret, it, msg)
	}
	return nil
}

func reviewGateResult(check sqlcheck.Result) *ticketexec.Result {
	res := &ticketexec.Result{
		Success:   false,
		Message:   "执行前审核规则未通过",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		EndedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	for _, st := range check.Statements {
		if !strings.EqualFold(st.ErrorLevel, "ERROR") {
			continue
		}
		res.Records = append(res.Records, ticketexec.Record{
			SQL:          st.SQL,
			Status:       "failed",
			ErrorMessage: st.Message,
			Order:        len(res.Records) + 1,
		})
	}
	if len(res.Records) == 0 {
		res.Records = append(res.Records, ticketexec.Record{
			Status: "failed", ErrorMessage: res.Message, Order: 1,
		})
	}
	return res
}

func scheduledInFuture(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if tm, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return tm.After(time.Now())
		}
	}
	return false
}

func (h *TicketHandler) Stats(c *gin.Context) {
	st, err := h.Store.IssueStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func dashboardFilterFromQuery(c *gin.Context, viewer string) store.DashboardFilter {
	return store.DashboardFilter{
		Q:         c.Query("q"),
		IssueType: firstQuery(c, "issue_type", "type"),
		Status:    c.Query("status"),
		DBType:    c.Query("db_type"),
		IDC:       c.Query("idc"),
		IDCScope:  c.Query("idc_scope"),
		DateFrom:  c.Query("date_from"),
		DateTo:    c.Query("date_to"),
		Viewer:    viewer,
		TenantID:  middleware.TenantID(c),
	}
}

func firstQuery(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(c.Query(k)); v != "" {
			return v
		}
	}
	return ""
}

func (h *TicketHandler) DashboardStats(c *gin.Context) {
	st, err := h.Store.DashboardStats(dashboardFilterFromQuery(c, middleware.Username(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func (h *TicketHandler) Todos(c *gin.Context) {
	page, size := store.ParsePage(c.Query("current"), c.Query("page_size"))
	seeAllRA := h.isPlatformResourceApplyReviewer(c)
	list, total, err := h.Store.DashboardTodos(dashboardFilterFromQuery(c, ""), middleware.Username(c), page, size, seeAllRA)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.DashboardTodo{}
	}
	names := tenantNameMap(h.Store)
	for i := range list {
		if n := names[list[i].TenantID]; n != "" {
			list[i].TenantName = n
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"pending_count": total, "list": list, "pagination": gin.H{"total": total, "current": page, "page_size": size}}})
}

func (h *TicketHandler) Activities(c *gin.Context) {
	limit := 20
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, err := h.Store.DashboardActivities(dashboardFilterFromQuery(c, ""), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.DashboardActivity{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"list": list}})
}
