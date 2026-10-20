package server

import (
	"log"
	"strings"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const instanceCryptoSampleLimit = 20

// warnIfInstanceSecretsUnreadable 启动时抽样解密实例密码；失败偏多则打醒目日志，避免只在测连时才发现密钥漂移。
func warnIfInstanceSecretsUnreadable(db *store.DB, fieldCrypto string) {
	if db == nil {
		return
	}
	fieldCrypto = strings.TrimSpace(fieldCrypto)
	samples, err := db.SampleEncryptedInstancePasswords(instanceCryptoSampleLimit)
	if err != nil {
		log.Printf("boot: WARN 抽样实例密文失败: %v", err)
		return
	}
	if len(samples) == 0 {
		return
	}
	fail := 0
	for _, enc := range samples {
		if _, err := secret.Decrypt(fieldCrypto, enc); err != nil {
			fail++
		}
	}
	n := len(samples)
	// ≥3 条失败，或抽样失败率 ≥50%
	if fail >= 3 || (n > 0 && fail*2 >= n) {
		log.Printf("ERROR: 实例密码抽样解密失败 %d/%d（密钥可能与入库时不一致）。请检查 app.crypto_secret 是否固定配置；"+
			"若曾改 jwt_secret/crypto_secret，将旧密钥填入 crypto_secret_previous 后重启，或到实例页重填密码",
			fail, n)
		return
	}
	if fail > 0 {
		log.Printf("boot: WARN 实例密码抽样解密失败 %d/%d，请核对 crypto_secret / crypto_secret_previous", fail, n)
	}
}
