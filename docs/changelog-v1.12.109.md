# KeePlus / auto-dba v1.12.109

相对 [v1.12.108](changelog-v1.12.108.md)。查询窗口的表名和字段名按名字中间的内容继续提示。无 Agent 协议变更，无表结构变更。

## 查询补全

- 表名和字段名按三档排序：名字开头最前，下划线分段开头其次，名字其它位置包含输入的排在后面。`shop` 可以提示 `dim_shop_real_item_prep_station`、`ods_bo_shop_tbl_area`。`discount` 可以提示 `actual_discount_amount`。字段注释不参与匹配。
- 没有更长的开头匹配时，只要还有包含匹配，列表保持打开。输入已经等于某个候选、且没有更长的名字以它开头时，仍然关掉。
- 关键字和函数仍按原来的规则。候选上限仍是 50 条。

## 打包与发版

```bash
STAMP=v1.12.109 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.109 -m "v1.12.109"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。刷新页面即可。
