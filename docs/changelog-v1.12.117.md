# KeePlus / auto-dba v1.12.117

相对 [v1.12.116](changelog-v1.12.116.md)。实例总览的 StarRocks 容量改为对齐监控指标，标签管理只看当前租户，同步列表的批量重启不再点了没反应。无 Agent 协议变更。无表结构变更。

## 实例总览

- StarRocks 的数据存储、数据盘优先用与 Grafana Used / Total Capacity 相同的指标：`starrocks_be_disks_data_used_capacity`、`starrocks_be_disks_total_capacity`，按 idc、domain、cname 汇总。
- idc 与标签管理里的候选值对齐：等于环境编码，或以「编码-」开头的值（如 `beijing` 与 `beijing-t`）才参与匹配。主机看实例地址、域名，以及 domain、cname、host 标签。cname 标签未定义时不按序列上的 cname 匹配。
- 指标对不上或盘容量不是正数时，仍用 SHOW BACKENDS 的结果。数据使用率是数据存储除以数据盘总大小。

## 标签管理

- 标签列表和「同步监控标签」只作用于当前租户。平台审计不再把各租户的同名标签叠在一页，同步也不会改到别的租户。
- `idc`、`env` 的候选值保留已有自定义值，并补上环境管理里的编码（含 `singapore` 以及以后新增的环境）。监控目标注册看到的候选值与此一致。

## 同步列表

- 修复「批量重启」点击无反应。行是否可重启不再与权限开关重名，确认框可以正常弹出。

## 租户授权

- 资源授权记录可按类型、资源、授权对象搜索，并分页。撤销按钮留在授权表单里，避免被列表挤到看不见。

## 打包与发版

```bash
STAMP=v1.12.117 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.117 -m "v1.12.117"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。总览容量和标签列表要使用本版平台进程，请重启平台后再看。同步列表的批量重启刷新页面即可。
