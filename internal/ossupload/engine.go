package ossupload

import (
	"context"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	DefaultChunkSize = 16 << 20 // 16 MiB
	MinChunkSize     = 5 << 20  // S3 非末片最小约 5MiB
	MaxChunkSize     = 100 << 20
)

// Engine 后台 Complete + SHA-256 校验。
type Engine struct {
	Store  *store.DB
	Secret string

	mu   sync.Mutex
	busy map[string]bool
}

func NewEngine(st *store.DB, secretKey string) *Engine {
	return &Engine{Store: st, Secret: secretKey, busy: map[string]bool{}}
}

func (e *Engine) clientFromProfile(profileID string) (*Client, *store.OSSSetting, error) {
	plain, err := e.Store.GetOSSSettingPlain(profileID)
	if err != nil || plain == nil {
		return nil, nil, fmt.Errorf("OSS 配置不存在")
	}
	sk := ""
	if plain.SecretEnc != "" {
		sk, err = secret.Decrypt(e.Secret, plain.SecretEnc)
		if err != nil {
			return nil, nil, fmt.Errorf("解密 SecretKey 失败: %w", err)
		}
	}
	cli, err := NewClient(plain.Endpoint, plain.Bucket, plain.AccessKey, sk, plain.CloudProduct)
	if err != nil {
		return nil, nil, err
	}
	return cli, plain, nil
}

// BuildObjectKey 按配置 prefix + 文件名生成对象键（不再追加 uploads/日期）。
func BuildObjectKey(prefix, fileName string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	if base == "" || base == "." || base == "/" {
		base = "upload.bin"
	}
	if prefix == "" {
		return base
	}
	return prefix + "/" + base
}

func NormalizeChunkSize(n int64) int64 {
	if n <= 0 {
		return DefaultChunkSize
	}
	if n < MinChunkSize {
		return MinChunkSize
	}
	if n > MaxChunkSize {
		return MaxChunkSize
	}
	return n
}

