# KeePlus / auto-dba v1.12.60

相对 [v1.12.59](changelog-v1.12.59.md)。

## DDL 数据源选择

- 多选下拉内置关键字过滤（域名/实例名）、全选/反选当前匹配项。
- 选项展示勾选态；收起时标签数量收敛；避免选择过程中误报红色校验描边。

## 打包与发版

```bash
STAMP=v1.12.60 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.60
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
