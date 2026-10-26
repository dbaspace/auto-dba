package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (d *DB) ListRegions() ([]Region, error) {
	rows, err := d.Query(`SELECT id, code, name, description, created_at, updated_at FROM tbl_regions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Region
	for rows.Next() {
		var r Region
		if err := rows.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetRegion(id string) (*Region, error) {
	var r Region
	err := d.QueryRow(`SELECT id, code, name, description, created_at, updated_at FROM tbl_regions WHERE id=?`, id).
		Scan(&r.ID, &r.Code, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func (d *DB) CreateRegion(r *Region) error {
	r.Code = strings.TrimSpace(r.Code)
	r.Name = strings.TrimSpace(r.Name)
	if r.Code == "" || r.Name == "" {
		return errors.New("code/name 必填")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_regions(id, code, name, description, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		r.ID, r.Code, r.Name, r.Description, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateRegion(r *Region) error {
	r.Code = strings.TrimSpace(r.Code)
	r.Name = strings.TrimSpace(r.Name)
	if r.Code == "" || r.Name == "" {
		return errors.New("code/name 必填")
	}
	r.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`UPDATE tbl_regions SET code=?, name=?, description=?, updated_at=? WHERE id=?`,
		r.Code, r.Name, r.Description, r.UpdatedAt, r.ID,
	)
	return err
}

func (d *DB) CountInstancesByRegion(id string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_instances WHERE region_id=?`, id).Scan(&n)
	return n, err
}

func (d *DB) DeleteRegion(id string) error {
	n, err := d.CountInstancesByRegion(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return errors.New("区域已被实例使用，无法删除")
	}
	if _, err := d.Exec(`DELETE FROM tbl_region_envs WHERE region_id=?`, id); err != nil {
		return err
	}
	_, err = d.Exec(`DELETE FROM tbl_regions WHERE id=?`, id)
	return err
}

func (d *DB) ListEnvironments() ([]Environment, error) {
	rows, err := d.Query(`SELECT id, code, name, description, created_at, updated_at FROM tbl_environments ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.Description, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) GetEnvironment(id string) (*Environment, error) {
	var e Environment
	err := d.QueryRow(`SELECT id, code, name, description, created_at, updated_at FROM tbl_environments WHERE id=?`, id).
		Scan(&e.ID, &e.Code, &e.Name, &e.Description, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

func (d *DB) CreateEnvironment(e *Environment) error {
	e.Code = strings.TrimSpace(e.Code)
	e.Name = strings.TrimSpace(e.Name)
	if e.Code == "" || e.Name == "" {
		return errors.New("code/name 必填")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.CreatedAt, e.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_environments(id, code, name, description, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		e.ID, e.Code, e.Name, e.Description, e.CreatedAt, e.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateEnvironment(e *Environment) error {
	e.Code = strings.TrimSpace(e.Code)
	e.Name = strings.TrimSpace(e.Name)
	if e.Code == "" || e.Name == "" {
		return errors.New("code/name 必填")
	}
	e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`UPDATE tbl_environments SET code=?, name=?, description=?, updated_at=? WHERE id=?`,
		e.Code, e.Name, e.Description, e.UpdatedAt, e.ID,
	)
	return err
}

func (d *DB) CountInstancesByEnv(id string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_instances WHERE env_id=?`, id).Scan(&n)
	return n, err
}

func (d *DB) DeleteEnvironment(id string) error {
	n, err := d.CountInstancesByEnv(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return errors.New("环境已被实例使用，无法删除")
	}
	if _, err := d.Exec(`DELETE FROM tbl_region_envs WHERE env_id=?`, id); err != nil {
		return err
	}
	_, err = d.Exec(`DELETE FROM tbl_environments WHERE id=?`, id)
	return err
}

func (d *DB) ListRegionEnvs(regionID string) ([]Environment, error) {
	rows, err := d.Query(`
		SELECT e.id, e.code, e.name, e.description, e.created_at, e.updated_at
		FROM tbl_environments e
		INNER JOIN tbl_region_envs b ON b.env_id = e.id
		WHERE b.region_id=?
		ORDER BY e.name
	`, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.Description, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RegionEnvBinding 区域与环境的绑定。
type RegionEnvBinding struct {
	RegionID string
	EnvID    string
}

func (d *DB) ListRegionEnvBindings() ([]RegionEnvBinding, error) {
	rows, err := d.Query(`SELECT region_id, env_id FROM tbl_region_envs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegionEnvBinding
	for rows.Next() {
		var b RegionEnvBinding
		if err := rows.Scan(&b.RegionID, &b.EnvID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (d *DB) HasRegionEnv(regionID, envID string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_region_envs WHERE region_id=? AND env_id=?`, regionID, envID).Scan(&n)
	return n > 0, err
}

func (d *DB) SetRegionEnvs(regionID string, envIDs []string) error {
	if _, err := d.Exec(`DELETE FROM tbl_region_envs WHERE region_id=?`, regionID); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, id := range envIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		env, err := d.GetEnvironment(id)
		if err != nil {
			return err
		}
		if env == nil {
			return errors.New("环境不存在: " + id)
		}
		if _, err := d.Exec(`INSERT INTO tbl_region_envs(region_id, env_id) VALUES(?,?)`, regionID, id); err != nil {
			return err
		}
	}
	return nil
}
