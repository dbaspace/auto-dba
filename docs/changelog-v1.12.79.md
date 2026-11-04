# KeePlus / auto-dba v1.12.79

相对 [v1.12.78](changelog-v1.12.78.md)。无表结构变更。无 Agent 协议变更。

## 工单

- DDL 数据源下拉：在筛选框里删除关键字时，不再把已经勾选的地址一起清掉。
- DML 申请页在窗口较矮时，「提交」和「保存草稿」固定在左侧栏底部，上方字段单独滚动，避免提交按钮被挡住。

## 打包与发版

```bash
STAMP=v1.12.79 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.79 -m "v1.12.79"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
