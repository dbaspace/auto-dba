# KeePlus / auto-dba v1.12.73

相对 [v1.12.72](changelog-v1.12.72.md)。无表结构变更。

## CDC 加表体验

- **旧 sql-agent 缺 meta**：`POST /v1/sql/meta` 返回 404 时改为 `ErrMetaUnsupported`（明确提示到「配置 → 执行器」用本版包重下发），不再误报「执行器上没有这个任务」。失败文案带执行器名；批量可选表 `failed` Alert 按相同 message 合并，升级类用 warning。
- **「可加 N/M」标签**：分母为已成功拉取源表的**父任务数**（带「父」单位与 Tooltip），避免与旁侧「分表 N」混淆；分表组按 members 取 min。
- **须用本版包重下发 sql-agent** 后，海外加表才会真正走 meta；仅升级平台仍会看到合并后的升级提示。包内含最新各 Agent 二进制。

## 平台 Agent 页

- KeepAlive 后台轮询只刷新 `deploys?limit=50`；sql-executors / environments 仅首次加载或打开下发表单时拉取。切离 `/config/agents` 时停止轮询，避免在 CDC 等其它页看到三连重复请求。

## 其它

- StarRocks 集群：启动节点与停止一样轮询变更并刷新节点 live。

## 打包与发版

```bash
STAMP=v1.12.73 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.73 -m "v1.12.73"
```

部署见 [deploy.md](deploy.md)。
