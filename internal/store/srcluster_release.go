package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// SrOSSSettings StarRocks 制品存储绑定：引用配置中心 OSS + 本模块制品 prefix。
type SrOSSSettings struct {
	ID           string `json:"id"`
	OSSSettingID string `json:"oss_setting_id"`
	Prefix       string `json:"prefix"` // 制品键前缀，如 starrocks/
	UpdatedBy    string `json:"updated_by,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	// 只读展示（来自配置中心）
	OSSEnvironment string `json:"oss_environment,omitempty"`
	OSSEndpoint    string `json:"oss_endpoint,omitempty"`
	OSSBucket      string `json:"oss_bucket,omitempty"`
	OSSConfigured  bool   `json:"oss_configured,omitempty"`
}

// SrRelease 版本目录。
type SrRelease struct {
	ID        string              `json:"id"`
	Version   string              `json:"version"`
	Channel   string              `json:"channel"`
	JDKMin    string              `json:"jdk_min"`
	JDKMax    string              `json:"jdk_max"` // 空表示不限上限
	Enabled   bool                `json:"enabled"`
	Remark    string              `json:"remark,omitempty"`
	CreatedBy string              `json:"created_by,omitempty"`
	UpdatedBy string              `json:"updated_by,omitempty"`
	CreatedAt string              `json:"created_at"`
	UpdatedAt string              `json:"updated_at"`
	Artifacts []SrReleaseArtifact `json:"artifacts,omitempty"`
}

// SrReleaseArtifact FE/BE 分包。
type SrReleaseArtifact struct {
	ID        string `json:"id"`
	ReleaseID string `json:"release_id"`
	Role      string `json:"role"` // fe | be
	ObjectKey string `json:"object_key"`
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Format    string `json:"format"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SrEnvVersionPolicy 环境版本策略。
type SrEnvVersionPolicy struct {
	ID                 string   `json:"id"`
	Environment        string   `json:"environment"`
	DefaultReleaseID   string   `json:"default_release_id"`
	AllowedReleaseIDs  []string `json:"allowed_release_ids"`
	AllowedReleaseJSON string   `json:"-"`
	UpdatedBy          string   `json:"updated_by,omitempty"`
	UpdatedAt          string   `json:"updated_at"`
}

func (d *DB) GetSrOSSSettings() (*SrOSSSettings, error) {
	row := d.QueryRow(`SELECT id, COALESCE(oss_setting_id,''), COALESCE(prefix,'starrocks/'), updated_by, updated_at FROM tbl_sr_oss_settings WHERE id='default'`)
	var s SrOSSSettings
	if err := row.Scan(&s.ID, &s.OSSSettingID, &s.Prefix, &s.UpdatedBy, &s.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return &SrOSSSettings{ID: "default", Prefix: "starrocks/"}, nil
		}
		return nil, err
	}
	if s.Prefix == "" {
		s.Prefix = "starrocks/"
	}
	d.fillSrOSSDisplay(&s)
	return &s, nil
}

func (d *DB) fillSrOSSDisplay(s *SrOSSSettings) {
	if s == nil || strings.TrimSpace(s.OSSSettingID) == "" {
		return
	}
	oss, err := d.GetOSSSetting(s.OSSSettingID)
	if err != nil || oss == nil {
		return
	}
	s.OSSEnvironment = oss.Environment
	s.OSSEndpoint = oss.Endpoint
	s.OSSBucket = oss.Bucket
	s.OSSConfigured = oss.Configured
}

