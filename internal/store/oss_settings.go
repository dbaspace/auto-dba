package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 配置中心 OSS · 云产品（对象存储）。
const (
	OSSProductAliyunOSS  = "aliyun_oss"     // 阿里云 OSS
	OSSProductTencentCOS = "tencent_cos"    // 腾讯云 COS
	OSSProductHuaweiOBS  = "huawei_obs"     // 华为云 OBS
	OSSProductAWSS3      = "aws_s3"         // AWS S3
	OSSProductS3Compat   = "s3_compatible"  // MinIO 等 S3 兼容
)

// OSSCloudProductLabel 云产品展示名。
func OSSCloudProductLabel(code string) string {
	switch strings.TrimSpace(code) {
	case OSSProductAliyunOSS:
		return "阿里云 OSS"
	case OSSProductTencentCOS:
		return "腾讯云 COS"
	case OSSProductHuaweiOBS:
		return "华为云 OBS"
	case OSSProductAWSS3:
		return "AWS S3"
	case OSSProductS3Compat:
		return "S3 兼容"
	default:
		return code
	}
}

// NormalizeOSSCloudProduct 规范化云产品码；非法返回空串。
func NormalizeOSSCloudProduct(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case OSSProductAliyunOSS, "aliyun", "oss":
		return OSSProductAliyunOSS
	case OSSProductTencentCOS, "tencent", "cos":
		return OSSProductTencentCOS
	case OSSProductHuaweiOBS, "huawei", "obs":
		return OSSProductHuaweiOBS
	case OSSProductAWSS3, "aws", "s3":
		return OSSProductAWSS3
	case OSSProductS3Compat, "minio", "s3_compat", "compatible":
		return OSSProductS3Compat
	default:
		return ""
	}
}

