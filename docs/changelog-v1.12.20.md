# KeePlus / auto-dba v1.12.20

相对 [v1.12.19](changelog-v1.12.19.md)。

## CDC 同步任务

- 打开任务详情时，顶栏页签标题为「CDC 任务详情」，不再与列表页共用「CDC 同步任务」。
- 任务 id 校验放宽：支持 Hub 迁移的 `cdc-…` 等非 UUID id；仅排除 `create` 等与路由冲突的保留字。列表任务名链接带真实 `href`，便于新窗口打开。

## 打包与发版

```bash
STAMP=v1.12.20 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.20
```

部署见 [deploy.md](deploy.md)。
