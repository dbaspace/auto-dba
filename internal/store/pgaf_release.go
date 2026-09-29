package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PGAFRelease struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Channel   string `json:"channel,omitempty"`
	Arch      string `json:"arch,omitempty"`
	OS        string `json:"os,omitempty"`
	Enabled   bool   `json:"enabled"`
	Remark    string `json:"remark,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Artifacts []PGAFReleaseArtifact `json:"artifacts,omitempty"`
}

type PGAFReleaseArtifact struct {
	ID         string `json:"id"`
	ReleaseID  string `json:"release_id"`
	Role       string `json:"role"` // monitor | keeper
	ObjectKey  string `json:"object_key"`
	SHA256     string `json:"sha256"`
	Filename   string `json:"filename,omitempty"`
	PackFormat string `json:"pack_format"` // rpm | deb
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type PGAFOSSBind struct {
	OSSSettingID   string `json:"oss_setting_id"`
	Prefix         string `json:"prefix"`
	UpdatedBy      string `json:"updated_by,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	OSSEnvironment string `json:"oss_environment,omitempty"`
	OSSEndpoint    string `json:"oss_endpoint,omitempty"`
	OSSBucket      string `json:"oss_bucket,omitempty"`
}

type PGAFInstallRun struct {
	ID        string `json:"id"`
	ClusterID string `json:"cluster_id"`
	ReleaseID string `json:"release_id"`
	Status    string `json:"status"`
	LogText   string `json:"log_text,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (d *DB) ensurePGAFReleaseTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_pgaf_releases (
			id TEXT PRIMARY KEY,
			version TEXT NOT NULL,
			channel TEXT NOT NULL DEFAULT 'stable',
			arch TEXT NOT NULL DEFAULT 'x86_64',
			os TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_pgaf_release_artifacts (
			id TEXT PRIMARY KEY,
			release_id TEXT NOT NULL,
			role TEXT NOT NULL,
			object_key TEXT NOT NULL,
			sha256 TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			pack_format TEXT NOT NULL DEFAULT 'rpm',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_pgaf_oss_bind (
			id TEXT PRIMARY KEY DEFAULT 'default',
			oss_setting_id TEXT NOT NULL DEFAULT '',
			prefix TEXT NOT NULL DEFAULT 'pgaf-artifacts/',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_ha_pgaf_install_runs (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			release_id TEXT NOT NULL,
			status TEXT NOT NULL,
			log_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ListPGAFReleases() ([]PGAFRelease, error) {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return nil, err
	}
	rows, err := d.Query(`SELECT id, version, channel, arch, os, enabled, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_pgaf_releases ORDER BY version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PGAFRelease
	for rows.Next() {
		var r PGAFRelease
		var en int
		if err := rows.Scan(&r.ID, &r.Version, &r.Channel, &r.Arch, &r.OS, &en, &r.Remark, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = en == 1
		arts, _ := d.ListPGAFReleaseArtifacts(r.ID)
		r.Artifacts = arts
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetPGAFRelease(id string) (*PGAFRelease, error) {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return nil, err
	}
	r := &PGAFRelease{}
	var en int
	err := d.QueryRow(`SELECT id, version, channel, arch, os, enabled, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_pgaf_releases WHERE id=?`, id).Scan(
		&r.ID, &r.Version, &r.Channel, &r.Arch, &r.OS, &en, &r.Remark, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = en == 1
	r.Artifacts, _ = d.ListPGAFReleaseArtifacts(r.ID)
	return r, nil
}

func (d *DB) UpsertPGAFRelease(r *PGAFRelease) error {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	if r.Channel == "" {
		r.Channel = "stable"
	}
	if r.Arch == "" {
		r.Arch = "x86_64"
	}
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_pgaf_releases(id, version, channel, arch, os, enabled, remark, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET version=EXCLUDED.version, channel=EXCLUDED.channel, arch=EXCLUDED.arch, os=EXCLUDED.os,
		enabled=EXCLUDED.enabled, remark=EXCLUDED.remark, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		r.ID, r.Version, r.Channel, r.Arch, r.OS, en, r.Remark, r.CreatedBy, r.UpdatedBy, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) DeletePGAFRelease(id string) error {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return err
	}
	_, _ = d.Exec(`DELETE FROM tbl_pgaf_release_artifacts WHERE release_id=?`, id)
	_, err := d.Exec(`DELETE FROM tbl_pgaf_releases WHERE id=?`, id)
	return err
}

func (d *DB) ListPGAFReleaseArtifacts(releaseID string) ([]PGAFReleaseArtifact, error) {
	rows, err := d.Query(`SELECT id, release_id, role, object_key, sha256, filename, pack_format, size_bytes, created_at, updated_at
		FROM tbl_pgaf_release_artifacts WHERE release_id=? ORDER BY role`, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PGAFReleaseArtifact
	for rows.Next() {
		var a PGAFReleaseArtifact
		if err := rows.Scan(&a.ID, &a.ReleaseID, &a.Role, &a.ObjectKey, &a.SHA256, &a.Filename, &a.PackFormat, &a.SizeBytes, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) UpsertPGAFReleaseArtifact(a *PGAFReleaseArtifact) error {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	role := strings.ToLower(strings.TrimSpace(a.Role))
	if role != "monitor" && role != "keeper" {
		return errors.New("role 须为 monitor 或 keeper")
	}
	a.Role = role
	pf := strings.ToLower(strings.TrimSpace(a.PackFormat))
	if pf != "rpm" && pf != "deb" {
		return errors.New("pack_format 须为 rpm 或 deb")
	}
	a.PackFormat = pf
	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_pgaf_release_artifacts(id, release_id, role, object_key, sha256, filename, pack_format, size_bytes, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET role=EXCLUDED.role, object_key=EXCLUDED.object_key, sha256=EXCLUDED.sha256,
		filename=EXCLUDED.filename, pack_format=EXCLUDED.pack_format, size_bytes=EXCLUDED.size_bytes, updated_at=EXCLUDED.updated_at`,
		a.ID, a.ReleaseID, a.Role, a.ObjectKey, a.SHA256, a.Filename, a.PackFormat, a.SizeBytes, a.CreatedAt, a.UpdatedAt)
	return err
}

func (d *DB) GetPGAFOSSBind() (*PGAFOSSBind, error) {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return nil, err
	}
	b := &PGAFOSSBind{}
	err := d.QueryRow(`SELECT oss_setting_id, prefix, updated_by, updated_at FROM tbl_pgaf_oss_bind WHERE id='default'`).
		Scan(&b.OSSSettingID, &b.Prefix, &b.UpdatedBy, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &PGAFOSSBind{Prefix: "pgaf-artifacts/"}, nil
	}
	if err != nil {
		return nil, err
	}
	if b.OSSSettingID != "" {
		if s, e := d.GetOSSSetting(b.OSSSettingID); e == nil && s != nil {
			b.OSSEnvironment = s.Environment
			b.OSSEndpoint = s.Endpoint
			b.OSSBucket = s.Bucket
		}
	}
	return b, nil
}

func (d *DB) PutPGAFOSSBind(b *PGAFOSSBind) error {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if b.Prefix == "" {
		b.Prefix = "pgaf-artifacts/"
	}
	b.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_pgaf_oss_bind(id, oss_setting_id, prefix, updated_by, updated_at) VALUES('default',?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET oss_setting_id=EXCLUDED.oss_setting_id, prefix=EXCLUDED.prefix,
		updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		b.OSSSettingID, b.Prefix, b.UpdatedBy, b.UpdatedAt)
	return err
}

func (d *DB) CreatePGAFInstallRun(r *PGAFInstallRun) error {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_ha_pgaf_install_runs(id, cluster_id, release_id, status, log_text, created_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.ClusterID, r.ReleaseID, r.Status, r.LogText, r.CreatedBy, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) UpdatePGAFInstallRun(id, status, log string) error {
	_, err := d.Exec(`UPDATE tbl_ha_pgaf_install_runs SET status=?, log_text=?, updated_at=? WHERE id=?`,
		status, log, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (d *DB) ListPGAFInstallRuns(clusterID string, limit int) ([]PGAFInstallRun, error) {
	if err := d.ensurePGAFReleaseTables(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.Query(`SELECT id, cluster_id, release_id, status, log_text, created_by, created_at, updated_at
		FROM tbl_ha_pgaf_install_runs WHERE cluster_id=? ORDER BY created_at DESC LIMIT ?`, clusterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PGAFInstallRun
	for rows.Next() {
		var r PGAFInstallRun
		if err := rows.Scan(&r.ID, &r.ClusterID, &r.ReleaseID, &r.Status, &r.LogText, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
