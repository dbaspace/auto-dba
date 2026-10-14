package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AccessDomain 访问域名资源（由实例 domains / domain_name 同步，可按租户划拨）。
type AccessDomain struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	TenantName   string `json:"tenant_name,omitempty"`
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name,omitempty"` // 运行时填充，非表列
	Domain       string `json:"domain"`
	EnvCode      string `json:"env_code"`
	DBType       string `json:"db_type"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// SyncAccessDomainsResult 同步统计。
type SyncAccessDomainsResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
	Skipped int `json:"skipped"`
}

const accessDomainCols = `id, COALESCE(tenant_id,''), COALESCE(instance_id,''), domain, COALESCE(env_code,''), COALESCE(db_type,''), created_at, updated_at`

func normalizeAccessDomainHost(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ".")
	return strings.ToLower(s)
}

// NormalizeEngineType 统一引擎别名，避免 mysql/pg 串写或唯一键冲突。
func NormalizeEngineType(dbType string) string {
	n := strings.ToLower(strings.TrimSpace(dbType))
	switch n {
	case "postgresql", "pg", "pgsql":
		return "postgres"
	case "mssql":
		return "sqlserver"
	case "cockroach", "cockroadchdb":
		return "cockroachdb"
	default:
		return n
	}
}

func instanceDomainList(it *Instance) []string {
	if it == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(it.Domains)+1)
	add := func(raw string) {
		h := normalizeAccessDomainHost(raw)
		if h == "" {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	for _, d := range it.Domains {
		add(d)
	}
	add(it.DomainName)
	return out
}

func (d *DB) ensureAccessDomainsTable() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_access_domains (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		domain TEXT NOT NULL,
		env_code TEXT NOT NULL DEFAULT '',
		db_type TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(domain, db_type)
	)`); err != nil {
		return err
	}
	if _, err := d.Exec(`CREATE INDEX IF NOT EXISTS idx_access_domains_tenant ON tbl_access_domains(tenant_id)`); err != nil {
		return err
	}
	if _, err := d.Exec(`CREATE INDEX IF NOT EXISTS idx_access_domains_instance ON tbl_access_domains(instance_id)`); err != nil {
		return err
	}
	_, err := d.Exec(`CREATE INDEX IF NOT EXISTS idx_access_domains_env_type ON tbl_access_domains(env_code, db_type)`)
	return err
}

