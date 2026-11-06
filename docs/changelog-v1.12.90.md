# KeePlus / auto-dba v1.12.90

相对 [v1.12.89](changelog-v1.12.89.md)。云清单按内网地址绑定平台实例；实例列表改为批量删除；查询结果可按白名单直接导出。无 Agent 协议变更。

## 云资源

- PostgreSQL 同步只取内网地址。云实例 ID 对不上时，用内网地址对照同租户、同引擎实例的连接主机。恰好对上一台就挂上平台实例，并补齐空着的云厂商、云实例 ID 和云 Region。
- 实例编辑增加「同步云资源」。已填云实例 ID 时只查这一条；为空或对不上时再按连接主机对照。云账号、连接主机和密码不改。

## 资源配置

- 各实例类型列表去掉行内「删除」。勾选后可批量删除，翻页保留已选。失败的留在列表里，并列出名称和原因。

## 数据库查询

- 在导出白名单且未过期的用户，可把当前查询结果导出为 CSV、JSON 或 SQL，并打成带密码的压缩包。不在白名单时改为发起导出工单。
- 结果里的 JSON 文本格式化时保留大整数位数。

## CDC

- 创建任务时记下源端主机和端口。被授权域名的实例在列表里带上可拨号的域名，本租实例仍走连接主机。

## 数据库

启动时若缺表，会执行：

```sql
CREATE TABLE IF NOT EXISTS tbl_export_whitelist (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    db_ids TEXT NOT NULL DEFAULT '[]',
    expire_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_export_whitelist_user ON tbl_export_whitelist(username);
```

## 打包与发版

```bash
STAMP=v1.12.90 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.90 -m "v1.12.90"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
