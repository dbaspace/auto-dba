# KeePlus / auto-dba v1.12.123

相对 [v1.12.122](changelog-v1.12.122.md)。实例总览主机增加系统盘使用率和磁盘增长分析，分析报告可保存并单独打开。无 Agent 协议变更。新增分析报告表，启动时自动创建。

## 实例总览 · 主机

- 去掉主机列表的「操作」列。
- 数据盘使用率旁增加「系统盘使用率」，只统计挂载点 `/`。达到 80% 时状态显示「系统盘偏高」。
- 汇总卡片右侧增加「智能分析」。可按域名、地址、环境，以及自定义时间范围分析数据盘和系统盘增长。时间跨度至少 1 天、最多 3 个月，结束时间不能晚于当前。
- 点击「开始分析」后任务在后台执行，页面不阻塞。同一租户同时只跑一个分析。完成后从「打开以前的报告」打开。
- 历史报告下拉在「开始分析」按钮右侧。选中后单独打开页签「磁盘分析报告」，展示开始和结束时间。报告时间按本地时区显示。

## 分析报告

- 报告写入 `tbl_analysis_reports`，主机磁盘分析的 `kind` 为 `host_disk`。后续其它分析复用这张表。
- 启动迁移会建表。已有库若跳过全量迁移，可在元数据库执行：

```sql
CREATE TABLE IF NOT EXISTS tbl_analysis_reports (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  username TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  scope TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_analysis_reports_tenant_kind
  ON tbl_analysis_reports(tenant_id, kind, created_at DESC);
```

## 开发自测

- 环境变量 `AUTO_DBA_HTTP_PORT` 覆盖本地配置和 Nacos 里的 `http.port`。自测请用 `18800`，不要占用日常开发的 `8800`。

## 打包与发版

```bash
STAMP=v1.12.123 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.123 -m "v1.12.123"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。磁盘分析和报告要使用本版平台进程与前端，请重启平台并刷新页面。
