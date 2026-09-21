package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

const cdcConnectorVersionCols = `id, task_id, version_no, config_json, change_source, operator, operator_ip, created_at`

func scanCDCConnectorVersion(sc interface {
	Scan(dest ...any) error
}) (*CDCConnectorConfigVersion, error) {
	it := &CDCConnectorConfigVersion{}
	if err := sc.Scan(
		&it.ID, &it.TaskID, &it.VersionNo, &it.ConfigJSON, &it.ChangeSource, &it.Operator, &it.OperatorIP, &it.CreatedAt,
	); err != nil {
		return nil, err
	}
	it.Version = it.VersionNo
	it.OperationID = it.ChangeSource
	return it, nil
}

func (d *DB) NextCDCConnectorVersionNo(taskID string) (int, error) {
	var n sql.NullInt64
	err := d.QueryRow(`SELECT MAX(version_no) FROM tbl_cdc_connector_config_versions WHERE task_id=?`, taskID).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 1, nil
	}
	return int(n.Int64) + 1, nil
}

func (d *DB) InsertCDCConnectorVersion(it *CDCConnectorConfigVersion) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if it.VersionNo <= 0 {
		n, err := d.NextCDCConnectorVersionNo(it.TaskID)
		if err != nil {
			return err
		}
		it.VersionNo = n
	}
	it.Version = it.VersionNo
	it.OperationID = it.ChangeSource
	_, err := d.Exec(
		`INSERT INTO tbl_cdc_connector_config_versions(`+cdcConnectorVersionCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		it.ID, it.TaskID, it.VersionNo, it.ConfigJSON, it.ChangeSource, it.Operator, it.OperatorIP, it.CreatedAt,
	)
	return err
}

func (d *DB) GetLatestCDCConnectorVersion(taskID string) (*CDCConnectorConfigVersion, error) {
	it, err := scanCDCConnectorVersion(d.QueryRow(
		`SELECT `+cdcConnectorVersionCols+` FROM tbl_cdc_connector_config_versions WHERE task_id=? ORDER BY version_no DESC LIMIT 1`,
		taskID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (d *DB) ListCDCConnectorVersions(taskID string, limit, offset int) ([]CDCConnectorConfigVersion, int, error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_cdc_connector_config_versions WHERE task_id=?`, taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.Query(
		`SELECT `+cdcConnectorVersionCols+` FROM tbl_cdc_connector_config_versions WHERE task_id=? ORDER BY version_no DESC LIMIT ? OFFSET ?`,
		taskID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []CDCConnectorConfigVersion{}
	for rows.Next() {
		it, err := scanCDCConnectorVersion(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *it)
	}
	return out, total, rows.Err()
}

func (d *DB) GetCDCConnectorVersion(taskID, versionID string) (*CDCConnectorConfigVersion, error) {
	versionID = strings.TrimSpace(versionID)
	it, err := scanCDCConnectorVersion(d.QueryRow(
		`SELECT `+cdcConnectorVersionCols+` FROM tbl_cdc_connector_config_versions WHERE task_id=? AND id=?`,
		taskID, versionID,
	))
	if err == sql.ErrNoRows {
		it, err = scanCDCConnectorVersion(d.QueryRow(
			`SELECT `+cdcConnectorVersionCols+` FROM tbl_cdc_connector_config_versions WHERE task_id=? AND CAST(version_no AS TEXT)=?`,
			taskID, versionID,
		))
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (d *DB) PruneCDCConnectorVersions(taskID string, keep int) error {
	if keep <= 0 {
		keep = 50
	}
	rows, err := d.Query(
		`SELECT id FROM tbl_cdc_connector_config_versions WHERE task_id=? ORDER BY version_no DESC`,
		taskID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) <= keep {
		return nil
	}
	for _, id := range ids[keep:] {
		if _, err := d.Exec(`DELETE FROM tbl_cdc_connector_config_versions WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}