// OSSSetting 配置中心 · 按环境的对象存储（供数据迁移 OSS 中转等引用）。
type OSSSetting struct {
	ID            string `json:"id"`
	Environment   string `json:"environment"`
	CloudProduct  string `json:"cloud_product"`
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	Prefix        string `json:"prefix"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key,omitempty"` // 列表脱敏；创建/更新可带明文
	SecretEnc     string `json:"-"`
	SecretSet     bool   `json:"secret_set"`
	RetentionDays int    `json:"retention_days"`
	ChunkSizeMB   int    `json:"chunk_size_mb"`
	Configured    bool   `json:"configured"`
	CreatedBy     string `json:"created_by,omitempty"`
	UpdatedBy     string `json:"updated_by,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

const ossSettingCols = `id, environment, cloud_product, endpoint, bucket, prefix, access_key, secret_enc, retention_days, chunk_size_mb, created_by, updated_by, created_at, updated_at`

func scanOSSSettingRaw(scanner interface {
	Scan(dest ...any) error
}) (*OSSSetting, error) {
	var it OSSSetting
	var accessKey string
	if err := scanner.Scan(
		&it.ID, &it.Environment, &it.CloudProduct, &it.Endpoint, &it.Bucket, &it.Prefix, &accessKey, &it.SecretEnc,
		&it.RetentionDays, &it.ChunkSizeMB, &it.CreatedBy, &it.UpdatedBy, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	it.SecretSet = strings.TrimSpace(it.SecretEnc) != ""
	akSet := strings.TrimSpace(accessKey) != ""
	it.Configured = strings.TrimSpace(it.Endpoint) != "" && strings.TrimSpace(it.Bucket) != "" && akSet && it.SecretSet &&
		NormalizeOSSCloudProduct(it.CloudProduct) != ""
	if akSet {
		it.AccessKey = "********"
	}
	if it.SecretSet {
		it.SecretKey = "********"
	}
	return &it, nil
}

func (d *DB) ListOSSSettings(env string) ([]OSSSetting, error) {
	q := `SELECT ` + ossSettingCols + ` FROM tbl_oss_settings WHERE 1=1`
	var args []any
	if e := strings.TrimSpace(env); e != "" {
		q += ` AND environment=?`
		args = append(args, e)
	}
	q += ` ORDER BY environment ASC, cloud_product ASC, updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OSSSetting
	for rows.Next() {
		it, err := scanOSSSettingRaw(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetOSSSetting(id string) (*OSSSetting, error) {
	it, err := scanOSSSettingRaw(d.QueryRow(`SELECT `+ossSettingCols+` FROM tbl_oss_settings WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) GetOSSSettingByEnv(env string) (*OSSSetting, error) {
	env = strings.TrimSpace(env)
	if env == "" {
		return nil, nil
	}
	it, err := scanOSSSettingRaw(d.QueryRow(`SELECT `+ossSettingCols+` FROM tbl_oss_settings WHERE environment=?`, env))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) GetOSSSettingPlain(id string) (*OSSSetting, error) {
	var it OSSSetting
	err := d.QueryRow(`SELECT `+ossSettingCols+` FROM tbl_oss_settings WHERE id=?`, id).Scan(
		&it.ID, &it.Environment, &it.CloudProduct, &it.Endpoint, &it.Bucket, &it.Prefix, &it.AccessKey, &it.SecretEnc,
		&it.RetentionDays, &it.ChunkSizeMB, &it.CreatedBy, &it.UpdatedBy, &it.CreatedAt, &it.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.SecretSet = strings.TrimSpace(it.SecretEnc) != ""
	it.Configured = it.Endpoint != "" && it.Bucket != "" && strings.TrimSpace(it.AccessKey) != "" && it.SecretSet &&
		NormalizeOSSCloudProduct(it.CloudProduct) != ""
	return &it, nil
}

// GetOSSSettingPlainByEnv 返回未脱敏的 AccessKey 与 SecretEnc（供解密后连通性测试 / 运行时使用）。
func (d *DB) GetOSSSettingPlainByEnv(env string) (*OSSSetting, error) {
	env = strings.TrimSpace(env)
	if env == "" {
		return nil, nil
	}
	var it OSSSetting
	err := d.QueryRow(`SELECT `+ossSettingCols+` FROM tbl_oss_settings WHERE environment=?`, env).Scan(
		&it.ID, &it.Environment, &it.CloudProduct, &it.Endpoint, &it.Bucket, &it.Prefix, &it.AccessKey, &it.SecretEnc,
		&it.RetentionDays, &it.ChunkSizeMB, &it.CreatedBy, &it.UpdatedBy, &it.CreatedAt, &it.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.SecretSet = strings.TrimSpace(it.SecretEnc) != ""
	it.Configured = it.Endpoint != "" && it.Bucket != "" && strings.TrimSpace(it.AccessKey) != "" && it.SecretSet &&
		NormalizeOSSCloudProduct(it.CloudProduct) != ""
	return &it, nil
}

func (d *DB) CreateOSSSetting(it *OSSSetting) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.Environment = strings.TrimSpace(it.Environment)
	it.CloudProduct = NormalizeOSSCloudProduct(it.CloudProduct)
	it.Endpoint = strings.TrimSpace(it.Endpoint)
	it.Bucket = strings.TrimSpace(it.Bucket)
	it.Prefix = strings.TrimSpace(it.Prefix)
	if it.Prefix == "" {
		it.Prefix = "migration"
	}
	it.AccessKey = strings.TrimSpace(it.AccessKey)
	it.RetentionDays = clampInt(it.RetentionDays, 1, 3650, 7)
	it.ChunkSizeMB = clampInt(it.ChunkSizeMB, 1, 10240, 100)
	if it.CreatedAt == "" {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_oss_settings(
		id, environment, cloud_product, endpoint, bucket, prefix, access_key, secret_enc, retention_days, chunk_size_mb,
		created_by, updated_by, created_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Environment, it.CloudProduct, it.Endpoint, it.Bucket, it.Prefix, it.AccessKey, it.SecretEnc,
		it.RetentionDays, it.ChunkSizeMB, it.CreatedBy, it.UpdatedBy, it.CreatedAt, it.UpdatedAt)
	return err
}

func (d *DB) UpdateOSSSetting(it *OSSSetting) error {
	it.Environment = strings.TrimSpace(it.Environment)
	it.CloudProduct = NormalizeOSSCloudProduct(it.CloudProduct)
	it.Endpoint = strings.TrimSpace(it.Endpoint)
	it.Bucket = strings.TrimSpace(it.Bucket)
	it.Prefix = strings.TrimSpace(it.Prefix)
	if it.Prefix == "" {
		it.Prefix = "migration"
	}
	it.AccessKey = strings.TrimSpace(it.AccessKey)
	it.RetentionDays = clampInt(it.RetentionDays, 1, 3650, 7)
	it.ChunkSizeMB = clampInt(it.ChunkSizeMB, 1, 10240, 100)
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_oss_settings SET
		environment=?, cloud_product=?, endpoint=?, bucket=?, prefix=?, access_key=?,
		secret_enc=CASE WHEN ?='' THEN secret_enc ELSE ? END,
		retention_days=?, chunk_size_mb=?, updated_by=?, updated_at=?
		WHERE id=?`,
		it.Environment, it.CloudProduct, it.Endpoint, it.Bucket, it.Prefix, it.AccessKey,
		it.SecretEnc, it.SecretEnc,
		it.RetentionDays, it.ChunkSizeMB, it.UpdatedBy, it.UpdatedAt, it.ID)
	return err
}

func (d *DB) DeleteOSSSetting(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_oss_settings WHERE id=?`, id)
	return err
}

// ListConfiguredOSSEnvs 已完整配置的环境码列表。
func (d *DB) ListConfiguredOSSEnvs() ([]string, error) {
	rows, err := d.Query(`SELECT environment FROM tbl_oss_settings
		WHERE endpoint<>'' AND bucket<>'' AND access_key<>'' AND secret_enc<>'' AND cloud_product<>''
		ORDER BY environment`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// ResolveMigrationOSS 按任务环境解析 OSS：优先 tbl_oss_settings，否则回退旧版 global_args。
func (d *DB) ResolveMigrationOSS(env string) MigrationOSSSettings {
	env = strings.TrimSpace(env)
	if env != "" {
		if plain, err := d.GetOSSSettingPlainByEnv(env); err == nil && plain != nil && plain.Configured {
			return MigrationOSSSettings{
				Endpoint:      plain.Endpoint,
				Bucket:        plain.Bucket,
				Prefix:        plain.Prefix,
				AccessKey:     plain.AccessKey,
				SecretKey:     "", // 调用方需自行解密 SecretEnc；Configured 仅表示已录入
				RetentionDays: plain.RetentionDays,
				ChunkSizeMB:   plain.ChunkSizeMB,
				Configured:    true,
			}
		}
		if row, err := d.GetOSSSettingByEnv(env); err == nil && row != nil {
			return MigrationOSSSettings{
				Endpoint:      row.Endpoint,
				Bucket:        row.Bucket,
				Prefix:        row.Prefix,
				RetentionDays: row.RetentionDays,
				ChunkSizeMB:   row.ChunkSizeMB,
				Configured:    false,
			}
		}
	}
	return d.GetMigrationSettings().OSS
}

// ResolveMigrationOSSConfigured 仅判断某环境是否已配置（不解密）。
func (d *DB) ResolveMigrationOSSConfigured(env string) bool {
	env = strings.TrimSpace(env)
	if env != "" {
		plain, err := d.GetOSSSettingPlainByEnv(env)
		if err == nil && plain != nil && plain.Configured {
			return true
		}
		if err == nil && plain != nil {
			return false
		}
	}
	return d.GetMigrationSettings().OSS.Configured
}
