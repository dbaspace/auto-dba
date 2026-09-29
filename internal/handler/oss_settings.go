package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// OSSSettingsHandler 配置中心 · 按环境 OSS 配置。
type OSSSettingsHandler struct {
	Store  *store.DB
	Secret string
}

func (h *OSSSettingsHandler) List(c *gin.Context) {
	env := c.Query("environment")
	if env == "" {
		env = c.Query("env")
	}
	list, err := h.Store.ListOSSSettings(env)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.OSSSetting{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *OSSSettingsHandler) Get(c *gin.Context) {
	it, err := h.Store.GetOSSSetting(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "OSS 配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *OSSSettingsHandler) Create(c *gin.Context) {
	var req store.OSSSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	req.Environment = strings.TrimSpace(req.Environment)
	if req.Environment == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择环境"})
		return
	}
	req.CloudProduct = store.NormalizeOSSCloudProduct(req.CloudProduct)
	if req.CloudProduct == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择云产品"})
		return
	}
	if strings.TrimSpace(req.Endpoint) == "" || strings.TrimSpace(req.Bucket) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "访问地址与存储桶不能为空"})
		return
	}
	if strings.TrimSpace(req.AccessKey) == "" || isMaskedOSSSecret(req.AccessKey) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请输入 AccessKey ID"})
		return
	}
	if strings.TrimSpace(req.SecretKey) == "" || isMaskedOSSSecret(req.SecretKey) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请输入 AccessKey Secret"})
		return
	}
	if exist, _ := h.Store.GetOSSSettingByEnv(req.Environment); exist != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该环境已存在 OSS 配置，请编辑现有记录"})
		return
	}
	enc, err := secret.Encrypt(h.Secret, strings.TrimSpace(req.SecretKey))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	it := store.OSSSetting{
		ID:            uuid.NewString(),
		Environment:   req.Environment,
		CloudProduct:  req.CloudProduct,
		Endpoint:      req.Endpoint,
		Bucket:        req.Bucket,
		Prefix:        req.Prefix,
		AccessKey:     strings.TrimSpace(req.AccessKey),
		SecretEnc:     enc,
		RetentionDays: req.RetentionDays,
		ChunkSizeMB:   req.ChunkSizeMB,
		CreatedBy:     actorName(c),
		UpdatedBy:     actorName(c),
	}
	if err := h.Store.CreateOSSSetting(&it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetOSSSetting(it.ID)
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *OSSSettingsHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetOSSSetting(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "OSS 配置不存在"})
		return
	}
	var req store.OSSSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	req.Environment = strings.TrimSpace(req.Environment)
	if req.Environment == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择环境"})
		return
	}
	req.CloudProduct = store.NormalizeOSSCloudProduct(req.CloudProduct)
	if req.CloudProduct == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择云产品"})
		return
	}
	if strings.TrimSpace(req.Endpoint) == "" || strings.TrimSpace(req.Bucket) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "访问地址与存储桶不能为空"})
		return
	}
	if other, _ := h.Store.GetOSSSettingByEnv(req.Environment); other != nil && other.ID != exist.ID {
		c.JSON(http.StatusBadRequest, gin.H{"message": "该环境已存在其它 OSS 配置"})
		return
	}

	plain, err := h.Store.GetOSSSettingPlain(exist.ID)
	if err != nil || plain == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "读取现有配置失败"})
		return
	}

	ak := strings.TrimSpace(req.AccessKey)
	if ak == "" || isMaskedOSSSecret(ak) {
		ak = plain.AccessKey
	}
	skEnc := ""
	if sk := strings.TrimSpace(req.SecretKey); sk != "" && !isMaskedOSSSecret(sk) {
		enc, eerr := secret.Encrypt(h.Secret, sk)
		if eerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": eerr.Error()})
			return
		}
		skEnc = enc
	}

	it := store.OSSSetting{
		ID:            exist.ID,
		Environment:   req.Environment,
		CloudProduct:  req.CloudProduct,
		Endpoint:      req.Endpoint,
		Bucket:        req.Bucket,
		Prefix:        req.Prefix,
		AccessKey:     ak,
		SecretEnc:     skEnc,
		RetentionDays: req.RetentionDays,
		ChunkSizeMB:   req.ChunkSizeMB,
		UpdatedBy:     actorName(c),
	}
	if err := h.Store.UpdateOSSSetting(&it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetOSSSetting(it.ID)
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *OSSSettingsHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteOSSSetting(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *OSSSettingsHandler) Test(c *gin.Context) {
	var req store.OSSSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	cfg := store.MigrationOSSSettings{
		Endpoint:  strings.TrimSpace(req.Endpoint),
		Bucket:    strings.TrimSpace(req.Bucket),
		Prefix:    strings.TrimSpace(req.Prefix),
		AccessKey: strings.TrimSpace(req.AccessKey),
		SecretKey: strings.TrimSpace(req.SecretKey),
	}
	// 编辑态脱敏：用环境已存密钥补齐
	if (cfg.AccessKey == "" || isMaskedOSSSecret(cfg.AccessKey) || cfg.SecretKey == "" || isMaskedOSSSecret(cfg.SecretKey)) && strings.TrimSpace(req.Environment) != "" {
		plain, err := h.Store.GetOSSSettingPlainByEnv(req.Environment)
		if err == nil && plain != nil {
			if cfg.AccessKey == "" || isMaskedOSSSecret(cfg.AccessKey) {
				cfg.AccessKey = plain.AccessKey
			}
			if cfg.SecretKey == "" || isMaskedOSSSecret(cfg.SecretKey) {
				if sk, derr := secret.Decrypt(h.Secret, plain.SecretEnc); derr == nil {
					cfg.SecretKey = sk
				}
			}
			if cfg.Endpoint == "" {
				cfg.Endpoint = plain.Endpoint
			}
			if cfg.Bucket == "" {
				cfg.Bucket = plain.Bucket
			}
		}
	}
	ok, msg := dbmig.TestOSS(c.Request.Context(), cfg)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": msg})
}

func isMaskedOSSSecret(v string) bool {
	s := strings.TrimSpace(v)
	if s == "" {
		return true
	}
	for _, c := range s {
		if c != '*' && c != '•' && c != '·' {
			return false
		}
	}
	return len(s) >= 4
}
