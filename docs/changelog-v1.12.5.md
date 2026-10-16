# KeePlus / auto-dba v1.12.5

相对 [v1.12.4](changelog-v1.12.4.md)。

## StarRocks 登记与运维

- 节点状态以 `SHOW PROC` **Alive** 为准，避免 FE 实际在跑却显示「已停止」；Alive 时把库状态 `stopped` 写回 `ready`。
- 登记已有：自动探测 FE/BE **software_base**（`ADMIN SHOW FRONTEND CONFIG` / BE `:8040/varz`）与 BE **storage_root_path**；FE/BE 软件根可分开配置（`software_base` / `be_software_base`）。
- 集群列表/详情标注来源（**新建** / **登记**）；支持「注销登记」——仅删平台拓扑与变更，不停 FE/BE、不删 Agent，可再登记。
- 详情提供「重新探测目录」写回路径。

库表增量（启动 `ensureSrClusterTables` 也会自动加列）：

```sql
ALTER TABLE tbl_sr_clusters ADD COLUMN IF NOT EXISTS be_software_base TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_sr_clusters ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';
```

## 监控目标

- 修复「标签管理」删改牵连所有目标组：任务级与各组 label 隔离；FE/BE 可分别设 `app` 等。

## 登录会话（可选）

- 支持 Redis 会话登记与管理端「登录会话」页（默认关闭，不影响现网纯 JWT）；配置见 [nacos-config.md](nacos-config.md)。

## 其它

- 查询页 / 工单详情等界面小改进。

## 打包与发版

```bash
STAMP=v1.12.5 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.5
```

部署见 [deploy.md](deploy.md)、[sr-cluster.md](sr-cluster.md)。
