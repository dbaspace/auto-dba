package store

import (
	"database/sql"
	"errors"
	"strings"
)

func tenantClause(tenantID string) (string, []any) {
	tid := strings.TrimSpace(tenantID)
	if tid == "" {
		return "", nil
	}
	return ` AND COALESCE(tenant_id,'')=?`, []any{tid}
}

func appendTenantFilter(query string, args []any, tenantID string) (string, []any) {
	cl, extra := tenantClause(tenantID)
	if cl == "" {
		return query, args
	}
	return query + cl, append(args, extra...)
}

func sameTenant(rowTenant, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	return strings.TrimSpace(rowTenant) == want
}

// RowTenantID 读业务行 tenant_id；不存在或空则返回空串。
func (d *DB) RowTenantID(table, id string) string {
	tid, _ := d.lookupRowTenantID(table, id)
	return tid
}

func (d *DB) lookupRowTenantID(table, id string) (string, error) {
	table = strings.TrimSpace(table)
	id = strings.TrimSpace(id)
	if table == "" || id == "" {
		return "", nil
	}
	var tid sql.NullString
	err := d.QueryRow(`SELECT tenant_id FROM `+table+` WHERE id=?`, id).Scan(&tid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return strings.TrimSpace(tid.String), err
}

func (d *DB) rowVisibleInTenant(table, id, tenantID string) (bool, error) {
	tid, err := d.lookupRowTenantID(table, id)
	if err != nil || tid == "" && tenantID != "" {
		if err != nil {
			return false, err
		}
		// 行不存在
		if tid == "" {
			var n int
			_ = d.QueryRow(`SELECT COUNT(1) FROM `+table+` WHERE id=?`, id).Scan(&n)
			if n == 0 {
				return false, nil
			}
		}
	}
	if tid == "" {
		// 行存在但无 tenant：仅空上下文可见
		return strings.TrimSpace(tenantID) == "", nil
	}
	if sameTenant(tid, tenantID) {
		return true, nil
	}
	return false, nil
}

func ownedInTenant[T any](d *DB, row *T, err error, table, id, tenantID string) (*T, error) {
	if err != nil || row == nil {
		return row, err
	}
	ok, err := d.rowVisibleInTenant(table, id, tenantID)
	if err != nil || !ok {
		return nil, err
	}
	return row, nil
}

func (d *DB) inheritTenantFromInstance(instanceID string) string {
	if strings.TrimSpace(instanceID) == "" {
		return ""
	}
	it, err := d.GetInstance(instanceID)
	if err != nil || it == nil {
		return ""
	}
	return strings.TrimSpace(it.TenantID)
}

func (d *DB) inheritTenantFromHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	list, err := d.ListInstances("", "")
	if err != nil {
		return ""
	}
	for _, it := range list {
		if !strings.EqualFold(strings.TrimSpace(it.Host), host) && !strings.EqualFold(strings.TrimSpace(it.DomainName), host) {
			continue
		}
		if port > 0 && it.Port != port {
			continue
		}
		return strings.TrimSpace(it.TenantID)
	}
	return ""
}
