package store

import (
	"database/sql"
	"errors"
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
	q := `SELECT ` + connectorCols + ` FROM tbl_connectors WHERE 1=1`
	var args []any
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
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetConnector(id string) (*Connector, error) {
	it, err := scanConnector(d.QueryRow(`SELECT `+connectorCols+` FROM tbl_connectors WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
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
	return err
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
