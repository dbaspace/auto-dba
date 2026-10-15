# KeePlus / auto-dba v1.12.2

相对 [v1.12.1](changelog-v1.12.1.md)：查询树短名约定与展示。

## 已登记库 / 数据查询

- tags 约定：`alias=` 为查询树短展示名（同环境同库名区分实例/业务；兼容 `label=`）；`idc=` 环境；`domain=` 访问域名。
- 查询页库树节点旁灰色括号**只显示 alias/label 值**；无则不展示，不再拼接整段 tags（如 idc、domain）。
- 已登记库「标签」表单项补充各 key 含义说明。

## 打包与发版

```bash
STAMP=v1.12.2 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.2
```

部署见 [deploy.md](deploy.md)。
