# KeePlus / auto-dba v1.12.34

相对 [v1.12.33](changelog-v1.12.33.md)。无表结构变更，无配置项变更。

## Redis 查询

- 每个域名单独开一个工作页签，命令、逻辑库和查询结果互不覆盖。关掉页签会终止该页签上正在跑的查询。
- 左侧点域名展开或收起逻辑库，可同时展开多个域名。

## 打包与发版

```bash
STAMP=v1.12.34 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.34
```

部署见 [deploy.md](deploy.md)。
