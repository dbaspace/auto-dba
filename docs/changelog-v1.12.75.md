# KeePlus / auto-dba v1.12.75

相对 [v1.12.74](changelog-v1.12.74.md)。无表结构变更。

## CDC 批量加表：「创建成功 0」与卡顿

- **BatchAddTable / BatchAlterSchema**：待加表优先取请求体 `tables`（与预检一致），再回落 Description；空表返回 400，避免「创建 0」假成功。
- **跳过文案**：已同步 vs 库不存在/缺表分开提示。
- **前端**：`created <= 0` 弹 Modal 列父任务明细，不 toast 成功、不跳转；确认页全部待加为 0 时禁用提交（加表/加列）。
- **包体减负**：available-tables 缺表/占用只回 `missing_parent_ids` / `occupied_parent_ids`，前端用 `parents` 解析展示。
- **meta 合并**：同执行器 + 同 host/port/user 合并一次 FetchMeta，减轻「下一步」多实例拆请求。

根因以业务跳过/空表路径与 UX 为主；sql-agent 仅在 meta 拉取失败时间接影响占用判断。

## 打包与发版

```bash
STAMP=v1.12.75 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.75 -m "v1.12.75"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。
