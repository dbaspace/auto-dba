# 本地 AI / CLI / MCP 客户端访问

## 一期（已交付）

- 管理员开通 **客户端访问授权**；用户签发 **PAT**。
- 机器通道强制 PAT；浏览器 JWT 不受影响。
- 接口审计 + 查询 SQL 审计（`client_type`）。
- 文档路径：配置 → 客户端访问授权 / 我的访问令牌。

### 白名单（含二期 Redis/Kafka）

| 方法 | 路径 |
|------|------|
| POST | `/api/v1/query` |
| POST | `/api/v1/query/table-info` |
| GET | `/api/v1/databases/:id/tables` |
| GET | `/api/v1/query/favorites`、`/history` |
| POST | `/api/v1/instances/:id/redis/execute` |
| GET | `/api/v1/instances/:id/kafka/topics` |
| GET | `/api/v1/instances/:id/kafka/topics/:topic/messages` |

可选（须全局参数 `client_access_allow_credential_scope=true` 且 PAT 含 `credential`）：

- `GET /api/v1/db-accounts/:id?view_credential=1`

### 豁免

`/feadmin/`、`/dsadmin/`、`/copilot/`、`/k8s/`、`/auth/`、`/settings/`（JWT 项目间/助手不强制 PAT）。

## 二期加固开关（默认全部 false，不破坏现网）

| 全局参数 | 作用 |
|----------|------|
| `client_access_harden_view_credential` | 看密需 URN/账号菜单；创建响应不回传 `real_password` |
| `client_access_harden_step_up` | 看密前 `POST /api/v1/auth/step-up`（约 10 分钟） |
| `client_access_harden_ticket_execute` | 工单执行须提交人/审批人/特权或 `um.action.tickets.execute` |
| `client_access_harden_k8s_tunnel` | tunnel 须 `token`/`X-Agent-Token` 匹配 `k8s_agent_tunnel_token` |
| `client_access_harden_readonly_session` | `/query` 启用 `WithReadOnlySession` |
| `client_access_allow_credential_scope` | 允许 PAT 含 `credential` |
| `client_access_audit_retention_days` | 审计保留天数（默认 90；任务 `client_access.audit_purge`） |
| `client_access_security_alert_threshold` | 10 分钟内拒绝达阈值通知管理员（默认 20） |
| `k8s_agent_tunnel_token` | Agent 共享 Token |

开启加固前请先给角色配好对应菜单/动作 URN，并做回归。

### 二次验证示例

```http
POST /api/v1/auth/step-up
{"password":"..."}
```

LDAP/SSO：`{"confirm":true}`。

### K8s agent

```bash
k8s-agent --agent-id=... --token="$KEEPLUS_AGENT_TOKEN"
# 与全局参数 k8s_agent_tunnel_token 一致，且 harden_k8s_tunnel=true
```

## 评估项（文档结论，本迭代不实现完整产品）

### 飞书入站 bot

若要做「飞书命令查库」：须校验 EncryptKey/签名 → 绑定平台用户 → 仅签发短期 scoped PAT → **禁止** bot 持有 admin JWT；敏感操作二次确认。与出站通知、建库飞书发明文无关。

### mTLS / 内网 ACL

可作为网关纵深，**不替代**访问授权 + PAT；可在 nginx 对 `/api/v1/query` 等做 allowlist，与平台授权并行。

## 吊销

吊销访问授权会使关联 PAT 立即失效；用户也可单独吊销 PAT。
