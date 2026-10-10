package srcluster

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// OSSClient 绑定某个配置中心 OSS + 制品 prefix 的通用客户端（供 db-deploy 等复用）。
type OSSClient struct {
	rt *ossRuntime
}

// NewOSSClient prefix 为空时不加默认前缀，由调用方决定对象键。
func NewOSSClient(st *store.DB, secretKey, ossSettingID, prefix string) (*OSSClient, error) {
	if st == nil {
		return nil, fmt.Errorf("store 未初始化")
	}
	if strings.TrimSpace(ossSettingID) == "" {
		return nil, fmt.Errorf("请先在工作台设置中选择配置中心 OSS，并填写制品 prefix")
	}
	rt, err := newOSSRuntime(st, secretKey, ossSettingID, prefix, "")
	if err != nil {
		return nil, err
	}
	return &OSSClient{rt: rt}, nil
}

// Prefix 规范化后的制品前缀（以 / 结尾，可能为空）。
func (c *OSSClient) Prefix() string { return c.rt.Prefix }

func (c *OSSClient) Upload(ctx context.Context, objectKey string, body io.Reader, size int64, contentType string) error {
	return putAWSv4(ctx, c.rt, objectKey, body, size, contentType)
}

func (c *OSSClient) List(ctx context.Context, prefix string) ([]OSSObject, error) {
	return listObjectsV2(ctx, c.rt, prefix)
}

// Hash 流式 GET 整个对象计算 sha256。
func (c *OSSClient) Hash(ctx context.Context, objectKey string) (string, int64, error) {
	return hashObjectAWSv4(ctx, c.rt, objectKey)
}

// ReadSmall 读取小对象（侧车 / 清单），最多 limit 字节。
func (c *OSSClient) ReadSmall(ctx context.Context, objectKey string, limit int64) ([]byte, error) {
	return getObjectBytes(ctx, c.rt, objectKey, limit)
}

func (c *OSSClient) PresignGET(objectKey string, expire time.Duration) (string, error) {
	if expire <= 0 {
		expire = 2 * time.Hour
	}
	return presignAWSv4GET(c.rt, objectKey, expire)
}

// KnownSHA256 不下载包本体取 sha256：{key}.sha256 侧车 > 同目录 SHA256SUMS > 对象元数据；都没有返回空串。
// objects 为同目录列举结果，用于判断侧车 / 清单是否存在，避免无谓请求。
func (c *OSSClient) KnownSHA256(ctx context.Context, objectKey string, objects []OSSObject) string {
	has := map[string]bool{}
	for _, o := range objects {
		has[o.Key] = true
	}
	if has[objectKey+".sha256"] {
		if sum, err := readSHA256Sidecar(ctx, c.rt, objectKey+".sha256"); err == nil {
			return sum
		}
	}
	dir := path.Dir(objectKey)
	for _, name := range []string{"SHA256SUMS", "sha256sums.txt", "SHA256SUMS.txt"} {
		k := dir + "/" + name
		if !has[k] {
			continue
		}
		if sum, err := readSHA256FromSums(ctx, c.rt, k, path.Base(objectKey)); err == nil {
			return sum
		}
	}
	if _, meta, err := headObjectMeta(ctx, c.rt, objectKey); err == nil && meta != "" {
		return meta
	}
	return ""
}
