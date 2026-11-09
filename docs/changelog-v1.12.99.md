# KeePlus / auto-dba v1.12.99

相对 [v1.12.98](changelog-v1.12.98.md)。PAT 不再接受敏感接口和脚本上传，查询进一步拦住账号、口令和平台登记表。每条访问授权必须填写来源地址，不在名单里的调用会被拒绝。补充 CLI 操作说明。无 Agent 协议变更。授权表新增 `source_cidrs`。

## PAT

- 客户端访问总开关关闭时，PAT 直接拒绝。浏览器登录不受这道门禁影响。
- 令牌只认 `Authorization: Bearer`。URL 参数里的 `adba_pat_` 不再当作登录凭据。
- 平台用户、登录身份、实例详情、登记账号、DCL、Nacos、系统设置、全局参数、FE/DS/K8s、对象存储、备份、平台 Agent、工单、建库、Redis RDB、上传和打开终端，PAT 一律拒绝。白名单写宽也不放行。
- 不接受 `multipart/form-data`，也不接受文件名为 `.js`、`.mjs`、`.sql`、`.txt`、`.sh` 的提交。
- PAT 的 `SHOW` 只保留 `TABLES`、`DATABASES`、`SCHEMAS`、`COLUMNS`、`INDEX`、`CREATE TABLE`、`CREATE VIEW`。`SHOW GRANTS`、`SHOW CREATE USER` 拒绝。
- 语句里出现 `pg_authid`、`pg_shadow`、`mysql.user`、读文件函数、`dblink`，或 `tbl_users`、`tbl_instances`、`tbl_db_accounts`、`tbl_client_pats`、`tbl_global_args`、`tbl_nacos_admin`、`tbl_oss_settings`，会被拒绝。浏览器查询页不走这道 PAT 检查。
- 查询、Redis 只读、Kafka 主题和消息仍可调用。`SELECT` 末尾必须自带 `LIMIT` 的规则不变。

## 来源地址

- 开通授权必须填写来源地址，支持单个 IP 和 CIDR，换行、逗号或中文逗号分隔。名单为空或格式不对时，该用户的 PAT 不能调用。
- 已开通的授权可在列表里改来源，立即作用于已经签发的 PAT。已有授权在补列后名单为空，需要管理员补上调用方地址后才能继续用。
- 来源按 TCP 对端判断，不读取未登记代理的 `X-Forwarded-For`。平台前面有反向代理时，在全局参数 `client_access_trusted_proxies` 填写代理 IP 或网段，才会采用转发头里最左侧的地址。
- 接口审计里的来源 IP 与这道判断使用同一个地址。

## 文档

- 新增 [client-access-cli.md](client-access-cli.md)，说明开通授权、签发 PAT、CLI 查询和常见 403。功能说明和运维手册已链到该文档。

## 打包与发版

```bash
STAMP=v1.12.99 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.99 -m "v1.12.99"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。平台进程需重启后，PAT 收紧和来源限制才生效。授权页的来源地址刷新即可。

已有库可在元数据库执行后再重启：

```sql
ALTER TABLE tbl_client_access_grants ADD COLUMN IF NOT EXISTS source_cidrs TEXT NOT NULL DEFAULT '';
```
