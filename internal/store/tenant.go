package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultTenantID      = "tenant-default"
	DefaultTenantCode    = "default"
	TenantStatusActive   = "active"
	TenantStatusDisabled = "disabled"
	MemberStatusActive   = "active"
	MemberStatusDisabled = "disabled"

	// 租户套餐（占位枚举；配额/计费后续挂接）
	TenantPlanFree       = "free"
	TenantPlanStandard   = "standard"
	TenantPlanEnterprise = "enterprise"
)

// TenantPlanOptions 前端下拉与校验共用。
var TenantPlanOptions = []struct {
	Value string `json:"value"`
	Label string `json:"label"`
}{
	{Value: TenantPlanFree, Label: "免费"},
	{Value: TenantPlanStandard, Label: "标准"},
	{Value: TenantPlanEnterprise, Label: "企业"},
}

// NormalizeTenantPlan 规范化套餐：空 → standard；推荐 free/standard/enterprise，也允许自定义（小写、长度限制）。
func NormalizeTenantPlan(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return TenantPlanStandard, nil
	}
	if len(s) > 64 {
		return "", fmt.Errorf("套餐长度不能超过 64")
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("套餐仅允许小写字母、数字、.-_")
	}
	return s, nil
}

// Tenant 租户实体。
type Tenant struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Plan        string `json:"plan,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// TenantMember 用户在租户内的成员关系；RoleIDs 决定租户内 RBAC。
type TenantMember struct {
	TenantID    string   `json:"tenant_id"`
	UserID      string   `json:"user_id"`
	Username    string   `json:"username,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	RoleIDs     []string `json:"role_ids"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// TenantGrant 跨租户资源授权。
type TenantGrant struct {
	ID           string `json:"id"`
	FromTenantID string `json:"from_tenant_id"`
	ToTenantID   string `json:"to_tenant_id,omitempty"`
	ToUserID     string `json:"to_user_id,omitempty"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Permission   string `json:"permission"` // read | write
	CreatedBy    string `json:"created_by"`
	CreatedAt    string `json:"created_at"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

// TenantAuditLog 租户相关审计。
type TenantAuditLog struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	ActorUserID string `json:"actor_user_id"`
	Action      string `json:"action"`
	Detail      string `json:"detail,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func normalizeTenantStatus(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), TenantStatusDisabled) {
		return TenantStatusDisabled
	}
	return TenantStatusActive
}

func (d *DB) CreateTenant(t *Tenant) error {
	if t == nil {
		return errors.New("tenant 必填")
	}
	t.Code = strings.ToLower(strings.TrimSpace(t.Code))
	t.Name = strings.TrimSpace(t.Name)
	if t.Code == "" || t.Name == "" {
		return errors.New("code 与 name 必填")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.Status = normalizeTenantStatus(t.Status)
	plan, err := NormalizeTenantPlan(t.Plan)
	if err != nil {
		return err
	}
	t.Plan = plan
	t.CreatedAt, t.UpdatedAt = now, now
	_, err = d.Exec(`INSERT INTO tbl_tenants(id, code, name, status, plan, description, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		t.ID, t.Code, t.Name, t.Status, t.Plan, t.Description, t.CreatedAt, t.UpdatedAt)
	return err
}

func (d *DB) UpdateTenant(t *Tenant) error {
	if t == nil || strings.TrimSpace(t.ID) == "" {
		return errors.New("tenant id 必填")
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return errors.New("name 必填")
	}
	t.Status = normalizeTenantStatus(t.Status)
	plan, err := NormalizeTenantPlan(t.Plan)
	if err != nil {
		return err
	}
	t.Plan = plan
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`UPDATE tbl_tenants SET name=?, status=?, plan=?, description=?, updated_at=? WHERE id=?`,
		t.Name, t.Status, t.Plan, t.Description, t.UpdatedAt, t.ID)
	return err
}

// TenantUsage 删除前占用统计：成员数 + 绑定业务资源（按表）。
type TenantUsage struct {
	Members       int64            `json:"members"`
	Resources     map[string]int64 `json:"resources"` // table -> count，仅 >0
	ResourceTotal int64            `json:"resource_total"`
}

