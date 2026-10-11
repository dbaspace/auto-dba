package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

func encodeJSON(v any) string {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return "[]"
	}
	return string(b)
}

func decodeStringSlice(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	return out
}

func IsHostInstanceType(dbType string) bool {
	t := strings.ToLower(strings.TrimSpace(dbType))
	return t == "host" || t == "os" || t == "主机"
}

func normalizeInstanceVisibility(it *Instance) {
	if it == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(it.Visibility), "private") {
		it.Visibility = "private"
	} else {
		it.Visibility = "global"
	}
	it.AccessUsers = uniqueTrimmed(it.AccessUsers)
	it.RoleIDs = uniqueTrimmed(it.RoleIDs)
	if !IsHostInstanceType(it.DBType) {
		// 非主机类型保持全局，避免误伤现有库实例
		it.Visibility = "global"
		it.AccessUsers = []string{}
		it.RoleIDs = []string{}
	}
}

func (it Instance) VisibleTo(v Viewer) bool {
	if !IsHostInstanceType(it.DBType) {
		return true
	}
	if v.Privileged || !strings.EqualFold(it.Visibility, "private") {
		return true
	}
	for _, name := range it.AccessUsers {
		if strings.EqualFold(strings.TrimSpace(name), v.Username) {
			return true
		}
	}
	roleSet := map[string]bool{}
	for _, id := range v.RoleIDs {
		if id != "" {
			roleSet[id] = true
		}
	}
	if v.RoleID != "" {
		roleSet[v.RoleID] = true
	}
	if len(roleSet) == 0 {
		return false
	}
	for _, id := range it.RoleIDs {
		if roleSet[id] {
			return true
		}
	}
	return false
}

func FilterVisibleInstances(list []Instance, v Viewer) []Instance {
	if list == nil {
		return []Instance{}
	}
	out := make([]Instance, 0, len(list))
	for _, it := range list {
		if it.VisibleTo(v) {
			out = append(out, it)
		}
	}
	return out
}

// FilterVisibleInstancesByTenant 按每行 tenant_id 构造 Viewer 过滤（审计多租列表用）。
func FilterVisibleInstancesByTenant(list []Instance, viewerFor func(tenantID string) Viewer) []Instance {
	if list == nil {
		return []Instance{}
	}
	cache := map[string]Viewer{}
	out := make([]Instance, 0, len(list))
	for _, it := range list {
		tid := strings.TrimSpace(it.TenantID)
		if tid == "" {
			tid = DefaultTenantID
		}
		v, ok := cache[tid]
		if !ok {
			v = viewerFor(tid)
			cache[tid] = v
		}
		if it.VisibleTo(v) {
			out = append(out, it)
		}
	}
	return out
}

func scanInstance(scanner interface {
	Scan(dest ...any) error
}) (*Instance, error) {
	var it Instance
	var approvers, domains, tags sql.NullString
	var regionID, envID, tenantID sql.NullString
	var visibility, accessUsers, roleIDs sql.NullString
	if err := scanner.Scan(
		&it.ID, &tenantID, &it.Name, &it.DomainName, &it.Port, &it.DBType, &it.IDC, &regionID, &envID, &it.Host,
		&it.Username, &it.Password, &approvers, &it.Description, &it.CreatedAt, &it.UpdatedAt, &domains, &tags,
		&it.CloudVendor, &it.CloudInstanceID, &it.CloudRegion,
		&visibility, &accessUsers, &roleIDs,
	); err != nil {
		return nil, err
	}
	it.TenantID = tenantID.String
	if it.TenantID == "" {
		it.TenantID = DefaultTenantID
	}
	it.RegionID = regionID.String
	it.EnvID = envID.String
	it.Approvers = decodeStringSlice(approvers.String)
	it.Domains = decodeStringSlice(domains.String)
	it.Tags = uniqueTrimmed(decodeStringSlice(tags.String))
	it.Visibility = visibility.String
	it.AccessUsers = decodeStringSlice(accessUsers.String)
	it.RoleIDs = decodeStringSlice(roleIDs.String)
	normalizeInstanceVisibility(&it)
	normalizeInstanceDomains(&it)
	return &it, nil
}