func (d *DB) UpsertSrOSSSettings(s *SrOSSSettings) error {
	s.ID = "default"
	if s.Prefix == "" {
		s.Prefix = "starrocks/"
	}
	if !strings.HasSuffix(s.Prefix, "/") {
		s.Prefix += "/"
	}
	s.UpdatedAt = nowSR()
	_, err := d.Exec(`INSERT INTO tbl_sr_oss_settings (id, oss_setting_id, prefix, updated_by, updated_at)
		VALUES ('default',?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET oss_setting_id=EXCLUDED.oss_setting_id, prefix=EXCLUDED.prefix,
		updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		s.OSSSettingID, s.Prefix, s.UpdatedBy, s.UpdatedAt)
	return err
}

func (d *DB) CreateSrRelease(r *SrRelease) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Channel == "" {
		r.Channel = "stable"
	}
	if r.JDKMin == "" {
		r.JDKMin = "11"
	}
	now := nowSR()
	r.CreatedAt, r.UpdatedAt = now, now
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_sr_releases (id,version,channel,jdk_min,jdk_max,enabled,remark,created_by,updated_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Version, r.Channel, r.JDKMin, r.JDKMax, en, r.Remark, r.CreatedBy, r.UpdatedBy, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) UpdateSrRelease(r *SrRelease) error {
	r.UpdatedAt = nowSR()
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`UPDATE tbl_sr_releases SET version=?, channel=?, jdk_min=?, jdk_max=?, enabled=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		r.Version, r.Channel, r.JDKMin, r.JDKMax, en, r.Remark, r.UpdatedBy, r.UpdatedAt, r.ID)
	return err
}

func (d *DB) DeleteSrRelease(id string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_release_artifacts WHERE release_id=?`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_releases WHERE id=?`), id); err != nil {
		return err
	}
	return tx.Commit()
}

