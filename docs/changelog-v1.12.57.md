# KeePlus / auto-dba v1.12.57

相对 [v1.12.56](changelog-v1.12.56.md)。

## 权限申请 / 建库（PostgreSQL）

- PG 权限申请按库查 `pg_database.datdba`，生成可执行的 `ALTER DEFAULT PRIVILEGES FOR ROLE`；schema 仅 `GRANT USAGE`（无 CREATE）。
- 账号不存在则 `CREATE USER … ENCRYPTED PASSWORD`，执行成功后飞书私聊申请人（新建带密码 / 已存在注明沿用）。
- 建库开通账号的 PG 授权逻辑与权限申请对齐。

## 跨租可见与工单

- 工单提交目标校验：本租自有或跨租授权均可；授权仅控制可见/使用，**不**自动写入库审批人。
- 审核人接口对未配置场景返回明确提示；DDL 分库多实例展示各源审核人/提示。
- 跨租授权支持目标为**指定用户**（与目标租户互斥）；用户所属租户已有授权则跳过用户级重复授权。

## 其它

- Nacos：`GetConfig`/建客户端失败时回退本机其它进程留下的最近磁盘缓存，减轻断网启动失败。
- 用户管理列表分页；查询树加载表失败时展示后端错误文案。

## 打包与发版

```bash
STAMP=v1.12.57 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.57
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
