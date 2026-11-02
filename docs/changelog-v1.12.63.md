# KeePlus / auto-dba v1.12.63

相对 [v1.12.62](changelog-v1.12.62.md)。

## CDC 新增同步表 / 源表加列（对齐 Hub）

- **跨多实例批量选父**：去掉同源实例限制；父任务上限 50→128；支持「全选当前筛选」。
- **选父体验**：环境 + 任务名过滤工具栏；`init-list` 支持 `env`/`q`；列表补源端 host:port、已同步数；Steps 不再被挤遮。
- **批量预检**：逐父返回 checks；补 `batch-alter-schema/precheck`；确认页展示批量提交计划；选表步多父摘要。
- **已同步占用**：含 pending/error 子任务占用源表；单/批量创建硬过滤，全跳过拒绝。
- **审批开关**：`sync_approval_enabled=false` 时免审直跑编排。
- **分表扩展**：目标已在父 mappings 时跳过重复 SR/RL；Debezium include.list 无变化则 skip PUT。
- **韧性**：batch 接口 404 时前端逐父单建并汇总失败。

## 打包与发版

```bash
STAMP=v1.12.63 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.63 -m "v1.12.63"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