func uniqueTrimmed(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func InstanceDomainList(it *Instance) []string {
	if it == nil {
		return []string{}
	}
	normalizeInstanceDomains(it)
	return append([]string{}, it.Domains...)
}

func ValidateDatabaseDomains(inst *Instance, domains []string) ([]string, error) {
	allow := map[string]struct{}{}
	for _, d := range InstanceDomainList(inst) {
		allow[d] = struct{}{}
	}
	out := uniqueTrimmed(domains)
	if len(allow) == 0 {
		return nil, errors.New("实例未配置域名，请先在「配置 → 实例」中绑定域名")
	}
	if len(out) == 0 {
		return nil, errors.New("请绑定域名")
	}
	for _, d := range out {
		if _, ok := allow[d]; !ok {
			return nil, errors.New("域名不属于该实例: " + d)
		}
	}
	return out, nil
}

func normalizeInstanceDomains(it *Instance) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(it.Domains)+1)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range it.Domains {
		add(s)
	}
	if len(out) == 0 {
		add(it.DomainName)
	}
	it.Domains = out
	if it.DomainName == "" && len(out) > 0 {
		it.DomainName = out[0]
	}
}

const instanceCols = `id, COALESCE(tenant_id,''), name, domain_name, port, db_type, idc, region_id, env_id, host, username, password, approvers, description, created_at, updated_at, domains, tags, cloud_vendor, cloud_instance_id, cloud_region, COALESCE(visibility,'global'), COALESCE(access_users,'[]'), COALESCE(role_ids,'[]')`

// GetInstanceUsableInTenant 本租户拥有该实例，或该实例上有本租户的已登记库。
func (d *DB) GetInstanceUsableInTenant(id, tenantID string) (*Instance, error) {
	it, err := d.GetInstance(id)
	if err != nil || it == nil {
		return it, err
	}
	tid := strings.TrimSpace(tenantID)
	if tid == "" || strings.TrimSpace(it.TenantID) == tid {
		return it, nil
	}
	n, err := d.CountDatabasesOnInstanceInTenant(id, tid)
	if err != nil || n == 0 {
		return nil, err
	}
	return it, nil
}

func (d *DB) ListInstances(dbType, q string) ([]Instance, error) {
	return d.ListInstancesInTenant("", dbType, q)
}

