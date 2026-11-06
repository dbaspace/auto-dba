package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const connectorCols = `id, dbz_host, env, kafka_host, message_schema_mode, remark, created_at, updated_at`

func NormalizeMessageSchemaMode(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "with_schema") {
		return "with_schema"
	}
	return "without_schema"
}

func scanConnector(scanner interface {
	Scan(dest ...any) error
}) (*Connector, error) {
	var it Connector
	var remark sql.NullString
	if err := scanner.Scan(&it.ID, &it.DBZHost, &it.Env, &it.KafkaHost, &it.MessageSchemaMode, &remark, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	it.Remark = remark.String
	it.MessageSchemaMode = NormalizeMessageSchemaMode(it.MessageSchemaMode)
	return &it, nil
}

func (d *DB) ListConnectors(env string) ([]Connector, error) {
	return d.ListConnectorsInTenant("", env)
}

func (d *DB) ListConnectorsInTenant(tenantID, env string) ([]Connector, error) {
	q := `SELECT ` + connectorCols + ` FROM tbl_connectors WHERE 1=1`
	var args []any
	q, args = appendTenantFilter(q, args, tenantID)
	if strings.TrimSpace(env) != "" {
		q += ` AND env=?`
		args = append(args, strings.TrimSpace(env))
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connector
	for rows.Next() {
		it, err := scanConnector(rows)
		if err != nil {
			return nil, err
		}
		if err := d.fillConnectorTenant(it); err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// ListGrantedConnectors 授权给当前租户或用户、且授权方仍是连接器主人的连接器。
func (d *DB) ListGrantedConnectors(toTenantID, toUserID, env string) ([]Connector, error) {
	gs, err := d.ListTenantGrantsTo(toTenantID, toUserID)
	if err != nil {
		return nil, err
	}
	env = strings.TrimSpace(env)
	var out []Connector
	seen := map[string]struct{}{}
	for _, g := range gs {
		if g.ResourceType != "connector" {
			continue
		}
		if _, ok := seen[g.ResourceID]; ok {
			continue
		}
		it, err := d.GetConnector(g.ResourceID)
		if err != nil || it == nil {
			continue
		}
		if strings.TrimSpace(g.FromTenantID) != strings.TrimSpace(it.TenantID) {
			continue
		}
		if env != "" && it.Env != env {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, *it)
	}
	return out, nil
}

// VisibleConnector 本租连接器，或授权给当前租户/用户的连接器。auditAll 时按 id 直接取。
func (d *DB) VisibleConnector(id, tenantID, userID string, auditAll bool) (*Connector, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if auditAll {
		return d.GetConnector(id)
	}
	it, err := d.GetConnectorInTenant(id, tenantID)
	if err != nil || it != nil {
		return it, err
	}
	ok, err := d.HasResourceGrant(tenantID, userID, "connector", id)
	if err != nil || !ok {
		return nil, err
	}
	return d.GetConnector(id)
}

// AssignConnectorTenant 只改连接器归属，不连带 Kafka 实例。已属于目标租户视为成功。
func (d *DB) AssignConnectorTenant(id, toTenantID string) error {
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" || toTenantID == "" {
		return fmt.Errorf("连接器与目标租户必填")
	}
	t, err := d.GetTenant(toTenantID)
	if err != nil {
		return err
	}
	if t == nil || t.Status != TenantStatusActive {
		return fmt.Errorf("目标租户不存在或已停用")
	}
	it, err := d.GetConnector(id)
	if err != nil {
		return err
	}
	if it == nil {
		return fmt.Errorf("连接器不存在")
	}
	if strings.TrimSpace(it.TenantID) == toTenantID {
		return nil
	}
	return d.AssignTenantID("tbl_connectors", id, toTenantID)
}

func (d *DB) GetConnector(id string) (*Connector, error) {
	it, err := scanConnector(d.QueryRow(`SELECT `+connectorCols+` FROM tbl_connectors WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || it == nil {
		return it, err
	}
	if err := d.fillConnectorTenant(it); err != nil {
		return nil, err
	}
	return it, nil
}

func (d *DB) fillConnectorTenant(it *Connector) error {
	if it == nil || strings.TrimSpace(it.ID) == "" {
		return nil
	}
	var tid sql.NullString
	err := d.QueryRow(`SELECT COALESCE(tenant_id,'') FROM tbl_connectors WHERE id=?`, it.ID).Scan(&tid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	it.TenantID = strings.TrimSpace(tid.String)
	return nil
}

func (d *DB) GetConnectorInTenant(id, tenantID string) (*Connector, error) {
	it, err := d.GetConnector(id)
	return ownedInTenant(d, it, err, "tbl_connectors", id, tenantID)
}

func (d *DB) CreateConnector(it *Connector) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.CreatedAt = now
	it.UpdatedAt = now
	it.MessageSchemaMode = NormalizeMessageSchemaMode(it.MessageSchemaMode)
	_, err := d.Exec(
		`INSERT INTO tbl_connectors(`+connectorCols+`) VALUES(?,?,?,?,?,?,?,?)`,
		it.ID, it.DBZHost, it.Env, it.KafkaHost, it.MessageSchemaMode, it.Remark, it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_connectors", it.ID, it.TenantID)
}

func (d *DB) UpdateConnector(it *Connector) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	it.MessageSchemaMode = NormalizeMessageSchemaMode(it.MessageSchemaMode)
	_, err := d.Exec(
		`UPDATE tbl_connectors SET dbz_host=?, env=?, kafka_host=?, message_schema_mode=?, remark=?, updated_at=? WHERE id=?`,
		it.DBZHost, it.Env, it.KafkaHost, it.MessageSchemaMode, it.Remark, it.UpdatedAt, it.ID,
	)
	return err
}

func (d *DB) DeleteConnector(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_connectors WHERE id=?`, id)
	return err
}
