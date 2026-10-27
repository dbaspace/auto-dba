package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ArgClientAccessEnabled           = "client_access_enabled"
	ArgClientAccessEnforceMachinePAT = "client_access_enforce_machine_pat"

	ClientAccessStatusActive  = "active"
	ClientAccessStatusRevoked = "revoked"

	ClientScopeQuery = "query"
	// ClientScopeCredential 见 harden.go；须 AllowCredentialScope 才写入

	PATPrefix = "adba_pat_"
)

// ClientAccessGrant 用户本地 AI/CLI/MCP 访问授权。
type ClientAccessGrant struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username,omitempty"`
	Display   string `json:"display_name,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	Scopes    string `json:"scopes"` // 逗号分隔，默认 query
	ExpiresAt string `json:"expires_at,omitempty"`
	CreatedBy string `json:"created_by"`
	Status    string `json:"status"`
	Remark    string `json:"remark,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ClientPAT 个人访问令牌（仅存哈希；创建时一次性返回明文）。
type ClientPAT struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	GrantID     string `json:"grant_id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	Scopes      string `json:"scopes"`
	ClientType  string `json:"client_type"` // mcp | cli | local_ai | machine
	ExpiresAt   string `json:"expires_at,omitempty"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	TokenHash   string `json:"-"`
}

// ClientAPIAudit 机器通道接口访问审计。
type ClientAPIAudit struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	TokenID    string `json:"token_id,omitempty"`
	ClientType string `json:"client_type"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Resource   string `json:"resource,omitempty"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent,omitempty"`
	Status     int    `json:"status"`
	LatencyMS  int64  `json:"latency_ms"`
	Message    string `json:"message,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (d *DB) ensureClientAccessSchema() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_client_access_grants (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		scopes TEXT NOT NULL DEFAULT 'query',
		expires_at TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		remark TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	if _, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_client_access_grants_user ON tbl_client_access_grants(user_id, status)`); err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_client_pats (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		grant_id TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		token_prefix TEXT NOT NULL DEFAULT '',
		token_hash TEXT NOT NULL UNIQUE,
		scopes TEXT NOT NULL DEFAULT 'query',
		client_type TEXT NOT NULL DEFAULT 'machine',
		expires_at TEXT NOT NULL DEFAULT '',
		last_used_at TEXT NOT NULL DEFAULT '',
		revoked_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	if _, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_client_pats_user ON tbl_client_pats(user_id)`); err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_client_api_audits (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		token_id TEXT NOT NULL DEFAULT '',
		client_type TEXT NOT NULL DEFAULT '',
		method TEXT NOT NULL DEFAULT '',
		path TEXT NOT NULL DEFAULT '',
		resource TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		status INTEGER NOT NULL DEFAULT 0,
		latency_ms BIGINT NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	if _, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_client_api_audits_created ON tbl_client_api_audits(created_at DESC)`); err != nil {
		return err
	}
	if err := d.ensureQueryAuditClientColumns(); err != nil {
		return err
	}
	return d.ensureCredentialAuditsTable()
}

