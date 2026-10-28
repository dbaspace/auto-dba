package store

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// AssignDatabaseTenant 只改库（及该库账号）的归属租户，不改实例 tenant_id / instance_id。
// 用于共享实例：地址留在基础设施租户，业务库可指定给其它租户。
func (d *DB) AssignDatabaseTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" {
		return fmt.Errorf("库必填")
	}
	if err := d.requireActiveTenant(toTenantID); err != nil {
		return err
	}
	dbRow, err := d.GetDatabase(id)
	if err != nil {
		return err
	}
	if dbRow == nil {
		return fmt.Errorf("登记库不存在")
	}
	src, err := d.GetInstance(dbRow.InstanceID)
	if err != nil {
		return err
	}
	if src == nil {
		return fmt.Errorf("所属实例不存在")
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339)
	// 库已属目标租户时仍同步账号，补齐上次漏更的账号归属。
	if strings.TrimSpace(dbRow.TenantID) != toTenantID {
		if _, err := tx.Exec(d.rewrite(`UPDATE tbl_databases SET tenant_id=?, updated_at=? WHERE id=?`),
			toTenantID, now, dbRow.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(d.rewrite(`UPDATE tbl_db_accounts SET tenant_id=?, updated_at=? WHERE database_id=? OR (instance_id=? AND db_name=?)`),
		toTenantID, now, dbRow.ID, dbRow.InstanceID, dbRow.DBName); err != nil {
		return err
	}
	return tx.Commit()
}

func pickSameAddressInstance(src *Instance, hits []Instance) (*Instance, error) {
	if len(hits) == 0 {
		return nil, fmt.Errorf("无同地址实例")
	}
	if len(hits) == 1 {
		return &hits[0], nil
	}
	var named []Instance
	srcName := strings.TrimSpace(src.Name)
	for _, it := range hits {
		if srcName != "" && strings.EqualFold(strings.TrimSpace(it.Name), srcName) {
			named = append(named, it)
		}
	}
	if len(named) == 1 {
		return &named[0], nil
	}
	if len(named) > 1 {
		hits = named
	}
	names := make([]string, 0, len(hits))
	for _, it := range hits {
		names = append(names, it.Name)
	}
	return nil, fmt.Errorf("目标租户有多条同地址实例（%s），请先在实例页合并或指定", strings.Join(names, "、"))
}

func sameAddressInstances(src *Instance, dbRow *Database, cands []Instance) []Instance {
	var out []Instance
	for i := range cands {
		if instanceSameAddress(src, dbRow, &cands[i]) {
			out = append(out, cands[i])
		}
	}
	return out
}

func instanceSameAddress(src *Instance, dbRow *Database, cand *Instance) bool {
	if src == nil || cand == nil || src.ID == cand.ID {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(src.DBType), strings.TrimSpace(cand.DBType)) {
		return false
	}
	srcHosts := instanceHosts(src)
	candHosts := instanceHosts(cand)
	if src.Port == cand.Port && hostsOverlap(srcHosts, candHosts) {
		return true
	}
	if dbRow == nil {
		return false
	}
	for _, raw := range dbRow.Domains {
		h, p := splitHostPortDefault(raw, src.Port)
		if p != cand.Port {
			continue
		}
		if hostIn(h, candHosts) {
			return true
		}
	}
	return false
}

func instanceHosts(it *Instance) []string {
	if it == nil {
		return nil
	}
	var out []string
	add := func(s string) {
		h, _ := splitHostPortDefault(s, it.Port)
		if h != "" {
			out = append(out, h)
		}
	}
	add(it.DomainName)
	add(it.Host)
	for _, d := range it.Domains {
		add(d)
	}
	return out
}

func hostsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, h := range a {
		set[h] = struct{}{}
	}
	for _, h := range b {
		if _, ok := set[h]; ok {
			return true
		}
	}
	return false
}

func hostIn(h string, list []string) bool {
	h = normalizeHost(h)
	if h == "" {
		return false
	}
	for _, x := range list {
		if x == h {
			return true
		}
	}
	return false
}

func splitHostPortDefault(raw string, defaultPort int) (string, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", defaultPort
	}
	if h, p, err := net.SplitHostPort(raw); err == nil {
		port := defaultPort
		if n, e := strconv.Atoi(p); e == nil && n > 0 {
			port = n
		}
		return normalizeHost(h), port
	}
	return normalizeHost(raw), defaultPort
}

func normalizeHost(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Trim(s, "[]")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}
