# KeePlus / auto-dba v1.12.33

相对 [v1.12.32](changelog-v1.12.32.md)。无表结构变更，无配置项变更。

## 实例总览

- 表格占满筛选和汇总下面的剩余高度，只在表体里纵向滚动。筛选、说明和分页留在视口内。

## 打包与发版

```bash
STAMP=v1.12.33 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.33
```

部署见 [deploy.md](deploy.md)。
