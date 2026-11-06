package handler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
)

type CreateDBHandler struct {
	Store  *store.DB
	Secret string
}

func (h *CreateDBHandler) ListInstances(c *gin.Context) {
	list, err := h.Store.ListInstancesInTenant(effectiveListTenantID(h.Store, c), c.Query("db_type"), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

var approverNameSplit = regexp.MustCompile(`[,，\s]+`)

// unionApproverNames 把逗号拼接的审批人拆开，再与其它来源取并集。相同账号只保留一条，默认审批人在前。
func unionApproverNames(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, raw := range group {
			for _, name := range approverNameSplit.Split(raw, -1) {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				out = append(out, name)
			}
		}
	}
	return out
}

const createDBApproverDenied = "审批人须为 admin、dba 租户用户、sync_default_approver 或超级审核白名单中的用户"

func (h *CreateDBHandler) ListApprovers(c *gin.Context) {
	tid := middleware.TenantID(c)
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	var names []string
	var err error
	// scope=tenant 只给数据导出的 DBA/安全审批人沿用旧的本租户候选人，不参与建库审批。
	if strings.EqualFold(strings.TrimSpace(c.Query("scope")), "tenant") {
		names, err = listTenantScopedApproverNames(h.Store, tid)
	} else {
		names, err = listCreateDBApproverNames(h.Store)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	type item struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	out := make([]item, 0, len(names))
	for _, name := range names {
		out = append(out, item{Type: "user", Name: name})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// mergeCreateDBApproverNames 建库审批人：sync_default_approver、admin、白名单、dba 租户成员。同名只保留先出现的。
func mergeCreateDBApproverNames(defaultApprover, whitelist string, dbaMembers []string) []string {
	raw := make([]string, 0, 1+len(dbaMembers))
	raw = append(raw, store.ParseApproverWhitelist(defaultApprover)...)
	raw = append(raw, "admin")
	raw = append(raw, store.ParseApproverWhitelist(whitelist)...)
	raw = append(raw, dbaMembers...)
	return store.ExpandApproverNames(raw)
}

func keepCreateDBApprover(name string, user *store.User) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if user != nil && user.IsLoginBlocked() {
		return false
	}
	if user == nil && !strings.EqualFold(name, "admin") {
		return false
	}
	return true
}

func listCreateDBApproverNames(db *store.DB) ([]string, error) {
	if db == nil {
		return nil, nil
	}
	def, err := db.GetGlobalArg("sync_default_approver")
	if err != nil {
		return nil, err
	}
	white, err := db.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	if err != nil {
		return nil, err
	}
	var dbaMembers []string
	t, err := db.GetTenantByCode(store.DBATenantCode)
	if err != nil {
		return nil, err
	}
	if t != nil {
		dbaMembers, err = listTenantMemberUsernames(db, t.ID)
		if err != nil {
			return nil, err
		}
	}
	names := mergeCreateDBApproverNames(def, white, dbaMembers)
	out := make([]string, 0, len(names))
	for _, name := range names {
		u, err := db.GetUserByUsername(name)
		if err != nil {
			return nil, err
		}
		if !keepCreateDBApprover(name, u) {
			continue
		}
		if u != nil && strings.TrimSpace(u.Username) != "" {
			name = u.Username
		}
		out = append(out, name)
	}
	return store.ExpandApproverNames(out), nil
}

// listTenantScopedApproverNames 本租户活跃成员，并在默认审批人同属该租户时放在前面。
func listTenantScopedApproverNames(db *store.DB, tenantID string) ([]string, error) {
	userNames, err := listTenantMemberUsernames(db, tenantID)
	if err != nil {
		return nil, err
	}
	def, _ := db.GetGlobalArg("sync_default_approver")
	if strings.TrimSpace(def) != "" && requireTenantMemberUsernames(db, tenantID, []string{def}) != nil {
		def = ""
	}
	return unionApproverNames([]string{def}, userNames), nil
}

func createDBApproverAllowed(db *store.DB, username string) (bool, error) {
	pool, err := listCreateDBApproverNames(db)
	if err != nil {
		return false, err
	}
	_, ok := matchCreateDBApprover(pool, username)
	return ok, nil
}

func matchCreateDBApprover(pool []string, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, p := range pool {
		if strings.EqualFold(p, name) {
			return p, true
		}
	}
	return "", false
}

func resolveCreateDBApprover(db *store.DB, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return "", fmt.Errorf("请选择审批人")
	}
	pool, err := listCreateDBApproverNames(db)
	if err != nil {
		return "", err
	}
	if name, ok := matchCreateDBApprover(pool, preferred); ok {
		return name, nil
	}
	return "", fmt.Errorf("%s", createDBApproverDenied)
}

func (h *CreateDBHandler) CheckSQL(c *gin.Context) {
	var req struct {
		DBType      string   `json:"db_type"`
		IDCScope    string   `json:"idc_scope"`
		InstanceID  string   `json:"instance_id"`
		InstanceIDs []string `json:"instance_ids"`
		SQLContent  string   `json:"sql_content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := uniqueIDs(req.InstanceIDs, req.InstanceID)
	instID := ""
	if len(ids) > 0 {
		instID = ids[0]
	}
	th := &TicketHandler{Store: h.Store}
	res := applyCreateDBReview(th, req.SQLContent, req.DBType, instID)
	names := sqlcheck.ParseCreateDBNames(req.SQLContent)
	for _, id := range ids {
		inst, err := resolveInstanceInTenant(h.Store, c, id)
		if err != nil || inst == nil {
			res.OK = false
			res.Statements = append(res.Statements, sqlcheck.StatementRow{
				Stage: "instance", ErrorLevel: "ERROR", Message: "实例未登记: " + id, SQL: req.SQLContent,
			})
			continue
		}
		if req.DBType != "" && !strings.EqualFold(inst.DBType, req.DBType) {
			res.OK = false
			res.Statements = append(res.Statements, sqlcheck.StatementRow{
				Stage: "instance", ErrorLevel: "ERROR", Message: "实例类型与所选 db_type 不一致: " + inst.Name, SQL: req.SQLContent,
			})
		}
		for _, name := range names {
			if exist, _ := h.Store.FindDatabase(id, name); exist != nil {
				res.OK = false
				res.Statements = append(res.Statements, sqlcheck.StatementRow{
					Stage: "duplicate", ErrorLevel: "ERROR", Message: "元数据已存在同名库: " + name, SQL: req.SQLContent,
				})
			}
		}
	}
	c.JSON(http.StatusOK, res)
}

func (h *CreateDBHandler) Submit(c *gin.Context) {
	var req struct {
		DBType      string   `json:"db_type"`
		IDCScope    string   `json:"idc_scope"`
		InstanceID  string   `json:"instance_id"`
		InstanceIDs []string `json:"instance_ids"`
		Domain      string   `json:"domain"`
		DataSources []struct {
			InstanceID string `json:"instance_id"`
			Domain     string `json:"domain"`
		} `json:"data_sources"`
		Username    string   `json:"username"`
		Permissions []string `json:"permissions"`
		AssignedTo  string   `json:"assigned_to"`
		Description string   `json:"description"`
		SQLContent  string   `json:"sql_content"`
		ScheduledAt string   `json:"scheduled_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := uniqueIDs(req.InstanceIDs, req.InstanceID)
	for _, ds := range req.DataSources {
		ids = uniqueIDs(ids, ds.InstanceID)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id 必填"})
		return
	}
	domain := strings.TrimSpace(req.Domain)
	if domain == "" && len(req.DataSources) > 0 {
		domain = strings.TrimSpace(req.DataSources[0].Domain)
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "数据库账号必填"})
		return
	}
	req.Permissions = normalizeAccountPerms(store.NormalizeReviewEngine(req.DBType), req.Permissions)
	if len(req.Permissions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "账号权限必填"})
		return
	}
	th := &TicketHandler{Store: h.Store}
	res := applyCreateDBReview(th, req.SQLContent, req.DBType, ids[0])
	if !res.OK {
		c.JSON(http.StatusBadRequest, gin.H{"message": "建库 SQL 未通过审核规则", "details": res})
		return
	}
	names := sqlcheck.ParseCreateDBNames(req.SQLContent)
	if len(names) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "未能解析出数据库名"})
		return
	}
	user := middleware.Username(c)
	ok, _, msg, err := th.canActOnTarget(middleware.TenantID(c), ids[0], "", middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	preferred := ""
	if names := unionApproverNames([]string{req.AssignedTo}); len(names) > 0 {
		preferred = names[0]
	}
	approver, aerr := resolveCreateDBApprover(h.Store, preferred)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	dataSources := make([]map[string]string, 0, len(req.DataSources))
	for _, ds := range req.DataSources {
		iid := strings.TrimSpace(ds.InstanceID)
		dom := strings.TrimSpace(ds.Domain)
		if iid == "" || dom == "" {
			continue
		}
		dataSources = append(dataSources, map[string]string{"instance_id": iid, "domain": dom})
	}
	for _, id := range ids {
		inst, _ := resolveInstanceInTenant(h.Store, c, id)
		if inst == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "实例未登记: " + id})
			return
		}
		if req.DBType != "" && !strings.EqualFold(inst.DBType, req.DBType) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "实例类型与 db_type 不一致"})
			return
		}
		dom := domain
		for _, ds := range dataSources {
			if ds["instance_id"] == id {
				dom = ds["domain"]
				break
			}
		}
		if dom != "" {
			if err := ensureTicketDomainVisible(h.Store, c, inst, dom); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
				return
			}
		}
		for _, name := range names {
			if exist, _ := h.Store.FindDatabase(id, name); exist != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "元数据已存在同名库: " + name})
				return
			}
		}
	}
	extra, _ := json.Marshal(map[string]any{
		"kind":         "createdb",
		"username":     req.Username,
		"permissions":  req.Permissions,
		"instance_ids": ids,
		"data_sources": dataSources,
		"domain":       domain,
		"env":          req.IDCScope,
		"db_type":      req.DBType,
	})
	issue := &store.Issue{
		IssueType:    store.IssueTypeCreateDB,
		Type:         store.IssueTypeCreateDB,
		InstanceID:   ids[0],
		Domain:       domain,
		DBName:       strings.Join(names, ","),
		SQLContent:   req.SQLContent,
		Description:  strings.TrimSpace(req.Description),
		Status:       store.StatusPending,
		Submitter:    user,
		AssignedTo:   approver,
		NeedRollback: string(extra),
		ScheduledAt:  req.ScheduledAt,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil {
			issue.TenantID = tid
		}
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{
		"id": issue.ID, "issue_id": issue.ID, "ticket_no": issue.TicketNo,
		"name": issue.TicketNo, "status": issue.Status, "data": issue,
	})
}