// TenantDeleteBlockedError 删除被拒绝（有成员或仍绑定资源）。
type TenantDeleteBlockedError struct {
	Kind      string           `json:"kind"` // members | resources | default
	Message   string           `json:"message"`
	Members   int64            `json:"members,omitempty"`
	Resources map[string]int64 `json:"resources,omitempty"`
}

func (e *TenantDeleteBlockedError) Error() string {
	if e == nil {
		return "租户不可删除"
	}
	return e.Message
}

func isDefaultTenant(t *Tenant) bool {
	if t == nil {
		return false
	}
	if t.ID == DefaultTenantID {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(t.Code), DefaultTenantCode)
}

func (d *DB) tableExistsPublic(table string) (bool, error) {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1
	`, table).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountTenantUsage 统计租户成员与业务资源占用（用于删除前置校验与提示）。
func (d *DB) CountTenantUsage(tenantID string) (*TenantUsage, error) {
	tenantID = strings.TrimSpace(tenantID)
	u := &TenantUsage{Resources: map[string]int64{}}
	if tenantID == "" {
		return u, nil
	}
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_tenant_members WHERE tenant_id=?`, tenantID).Scan(&u.Members); err != nil {
		return nil, err
	}
	for _, table := range tenantScopedTables {
		ok, err := d.tableExistsPublic(table)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		var c int64
		// table 来自白名单 tenantScopedTables，非用户输入
		if err := d.QueryRow(`SELECT COUNT(1) FROM `+table+` WHERE tenant_id=?`, tenantID).Scan(&c); err != nil {
			return nil, fmt.Errorf("%s: %w", table, err)
		}
		if c > 0 {
			u.Resources[table] = c
			u.ResourceTotal += c
		}
	}
	return u, nil
}

// DeleteTenant 硬删空租户：须无成员且无绑定业务资源；自动清理 grant / audit。
func (d *DB) DeleteTenant(tenantID string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return errors.New("tenant id 必填")
	}
	t, err := d.GetTenant(tenantID)
	if err != nil {
		return err
	}
	if t == nil {
		return errors.New("租户不存在")
	}
	if isDefaultTenant(t) {
		return &TenantDeleteBlockedError{Kind: "default", Message: "默认租户不可删除"}
	}
	usage, err := d.CountTenantUsage(tenantID)
	if err != nil {
		return err
	}
	if usage.Members > 0 {
		return &TenantDeleteBlockedError{
			Kind:    "members",
			Message: "请先移除全部成员",
			Members: usage.Members,
		}
	}
	if usage.ResourceTotal > 0 {
		return &TenantDeleteBlockedError{
			Kind:      "resources",
			Message:   "需要先释放资源",
			Resources: usage.Resources,
		}
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_tenant_grants WHERE from_tenant_id=? OR to_tenant_id=?`), tenantID, tenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_tenant_audit_logs WHERE tenant_id=?`), tenantID); err != nil {
		return err
	}
	// 双保险：理论上成员已为 0
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_tenant_members WHERE tenant_id=?`), tenantID); err != nil {
		return err
	}
	res, err := tx.Exec(d.rewrite(`DELETE FROM tbl_tenants WHERE id=?`), tenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("租户不存在")
	}
	return tx.Commit()
}

func (d *DB) GetTenant(id string) (*Tenant, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT id, code, name, status, plan, description, created_at, updated_at FROM tbl_tenants WHERE id=?`, id)
	var t Tenant
	var plan, desc sql.NullString
	if err := row.Scan(&t.ID, &t.Code, &t.Name, &t.Status, &plan, &desc, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t.Plan = plan.String
	t.Description = desc.String
	return &t, nil
}

