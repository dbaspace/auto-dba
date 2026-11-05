package store

import (
	"strings"
	"sync"
	"time"
)

// QueryCatalogDB 查询页库树用的已登记库，不含账号密码。
type QueryCatalogDB struct {
	ID         string   `json:"id"`
	DBName     string   `json:"db_name"`
	InstanceID string   `json:"instance_id"`
	Tags       []string `json:"tags"`
}

// QueryCatalogInstance 库树需要的实例字段。不含主机、账号和密码。
type QueryCatalogInstance struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	DBType string `json:"db_type"`
	IDC    string `json:"idc"`
}

type catalogRow struct {
	db   Database
	inst QueryCatalogInstance
}

const catalogSelect = `SELECT d.id, COALESCE(d.tenant_id,''), d.instance_id, d.db_name,
	COALESCE(d.visibility,''), COALESCE(d.access_users,''), COALESCE(d.role_ids,''), COALESCE(d.tags,''),
	i.id, COALESCE(i.name,''), COALESCE(i.db_type,''), COALESCE(i.idc,'')`

const catalogEngineSQL = ` AND lower(COALESCE(i.db_type,'')) NOT IN ('kafka','redis')`

const catalogGrantAliveSQL = ` AND (g.expires_at IS NULL OR g.expires_at='' OR g.expires_at > ?)`

func queryCatalogExcludedEngine(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "kafka", "redis":
		return true
	default:
		return false
	}
}

// ListQueryCatalog 返回当前用户在查询页可见的已登记库。
// 本租户库按 VisibleTo 过滤；显式库授权、实例或域名授权只带出已登记行，不再按域名逐个重查。
func (d *DB) ListQueryCatalog(tenantID, userID, username, jwtRole string) ([]QueryCatalogDB, []QueryCatalogInstance, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" {
		return []QueryCatalogDB{}, []QueryCatalogInstance{}, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var owned, dbGrants, instGrants, domainGrants, viaDomain []catalogRow
	var viewer Viewer
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	run(func() error {
		rows, err := d.listCatalogRows(` FROM tbl_databases d
		JOIN tbl_instances i ON i.id = d.instance_id
		WHERE d.tenant_id=?`+catalogEngineSQL, tenantID)
		owned = rows
		return err
	})
	run(func() error {
		rows, err := d.listCatalogRows(` FROM tbl_tenant_grants g
		JOIN tbl_databases d ON d.id = g.resource_id AND COALESCE(d.tenant_id,'') = g.from_tenant_id
		JOIN tbl_instances i ON i.id = d.instance_id
		WHERE g.resource_type='database' AND (g.to_tenant_id=? OR g.to_user_id=?)`+catalogGrantAliveSQL+catalogEngineSQL, tenantID, userID, now)
		dbGrants = rows
		return err
	})
	run(func() error {
		rows, err := d.listCatalogRows(` FROM tbl_tenant_grants g
		JOIN tbl_instances i ON i.id = g.resource_id AND COALESCE(i.tenant_id,'') = g.from_tenant_id
		JOIN tbl_databases d ON d.instance_id = i.id AND COALESCE(d.tenant_id,'') = g.from_tenant_id
		WHERE g.resource_type='instance' AND (g.to_tenant_id=? OR g.to_user_id=?)
		  AND g.from_tenant_id <> ?`+catalogGrantAliveSQL+catalogEngineSQL, tenantID, userID, tenantID, now)
		instGrants = rows
		return err
	})
	run(func() error {
		rows, err := d.listCatalogRows(` FROM tbl_tenant_grants g
		JOIN tbl_access_domains ad ON ad.id = g.resource_id AND COALESCE(ad.tenant_id,'') = g.from_tenant_id
		JOIN tbl_instances i ON i.id = ad.instance_id
		JOIN tbl_databases d ON d.instance_id = ad.instance_id AND COALESCE(d.tenant_id,'') = g.from_tenant_id
		WHERE g.resource_type='domain' AND (g.to_tenant_id=? OR g.to_user_id=?)
		  AND g.from_tenant_id <> ?`+catalogGrantAliveSQL+catalogEngineSQL, tenantID, userID, tenantID, now)
		domainGrants = rows
		return err
	})
	run(func() error {
		rows, err := d.listCatalogRows(` FROM tbl_access_domains ad
		JOIN tbl_instances i ON i.id = ad.instance_id
		JOIN tbl_databases d ON d.instance_id = ad.instance_id
		WHERE COALESCE(ad.tenant_id,'')=?
		  AND COALESCE(d.tenant_id,'') <> ''
		  AND COALESCE(d.tenant_id,'') <> ?`+catalogEngineSQL, tenantID, tenantID)
		viaDomain = rows
		return err
	})
	run(func() error {
		viewer = d.ViewerOfInTenant(username, jwtRole, tenantID)
		return nil
	})
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	dbs, insts := mergeQueryCatalog(owned, viewer, dbGrants, instGrants, domainGrants, viaDomain)
	return dbs, insts, nil
}

func (d *DB) listCatalogRows(fromAndWhere string, args ...any) ([]catalogRow, error) {
	rows, err := d.Query(catalogSelect+fromAndWhere, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []catalogRow
	for rows.Next() {
		var row catalogRow
		var visibility, accessUsers, roleIDs, tags string
		if err := rows.Scan(
			&row.db.ID, &row.db.TenantID, &row.db.InstanceID, &row.db.DBName,
			&visibility, &accessUsers, &roleIDs, &tags,
			&row.inst.ID, &row.inst.Name, &row.inst.DBType, &row.inst.IDC,
		); err != nil {
			return nil, err
		}
		if row.db.TenantID == "" {
			row.db.TenantID = DefaultTenantID
		}
		row.db.Visibility = strings.TrimSpace(visibility)
		if row.db.Visibility == "" {
			row.db.Visibility = "global"
		}
		row.db.AccessUsers = decodeStringSlice(accessUsers)
		row.db.RoleIDs = decodeStringSlice(roleIDs)
		row.db.Tags = decodeStringSlice(tags)
		if row.db.InstanceID == "" {
			row.db.InstanceID = row.inst.ID
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func mergeQueryCatalog(owned []catalogRow, viewer Viewer, extras ...[]catalogRow) ([]QueryCatalogDB, []QueryCatalogInstance) {
	seen := map[string]struct{}{}
	instSeen := map[string]QueryCatalogInstance{}
	var dbs []QueryCatalogDB
	add := func(row catalogRow) {
		id := strings.TrimSpace(row.db.ID)
		if id == "" || queryCatalogExcludedEngine(row.inst.DBType) {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		tags := row.db.Tags
		if tags == nil {
			tags = []string{}
		}
		dbs = append(dbs, QueryCatalogDB{
			ID: id, DBName: row.db.DBName, InstanceID: row.db.InstanceID, Tags: tags,
		})
		if row.inst.ID != "" {
			instSeen[row.inst.ID] = row.inst
		}
	}
	for _, row := range owned {
		if row.db.VisibleTo(viewer) {
			add(row)
		}
	}
	for _, extra := range extras {
		for _, row := range extra {
			add(row)
		}
	}
	insts := make([]QueryCatalogInstance, 0, len(instSeen))
	for _, inst := range instSeen {
		insts = append(insts, inst)
	}
	if dbs == nil {
		dbs = []QueryCatalogDB{}
	}
	return dbs, insts
}
