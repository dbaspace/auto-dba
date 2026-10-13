package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	prefixV1     = "enc:v1:"
	prefixV2     = "enc:v2:"
	defaultKeyID = "k1"
)

// AuditFunc 解密审计（勿记录明文或完整密文）。
type AuditFunc func(ok bool, format string)

var (
	auditFn atomic.Value // AuditFunc
	prevMu  sync.RWMutex
	prevKeys []string
	defaultKid atomic.Value // string
	plainWarn  uint64
)

func init() {
	defaultKid.Store(defaultKeyID)
	auditFn.Store(AuditFunc(nil))
}

// SetAudit 注册解密审计回调；传 nil 关闭。
func SetAudit(fn AuditFunc) {
	auditFn.Store(fn)
}

func audit(ok bool, format string) {
	v, _ := auditFn.Load().(AuditFunc)
	if v != nil {
		v(ok, format)
	}
}

// SetPreviousSecrets 轮换窗口内仍可用来解密的旧主密钥（不含当前 active）。
func SetPreviousSecrets(keys []string) {
	clean := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		clean = append(clean, k)
	}
	prevMu.Lock()
	prevKeys = clean
	prevMu.Unlock()
}

func previousSecrets() []string {
	prevMu.RLock()
	defer prevMu.RUnlock()
	if len(prevKeys) == 0 {
		return nil
	}
	out := make([]string, len(prevKeys))
	copy(out, prevKeys)
	return out
}

// SetDefaultKeyID 新密文写入使用的 kid（enc:v2）。
func SetDefaultKeyID(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = defaultKeyID
	}
	defaultKid.Store(id)
}

func activeKid() string {
	v, _ := defaultKid.Load().(string)
	if v == "" {
		return defaultKeyID
	}
	return v
}

func keyFrom(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func seal(secret, plain string) (string, error) {
	block, err := aes.NewCipher(keyFrom(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func open(secret, b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(keyFrom(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("密文不完整")
	}
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Encrypt 将明文加密为 enc:v2:<kid>:<b64>。已是 enc:v1/v2 则原样返回。
func Encrypt(secret, plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	if IsEncrypted(plain) {
		return plain, nil
	}
	b64, err := seal(secret, plain)
	if err != nil {
		return "", err
	}
	kid := activeKid()
	return prefixV2 + kid + ":" + b64, nil
}

// Decrypt 解密 enc:v1 / enc:v2；无前缀视为遗留明文（告警后原样返回）。
// 先用 secret，再试 SetPreviousSecrets 中的旧密钥。
func Decrypt(secret, enc string) (string, error) {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return "", nil
	}
	if !IsEncrypted(enc) {
		n := atomic.AddUint64(&plainWarn, 1)
		if n == 1 || n%100 == 0 {
			log.Printf("secret: decrypt got legacy plaintext (count=%d); re-save to upgrade to enc:v2", n)
		}
		audit(true, "plain")
		return enc, nil
	}

	candidates := []string{strings.TrimSpace(secret)}
	for _, p := range previousSecrets() {
		if p != "" && p != secret {
			candidates = append(candidates, p)
		}
	}

	var (
		format string
		payload string
		tryOpen func(sec string) (string, error)
	)

	switch {
	case strings.HasPrefix(enc, prefixV2):
		format = "v2"
		rest := strings.TrimPrefix(enc, prefixV2)
		kid, b64, ok := strings.Cut(rest, ":")
		if !ok || kid == "" || b64 == "" {
			audit(false, format)
			return "", errors.New("enc:v2 格式无效")
		}
		_ = kid // kid 仅标识；密钥材料由 active/previous 列表提供
		payload = b64
		tryOpen = func(sec string) (string, error) { return open(sec, payload) }
	case strings.HasPrefix(enc, prefixV1):
		format = "v1"
		payload = strings.TrimPrefix(enc, prefixV1)
		tryOpen = func(sec string) (string, error) { return open(sec, payload) }
	default:
		audit(false, "unknown")
		return "", errors.New("未知密文前缀")
	}

	var lastErr error
	for _, sec := range candidates {
		if sec == "" {
			continue
		}
		plain, err := tryOpen(sec)
		if err == nil {
			audit(true, format)
			return plain, nil
		}
		lastErr = err
	}
	audit(false, format)
	if lastErr == nil {
		lastErr = errors.New("无可用解密密钥")
	}
	return "", lastErr
}

// IsEncrypted 是否已是平台字段密文。
func IsEncrypted(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, prefixV1) || strings.HasPrefix(v, prefixV2)
}