func uniqueIDs(ids []string, first string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range append([]string{first}, ids...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

type createDBExtra struct {
	Kind        string   `json:"kind"`
	Username    string   `json:"username"`
	Permissions []string `json:"permissions"`
	InstanceIDs []string `json:"instance_ids"`
	Env         string   `json:"env"`
	DBType      string   `json:"db_type"`
}

func parseCreateDBExtra(it *store.Issue) *createDBExtra {
	if it == nil {
		return nil
	}
	raw := strings.TrimSpace(it.NeedRollback)
	if raw != "" {
		var extra createDBExtra
		if json.Unmarshal([]byte(raw), &extra) == nil && (extra.Kind == "createdb" || extra.Username != "") {
			if len(extra.InstanceIDs) == 0 && it.InstanceID != "" {
				extra.InstanceIDs = []string{it.InstanceID}
			}
			return &extra
		}
	}
	if it.IssueType == store.IssueTypeCreateDB {
		return &createDBExtra{
			Kind:        "createdb",
			Username:    "",
			InstanceIDs: []string{it.InstanceID},
		}
	}
	return nil
}

func randomAccountPassword(n int) string {
	const (
		lower = "abcdefghijklmnopqrstuvwxyz"
		upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		digit = "0123456789"
		all   = lower + upper + digit
	)
	if n < 16 {
		n = 16
	}
	out := make([]byte, n)
	// 先各放一类，保证含大小写字母与数字，且不含特殊字符
	out[0] = lower[cryptoRandInt(len(lower))]
	out[1] = upper[cryptoRandInt(len(upper))]
	out[2] = digit[cryptoRandInt(len(digit))]
	for i := 3; i < n; i++ {
		out[i] = all[cryptoRandInt(len(all))]
	}
	// 打乱顺序，避免前三位类型固定
	for i := n - 1; i > 0; i-- {
		j := cryptoRandInt(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func cryptoRandInt(max int) int {
	if max <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

// materializeCreateDBAccounts 审批通过后写入账号管理（密码 AES-256-GCM 加密）。幂等。
func materializeCreateDBAccounts(st *store.DB, secretKey string, it *store.Issue) error {
	if it == nil {
		return nil
	}
	extra := parseCreateDBExtra(it)
	if extra == nil {
		return nil
	}
	existed, err := st.ListAccountsByTicket(it.ID)
	if err != nil {
		return err
	}
	if len(existed) > 0 {
		return nil
	}
	username := strings.TrimSpace(extra.Username)
	if username == "" {
		return nil
	}
	names := sqlcheck.ParseCreateDBNames(it.SQLContent)
	if len(names) == 0 && strings.TrimSpace(it.DBName) != "" {
		for _, n := range strings.Split(it.DBName, ",") {
			if s := strings.TrimSpace(n); s != "" {
				names = append(names, s)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	plain := randomAccountPassword(16)
	enc, err := secret.Encrypt(secretKey, plain)
	if err != nil {
		return err
	}
	ids := extra.InstanceIDs
	if len(ids) == 0 && it.InstanceID != "" {
		ids = []string{it.InstanceID}
	}
	for _, instanceID := range ids {
		inst, _ := st.GetInstance(instanceID)
		if inst == nil {
			continue
		}
		pwdEnc := enc
		if prev, _ := st.FindAccountByInstanceUser(instanceID, username, it.ID); prev != nil && strings.TrimSpace(prev.Password) != "" {
			// 实例上账号已存在时 CREATE USER 会被跳过，实际密码仍是旧的
			pwdEnc = prev.Password
		}
		for _, dbName := range names {
			if found, _ := st.FindAccount(instanceID, username, dbName); found != nil {
				continue
			}
			var dbID string
			if rec, _ := st.FindDatabase(instanceID, dbName); rec != nil {
				dbID = rec.ID
			} else {
				dom := ddlDomainForInstance(it.NeedRollback, instanceID, it.Domain)
				rec := &store.Database{InstanceID: instanceID, DBName: dbName, Visibility: "global"}
				if dom != "" {
					rec.Domains = []string{dom}
				}
				if err := st.CreateDatabase(rec); err == nil {
					dbID = rec.ID
				}
			}
			acc := &store.DBAccount{
				InstanceID:  instanceID,
				DatabaseID:  dbID,
				DBName:      dbName,
				DBType:      inst.DBType,
				IDC:         inst.IDC,
				Username:    username,
				Password:    pwdEnc,
				Permissions: extra.Permissions,
				Description: strings.TrimSpace(it.Description),
				TicketID:    it.ID,
				CreatedBy:   it.Submitter,
				UpdatedBy:   it.Submitter,
			}
			if extra.DBType != "" {
				acc.DBType = extra.DBType
			}
			if extra.Env != "" {
				acc.IDC = extra.Env
			}
			if err := st.CreateAccount(acc); err != nil {
				return err
			}
		}
	}
	return nil
}