func scanSrRelease(sc interface{ Scan(dest ...any) error }) (*SrRelease, error) {
	var r SrRelease
	var en int
	if err := sc.Scan(&r.ID, &r.Version, &r.Channel, &r.JDKMin, &r.JDKMax, &en, &r.Remark, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Enabled = en != 0
	return &r, nil
}

func (d *DB) GetSrRelease(id string) (*SrRelease, error) {
	row := d.QueryRow(`SELECT id,version,channel,jdk_min,COALESCE(jdk_max,''),enabled,remark,created_by,updated_by,created_at,updated_at FROM tbl_sr_releases WHERE id=?`, id)
	r, err := scanSrRelease(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	arts, err := d.ListSrReleaseArtifacts(id)
	if err != nil {
		return nil, err
	}
	r.Artifacts = arts
	return r, nil
}

func (d *DB) GetSrReleaseByVersion(version string) (*SrRelease, error) {
	row := d.QueryRow(`SELECT id,version,channel,jdk_min,COALESCE(jdk_max,''),enabled,remark,created_by,updated_by,created_at,updated_at FROM tbl_sr_releases WHERE version=?`, version)
	r, err := scanSrRelease(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	arts, _ := d.ListSrReleaseArtifacts(r.ID)
	r.Artifacts = arts
	return r, nil
}

func (d *DB) ListSrReleases(enabledOnly bool) ([]SrRelease, error) {
	q := `SELECT id,version,channel,jdk_min,COALESCE(jdk_max,''),enabled,remark,created_by,updated_by,created_at,updated_at FROM tbl_sr_releases`
	if enabledOnly {
		q += ` WHERE enabled=1`
	}
	q += ` ORDER BY version DESC`
	rows, err := d.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrRelease
	for rows.Next() {
		r, err := scanSrRelease(rows)
		if err != nil {
			return nil, err
		}
		arts, _ := d.ListSrReleaseArtifacts(r.ID)
		r.Artifacts = arts
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (d *DB) UpsertSrReleaseArtifact(a *SrReleaseArtifact) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.Role = strings.ToLower(strings.TrimSpace(a.Role))
	if a.Role != "fe" && a.Role != "be" {
		return fmt.Errorf("role 须为 fe 或 be")
	}
	if a.Format == "" {
		a.Format = "tar.gz"
	}
	now := nowSR()
	a.UpdatedAt = now
	if a.CreatedAt == "" {
		a.CreatedAt = now
	}
	_, err := d.Exec(`INSERT INTO tbl_sr_release_artifacts
		(id,release_id,role,object_key,filename,sha256,size_bytes,format,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (release_id, role) DO UPDATE SET
		object_key=EXCLUDED.object_key, filename=EXCLUDED.filename, sha256=EXCLUDED.sha256,
		size_bytes=EXCLUDED.size_bytes, format=EXCLUDED.format, updated_at=EXCLUDED.updated_at`,
		a.ID, a.ReleaseID, a.Role, a.ObjectKey, a.Filename, a.SHA256, a.SizeBytes, a.Format, a.CreatedAt, a.UpdatedAt)
	return err
}

func (d *DB) GetSrReleaseArtifact(releaseID, role string) (*SrReleaseArtifact, error) {
	row := d.QueryRow(`SELECT id,release_id,role,object_key,filename,sha256,size_bytes,format,created_at,updated_at
		FROM tbl_sr_release_artifacts WHERE release_id=? AND role=?`, releaseID, strings.ToLower(role))
	var a SrReleaseArtifact
	if err := row.Scan(&a.ID, &a.ReleaseID, &a.Role, &a.ObjectKey, &a.Filename, &a.SHA256, &a.SizeBytes, &a.Format, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (d *DB) ListSrReleaseArtifacts(releaseID string) ([]SrReleaseArtifact, error) {
	rows, err := d.Query(`SELECT id,release_id,role,object_key,filename,sha256,size_bytes,format,created_at,updated_at
		FROM tbl_sr_release_artifacts WHERE release_id=? ORDER BY role`, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrReleaseArtifact
	for rows.Next() {
		var a SrReleaseArtifact
		if err := rows.Scan(&a.ID, &a.ReleaseID, &a.Role, &a.ObjectKey, &a.Filename, &a.SHA256, &a.SizeBytes, &a.Format, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSrReleaseArtifact(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_sr_release_artifacts WHERE id=?`, id)
	return err
}

func (d *DB) UpsertSrEnvVersionPolicy(p *SrEnvVersionPolicy) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.AllowedReleaseIDs == nil {
		p.AllowedReleaseIDs = []string{}
	}
	b, _ := json.Marshal(p.AllowedReleaseIDs)
	p.AllowedReleaseJSON = string(b)
	p.UpdatedAt = nowSR()
	_, err := d.Exec(`INSERT INTO tbl_sr_env_version_policies (id,environment,default_release_id,allowed_release_ids,updated_by,updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT (environment) DO UPDATE SET default_release_id=EXCLUDED.default_release_id,
		allowed_release_ids=EXCLUDED.allowed_release_ids, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		p.ID, p.Environment, p.DefaultReleaseID, p.AllowedReleaseJSON, p.UpdatedBy, p.UpdatedAt)
	return err
}

func (d *DB) GetSrEnvVersionPolicy(env string) (*SrEnvVersionPolicy, error) {
	row := d.QueryRow(`SELECT id,environment,default_release_id,allowed_release_ids,updated_by,updated_at FROM tbl_sr_env_version_policies WHERE environment=?`, env)
	var p SrEnvVersionPolicy
	if err := row.Scan(&p.ID, &p.Environment, &p.DefaultReleaseID, &p.AllowedReleaseJSON, &p.UpdatedBy, &p.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(p.AllowedReleaseJSON), &p.AllowedReleaseIDs)
	if p.AllowedReleaseIDs == nil {
		p.AllowedReleaseIDs = []string{}
	}
	return &p, nil
}

func (d *DB) ListSrEnvVersionPolicies() ([]SrEnvVersionPolicy, error) {
	rows, err := d.Query(`SELECT id,environment,default_release_id,allowed_release_ids,updated_by,updated_at FROM tbl_sr_env_version_policies ORDER BY environment`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrEnvVersionPolicy
	for rows.Next() {
		var p SrEnvVersionPolicy
		if err := rows.Scan(&p.ID, &p.Environment, &p.DefaultReleaseID, &p.AllowedReleaseJSON, &p.UpdatedBy, &p.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(p.AllowedReleaseJSON), &p.AllowedReleaseIDs)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ValidateReleaseForEnv 校验制品版本存在且已启用。
// 版本以 release_id 为准，不再按环境白名单拦截（环境字段仅作标签）。
func (d *DB) ValidateReleaseForEnv(env, releaseID string) error {
	_ = env
	rid := strings.TrimSpace(releaseID)
	if rid == "" {
		return fmt.Errorf("未指定制品版本（release_id 为空），请编辑集群选择「设置 → 版本目录」中已登记的版本")
	}
	rel, err := d.GetSrRelease(rid)
	if err != nil || rel == nil {
		return fmt.Errorf("制品版本不存在（release_id=%s），请到「设置 → 版本目录」确认该版本仍在，或编辑集群重新选择版本", rid)
	}
	if !rel.Enabled {
		return fmt.Errorf("版本 %s（release_id=%s）未启用", rel.Version, rid)
	}
	return nil
}
