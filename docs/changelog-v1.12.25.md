# KeePlus / auto-dba v1.12.25

相对 [v1.12.24](changelog-v1.12.24.md)。无独立 DDL 迁移脚本；启动时幂等补菜单（备份恢复、MySQL 慢 SQL、定时任务编辑/触发动作）与 jobreg 注册项。能力缺口对照见 [roadmap-gap-fill.md](roadmap-gap-fill.md)。

## 备份恢复

- **恢复工单** `/ops/backup/restore`：逻辑恢复、物理全量、PITR 骨架；MySQL 物理增量沿 `parent_run_id` 拼 base→tip 链。
- Backup Agent 增加 `/v1/restore/logical|physical|pitr`；平台 `POST /backup/restore` 审批后下发。详见 [backup.md](backup.md)。

## CDC / MySQL

- 源引擎可选 **MySQL**：Debezium MySqlConnector、编排预检、`database.server.id` 与实例 `@@server_id` 及同源活跃任务去重。
- 复制槽采集纳入 jobreg（`sync.slots.collect`）；分表 Publication / 创建页交互继续打磨。

## 诊断与会话

- **MySQL 慢 SQL** `/ops/mysql-slow`：performance_schema digest Top SQL（只读）。
- 会话管理支持 MySQL 族 processlist + KILL。

## 工单与定时任务

- 工单 **预约到期自动执行**（`ticket.scheduled_exec`）；执行摘要带「成功/计划」语句数（按分号拆分）。
- 定时任务：编辑/触发独立动作 URN；失败可走邮件 + 告警向机器人（`notify.JobFailed`）；managed args 落库。

## HA / 闪回 / 结构对比 / K8s

- HA ForcedFailover 对称补齐（ProxySQL / repmgr / Sentinel 等路径）。
- 闪回多云 WAL：腾讯云路径增强；阿里云 / 华为 / AWS 为 HTTP stub 骨架。
- MySQL 结构对比：列 MODIFY、索引 DROP+ADD。
- K8s：Operator 可安装或仅检测；Helm CLI Day2 骨架；生产变更工单 `k8s_change`；隧道与决策文档对齐 [k8s.md](k8s.md)。

## 打包与发版

```bash
STAMP=v1.12.25 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.25
```

部署见 [deploy.md](deploy.md)。
