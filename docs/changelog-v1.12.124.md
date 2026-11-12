# KeePlus / auto-dba v1.12.124

相对 [v1.12.123](changelog-v1.12.123.md)。SQL 执行器增加协议号。旧执行器在下发前被拦住，避免再用旧拆句把 `INSERT ... SELECT` 切成半截。无表结构变更。

## SQL 执行器

- sql-agent 的 `/healthz` 返回协议号。平台探测时，没有协议号或协议号过低，会提示「执行器版本过旧，请在 SQL 执行器页重新下发后重试」，任务不会下发。
- 本版改了 sql-agent 协议，**须用本版包重新下发 SQL 执行器**。只升级平台、不重下发执行器时，远端 DML/DDL 会在探测阶段失败。
- 下发内容仍是工单原文。当前执行器与平台使用同一套拆句，列清单换行后的 `SELECT` 会留在同一条 `INSERT` 里。

## 打包与发版

```bash
STAMP=v1.12.124 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.124 -m "v1.12.124"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。node-agent、db-agent、platform-agent 协议未改，不必为这个版本重装。SQL 执行器必须用本版包重下发。
