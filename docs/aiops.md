# AIOps（DBA 向）

KeePlus 定位：**DBA Copilot + 告警上下文包 + 案例/Runbook**，不是通用 ITSM AIOps。近两期不用 LLM 替代 CMDB 确定性 impact。

## 入口（前端）

| 页面 | 路径 | 说明 |
|------|------|------|
| AIOps 知识库 | `/ops/aiops/knowledge` | 内置目录 / 库内案例、alertname 试算、DBA 助手 |
| AIOps 关联 | `/ops/aiops/incidents` | 按实例时间窗聚合活跃告警 |
| 活跃告警详情 | `/ops/alerts` | 诊断摘要、Runbook、助手 |

菜单挂在「告警管理」下（种子 `m-alert-aiops-kb` / `m-alert-aiops-incidents`）。

## Phase 0 / 0.1（已落地）

- `GET /api/v1/alerts/:id/context-bundle`
- Webhook 扩展：`instance_id` / `impact_summary` / `context_summary` / `case_hints`
- 案例 `alert_names` 与 alertname 对齐；内置知识库按引擎拆分：
  - `pg_kb.go`：PostgreSQL（图谱/值班 + WAL/膨胀/XID 等）
  - `mysql_kb.go`：连接打满、主从延迟、死锁/锁等待、慢 SQL、Buffer Pool、Binlog、临时表等
  - `redis_kb.go`：内存/大 Key/热 Key、慢查询、复制、哨兵切换、持久化 Fork 等
  - `starrocks_kb.go`：BE 宕机、不健康 Tablet、导入/查询超时、FE 连接与 GC、Compaction 等
  - 文案为平台原创排查模板（主题参考公开运维共识，不照搬站点原文）
- `GET /aiops/knowledge`、`GET /aiops/knowledge/db`、`GET /aiops/knowledge/match`

## Phase 1：Copilot（已落地）

- `POST /api/v1/copilot/chat`（URN `um.action.copilot.chat`）
- `POST /api/v1/alerts/:id/copilot`
- 配置（**仅** Nacos DataId / 本地 YAML 的 `copilot:` 段，**不读**库表全局参数）：
  - `base_url` / `api_key` / `model`
  - 可选 `provider`：`cursor` | `openai`（空则按 base_url 自动识别）
  - **勿混用**：Cursor（`api.cursor.com` + `crsr_…`）与 OpenAI 兼容网关（`/v1` + `sk-…`，含通义/qwen）是两套配置，不要同一段 `base_url` 挂「通义」标签却打 Cursor `/v1/agents`
- **Cursor Cloud Agents**（推荐本环境）：
  - `base_url` = `https://api.cursor.com`（勿填 dashboard，勿带 `/v1/agents`）
  - `api_key` = Dashboard → User API Keys 的 `crsr_…`（401 `Invalid User API Key` 时重新生成）
  - `model`：建议 `auto`；从 `GET /v1/models` 挑选（优先 composer 等）
  - **注意**：IDE 里的模型列表 ≠ Cloud Agents API 可用 id；两者以 `GET https://api.cursor.com/v1/models` 为准
  - 实现：`POST /v1/agents`（no-repo）→ 轮询 run `result`（非 `/chat/completions`）
  - 耗时可达 1–3 分钟；前端请求 timeout=180s，建议 `http.write_timeout` ≥ 200
- **OpenAI 兼容网关**（通义/qwen 等）：
  - `base_url` = `https://api.openai.com/v1` 或兼容网关 `/v1`（勿带 `/chat/completions`）
  - `provider: openai`，`api_key` 为网关密钥
  - `model`：单值 / 逗号列表 / `value|label` / JSON `[{value,label}]`（YAML 中 JSON 用单引号包裹）
- `GET /copilot/models`；`POST /copilot/chat` body 可带 `model`、`images`、`session_id`、`force_refresh`
- 前端助手输入框支持 **粘贴/上传截图** 识图分析
- **对话记忆**：同 `session_id` 多轮追问（Cursor 复用 agent；OpenAI 附带近期 turns）；问答落库 `tbl_copilot_sessions/turns`
- **历史会话**：`GET /copilot/sessions`、`GET /copilot/sessions/:id/turns`；助手抽屉左侧可切换实例，刷新不丢
- **知识库优先**：中文分词 + 同义词 RAG；高分命中时 `mode=rag` **直答内置/库内**，不调大模型（优先于问答缓存；`force_refresh` 可强制 LLM）
- **问答缓存**：无附图且未命中强 RAG 时，相同问题命中 `tbl_copilot_cache`，`mode=cache`；点「新对话」开新 session
- **学习入库（人工确认）**：内置/库内未强命中时，LLM 回答带 `learnable=true`，助手气泡显示「学习」；确认后 `POST /aiops/knowledge/learn` 写入库内（可选 `as_system` 标记为平台系统案例）。**不再自动沉淀**，避免错挂主题
- 未配置 URL/Key 时 **离线模式**：Context Bundle + 关键词 RAG

## Phase 2：关联 + RAG（已落地）

- `GET /api/v1/aiops/incidents?window=30m`：按 instance_id/host 聚合 firing，附 Todo / CMDB drift（不落库）
- `POST /api/v1/aiops/rag/search` body `{q, limit}`：案例关键词检索

## Phase 3：受控 Runbook（已落地）

- `GET /api/v1/alerts/:id/runbook` 或 `POST /runbook/preview`
- `POST /runbook/execute` body `{alert_id, action, confirm:true}`（URN `um.action.runbook.execute`）

| action | 行为 |
|--------|------|
| `claim_alert` | 认领 |
| `create_todo` | 转待办 |
| `silence_2h` | 快捷静默 2h |
| `kill_session` / `cdc_restart` | **仅生成待办草稿**，不直连执行 |

审计：`tbl_alert_events.action=runbook`。

## 路线图状态

| 阶段 | 状态 |
|------|------|
| 0 / 0.1 Context Bundle + PG 知识库 | 已落地 |
| 1 Copilot | 已落地 |
| 2 关联 + RAG | 已落地 |
| 3 受控 Runbook | 已落地 |

## 明确不做

- LLM 替代 CMDB impact；无审批杀主库/写库；向量数据库；企微入站 bot（缓做）

## 相关文档

- [告警管理](alerts.md)
- [CMDB](cmdb.md)
- [功能总览](features.md)
