# KeePlus / auto-dba v1.12.45

相对 [v1.12.44](changelog-v1.12.44.md)。启动时创建客户端访问、云资源相关表（已存在则跳过），并写入菜单与全局参数缺省值。

## 客户端访问

- 配置中心增加「客户端访问授权」与「我的访问令牌」。管理员开通授权后，用户可签发 PAT，供本地 AI / CLI / MCP 调用白名单接口。机器通道默认强制 PAT，浏览器登录 JWT 不受影响。说明见 [client-access.md](client-access.md)。
- 加固开关默认关闭：看密二次验证、工单执行权限、只读会话、K8s tunnel Token、审计保留与拒绝告警等。

## 云资源

- 侧栏增加「云资源」：云 PostgreSQL / MySQL / Redis / 主机、自愈策略；配置中心「云账号」登记密钥。可同步资源、报价与提交变更工单，SQL 洞察可同步云慢日志。

## 工单与 SQL 执行器

- 远程 DDL/DML 进度更细：执行器返回语句级结果与会话号，列表和详情可跟到当前库与语句。服务重启后会回收仍标「执行中」但进程已不在的工单。
- PostgreSQL DML 可设 `lock_timeout`。执行器任务支持按已推送偏移拉取进度，停止后清理。
- 一体包、后端包和 Docker 镜像仍编译并带上 `deploy/sql-agent/bin/sql-agent`。下发优先用包内二进制。

## 主机终端

- WebSocket 约每 20 秒 Ping；SSH 带 `ServerAliveInterval`。Nginx 示例为 `/api/v1/terminal/ws` 单独配 Upgrade 与更长超时。

## 打包与发版

```bash
STAMP=v1.12.45 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.45
```

部署见 [deploy.md](deploy.md)。
