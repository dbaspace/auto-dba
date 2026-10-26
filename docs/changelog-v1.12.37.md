# KeePlus / auto-dba v1.12.37

相对 [v1.12.36](changelog-v1.12.36.md)。无表结构变更，无配置项变更。

## 同步列表

- 筛选栏增加「刷新」，重新加载当前复制槽列表，不触发采集。

## 打包与发版

```bash
STAMP=v1.12.37 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.37
```

部署见 [deploy.md](deploy.md)。