func (d *DB) ListInstancesInTenant(tenantID, dbType, q string) ([]Instance, error) {
	query := `SELECT ` + instanceCols + ` FROM tbl_instances WHERE 1=1`
	var args []any
	if tid := strings.TrimSpace(tenantID); tid != "" {
		query += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	if strings.TrimSpace(dbType) != "" {
		query += ` AND db_type=?`
		args = append(args, dbType)
	}
	if strings.TrimSpace(q) != "" {
		like := "%" + strings.TrimSpace(q) + "%"
		query += ` AND (name LIKE ? OR domain_name LIKE ? OR host LIKE ? OR COALESCE(domains,'') LIKE ? OR COALESCE(tags,'') LIKE ?)`
		args = append(args, like, like, like, like, like)
	}
	query += ` ORDER BY name`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Instance
	for rows.Next() {
		it, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetInstance(id string) (*Instance, error) {
	row := d.QueryRow(`SELECT `+instanceCols+` FROM tbl_instances WHERE id=?`, id)
	it, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

// GetInstanceInTenant 按租户取实例；不存在或不属本租户返回 nil（防串租不暴露存在性）。
func (d *DB) GetInstanceInTenant(id, tenantID string) (*Instance, error) {
	it, err := d.GetInstance(id)
	if err != nil || it == nil {
		return it, err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" && it.TenantID != tenantID {
		return nil, nil
	}
	return it, nil
}

func (d *DB) CreateInstance(it *Instance) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if strings.TrimSpace(it.TenantID) == "" {
		it.TenantID = DefaultTenantID
	}
	it.CreatedAt, it.UpdatedAt = now, now
	if it.Approvers == nil {
		it.Approvers = []string{}
	}
	normalizeInstanceDomains(it)
	it.Tags = uniqueTrimmed(it.Tags)
	normalizeInstanceVisibility(it)
	_, err := d.Exec(
		`INSERT INTO tbl_instances(id, tenant_id, name, domain_name, port, db_type, idc, region_id, env_id, host, username, password, approvers, description, created_at, updated_at, domains, tags, cloud_vendor, cloud_instance_id, cloud_region, visibility, access_users, role_ids)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.TenantID, it.Name, it.DomainName, it.Port, it.DBType, it.IDC, it.RegionID, it.EnvID, it.Host,
		it.Username, it.Password, encodeJSON(it.Approvers), it.Description, it.CreatedAt, it.UpdatedAt, encodeJSON(it.Domains), encodeJSON(it.Tags),
		it.CloudVendor, it.CloudInstanceID, it.CloudRegion, it.Visibility, encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs),
	)
	return err
}

func (d *DB) UpdateInstance(it *Instance) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	normalizeInstanceDomains(it)
	it.Tags = uniqueTrimmed(it.Tags)
	normalizeInstanceVisibility(it)
	_, err := d.Exec(
		`UPDATE tbl_instances SET name=?, domain_name=?, port=?, db_type=?, idc=?, region_id=?, env_id=?, host=?, username=?, password=?, approvers=?, description=?, updated_at=?, domains=?, tags=?, cloud_vendor=?, cloud_instance_id=?, cloud_region=?, visibility=?, access_users=?, role_ids=? WHERE id=? AND COALESCE(tenant_id,?)=?`,
		it.Name, it.DomainName, it.Port, it.DBType, it.IDC, it.RegionID, it.EnvID, it.Host, it.Username, it.Password,
		encodeJSON(it.Approvers), it.Description, it.UpdatedAt, encodeJSON(it.Domains), encodeJSON(it.Tags),
		it.CloudVendor, it.CloudInstanceID, it.CloudRegion, it.Visibility, encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs),
		it.ID, it.TenantID, it.TenantID,
	)
	if err == nil {
		d.MaybeSnapshotInstance(it, "")
	}
	return err
}

// AssignInstanceTenant 将实例及下属已登记库/账号划拨到目标租户。
func (d *DB) AssignInstanceTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" || toTenantID == "" {
		return fmt.Errorf("实例与目标租户必填")
	}
	t, err := d.GetTenant(toTenantID)
	if err != nil {
		return err
	}
	if t == nil || t.Status != TenantStatusActive {
		return fmt.Errorf("目标租户不存在或已停用")
	}
	it, err := d.GetInstance(id)
	if err != nil {
		return err
	}
	if it == nil {
		return fmt.Errorf("实例不存在")
	}
	if strings.TrimSpace(it.TenantID) == toTenantID {
		return nil
	}
	var n int
	err = d.QueryRow(
		`SELECT COUNT(1) FROM tbl_instances WHERE COALESCE(tenant_id,'')=? AND domain_name=? AND port=? AND id<>?`,
		toTenantID, it.DomainName, it.Port, id,
	).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("目标租户已存在相同域名+端口的实例（%s:%d），无法划拨", it.DomainName, it.Port)
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(d.rewrite(`UPDATE tbl_instances SET tenant_id=?, updated_at=? WHERE id=?`),
		toTenantID, now, id); err != nil {
		return err
	}
	if err := d.reassignInstanceOwnedCatalogTx(tx, id, it.TenantID, toTenantID, now); err != nil {
		return err
	}
	if err := assignLinkedDeployClustersTx(tx, d, it, toTenantID, now); err != nil {
		return err
	}
	return tx.Commit()
}

// assignInstanceTenantOnly 只迁实例及库/账号，不回头改部署集群（供集群划拨调用，避免递归）。
func (d *DB) assignInstanceTenantOnly(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" || toTenantID == "" {
		return fmt.Errorf("实例与目标租户必填")
	}
	t, err := d.GetTenant(toTenantID)
	if err != nil {
		return err
	}
	if t == nil || t.Status != TenantStatusActive {
		return fmt.Errorf("目标租户不存在或已停用")
	}
	it, err := d.GetInstance(id)
	if err != nil {
		return err
	}
	if it == nil {
		return fmt.Errorf("实例不存在")
	}
	if strings.TrimSpace(it.TenantID) == toTenantID {
		return nil
	}
	var n int
	err = d.QueryRow(
		`SELECT COUNT(1) FROM tbl_instances WHERE COALESCE(tenant_id,'')=? AND domain_name=? AND port=? AND id<>?`,
		toTenantID, it.DomainName, it.Port, id,
	).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("目标租户已存在相同域名+端口的实例（%s:%d），无法划拨", it.DomainName, it.Port)
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(d.rewrite(`UPDATE tbl_instances SET tenant_id=?, updated_at=? WHERE id=?`),
		toTenantID, now, id); err != nil {
		return err
	}
	if err := d.reassignInstanceOwnedCatalogTx(tx, id, it.TenantID, toTenantID, now); err != nil {
		return err
	}
	return tx.Commit()
}

// reassignInstanceOwnedCatalogTx 只迁移仍归属 fromTid（或空）的库与对应账号，异租库不动。
func (d *DB) reassignInstanceOwnedCatalogTx(tx *sql.Tx, instanceID, fromTid, toTid, now string) error {
	if _, err := tx.Exec(d.rewrite(`UPDATE tbl_databases SET tenant_id=?, updated_at=? WHERE instance_id=? AND (COALESCE(tenant_id,'')=? OR COALESCE(tenant_id,'')='')`),
		toTid, now, instanceID, fromTid); err != nil {
		return err
	}
	_, err := tx.Exec(d.rewrite(`UPDATE tbl_db_accounts SET tenant_id=?, updated_at=? WHERE instance_id=?
		AND (COALESCE(tenant_id,'')=? OR COALESCE(tenant_id,'')='')
		AND (
			COALESCE(database_id,'') IN (SELECT id FROM tbl_databases WHERE instance_id=? AND COALESCE(tenant_id,'')=?)
			OR (COALESCE(database_id,'')='' AND db_name IN (SELECT db_name FROM tbl_databases WHERE instance_id=? AND COALESCE(tenant_id,'')=?))
			OR NOT EXISTS (SELECT 1 FROM tbl_databases d WHERE d.instance_id=tbl_db_accounts.instance_id AND d.db_name=tbl_db_accounts.db_name)
		)`),
		toTid, now, instanceID, fromTid, instanceID, toTid, instanceID, toTid)
	return err
}

const tagSrClusterPrefix = "sr-cluster="
const tagHAFromDbDeploy = "ha-from-dbdeploy"

func srClusterIDFromTags(tags []string) string {
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(t), tagSrClusterPrefix) {
			return strings.TrimSpace(t[len(tagSrClusterPrefix):])
		}
	}
	return ""
}

func hasHAFromDbDeployTag(tags []string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), tagHAFromDbDeploy) {
			return true
		}
	}
	return false
}

func assignLinkedDeployClustersTx(tx *sql.Tx, d *DB, it *Instance, toTenantID, now string) error {
	if it == nil {
		return nil
	}
	if cid := srClusterIDFromTags(it.Tags); cid != "" {
		if _, err := tx.Exec(d.rewrite(`UPDATE tbl_sr_clusters SET tenant_id=?, updated_at=? WHERE id=?`),
			toTenantID, now, cid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(d.rewrite(`UPDATE tbl_db_deploy_clusters SET tenant_id=?, updated_at=? WHERE linked_instance_id=?`),
		toTenantID, now, it.ID); err != nil {
		return err
	}
	if hasHAFromDbDeployTag(it.Tags) && strings.TrimSpace(it.Host) != "" && it.Port > 0 {
		if _, err := tx.Exec(d.rewrite(`UPDATE tbl_db_deploy_clusters SET tenant_id=?, updated_at=?
			WHERE id IN (SELECT cluster_id FROM tbl_db_deploy_nodes WHERE LOWER(TRIM(host))=LOWER(TRIM(?)) AND port=?)`),
			toTenantID, now, it.Host, it.Port); err != nil {
			return err
		}
	}
	return nil
}

// RealignDeployInstanceTenants 把带 sr-cluster= / ha-from-dbdeploy 标签的实例拉回所属集群租户。
func (d *DB) RealignDeployInstanceTenants() error {
	rows, err := d.Query(`SELECT ` + instanceCols + ` FROM tbl_instances WHERE COALESCE(tags,'') <> ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var insts []Instance
	for rows.Next() {
		it, err := scanInstance(rows)
		if err != nil {
			return err
		}
		if srClusterIDFromTags(it.Tags) != "" || hasHAFromDbDeployTag(it.Tags) {
			insts = append(insts, *it)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range insts {
		it := &insts[i]
		want := ""
		if cid := srClusterIDFromTags(it.Tags); cid != "" {
			want = d.RowTenantID("tbl_sr_clusters", cid)
		}
		if want == "" && hasHAFromDbDeployTag(it.Tags) {
			_ = d.QueryRow(`SELECT COALESCE(c.tenant_id,'') FROM tbl_db_deploy_clusters c
				WHERE c.linked_instance_id=?
				   OR c.id IN (SELECT cluster_id FROM tbl_db_deploy_nodes WHERE LOWER(TRIM(host))=LOWER(TRIM(?)) AND port=?)
				LIMIT 1`, it.ID, it.Host, it.Port).Scan(&want)
			want = strings.TrimSpace(want)
		}
		if want == "" || want == strings.TrimSpace(it.TenantID) {
			continue
		}
		if err := d.AssignInstanceTenant(it.ID, want); err != nil {
			log.Printf("tenancy: realign instance %s (%s) -> %s: %v", it.ID, it.Name, want, err)
		}
	}
	return nil
}

func (d *DB) DeleteInstance(id string) error {
	return d.DeleteInstanceInTenant(id, "")
}

func (d *DB) DeleteInstanceInTenant(id, tenantID string) error {
	if strings.TrimSpace(tenantID) != "" {
		it, err := d.GetInstanceInTenant(id, tenantID)
		if err != nil {
			return err
		}
		if it == nil {
			return nil
		}
	}
	if _, err := d.Exec(`DELETE FROM tbl_databases WHERE instance_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_instances WHERE id=?`, id)
	return err
}
