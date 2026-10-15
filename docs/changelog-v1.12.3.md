# KeePlus / auto-dba v1.12.3

相对 [v1.12.2](changelog-v1.12.2.md) 的界面微调。

## 界面

- 左侧导航栏宽度 220 → **192**，减少空白。
- 数据查询库树：`alias` 短名**不再加方括号**（如显示 `bj` 而非 `[bj]`）。
- 已登记库 / 域名管理：引擎类型 Tab **固定露出 6 个**，其余收入右侧「…」下拉。

## 打包与发版

```bash
STAMP=v1.12.3 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.3
```

部署见 [deploy.md](deploy.md)。
