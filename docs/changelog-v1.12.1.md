# KeePlus / auto-dba v1.12.1

相对 [v1.12.0](changelog-v1.12.0.md) 的已登记库列表体验补丁。

## 已登记库

- 列表增加**环境**、**域名**筛选（可与库名、引擎 Tab 组合）。
- 域名列按**库自身**绑定展示（`domains` / `tags` 的 `domain=`），不再把实例下全部域名摊到每一行；仅库未绑定时回退实例主域名。
- 环境优先取库 tags 的 `idc=`，否则回退实例 IDC。

## 打包与发版

```bash
STAMP=v1.12.1 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.1
```

部署见 [deploy.md](deploy.md)。