func (d *DB) GetTenantByCode(code string) (*Tenant, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT id, code, name, status, plan, description, created_at, updated_at FROM tbl_tenants WHERE code=?`, code)
	var t Tenant
	var plan, desc sql.NullString
	if err := row.Scan(&t.ID, &t.Code, &t.Name, &t.Status, &plan, &desc, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t.Plan = plan.String
	t.Description = desc.String
	return &t, nil
}

func (d *DB) ListTenants() ([]Tenant, error) {
	rows, err := d.Query(`SELECT id, code, name, status, plan, description, created_at, updated_at FROM tbl_tenants ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		var plan, desc sql.NullString
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.Status, &plan, &desc, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Plan = plan.String
		t.Description = desc.String
		out = append(out, t)
	}
	if out == nil {
		out = []Tenant{}
	}
	return out, rows.Err()
}

func encodeRoleIDs(ids []string) string {
	ids = uniqueTrimmed(ids)
	b, _ := json.Marshal(ids)
	if len(b) == 0 {
		return "[]"
	}
	return string(b)
}

func decodeRoleIDs(s string) []string {
	return uniqueTrimmed(decodeStringSlice(s))
}

func (d *DB) UpsertTenantMember(m *TenantMember) error {
	if m == nil {
		return errors.New("member 必填")
	}
	m.TenantID = strings.TrimSpace(m.TenantID)
	m.UserID = strings.TrimSpace(m.UserID)
	if m.TenantID == "" || m.UserID == "" {
		return errors.New("tenant_id 与 user_id 必填")
	}
	if len(uniqueTrimmed(m.RoleIDs)) == 0 {
		return errors.New("至少绑定一个角色")
	}
	for _, rid := range m.RoleIDs {
		r, err := d.GetRole(strings.TrimSpace(rid))
		if err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("角色不存在: %s", rid)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if m.Status == "" {
		m.Status = MemberStatusActive
	}
	if strings.EqualFold(m.Status, MemberStatusDisabled) {
		m.Status = MemberStatusDisabled
	} else {
		m.Status = MemberStatusActive
	}
	m.UpdatedAt = now
	if m.CreatedAt == "" {
		m.CreatedAt = now
	}
	_, err := d.Exec(`INSERT INTO tbl_tenant_members(tenant_id, user_id, role_ids, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(tenant_id, user_id) DO UPDATE SET role_ids=EXCLUDED.role_ids, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`,
		m.TenantID, m.UserID, encodeRoleIDs(m.RoleIDs), m.Status, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return err
	}
	// 普通用户：加入业务租户后离开默认租户；平台管理员需同时管理多租户，保留默认成员
	if m.TenantID != DefaultTenantID && m.Status == MemberStatusActive {
		isAdmin, err := d.UserIsPlatformAdmin(m.UserID)
		if err != nil {
			return err
		}
		if !isAdmin {
			return d.RemoveTenantMember(DefaultTenantID, m.UserID)
		}
	}
	return nil
}

func (d *DB) RemoveTenantMember(tenantID, userID string) error {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	// 平台管理员必须保有默认租户，避免创建/切换其它租户后顶栏只剩业务租户名称
	if tenantID == DefaultTenantID {
		ok, err := d.UserIsPlatformAdmin(userID)
		if err != nil {
			return err
		}
		if ok {
			return errors.New("平台管理员不可移出默认租户")
		}
	}
	_, err := d.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=? AND user_id=?`, tenantID, userID)
	return err
}

// EnsurePlatformAdminDefaultMembership 平台管理员始终保有默认租户成员资格（幂等，用于登录/自愈）。
func (d *DB) EnsurePlatformAdminDefaultMembership(userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	ok, err := d.UserIsPlatformAdmin(userID)
	if err != nil || !ok {
		return err
	}
	m, err := d.GetTenantMember(DefaultTenantID, userID)
	if err != nil {
		return err
	}
	if m != nil && m.Status == MemberStatusActive {
		return nil
	}
	roleIDs, err := d.ListUserRoleIDs(userID)
	if err != nil {
		return err
	}
	if len(roleIDs) == 0 {
		if r, _ := d.GetRoleByCode("admin"); r != nil {
			roleIDs = []string{r.ID}
		}
	}
	if len(roleIDs) == 0 {
		return errors.New("平台管理员缺少可用角色，无法加入默认租户")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return d.UpsertTenantMember(&TenantMember{
		TenantID:  DefaultTenantID,
		UserID:    userID,
		RoleIDs:   roleIDs,
		Status:    MemberStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (d *DB) GetTenantMember(tenantID, userID string) (*TenantMember, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT tenant_id, user_id, role_ids, status, created_at, updated_at
		FROM tbl_tenant_members WHERE tenant_id=? AND user_id=?`, tenantID, userID)
	var m TenantMember
	var roleIDs string
	if err := row.Scan(&m.TenantID, &m.UserID, &roleIDs, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m.RoleIDs = decodeRoleIDs(roleIDs)
	return &m, nil
}

// IsActiveTenantMember 用户是否为租户的有效成员。
func (d *DB) IsActiveTenantMember(tenantID, userID string) (bool, error) {
	m, err := d.GetTenantMember(tenantID, userID)
	if err != nil || m == nil {
		return false, err
	}
	t, err := d.GetTenant(tenantID)
	if err != nil || t == nil {
		return false, err
	}
	return m.Status == MemberStatusActive && t.Status == TenantStatusActive, nil
}

func (d *DB) ListTenantMembers(tenantID string) ([]TenantMember, error) {
	tenantID = strings.TrimSpace(tenantID)
	// 列表时顺带清幽灵成员，避免删用户后旧进程/漏清理仍露出空行。
	_ = d.CleanupOrphanTenantMemberships()
	rows, err := d.Query(`SELECT m.tenant_id, m.user_id, COALESCE(u.username,''), COALESCE(u.display_name,''), m.role_ids, m.status, m.created_at, m.updated_at
		FROM tbl_tenant_members m
		INNER JOIN tbl_users u ON u.id = m.user_id
		WHERE m.tenant_id=? ORDER BY u.username`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantMember
	for rows.Next() {
		var m TenantMember
		var roleIDs string
		if err := rows.Scan(&m.TenantID, &m.UserID, &m.Username, &m.DisplayName, &roleIDs, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.RoleIDs = decodeRoleIDs(roleIDs)
		out = append(out, m)
	}
	if out == nil {
		out = []TenantMember{}
	}
	return out, rows.Err()
}

func (d *DB) ListUserTenants(userID string) ([]Tenant, error) {
	userID = strings.TrimSpace(userID)
	rows, err := d.Query(`SELECT t.id, t.code, t.name, t.status, t.plan, t.description, t.created_at, t.updated_at
		FROM tbl_tenants t
		INNER JOIN tbl_tenant_members m ON m.tenant_id = t.id
		WHERE m.user_id=? AND m.status=? AND t.status=?
		ORDER BY t.code`, userID, MemberStatusActive, TenantStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		var plan, desc sql.NullString
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.Status, &plan, &desc, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Plan = plan.String
		t.Description = desc.String
		out = append(out, t)
	}
	if out == nil {
		out = []Tenant{}
	}
	return out, rows.Err()
}

func (d *DB) CreateTenantGrant(g *TenantGrant) error {
	if g == nil {
		return errors.New("grant 必填")
	}
	g.FromTenantID = strings.TrimSpace(g.FromTenantID)
	g.ToTenantID = strings.TrimSpace(g.ToTenantID)
	g.ToUserID = strings.TrimSpace(g.ToUserID)
	g.ResourceType = strings.TrimSpace(g.ResourceType)
	g.ResourceID = strings.TrimSpace(g.ResourceID)
	g.Permission = strings.ToLower(strings.TrimSpace(g.Permission))
	if g.FromTenantID == "" || g.ResourceType == "" || g.ResourceID == "" {
		return errors.New("from_tenant_id / resource_type / resource_id 必填")
	}
	if g.ToTenantID == "" && g.ToUserID == "" {
		return errors.New("to_tenant_id 或 to_user_id 至少其一")
	}
	if g.Permission != "write" {
		g.Permission = "read"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	g.CreatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_tenant_grants(id, from_tenant_id, to_tenant_id, to_user_id, resource_type, resource_id, permission, created_by, created_at, expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.FromTenantID, nullIfEmpty(g.ToTenantID), nullIfEmpty(g.ToUserID),
		g.ResourceType, g.ResourceID, g.Permission, g.CreatedBy, g.CreatedAt, nullIfEmpty(g.ExpiresAt))
	return err
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (d *DB) DeleteTenantGrant(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_tenant_grants WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) ListTenantGrantsFrom(tenantID string) ([]TenantGrant, error) {
	return d.listTenantGrants(`from_tenant_id=?`, strings.TrimSpace(tenantID))
}

func (d *DB) ListTenantGrantsTo(tenantID, userID string) ([]TenantGrant, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	rows, err := d.Query(`SELECT id, from_tenant_id, COALESCE(to_tenant_id,''), COALESCE(to_user_id,''), resource_type, resource_id, permission, created_by, created_at, COALESCE(expires_at,'')
		FROM tbl_tenant_grants
		WHERE (to_tenant_id=? OR to_user_id=?)
		  AND (expires_at IS NULL OR expires_at='' OR expires_at > ?)
		ORDER BY created_at DESC`, tenantID, userID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenantGrants(rows)
}

func (d *DB) listTenantGrants(where string, arg any) ([]TenantGrant, error) {
	rows, err := d.Query(`SELECT id, from_tenant_id, COALESCE(to_tenant_id,''), COALESCE(to_user_id,''), resource_type, resource_id, permission, created_by, created_at, COALESCE(expires_at,'')
		FROM tbl_tenant_grants WHERE `+where+` ORDER BY created_at DESC`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenantGrants(rows)
}

func scanTenantGrants(rows *sql.Rows) ([]TenantGrant, error) {
	var out []TenantGrant
	for rows.Next() {
		var g TenantGrant
		if err := rows.Scan(&g.ID, &g.FromTenantID, &g.ToTenantID, &g.ToUserID, &g.ResourceType, &g.ResourceID, &g.Permission, &g.CreatedBy, &g.CreatedAt, &g.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if out == nil {
		out = []TenantGrant{}
	}
	return out, rows.Err()
}

// HasResourceGrant 当前租户/用户是否被授权访问某资源。
func (d *DB) HasResourceGrant(toTenantID, toUserID, resourceType, resourceID string) (bool, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	now := time.Now().UTC().Format(time.RFC3339)
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_tenant_grants
		WHERE resource_type=? AND resource_id=?
		  AND (to_tenant_id=? OR to_user_id=?)
		  AND (expires_at IS NULL OR expires_at='' OR expires_at > ?)`,
		resourceType, resourceID, strings.TrimSpace(toTenantID), strings.TrimSpace(toUserID), now).Scan(&n)
	return n > 0, err
}

func (d *DB) AppendTenantAudit(tenantID, actorUserID, action, detail string) error {
	_, err := d.Exec(`INSERT INTO tbl_tenant_audit_logs(id, tenant_id, actor_user_id, action, detail, created_at)
		VALUES(?,?,?,?,?,?)`,
		uuid.NewString(), strings.TrimSpace(tenantID), strings.TrimSpace(actorUserID),
		strings.TrimSpace(action), strings.TrimSpace(detail), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (d *DB) ListTenantAuditLogs(tenantID string, limit int) ([]TenantAuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.Query(`SELECT id, tenant_id, actor_user_id, action, detail, created_at
		FROM tbl_tenant_audit_logs WHERE tenant_id=? ORDER BY created_at DESC LIMIT ?`,
		strings.TrimSpace(tenantID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantAuditLog
	for rows.Next() {
		var a TenantAuditLog
		if err := rows.Scan(&a.ID, &a.TenantID, &a.ActorUserID, &a.Action, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []TenantAuditLog{}
	}
	return out, rows.Err()
}

// AssignTenantID 写入业务行的 tenant_id（创建后补齐，兼容旧 INSERT 未含该列的路径）。
func (d *DB) AssignTenantID(table, id, tenantID string) error {
	table = strings.TrimSpace(table)
	id = strings.TrimSpace(id)
	tenantID = strings.TrimSpace(tenantID)
	if table == "" || id == "" {
		return nil
	}
	if tenantID == "" {
		return errors.New("tenant_id 必填")
	}
	_, err := d.Exec(`UPDATE `+table+` SET tenant_id=? WHERE id=?`, tenantID, id)
	return err
}

// CountActiveMemberships 用户当前有效租户成员数。
func (d *DB) CountActiveMemberships(userID string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_tenant_members WHERE user_id=? AND status=?`,
		strings.TrimSpace(userID), MemberStatusActive).Scan(&n)
	return n, err
}

// EnsureDefaultMembership 仅当用户没有任何有效成员时加入默认租户。
func (d *DB) EnsureDefaultMembership(userID string, roleIDs []string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	n, err := d.CountActiveMemberships(userID)
	if err != nil || n > 0 {
		return err
	}
	if len(uniqueTrimmed(roleIDs)) == 0 {
		if r, _ := d.GetRoleByCode("viewer"); r != nil {
			roleIDs = []string{r.ID}
		}
	}
	if len(uniqueTrimmed(roleIDs)) == 0 {
		return nil
	}
	return d.UpsertTenantMember(&TenantMember{
		TenantID: DefaultTenantID,
		UserID:   userID,
		RoleIDs:  roleIDs,
		Status:   MemberStatusActive,
	})
}

// CleanupDefaultMembershipsForBusinessMembers 已加入业务租户的普通用户不再保留 default 成员；平台管理员除外。
func (d *DB) CleanupDefaultMembershipsForBusinessMembers() error {
	rows, err := d.Query(`SELECT d.user_id FROM tbl_tenant_members d
		WHERE d.tenant_id=? AND EXISTS (
			SELECT 1 FROM tbl_tenant_members o
			WHERE o.user_id=d.user_id AND o.tenant_id<>? AND o.status=?
		)`, DefaultTenantID, DefaultTenantID, MemberStatusActive)
	if err != nil {
		return err
	}
	defer rows.Close()
	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return err
		}
		userIDs = append(userIDs, uid)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, uid := range userIDs {
		isAdmin, err := d.UserIsPlatformAdmin(uid)
		if err != nil {
			return err
		}
		if isAdmin {
			continue
		}
		if _, err := d.Exec(`DELETE FROM tbl_tenant_members WHERE tenant_id=? AND user_id=?`,
			DefaultTenantID, uid); err != nil {
			return err
		}
	}
	return nil
}

// CleanupOrphanTenantMemberships 清理用户已删除后残留的成员行与按用户授权。
func (d *DB) CleanupOrphanTenantMemberships() error {
	if _, err := d.Exec(`DELETE FROM tbl_tenant_members
		WHERE NOT EXISTS (SELECT 1 FROM tbl_users u WHERE u.id = tbl_tenant_members.user_id)`); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_tenant_grants
		WHERE COALESCE(to_user_id,'')<>''
		  AND NOT EXISTS (SELECT 1 FROM tbl_users u WHERE u.id = tbl_tenant_grants.to_user_id)`)
	return err
}

// PickLoginTenant 登录时选择租户：显式指定 > 唯一成员 > 默认租户（若成员）> 空。
func (d *DB) PickLoginTenant(userID, preferredTenantID string) (*Tenant, []Tenant, error) {
	list, err := d.ListUserTenants(userID)
	if err != nil {
		return nil, nil, err
	}
	preferredTenantID = strings.TrimSpace(preferredTenantID)
	if preferredTenantID != "" {
		for i := range list {
			if list[i].ID == preferredTenantID || list[i].Code == preferredTenantID {
				return &list[i], list, nil
			}
		}
		return nil, list, errors.New("无权进入该租户")
	}
	if len(list) == 1 {
		return &list[0], list, nil
	}
	for i := range list {
		if list[i].ID == DefaultTenantID || list[i].Code == DefaultTenantCode {
			return &list[i], list, nil
		}
	}
	return nil, list, nil
}
