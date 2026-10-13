# KeePlus / auto-dba v2.2.0

相对 [v2.0.0](https://github.com/dbaspace/auto-dba/releases/tag/v2.0.0) 的更新点。

## 字段加密加固

- 新增 `app.crypto_secret`（及 `crypto_secret_previous` / `crypto_key_id`），与 `jwt_secret` 职责分离：JWT 只签 token，字段加密用 crypto
- 新写入密文格式 `enc:v2:<kid>:…`；仍可读旧 `enc:v1:`；轮换时先填 previous 再改主密钥
- 未配置 `crypto_secret` 时回退 `jwt_secret`（兼容现网）；启动会提示建议单独配置
- 解密审计：失败与遗留明文打 `secret_decrypt` 日志（不含明文）
- 说明见 [nacos-config.md](nacos-config.md)、[features.md](features.md)（SSO/LDAP 与密钥关系）

## Nacos 引导示例

- 默认配置示例**不再写** `nacos.namespace`（省略 = public），避免只改 `data_id` 时误带 autosql ID 导致 `config data not exist`
- 非 public 时再显式填写 Namespace ID 或使用 `NACOS_NAMESPACE`