func (d *DB) ensureQueryAuditClientColumns() error {
	for _, col := range []struct{ name, ddl string }{
		{"client_type", `ALTER TABLE tbl_query_audits ADD COLUMN client_type TEXT NOT NULL DEFAULT ''`},
		{"token_id", `ALTER TABLE tbl_query_audits ADD COLUMN token_id TEXT NOT NULL DEFAULT ''`},
	} {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.columns
			WHERE table_name = 'tbl_query_audits' AND column_name = $1
		`, col.name).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.db.Exec(col.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func NormalizeClientScopes(raw string) string {
	return NormalizeClientScopesOpt(raw, false)
}

// NormalizeClientScopesOpt allowCredential 为 true 时才保留 credential（二期开关）。
func NormalizeClientScopesOpt(raw string, allowCredential bool) string {
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		switch p {
		case ClientScopeQuery:
		case ClientScopeCredential:
			if !allowCredential {
				continue
			}
		default:
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return ClientScopeQuery
	}
	return strings.Join(out, ",")
}

func (d *DB) NormalizeClientScopesLive(raw string) string {
	allow := false
	if d != nil {
		allow = d.AllowCredentialScope()
	}
	return NormalizeClientScopesOpt(raw, allow)
}

func ScopeHas(scopes, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, p := range strings.Split(scopes, ",") {
		if strings.ToLower(strings.TrimSpace(p)) == want {
			return true
		}
	}
	return false
}

func (d *DB) ClientAccessEnabled() bool {
	v, _ := d.GetGlobalArg(ArgClientAccessEnabled)
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (d *DB) ClientAccessEnforceMachinePAT() bool {
	v, _ := d.GetGlobalArg(ArgClientAccessEnforceMachinePAT)
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func grantExpired(expiresAt string) bool {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return time.Now().After(t)
}

func (d *DB) CreateClientAccessGrant(it *ClientAccessGrant) error {
	if it == nil || strings.TrimSpace(it.UserID) == "" {
		return errors.New("user_id required")
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if it.CreatedAt == "" {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	it.Scopes = d.NormalizeClientScopesLive(it.Scopes)
	it.Status = ClientAccessStatusActive
	it.TenantID = strings.TrimSpace(it.TenantID)
	it.ExpiresAt = strings.TrimSpace(it.ExpiresAt)
	it.CreatedBy = strings.TrimSpace(it.CreatedBy)
	it.Remark = strings.TrimSpace(it.Remark)
	_, err := d.Exec(`INSERT INTO tbl_client_access_grants(
		id, user_id, tenant_id, scopes, expires_at, created_by, status, remark, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.TenantID, it.Scopes, it.ExpiresAt, it.CreatedBy, it.Status, it.Remark, it.CreatedAt, it.UpdatedAt)
	return err
}

func (d *DB) RevokeClientAccessGrant(id, by string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("id required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_client_access_grants SET status=?, updated_at=? WHERE id=?`,
		ClientAccessStatusRevoked, now, id)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE tbl_client_pats SET revoked_at=? WHERE grant_id=? AND (revoked_at IS NULL OR TRIM(revoked_at)='')`, now, id)
	return err
}

func (d *DB) GetClientAccessGrant(id string) (*ClientAccessGrant, error) {
	row := d.QueryRow(`SELECT id, user_id, tenant_id, scopes, expires_at, created_by, status, remark, created_at, updated_at
		FROM tbl_client_access_grants WHERE id=?`, strings.TrimSpace(id))
	return scanClientAccessGrant(row)
}

func (d *DB) ListClientAccessGrants(username, status string, page, pageSize int) ([]ClientAccessGrant, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	where := `WHERE 1=1`
	args := []any{}
	if s := strings.TrimSpace(status); s != "" {
		where += ` AND g.status=?`
		args = append(args, s)
	}
	if s := strings.TrimSpace(username); s != "" {
		where += ` AND (u.username LIKE ? OR u.display_name LIKE ?)`
		like := "%" + s + "%"
		args = append(args, like, like)
	}
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_client_access_grants g
		LEFT JOIN tbl_users u ON u.id=g.user_id `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT g.id, g.user_id, COALESCE(u.username,''), COALESCE(u.display_name,''),
		g.tenant_id, g.scopes, g.expires_at, g.created_by, g.status, g.remark, g.created_at, g.updated_at
		FROM tbl_client_access_grants g
		LEFT JOIN tbl_users u ON u.id=g.user_id ` + where + ` ORDER BY g.created_at DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ClientAccessGrant{}
	for rows.Next() {
		var it ClientAccessGrant
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.Display, &it.TenantID, &it.Scopes,
			&it.ExpiresAt, &it.CreatedBy, &it.Status, &it.Remark, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// ActiveClientAccessGrantForUser 返回用户当前有效授权（未吊销且未过期）。
func (d *DB) ActiveClientAccessGrantForUser(userID string) (*ClientAccessGrant, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT id, user_id, tenant_id, scopes, expires_at, created_by, status, remark, created_at, updated_at
		FROM tbl_client_access_grants WHERE user_id=? AND status=? ORDER BY created_at DESC`, userID, ClientAccessStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ClientAccessGrant
		if err := rows.Scan(&it.ID, &it.UserID, &it.TenantID, &it.Scopes, &it.ExpiresAt, &it.CreatedBy, &it.Status, &it.Remark, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		if !grantExpired(it.ExpiresAt) {
			return &it, nil
		}
	}
	return nil, rows.Err()
}

func scanClientAccessGrant(row *sql.Row) (*ClientAccessGrant, error) {
	var it ClientAccessGrant
	err := row.Scan(&it.ID, &it.UserID, &it.TenantID, &it.Scopes, &it.ExpiresAt, &it.CreatedBy, &it.Status, &it.Remark, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func HashClientPAT(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func GenerateClientPATRaw() (raw, prefix string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = PATPrefix + hex.EncodeToString(b)
	prefix = raw
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return raw, prefix, nil
}

func (d *DB) CreateClientPAT(it *ClientPAT, rawToken string) error {
	if it == nil || strings.TrimSpace(it.UserID) == "" || strings.TrimSpace(it.GrantID) == "" {
		return errors.New("user_id and grant_id required")
	}
	if strings.TrimSpace(rawToken) == "" {
		return errors.New("token required")
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.CreatedAt = now
	it.Scopes = d.NormalizeClientScopesLive(it.Scopes)
	it.ClientType = strings.ToLower(strings.TrimSpace(it.ClientType))
	if it.ClientType == "" {
		it.ClientType = "machine"
	}
	it.Name = strings.TrimSpace(it.Name)
	if it.Name == "" {
		it.Name = "default"
	}
	it.TokenHash = HashClientPAT(rawToken)
	if it.TokenPrefix == "" {
		p := rawToken
		if len(p) > 16 {
			p = p[:16]
		}
		it.TokenPrefix = p
	}
	_, err := d.Exec(`INSERT INTO tbl_client_pats(
		id, user_id, grant_id, name, token_prefix, token_hash, scopes, client_type, expires_at, last_used_at, revoked_at, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.GrantID, it.Name, it.TokenPrefix, it.TokenHash, it.Scopes, it.ClientType,
		strings.TrimSpace(it.ExpiresAt), "", "", it.CreatedAt)
	return err
}

func (d *DB) ListClientPATsByUser(userID string) ([]ClientPAT, error) {
	rows, err := d.Query(`SELECT id, user_id, grant_id, name, token_prefix, scopes, client_type, expires_at, last_used_at, revoked_at, created_at
		FROM tbl_client_pats WHERE user_id=? ORDER BY created_at DESC`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClientPAT{}
	for rows.Next() {
		var it ClientPAT
		if err := rows.Scan(&it.ID, &it.UserID, &it.GrantID, &it.Name, &it.TokenPrefix, &it.Scopes, &it.ClientType,
			&it.ExpiresAt, &it.LastUsedAt, &it.RevokedAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) RevokeClientPAT(id, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(`UPDATE tbl_client_pats SET revoked_at=? WHERE id=? AND user_id=? AND (revoked_at IS NULL OR TRIM(revoked_at)='')`,
		now, strings.TrimSpace(id), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// LookupValidClientPAT 按明文令牌查找未吊销、未过期且授权仍有效的 PAT。
func (d *DB) LookupValidClientPAT(raw string) (*ClientPAT, *ClientAccessGrant, *User, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, PATPrefix) {
		return nil, nil, nil, nil
	}
	hash := HashClientPAT(raw)
	var it ClientPAT
	err := d.QueryRow(`SELECT id, user_id, grant_id, name, token_prefix, token_hash, scopes, client_type, expires_at, last_used_at, revoked_at, created_at
		FROM tbl_client_pats WHERE token_hash=?`, hash).Scan(
		&it.ID, &it.UserID, &it.GrantID, &it.Name, &it.TokenPrefix, &it.TokenHash, &it.Scopes, &it.ClientType,
		&it.ExpiresAt, &it.LastUsedAt, &it.RevokedAt, &it.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(it.RevokedAt) != "" {
		return nil, nil, nil, errors.New("token revoked")
	}
	if grantExpired(it.ExpiresAt) {
		return nil, nil, nil, errors.New("token expired")
	}
	g, err := d.GetClientAccessGrant(it.GrantID)
	if err != nil {
		return nil, nil, nil, err
	}
	if g == nil || g.Status != ClientAccessStatusActive || grantExpired(g.ExpiresAt) {
		return nil, nil, nil, errors.New("access grant inactive")
	}
	u, err := d.GetUserByID(it.UserID)
	if err != nil || u == nil {
		return nil, nil, nil, errors.New("user not found")
	}
	if u.IsLoginBlocked() {
		return nil, nil, nil, errors.New(u.LoginBlockedMessage())
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = d.Exec(`UPDATE tbl_client_pats SET last_used_at=? WHERE id=?`, now, it.ID)
	return &it, g, u, nil
}

func (d *DB) CreateClientAPIAudit(it *ClientAPIAudit) error {
	if it == nil {
		return nil
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if len(it.UserAgent) > 512 {
		it.UserAgent = it.UserAgent[:512]
	}
	it.Message = TruncateQueryAuditStatement(it.Message)
	_, err := d.Exec(`INSERT INTO tbl_client_api_audits(
		id, user_id, username, token_id, client_type, method, path, resource, ip, user_agent, status, latency_ms, message, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.Username, it.TokenID, it.ClientType, it.Method, it.Path, it.Resource,
		it.IP, it.UserAgent, it.Status, it.LatencyMS, it.Message, it.CreatedAt)
	return err
}

type ClientAPIAuditFilter struct {
	Username   string
	ClientType string
	Page       int
	PageSize   int
}

func (d *DB) ListClientAPIAudits(f ClientAPIAuditFilter) ([]ClientAPIAudit, int, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	where := `WHERE 1=1`
	args := []any{}
	if s := strings.TrimSpace(f.Username); s != "" {
		where += ` AND username LIKE ?`
		args = append(args, "%"+s+"%")
	}
	if s := strings.TrimSpace(f.ClientType); s != "" {
		where += ` AND client_type=?`
		args = append(args, strings.ToLower(s))
	}
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_client_api_audits `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id, user_id, username, token_id, client_type, method, path, resource, ip, user_agent, status, latency_ms, message, created_at
		FROM tbl_client_api_audits ` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ClientAPIAudit{}
	for rows.Next() {
		var it ClientAPIAudit
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.TokenID, &it.ClientType, &it.Method, &it.Path, &it.Resource,
			&it.IP, &it.UserAgent, &it.Status, &it.LatencyMS, &it.Message, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}
