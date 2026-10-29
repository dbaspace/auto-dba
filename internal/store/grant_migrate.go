package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// GrantMigrateResult 划拨时跨租授权迁移结果。
type GrantMigrateResult struct {
	Migrated int // from 更新为新归属
	Deleted  int // to 已是新归属则删除
}

// migrateResourceGrantsOnAssignTx 资源归属变更后：删除 to=新归属 的冗余 grant，其余将 from 迁到新归属。
func (d *DB) migrateResourceGrantsOnAssignTx(tx *sql.Tx, resourceType, resourceID, newTenantID string) (GrantMigrateResult, error) {
	var res GrantMigrateResult
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	newTenantID = strings.TrimSpace(newTenantID)
	if tx == nil || resourceType == "" || resourceID == "" || newTenantID == "" {
		return res, nil
	}
	del, err := tx.Exec(d.rewrite(`DELETE FROM tbl_tenant_grants
		WHERE resource_type=? AND resource_id=? AND COALESCE(to_tenant_id,'')=?`),
		resourceType, resourceID, newTenantID)
	if err != nil {
		return res, err
	}
	if n, _ := del.RowsAffected(); n > 0 {
		res.Deleted = int(n)
	}
	upd, err := tx.Exec(d.rewrite(`UPDATE tbl_tenant_grants SET from_tenant_id=?
		WHERE resource_type=? AND resource_id=? AND COALESCE(from_tenant_id,'')<>?`),
		newTenantID, resourceType, resourceID, newTenantID)
	if err != nil {
		return res, err
	}
	if n, _ := upd.RowsAffected(); n > 0 {
		res.Migrated = int(n)
	}
	return res, nil
}

// MigrateResourceGrantsOnAssign 非事务入口（自开短事务）。
func (d *DB) MigrateResourceGrantsOnAssign(resourceType, resourceID, newTenantID string) (GrantMigrateResult, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return GrantMigrateResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := d.migrateResourceGrantsOnAssignTx(tx, resourceType, resourceID, newTenantID)
	if err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

func (res GrantMigrateResult) Empty() bool {
	return res.Migrated == 0 && res.Deleted == 0
}

func (res GrantMigrateResult) Add(other GrantMigrateResult) GrantMigrateResult {
	return GrantMigrateResult{Migrated: res.Migrated + other.Migrated, Deleted: res.Deleted + other.Deleted}
}

func formatGrantMigrateAudit(resourceType, resourceID, fromTid, toTid string, res GrantMigrateResult) string {
	return fmt.Sprintf("%s:%s %s->%s migrated=%d deleted=%d",
		strings.TrimSpace(resourceType), strings.TrimSpace(resourceID),
		strings.TrimSpace(fromTid), strings.TrimSpace(toTid), res.Migrated, res.Deleted)
}
