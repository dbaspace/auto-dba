# KeePlus / auto-dba v1.12.38

相对 [v1.12.37](changelog-v1.12.37.md)。无表结构变更。启动时若缺少下列全局参数则写入缺省值，已有值不覆盖。

## DDL 工单

- 全局参数 `ticket_ddl_constraint_check_enabled` 默认开启。设为 `false` / `0` / `no` / `off` 时，DDL 跳过全部 SQL 审核，只保留提交门禁；检测页提示已关闭，工单详情记「提交时已关闭 DDL 约束检查」。
- PostgreSQL `ADD COLUMN IF NOT EXISTS` 在列注释规则开启时，仍要求工单里带 `COMMENT ON COLUMN`。跨行的 `ALTER TABLE ... ADD` 也会命中这条规则。

## StarRocks 集群

- FE 软件根、BE 软件根、BE 数据目录改为全局默认：`sr_fe_software_base`、`sr_be_software_base`、`sr_be_data_root`（缺省 `/home/dorisdb_base`、`/home/dorisdb_base`、`/data`）。集群或节点单独填写时优先。BE 软件根不再在为空时回退到 FE 路径。
- 工作台可查看并保存这三项全局路径（`GET` / `PUT /sr-cluster/layout-defaults`），须为绝对路径。
- 启动、停止变更成功后的摘要使用步骤标题（含主机）。BE 实时启动时间读取 `LastStartTime`。

## 实例总览

- 表格不再锁在页内滚动，整页随内容滚动。

## 打包与发版

```bash
STAMP=v1.12.38 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.38
```

部署见 [deploy.md](deploy.md)。
