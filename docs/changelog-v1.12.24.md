# KeePlus / auto-dba v1.12.24

相对 [v1.12.23](changelog-v1.12.23.md)。无表结构 DDL；启动时向 `ticket_review_rules` 合并新规则 `SQLRequireSemicolon`（已有库自动补缺省项）。

## 工单 SQL 体验

- **SQL 美化**：前端改用 `sql-formatter`（PostgreSQL 方言），支持多语句、`--` 注释与 `INTERVAL '1 second'`；已是规范格式时提示。
- **关键字高亮编辑框**：工单填写 SQL / 通用查询共用高亮编辑器；修复大批量脚本高度错位导致无法编辑。
- **详情执行 SQL**：默认折叠长脚本，支持展开/收起与一键复制完整提交 SQL。
- **检测结果**：按视口高度分页；支持按状态（全部/通过/警告/错误）过滤；结果表含序号。
- **审核规则 `SQLRequireSemicolon`**：多语句缺分号强制分段时检测失败（默认开启，可在审核规则中关闭）。不改变执行侧拆句行为。
- **执行明细**：失败与 records 中的 SQL 保留全文，避免弹窗只能看到截断片段；历史截断记录弹窗提示「展示数据不全」。

## 打包与发版

```bash
STAMP=v1.12.24 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.24
```

部署见 [deploy.md](deploy.md)。
