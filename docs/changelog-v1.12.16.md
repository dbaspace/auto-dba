# KeePlus / auto-dba v1.12.16

相对 [v1.12.15](changelog-v1.12.15.md)。

## 工单申请

- DDL、DML、Kafka、Redis、权限申请、新增数据库、平台资源申请、DQL 提交成功后打开工单列表，并关闭当前提交页，不再进入工单详情。

## 邮件

- SMTP 增加认证方式 `auth_way`：`PLAIN`（默认）、`LOGIN`、`CRAM-MD5`。465 隐式 TLS 不再固定 `AUTH PLAIN`，避免对方返回 `500 Unrecognized command`。
- 增加发信限速 `rate`，未填时每秒最多 2 封。

## 打包与发版

```bash
STAMP=v1.12.16 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.16
```

部署见 [deploy.md](deploy.md)。