func scanAccessDomain(row interface{ Scan(dest ...any) error }) (*AccessDomain, error) {
	var it AccessDomain
	if err := row.Scan(&it.ID, &it.TenantID, &it.InstanceID, &it.Domain, &it.EnvCode, &it.DBType, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	return &it, nil
}

func (d *DB) GetAccessDomain(id string) (*AccessDomain, error) {
	it, err := scanAccessDomain(d.QueryRow(`SELECT `+accessDomainCols+` FROM tbl_access_domains WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) GetAccessDomainByDomainType(domain, dbType string) (*AccessDomain, error) {
	domain = normalizeAccessDomainHost(domain)
	dbType = NormalizeEngineType(dbType)
	if domain == "" || dbType == "" {
		return nil, nil
	}
	it, err := scanAccessDomain(d.QueryRow(
		`SELECT `+accessDomainCols+` FROM tbl_access_domains WHERE domain=? AND db_type=?`,
		domain, dbType,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) ListAccessDomains(dbType, envCode, q string) ([]AccessDomain, error) {
	dbType = NormalizeEngineType(dbType)
	envCode = strings.TrimSpace(envCode)
	q = strings.TrimSpace(q)
	query := `SELECT ` + accessDomainCols + ` FROM tbl_access_domains WHERE 1=1`
	var args []any
	if dbType != "" {
		query += ` AND db_type=?`
		args = append(args, dbType)
	}
	if envCode != "" {
		query += ` AND env_code=?`
		args = append(args, envCode)
	}
	if q != "" {
		query += ` AND (domain LIKE ? OR instance_id LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY env_code, domain`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessDomain
	for rows.Next() {
		it, err := scanAccessDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) ListAccessDomainsInTenant(tenantID, dbType, envCode, q string) ([]AccessDomain, error) {
	tenantID = strings.TrimSpace(tenantID)
	dbType = NormalizeEngineType(dbType)
	envCode = strings.TrimSpace(envCode)
	q = strings.TrimSpace(q)
	query := `SELECT ` + accessDomainCols + ` FROM tbl_access_domains WHERE COALESCE(tenant_id,'')=?`
	args := []any{tenantID}
	if dbType != "" {
		query += ` AND db_type=?`
		args = append(args, dbType)
	}
	if envCode != "" {
		query += ` AND env_code=?`
		args = append(args, envCode)
	}
	if q != "" {
		query += ` AND (domain LIKE ? OR instance_id LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY env_code, domain`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessDomain
	for rows.Next() {
		it, err := scanAccessDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// InstanceReachableViaAccessDomain 本租户是否通过域名归属或 domain grant 可见该实例。
func (d *DB) InstanceReachableViaAccessDomain(instanceID, tenantID, userID string) (bool, error) {
	instanceID = strings.TrimSpace(instanceID)
	tenantID = strings.TrimSpace(tenantID)
	if instanceID == "" || tenantID == "" {
		return false, nil
	}
	var n int
	if err := d.QueryRow(
		`SELECT COUNT(1) FROM tbl_access_domains WHERE instance_id=? AND COALESCE(tenant_id,'')=?`,
		instanceID, tenantID,
	).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	gs, err := d.ListTenantGrantsTo(tenantID, userID)
	if err != nil {
		return false, err
	}
	for _, g := range gs {
		if g.ResourceType != "domain" {
			continue
		}
		it, err := d.GetAccessDomain(g.ResourceID)
		if err != nil || it == nil {
			continue
		}
		if strings.TrimSpace(it.InstanceID) == instanceID {
			return true, nil
		}
	}
	return false, nil
}

func (d *DB) ListGrantedAccessDomains(toTenantID, toUserID, dbType, envCode, q string) ([]AccessDomain, error) {
	gs, err := d.ListTenantGrantsTo(toTenantID, toUserID)
	if err != nil {
		return nil, err
	}
	dbType = NormalizeEngineType(dbType)
	envCode = strings.TrimSpace(envCode)
	q = strings.TrimSpace(q)
	var out []AccessDomain
	seen := map[string]struct{}{}
	for _, g := range gs {
		if g.ResourceType != "domain" {
			continue
		}
		if _, ok := seen[g.ResourceID]; ok {
			continue
		}
		it, err := d.GetAccessDomain(g.ResourceID)
		if err != nil || it == nil {
			continue
		}
		if dbType != "" && NormalizeEngineType(it.DBType) != dbType {
			continue
		}
		if envCode != "" && it.EnvCode != envCode {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Domain), strings.ToLower(q)) {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, *it)
	}
	return out, nil
}

func (d *DB) AssignAccessDomainTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" || toTenantID == "" {
		return fmt.Errorf("域名与目标租户必填")
	}
	if t, err := d.GetTenant(toTenantID); err != nil || t == nil {
		return fmt.Errorf("目标租户不存在")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(`UPDATE tbl_access_domains SET tenant_id=?, updated_at=? WHERE id=?`, toTenantID, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("域名不存在")
	}
	return nil
}

// SyncAccessDomainsFromInstance 按实例 domains 同步；新行归属默认租户；禁止跨 db_type 覆盖。
func (d *DB) SyncAccessDomainsFromInstance(it *Instance) (SyncAccessDomainsResult, error) {
	var res SyncAccessDomainsResult
	if it == nil || strings.TrimSpace(it.ID) == "" {
		return res, fmt.Errorf("实例无效")
	}
	dbType := NormalizeEngineType(it.DBType)
	if dbType == "" {
		return res, fmt.Errorf("实例类型无效")
	}
	domains := instanceDomainList(it)
	want := map[string]struct{}{}
	for _, h := range domains {
		want[h] = struct{}{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	envCode := strings.TrimSpace(it.IDC)

	for _, host := range domains {
		exist, err := d.GetAccessDomainByDomainType(host, dbType)
		if err != nil {
			return res, err
		}
		if exist == nil {
			row := &AccessDomain{
				ID:         uuid.NewString(),
				TenantID:   DefaultTenantID,
				InstanceID: it.ID,
				Domain:     host,
				EnvCode:    envCode,
				DBType:     dbType,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if _, err := d.Exec(
				`INSERT INTO tbl_access_domains(id, tenant_id, instance_id, domain, env_code, db_type, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?)`,
				row.ID, row.TenantID, row.InstanceID, row.Domain, row.EnvCode, row.DBType, row.CreatedAt, row.UpdatedAt,
			); err != nil {
				return res, err
			}
			res.Created++
			continue
		}
		// 已存在同 domain+db_type：更新 instance/env，保留 tenant_id
		if _, err := d.Exec(
			`UPDATE tbl_access_domains SET instance_id=?, env_code=?, updated_at=? WHERE id=?`,
			it.ID, envCode, now, exist.ID,
		); err != nil {
			return res, err
		}
		res.Updated++
	}

	// 删除本实例下、源列表已不存在的域名行
	rows, err := d.Query(
		`SELECT id, domain FROM tbl_access_domains WHERE instance_id=? AND db_type=?`,
		it.ID, dbType,
	)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	var staleIDs []string
	for rows.Next() {
		var id, domain string
		if err := rows.Scan(&id, &domain); err != nil {
			return res, err
		}
		if _, ok := want[normalizeAccessDomainHost(domain)]; !ok {
			staleIDs = append(staleIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, id := range staleIDs {
		if _, err := d.Exec(`DELETE FROM tbl_access_domains WHERE id=?`, id); err != nil {
			return res, err
		}
		res.Deleted++
	}
	return res, nil
}

// SyncAllAccessDomains 一键同步；dbTypeFilter 非空时只扫该类型实例。
func (d *DB) SyncAllAccessDomains(dbTypeFilter string) (SyncAccessDomainsResult, error) {
	var total SyncAccessDomainsResult
	filter := NormalizeEngineType(dbTypeFilter)
	list, err := d.ListInstances("", "")
	if err != nil {
		return total, err
	}
	for i := range list {
		it := &list[i]
		if filter != "" && NormalizeEngineType(it.DBType) != filter {
			total.Skipped++
			continue
		}
		if NormalizeEngineType(it.DBType) == "" {
			total.Skipped++
			continue
		}
		part, err := d.SyncAccessDomainsFromInstance(it)
		if err != nil {
			return total, fmt.Errorf("实例 %s: %w", it.Name, err)
		}
		total.Created += part.Created
		total.Updated += part.Updated
		total.Deleted += part.Deleted
	}
	return total, nil
}

// BackfillAccessDomainsFromInstances 首次回填（与全量同步同语义）。
func (d *DB) BackfillAccessDomainsFromInstances() (SyncAccessDomainsResult, error) {
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_access_domains`).Scan(&n); err != nil {
		return SyncAccessDomainsResult{}, err
	}
	if n > 0 {
		return SyncAccessDomainsResult{}, nil
	}
	return d.SyncAllAccessDomains("")
}

func (d *DB) DeleteAccessDomainsByInstance(instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}
	_, err := d.Exec(`DELETE FROM tbl_access_domains WHERE instance_id=?`, instanceID)
	return err
}
