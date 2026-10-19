# KeePlus / auto-dba v1.12.17

相对 [v1.12.16](changelog-v1.12.16.md)。无数据库脚本。

## 用户管理

- 「同步 Lark」只处理登录过本平台的用户（内置 admin 除外）。先按飞书邮箱、企业邮箱、手机号批量点查；没对上的再读国际版通讯录，按企业邮箱比对，全部对上即停。
- 列表展示企业邮箱、飞书邮箱和同步状态。编辑用户可填写飞书邮箱。已同步用户的工单私聊按 open_id、user_id、飞书邮箱发送，不再把企业邮箱当私聊地址。
- 飞书身份写在已有用户资料 JSON 中，不新增表或字段。凭证仍用 Nacos `notify.provider.lark`。

## SQL 审核

- `WITH ... INSERT` 已写列清单时不再误报「INSERT 必须指定列清单」。

## 打包与发版

```bash
STAMP=v1.12.17 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.17
```

部署见 [deploy.md](deploy.md)。
