# KeePlus / auto-dba v1.12.22

相对 [v1.12.21](changelog-v1.12.21.md)。已有库启动时补定时任务表与「定时任务」菜单（`schema_version` / `seed_version` 递增），无需单独执行 SQL。

## 定时任务中心

- 新增配置页「定时任务」`/config/scheduled-jobs`：统一启停、改调度、立即执行、查看执行史与租约。
- 平台周期任务（会话对账、PG 诊断/空间、AWR、SQL Insight、迁移/归档/备份调度、工单超时、CDC 结构巡检、告警等）接入 `jobreg` 注册与多副本租约，由统一调度器驱动。

## CDC

- 详情表映射分表源表用网格展示；Publication 相关逻辑拆出独立模块，交互与样式对齐。

## K8s（方案）

- 增加 [k8s.md](k8s.md) 多集群纳管与数据库上 K8s 产品技术方案；附带本地 `scripts/k8s/kind-up.sh` 辅助脚本。实现仍按方案分阶段推进。

## 打包与发版

```bash
STAMP=v1.12.22 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.22
```

部署见 [deploy.md](deploy.md)。
