package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OSS 上传任务状态。
const (
	OSSUploadUploading  = "uploading"
	OSSUploadCompleting = "completing"
	OSSUploadVerifying  = "verifying"
	OSSUploadSuccess    = "success"
	OSSUploadFailed     = "failed"
	OSSUploadAborted    = "aborted"
)

// OSSUploadPart 已成功上传的分片。
type OSSUploadPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

// OSSUploadJob 配置中心 · OSS 分片上传任务。
type OSSUploadJob struct {
	ID          string          `json:"id"`
	ProfileID   string          `json:"profile_id"`
	ObjectKey   string          `json:"object_key"`
	FileName    string          `json:"file_name"`
	FileSize    int64           `json:"file_size"`
	SHA256      string          `json:"sha256"`
	ChunkSize   int64           `json:"chunk_size"`
	Status      string          `json:"status"`
	OSSUploadID string          `json:"oss_upload_id"`
	PartsJSON   string          `json:"-"`
	Parts       []OSSUploadPart `json:"parts"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	FinishedAt  string          `json:"finished_at,omitempty"`
	// 展示
	ProfileEnv    string `json:"profile_env,omitempty"`
	ProfileBucket string `json:"profile_bucket,omitempty"`
	TotalParts    int    `json:"total_parts"`
	DoneParts     int    `json:"done_parts"`
}

func (d *DB) ensureOSSUploadTables() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_oss_upload_jobs (
		id TEXT PRIMARY KEY,
		profile_id TEXT NOT NULL DEFAULT '',
		object_key TEXT NOT NULL DEFAULT '',
		file_name TEXT NOT NULL DEFAULT '',
		file_size INTEGER NOT NULL DEFAULT 0,
		sha256 TEXT NOT NULL DEFAULT '',
		chunk_size INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'uploading',
		oss_upload_id TEXT NOT NULL DEFAULT '',
		parts_json TEXT NOT NULL DEFAULT '[]',
		error_message TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		finished_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_oss_upload_jobs_updated ON tbl_oss_upload_jobs(updated_at DESC)`)
	return err
}

func encodeOSSParts(parts []OSSUploadPart) string {
	if parts == nil {
		parts = []OSSUploadPart{}
	}
	b, _ := json.Marshal(parts)
	return string(b)
}

func decodeOSSParts(raw string) []OSSUploadPart {
	var out []OSSUploadPart
	if strings.TrimSpace(raw) == "" {
		return []OSSUploadPart{}
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []OSSUploadPart{}
	}
	return out
}

func decorateOSSUploadJob(j *OSSUploadJob) {
	if j == nil {
		return
	}
	j.Parts = decodeOSSParts(j.PartsJSON)
	j.DoneParts = len(j.Parts)
	if j.ChunkSize > 0 && j.FileSize > 0 {
		n := int((j.FileSize + j.ChunkSize - 1) / j.ChunkSize)
		j.TotalParts = n
	}
}

func (d *DB) CreateOSSUploadJob(j *OSSUploadJob) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	if j.Status == "" {
		j.Status = OSSUploadUploading
	}
	j.PartsJSON = encodeOSSParts(j.Parts)
	j.CreatedAt, j.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_oss_upload_jobs(
		id, profile_id, object_key, file_name, file_size, sha256, chunk_size, status, oss_upload_id, parts_json,
		error_message, created_by, created_at, updated_at, finished_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.ProfileID, j.ObjectKey, j.FileName, j.FileSize, j.SHA256, j.ChunkSize, j.Status, j.OSSUploadID, j.PartsJSON,
		j.ErrorMessage, j.CreatedBy, j.CreatedAt, j.UpdatedAt, j.FinishedAt)
	return err
}

func (d *DB) UpdateOSSUploadJob(j *OSSUploadJob) error {
	j.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	j.PartsJSON = encodeOSSParts(j.Parts)
	_, err := d.Exec(`UPDATE tbl_oss_upload_jobs SET profile_id=?, object_key=?, file_name=?, file_size=?, sha256=?, chunk_size=?,
		status=?, oss_upload_id=?, parts_json=?, error_message=?, updated_at=?, finished_at=? WHERE id=?`,
		j.ProfileID, j.ObjectKey, j.FileName, j.FileSize, j.SHA256, j.ChunkSize,
		j.Status, j.OSSUploadID, j.PartsJSON, j.ErrorMessage, j.UpdatedAt, j.FinishedAt, j.ID)
	return err
}

func (d *DB) GetOSSUploadJob(id string) (*OSSUploadJob, error) {
	j := &OSSUploadJob{}
	err := d.QueryRow(`SELECT id, profile_id, object_key, file_name, file_size, sha256, chunk_size, status, oss_upload_id, parts_json,
		error_message, created_by, created_at, updated_at, finished_at FROM tbl_oss_upload_jobs WHERE id=?`, id).Scan(
		&j.ID, &j.ProfileID, &j.ObjectKey, &j.FileName, &j.FileSize, &j.SHA256, &j.ChunkSize, &j.Status, &j.OSSUploadID, &j.PartsJSON,
		&j.ErrorMessage, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt, &j.FinishedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decorateOSSUploadJob(j)
	d.fillOSSUploadJobProfile(j)
	return j, nil
}

func (d *DB) ListOSSUploadJobs(limit int) ([]OSSUploadJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(`SELECT id, profile_id, object_key, file_name, file_size, sha256, chunk_size, status, oss_upload_id, parts_json,
		error_message, created_by, created_at, updated_at, finished_at FROM tbl_oss_upload_jobs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OSSUploadJob
	for rows.Next() {
		var j OSSUploadJob
		if err := rows.Scan(&j.ID, &j.ProfileID, &j.ObjectKey, &j.FileName, &j.FileSize, &j.SHA256, &j.ChunkSize, &j.Status, &j.OSSUploadID, &j.PartsJSON,
			&j.ErrorMessage, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		decorateOSSUploadJob(&j)
		d.fillOSSUploadJobProfile(&j)
		out = append(out, j)
	}
	if out == nil {
		out = []OSSUploadJob{}
	}
	return out, rows.Err()
}

func (d *DB) fillOSSUploadJobProfile(j *OSSUploadJob) {
	if j == nil || strings.TrimSpace(j.ProfileID) == "" {
		return
	}
	s, err := d.GetOSSSetting(j.ProfileID)
	if err != nil || s == nil {
		return
	}
	j.ProfileEnv = s.Environment
	j.ProfileBucket = s.Bucket
}

// UpsertOSSUploadPart 登记分片（同 part_number 覆盖）。
func (d *DB) UpsertOSSUploadPart(jobID string, part OSSUploadPart) (*OSSUploadJob, error) {
	j, err := d.GetOSSUploadJob(jobID)
	if err != nil || j == nil {
		return nil, fmt.Errorf("上传任务不存在")
	}
	found := false
	for i := range j.Parts {
		if j.Parts[i].PartNumber == part.PartNumber {
			j.Parts[i] = part
			found = true
			break
		}
	}
	if !found {
		j.Parts = append(j.Parts, part)
	}
	if err := d.UpdateOSSUploadJob(j); err != nil {
		return nil, err
	}
	decorateOSSUploadJob(j)
	return j, nil
}
