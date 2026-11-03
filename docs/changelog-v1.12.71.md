# KeePlus / auto-dba v1.12.71

相对 [v1.12.70](changelog-v1.12.70.md)。无表结构变更。

## 划拨后目标租户仍能用源实例

- **任务附实例摘要**：列表、详情返回 `source_instance` / `target_instance`（不含账号密码）。目标租户看不到实例列表时，详情仍显示实例名。
- **按任务放行源/目标实例**：可选表、Publication、结构对比、Routine Load、加表提交与预检，任务归属当前租户即可使用该任务自己的源/目标实例。实例归属与 `GET /instances` 列表不变。
- **划拨不再警告可见性**：去掉「目标租户看不到源端实例」提示；划拨不改实例归属。

## 新增表：生效地址、批量可选表、核对 publication

- **创建页只显示一个源端地址**：父任务选择、确认页 PG 实例与「源端」列改用生效地址（任务 `source_host` > Debezium hostname > 实例 host），不再拼出两个地址。
- **批量可选表**：`POST /cdc-sync/tasks/batch-add-table/available-tables` 按每个父任务连本库取表并减占用，返回并集与「可加 n/总数」。接口 404 时前端回退单父接口。
- **占用核对源库 publication**：记录已加但 publication 缺失的表不再占用，可重新加；进行中的加表子任务仍占用。连不上源库时回退原口径。
- **跳过该库不存在的表**：批量预检与提交跳过父任务源库里没有的表；确认页「跳过」列悬浮显示原因。
- **跨实例提示**：已选父任务分布在多个源实例时警告；第 2 步显示代表任务实例与生效地址。

## 打包与发版

```bash
STAMP=v1.12.71 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.71 -m "v1.12.71"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
