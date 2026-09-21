package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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

func scanInstance(scanner interface {
	Scan(dest ...any) error
}) (*Instance, error) {
	var it Instance
	var approvers, domains, tags sql.NullString
	var regionID, envID sql.NullString
	var visibility, accessUsers, roleIDs sql.NullString
	if err := scanner.Scan(
		&it.ID, &it.Name, &it.DomainName, &it.Port, &it.DBType, &it.IDC, &regionID, &envID, &it.Host,
		&it.Username, &it.Password, &approvers, &it.Description, &it.CreatedAt, &it.UpdatedAt, &domains, &tags,
		&it.CloudVendor, &it.CloudInstanceID, &it.CloudRegion,
		&visibility, &accessUsers, &roleIDs,
	); err != nil {
		return nil, err
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

const instanceCols = `id, name, domain_name, port, db_type, idc, region_id, env_id, host, username, password, approvers, description, created_at, updated_at, domains, tags, cloud_vendor, cloud_instance_id, cloud_region, COALESCE(visibility,'global'), COALESCE(access_users,'[]'), COALESCE(role_ids,'[]')`

func (d *DB) ListInstances(dbType, q string) ([]Instance, error) {
	query := `SELECT ` + instanceCols + ` FROM tbl_instances WHERE 1=1`
	var args []any
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

func (d *DB) CreateInstance(it *Instance) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	it.CreatedAt, it.UpdatedAt = now, now
	if it.Approvers == nil {
		it.Approvers = []string{}
	}
	normalizeInstanceDomains(it)
	it.Tags = uniqueTrimmed(it.Tags)
	normalizeInstanceVisibility(it)
	_, err := d.Exec(
		`INSERT INTO tbl_instances(id, name, domain_name, port, db_type, idc, region_id, env_id, host, username, password, approvers, description, created_at, updated_at, domains, tags, cloud_vendor, cloud_instance_id, cloud_region, visibility, access_users, role_ids)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Name, it.DomainName, it.Port, it.DBType, it.IDC, it.RegionID, it.EnvID, it.Host,
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
		`UPDATE tbl_instances SET name=?, domain_name=?, port=?, db_type=?, idc=?, region_id=?, env_id=?, host=?, username=?, password=?, approvers=?, description=?, updated_at=?, domains=?, tags=?, cloud_vendor=?, cloud_instance_id=?, cloud_region=?, visibility=?, access_users=?, role_ids=? WHERE id=?`,
		it.Name, it.DomainName, it.Port, it.DBType, it.IDC, it.RegionID, it.EnvID, it.Host, it.Username, it.Password,
		encodeJSON(it.Approvers), it.Description, it.UpdatedAt, encodeJSON(it.Domains), encodeJSON(it.Tags),
		it.CloudVendor, it.CloudInstanceID, it.CloudRegion, it.Visibility, encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs), it.ID,
	)
	return err
}

func (d *DB) DeleteInstance(id string) error {
	if _, err := d.Exec(`DELETE FROM tbl_databases WHERE instance_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_instances WHERE id=?`, id)
	return err
}
