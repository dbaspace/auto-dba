package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const passwordMask = "******"

type FeAdmin struct {
	ID          string `json:"id"`
	TaskName    string `json:"task_name"`
	Environment string `json:"environment"`
	Domain      string `json:"domain"`
	Port        int    `json:"port"`
	FeNode      string `json:"fe_node"`
	Username    string `json:"username"`
	Password    string `json:"password"` // list/detail always masked; create/update may carry plain
	PasswordEnc string `json:"-"`
	CreatedBy   string `json:"created_by,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type DsAdmin struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	LoginURL    string `json:"login_url"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PasswordEnc string `json:"-"`
	CreatedBy   string `json:"created_by,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

const feAdminCols = `id, task_name, environment, domain, port, fe_node, username, password_enc, created_by, updated_by, created_at, updated_at`
const dsAdminCols = `id, environment, login_url, username, password_enc, created_by, updated_by, created_at, updated_at`

func maskPassword(enc string) string {
	if strings.TrimSpace(enc) == "" {
		return ""
	}
	return passwordMask
}

func scanFeAdmin(scanner interface {
	Scan(dest ...any) error
}) (*FeAdmin, error) {
	var it FeAdmin
	if err := scanner.Scan(
		&it.ID, &it.TaskName, &it.Environment, &it.Domain, &it.Port, &it.FeNode,
		&it.Username, &it.PasswordEnc, &it.CreatedBy, &it.UpdatedBy, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	it.Password = maskPassword(it.PasswordEnc)
	return &it, nil
}

func scanDsAdmin(scanner interface {
	Scan(dest ...any) error
}) (*DsAdmin, error) {
	var it DsAdmin
	if err := scanner.Scan(
		&it.ID, &it.Environment, &it.LoginURL, &it.Username, &it.PasswordEnc,
		&it.CreatedBy, &it.UpdatedBy, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	it.Password = maskPassword(it.PasswordEnc)
	return &it, nil
}

func (d *DB) ListFeAdmins(env, keyword string) ([]FeAdmin, error) {
	q := `SELECT ` + feAdminCols + ` FROM tbl_fe_admin WHERE 1=1`
	var args []any
	if e := strings.TrimSpace(env); e != "" {
		q += ` AND environment=?`
		args = append(args, e)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		q += ` AND (task_name ILIKE ? OR domain ILIKE ? OR fe_node ILIKE ? OR username ILIKE ?)`
		like := "%" + kw + "%"
		args = append(args, like, like, like, like)
	}
	q += ` ORDER BY updated_at DESC, created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeAdmin
	for rows.Next() {
		it, err := scanFeAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetFeAdmin(id string) (*FeAdmin, error) {
	it, err := scanFeAdmin(d.QueryRow(`SELECT `+feAdminCols+` FROM tbl_fe_admin WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) CreateFeAdmin(it *FeAdmin) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.CreatedAt = now
	it.UpdatedAt = now
	it.TaskName = strings.TrimSpace(it.TaskName)
	it.Environment = strings.TrimSpace(it.Environment)
	it.Domain = strings.TrimSpace(it.Domain)
	it.FeNode = strings.TrimSpace(it.FeNode)
	it.Username = strings.TrimSpace(it.Username)
	_, err := d.Exec(
		`INSERT INTO tbl_fe_admin(`+feAdminCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.TaskName, it.Environment, it.Domain, it.Port, it.FeNode,
		it.Username, it.PasswordEnc, it.CreatedBy, it.UpdatedBy, it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return err
	}
	it.Password = maskPassword(it.PasswordEnc)
	return nil
}

func (d *DB) UpdateFeAdmin(it *FeAdmin) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	it.TaskName = strings.TrimSpace(it.TaskName)
	it.Environment = strings.TrimSpace(it.Environment)
	it.Domain = strings.TrimSpace(it.Domain)
	it.FeNode = strings.TrimSpace(it.FeNode)
	it.Username = strings.TrimSpace(it.Username)
	_, err := d.Exec(
		`UPDATE tbl_fe_admin SET task_name=?, environment=?, domain=?, port=?, fe_node=?, username=?, password_enc=?, updated_by=?, updated_at=? WHERE id=?`,
		it.TaskName, it.Environment, it.Domain, it.Port, it.FeNode, it.Username,
		it.PasswordEnc, it.UpdatedBy, it.UpdatedAt, it.ID,
	)
	if err != nil {
		return err
	}
	it.Password = maskPassword(it.PasswordEnc)
	return nil
}

func (d *DB) DeleteFeAdmin(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_fe_admin WHERE id=?`, id)
	return err
}

func (d *DB) ListDsAdmins(env, keyword string) ([]DsAdmin, error) {
	q := `SELECT ` + dsAdminCols + ` FROM tbl_ds_admin WHERE 1=1`
	var args []any
	if e := strings.TrimSpace(env); e != "" {
		q += ` AND environment=?`
		args = append(args, e)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		q += ` AND (environment ILIKE ? OR login_url ILIKE ? OR username ILIKE ?)`
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY updated_at DESC, created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DsAdmin
	for rows.Next() {
		it, err := scanDsAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetDsAdmin(id string) (*DsAdmin, error) {
	it, err := scanDsAdmin(d.QueryRow(`SELECT `+dsAdminCols+` FROM tbl_ds_admin WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) GetDsAdminByEnv(env string) (*DsAdmin, error) {
	env = strings.TrimSpace(env)
	it, err := scanDsAdmin(d.QueryRow(`SELECT `+dsAdminCols+` FROM tbl_ds_admin WHERE environment=?`, env))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) CreateDsAdmin(it *DsAdmin) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.CreatedAt = now
	it.UpdatedAt = now
	it.Environment = strings.TrimSpace(it.Environment)
	it.LoginURL = strings.TrimSpace(it.LoginURL)
	it.Username = strings.TrimSpace(it.Username)
	_, err := d.Exec(
		`INSERT INTO tbl_ds_admin(`+dsAdminCols+`) VALUES(?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Environment, it.LoginURL, it.Username, it.PasswordEnc,
		it.CreatedBy, it.UpdatedBy, it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return err
	}
	it.Password = maskPassword(it.PasswordEnc)
	return nil
}

func (d *DB) UpdateDsAdmin(it *DsAdmin) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	it.Environment = strings.TrimSpace(it.Environment)
	it.LoginURL = strings.TrimSpace(it.LoginURL)
	it.Username = strings.TrimSpace(it.Username)
	_, err := d.Exec(
		`UPDATE tbl_ds_admin SET environment=?, login_url=?, username=?, password_enc=?, updated_by=?, updated_at=? WHERE id=?`,
		it.Environment, it.LoginURL, it.Username, it.PasswordEnc, it.UpdatedBy, it.UpdatedAt, it.ID,
	)
	if err != nil {
		return err
	}
	it.Password = maskPassword(it.PasswordEnc)
	return nil
}

func (d *DB) DeleteDsAdmin(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_ds_admin WHERE id=?`, id)
	return err
}