// InitUpload 创建 Multipart 任务。
func (e *Engine) InitUpload(ctx context.Context, profileID, fileName string, fileSize int64, sha256Hex string, chunkSize int64, createdBy string) (*store.OSSUploadJob, error) {
	cli, setting, err := e.clientFromProfile(profileID)
	if err != nil {
		return nil, err
	}
	sha256Hex = strings.ToLower(strings.TrimSpace(sha256Hex))
	if len(sha256Hex) != 64 {
		return nil, fmt.Errorf("sha256 须为 64 位十六进制")
	}
	if fileSize <= 0 {
		return nil, fmt.Errorf("file_size 无效")
	}
	if chunkSize <= 0 && setting.ChunkSizeMB > 0 {
		chunkSize = int64(setting.ChunkSizeMB) << 20
	}
	chunkSize = NormalizeChunkSize(chunkSize)
	objectKey := BuildObjectKey(setting.Prefix, fileName)
	uploadID, err := cli.CreateMultipartUpload(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	job := &store.OSSUploadJob{
		ProfileID:   profileID,
		ObjectKey:   objectKey,
		FileName:    fileName,
		FileSize:    fileSize,
		SHA256:      sha256Hex,
		ChunkSize:   chunkSize,
		Status:      store.OSSUploadUploading,
		OSSUploadID: uploadID,
		Parts:       []store.OSSUploadPart{},
		CreatedBy:   createdBy,
	}
	if err := e.Store.CreateOSSUploadJob(job); err != nil {
		_ = cli.AbortMultipartUpload(context.Background(), objectKey, uploadID)
		return nil, err
	}
	return e.Store.GetOSSUploadJob(job.ID)
}

// UploadPart 转发分片到 OSS 并登记。
func (e *Engine) UploadPart(ctx context.Context, jobID string, partNumber int, body io.Reader, size int64) (*store.OSSUploadJob, error) {
	job, err := e.Store.GetOSSUploadJob(jobID)
	if err != nil || job == nil {
		return nil, fmt.Errorf("上传任务不存在")
	}
	if job.Status != store.OSSUploadUploading {
		return nil, fmt.Errorf("任务状态不允许上传分片: %s", job.Status)
	}
	if partNumber < 1 {
		return nil, fmt.Errorf("part_number 无效")
	}
	total := int((job.FileSize + job.ChunkSize - 1) / job.ChunkSize)
	if partNumber > total {
		return nil, fmt.Errorf("part_number 超出范围")
	}
	expected := job.ChunkSize
	if partNumber == total {
		expected = job.FileSize - job.ChunkSize*int64(total-1)
	}
	if size != expected {
		return nil, fmt.Errorf("分片大小不符: got=%d want=%d", size, expected)
	}
	cli, _, err := e.clientFromProfile(job.ProfileID)
	if err != nil {
		return nil, err
	}
	etag, err := cli.UploadPart(ctx, job.ObjectKey, job.OSSUploadID, partNumber, body, size)
	if err != nil {
		return nil, err
	}
	return e.Store.UpsertOSSUploadPart(jobID, store.OSSUploadPart{
		PartNumber: partNumber,
		ETag:       etag,
		Size:       size,
	})
}

// EnqueueComplete 片齐后后台 Complete + 校验。
func (e *Engine) EnqueueComplete(jobID string) error {
	job, err := e.Store.GetOSSUploadJob(jobID)
	if err != nil || job == nil {
		return fmt.Errorf("上传任务不存在")
	}
	if job.Status != store.OSSUploadUploading {
		return fmt.Errorf("任务状态不允许完成: %s", job.Status)
	}
	total := int((job.FileSize + job.ChunkSize - 1) / job.ChunkSize)
	if len(job.Parts) < total {
		return fmt.Errorf("分片未齐: %d/%d", len(job.Parts), total)
	}
	seen := map[int]bool{}
	for _, p := range job.Parts {
		seen[p.PartNumber] = true
	}
	for i := 1; i <= total; i++ {
		if !seen[i] {
			return fmt.Errorf("缺少分片 %d", i)
		}
	}
	job.Status = store.OSSUploadCompleting
	job.ErrorMessage = ""
	if err := e.Store.UpdateOSSUploadJob(job); err != nil {
		return err
	}
	e.mu.Lock()
	if e.busy[jobID] {
		e.mu.Unlock()
		return nil
	}
	e.busy[jobID] = true
	e.mu.Unlock()
	go e.runComplete(jobID)
	return nil
}

func (e *Engine) runComplete(jobID string) {
	defer func() {
		e.mu.Lock()
		delete(e.busy, jobID)
		e.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	job, err := e.Store.GetOSSUploadJob(jobID)
	if err != nil || job == nil {
		return
	}
	cli, _, err := e.clientFromProfile(job.ProfileID)
	if err != nil {
		e.fail(job, err.Error())
		return
	}
	refs := make([]PartRef, 0, len(job.Parts))
	for _, p := range job.Parts {
		refs = append(refs, PartRef{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	if err := cli.CompleteMultipartUpload(ctx, job.ObjectKey, job.OSSUploadID, refs); err != nil {
		e.fail(job, "CompleteMultipart: "+err.Error())
		_ = cli.AbortMultipartUpload(context.Background(), job.ObjectKey, job.OSSUploadID)
		return
	}
	job.Status = store.OSSUploadVerifying
	_ = e.Store.UpdateOSSUploadJob(job)

	if err := cli.VerifySHA256(ctx, job.ObjectKey, job.FileSize, job.SHA256); err != nil {
		e.fail(job, "完整性校验失败: "+err.Error())
		return
	}
	job.Status = store.OSSUploadSuccess
	job.ErrorMessage = ""
	job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := e.Store.UpdateOSSUploadJob(job); err != nil {
		log.Printf("[ossupload] update success job %s: %v", jobID, err)
	}
}

func (e *Engine) fail(job *store.OSSUploadJob, msg string) {
	job.Status = store.OSSUploadFailed
	job.ErrorMessage = msg
	job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = e.Store.UpdateOSSUploadJob(job)
	log.Printf("[ossupload] job %s failed: %s", job.ID, msg)
}

// Abort 取消任务并 AbortMultipart。
func (e *Engine) Abort(ctx context.Context, jobID string) (*store.OSSUploadJob, error) {
	job, err := e.Store.GetOSSUploadJob(jobID)
	if err != nil || job == nil {
		return nil, fmt.Errorf("上传任务不存在")
	}
	if job.Status == store.OSSUploadSuccess {
		return nil, fmt.Errorf("已成功任务不可取消")
	}
	if job.Status == store.OSSUploadCompleting || job.Status == store.OSSUploadVerifying {
		return nil, fmt.Errorf("后台合并/校验中，请稍后再试")
	}
	cli, _, err := e.clientFromProfile(job.ProfileID)
	if err == nil && job.OSSUploadID != "" {
		_ = cli.AbortMultipartUpload(ctx, job.ObjectKey, job.OSSUploadID)
	}
	job.Status = store.OSSUploadAborted
	job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := e.Store.UpdateOSSUploadJob(job); err != nil {
		return nil, err
	}
	return e.Store.GetOSSUploadJob(jobID)
}
