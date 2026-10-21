# KeePlus / auto-dba v1.12.27

相对 [v1.12.26](changelog-v1.12.26.md)。启动时幂等补菜单「AI助手」（`/ops/ai-assistant`）和「实例总览」（`/ops/instance-overview`）。无独立 DDL。

## AI 助手

- 整页 **AI助手** `/ops/ai-assistant`，与知识库内「DBA 助手」共享会话。默认优先知识库直答，可强制走大模型。
- 问句按整句解析（动作 / 对象 / 症状）；只读查询可在会话上打开 `transaction_read_only` / `default_transaction_read_only`。写操作与危险 SQL 拒绝，并可按通知配置外发（默认关闭：接收人、额外邮箱）。
- 指标查询走 Prometheus；统计脚本在受限 Python 环境执行。

## 实例总览

- 性能诊断下 **实例总览** `/ops/instance-overview`：已登记实例横向一览。打开只读元库；点刷新才限流采集 PostgreSQL / Redis 的版本、角色、连接、内存、从库延迟。不是监控曲线。
- 监控页支持 `?instance=` 预选实例。

## 同步列表

- 采集按全局唯一的 `slot_name` 覆盖同一行（主机写法不同也不另插一行），并保留 id。
- 操作日志分页（第 1 页为最新），`GET /sync-slots/logs?page=&page_size=`。

## 打包与发版

```bash
STAMP=v1.12.27 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.27
```

部署见 [deploy.md](deploy.md)。
