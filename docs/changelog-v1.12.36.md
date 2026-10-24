# KeePlus / auto-dba v1.12.36

相对 [v1.12.35](changelog-v1.12.35.md)。无表结构变更，无配置项变更。

## Redis 查询

- 已打开的域名页签改到页面顶部，从左侧起排，不再贴在查询区中间。
- 去掉侧栏标题「Redis 域名」。
- 页签名只显示域名前 10 个字符，悬停仍显示完整域名。

## 打包与发版

```bash
STAMP=v1.12.36 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.36
```

部署见 [deploy.md](deploy.md)。
