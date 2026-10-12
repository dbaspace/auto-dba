import React, { useCallback, useEffect, useState } from 'react'
import {
  Button, Card, Checkbox, Drawer, Input, Modal, Select, Space, Table, Tabs, Tag, Typography, message,
} from 'antd'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import CopilotImageInput, { CopilotImage, toApiImages } from '../../components/CopilotImageInput'

type BuiltinItem = {
  fingerprint: string
  metric_key: string
  severity: string
  direction: string
  title: string
  root_cause_template: string
  action_template: string
  alert_names?: string[]
  engine: string
  tags?: string[]
}

type DBItem = {
  id: string
  fingerprint: string
  metric_key: string
  severity: string
  title: string
  root_cause_template: string
  action_template: string
  alert_names: string
  engine?: string
  system: boolean
  hit_count: number
  confirm_count: number
}

/** 学习补充等长文常无换行：按句读/步骤软折行，便于阅读 */
function softWrapCaseText(s?: string) {
  if (!s) return '—'
  let t = s.replace(/\r\n/g, '\n').trim()
  if (!t.includes('\n')) {
    t = t
      .replace(/([；。！？])\s*/g, '$1\n')
      .replace(/(【助手补充】)/g, '\n$1\n')
      .replace(/(\d+[)）．、])\s*/g, '\n$1 ')
      .replace(/\n{3,}/g, '\n\n')
      .replace(/^\n+/, '')
  }
  return t
}

const caseTextStyle: React.CSSProperties = {
  marginBottom: 0,
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
  overflowWrap: 'anywhere',
  lineHeight: 1.65,
}

function CaseExpandDetail(props: { root?: string; action?: string }) {
  return (
    <div style={{ maxWidth: 'min(960px, 100%)', overflow: 'hidden' }}>
      <Typography.Paragraph style={{ ...caseTextStyle, marginBottom: 12 }}>
        <b>根因：</b>{softWrapCaseText(props.root)}
      </Typography.Paragraph>
      <div style={{ maxHeight: 560, overflowY: 'auto', paddingRight: 4 }}>
        <Typography.Paragraph style={caseTextStyle}>
          <b>动作：</b>{softWrapCaseText(props.action)}
        </Typography.Paragraph>
      </div>
    </div>
  )
}

export default function AIOpsKnowledge() {
  const { hasUrn } = usePermission()
  const [engine, setEngine] = useState('postgres')
  const [builtin, setBuiltin] = useState<BuiltinItem[]>([])
  const [dbRows, setDbRows] = useState<DBItem[]>([])
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [matchName, setMatchName] = useState('PGConnHigh')
  const [matchHits, setMatchHits] = useState<{ title: string; metric_key: string; match_reason?: string }[]>([])
  const [chatOpen, setChatOpen] = useState(false)
  type ChatTurn = {
    role: 'user' | 'assistant'
    content: string
    meta?: string
    learnable?: boolean
    question?: string
    learned?: boolean
  }
  type ChatTab = {
    id: string
    title: string
    turns: ChatTurn[]
    draft: string
    images: CopilotImage[]
    loaded?: boolean
  }

  /** 新对话 Tab 随机短名，避免多个「新对话」难以区分 */
  const randomChatTitle = () => {
    const tags = [
      '青岚', '暮雪', '流云', '星河', '竹影', '霜白', '晓风', '听潮',
      '栖梧', '远山', '拾光', '静川', '墨池', '归雁', '疏桐', '清茗',
    ]
    const tag = tags[Math.floor(Math.random() * tags.length)]
    const suf = Math.random().toString(36).slice(2, 5).toUpperCase()
    return `对话·${tag}-${suf}`
  }

  const initTabs = (): ChatTab[] => {
    try {
      const raw = localStorage.getItem('copilot_open_tabs')
      if (raw) {
        const arr = JSON.parse(raw) as { id: string; title: string }[]
        if (Array.isArray(arr) && arr.length > 0) {
          return arr.map((t) => ({
            id: t.id,
            title: t.title && t.title !== '新对话' ? t.title : randomChatTitle(),
            turns: [], draft: '', images: [], loaded: false,
          }))
        }
      }
    } catch { /* ignore */ }
    const id = localStorage.getItem('copilot_session_id') || `cps-${Date.now()}`
    return [{ id, title: randomChatTitle(), turns: [], draft: '', images: [], loaded: true }]
  }

  const [chatTabs, setChatTabs] = useState<ChatTab[]>(initTabs)
  const [sessionId, setSessionId] = useState(() => {
    const tabs = initTabs()
    const saved = localStorage.getItem('copilot_session_id')
    if (saved && tabs.some((t) => t.id === saved)) return saved
    return tabs[0].id
  })
  const [chatLoading, setChatLoading] = useState(false)
  const [chatModel, setChatModel] = useState<string>()
  const [modelOpts, setModelOpts] = useState<{ value: string; label: string }[]>([])
  const [llmEnabled, setLlmEnabled] = useState(false)
  const [histLoading, setHistLoading] = useState(false)
  const [sessions, setSessions] = useState<{ id: string; title: string; updated_at: string; turn_count: number }[]>([])

  const activeTab = chatTabs.find((t) => t.id === sessionId) || chatTabs[0]
  const turns = activeTab?.turns || []
  const chatMsg = activeTab?.draft ?? ''
  const chatImages = activeTab?.images || []

  const persistOpenTabs = (tabs: ChatTab[], active: string) => {
    localStorage.setItem('copilot_open_tabs', JSON.stringify(tabs.map((t) => ({ id: t.id, title: t.title }))))
    localStorage.setItem('copilot_session_id', active)
  }

  const updateTab = (id: string, updater: (tab: ChatTab) => ChatTab) => {
    setChatTabs((prev) => {
      const next = prev.map((t) => (t.id === id ? updater(t) : t))
      const active = localStorage.getItem('copilot_session_id') || id
      persistOpenTabs(next, active)
      return next
    })
  }

  const setChatMsg = (v: string) => {
    updateTab(sessionId, (t) => ({ ...t, draft: v }))
  }
  const setChatImages = (v: CopilotImage[] | ((prev: CopilotImage[]) => CopilotImage[])) => {
    updateTab(sessionId, (t) => ({
      ...t,
      images: typeof v === 'function' ? v(t.images) : v,
    }))
  }
  const setTurns = (v: ChatTurn[] | ((prev: ChatTurn[]) => ChatTurn[])) => {
    updateTab(sessionId, (t) => {
      const next = typeof v === 'function' ? v(t.turns) : v
      const titleFromQ = next.find((x) => x.role === 'user')?.content?.replace(/（强制刷新）$/, '') || ''
      const title = titleFromQ
        ? (titleFromQ.length > 24 ? `${titleFromQ.slice(0, 24)}…` : titleFromQ)
        : (t.title || '新对话')
      return { ...t, turns: next, title, loaded: true }
    })
  }

  const loadSessions = useCallback(async () => {
    try {
      const res = await api.get('/copilot/sessions', { params: { limit: 40 } })
      const items = (res.data?.data?.items || []) as {
        id: string; title: string; updated_at: string; turn_count: number
      }[]
      // 去掉空会话；同标题只保留最近一条，避免「相同历史」堆叠
      const seen = new Set<string>()
      const deduped: typeof items = []
      for (const s of items) {
        if (!(s.turn_count > 0)) continue
        const key = (s.title || '').trim().replace(/（强制刷新）$/, '') || s.id
        if (seen.has(key)) continue
        seen.add(key)
        deduped.push(s)
      }
      setSessions(deduped)
    } catch {
      setSessions([])
    }
  }, [])

  const fetchTurnsIntoTab = async (id: string) => {
    setHistLoading(true)
    try {
      const res = await api.get(`/copilot/sessions/${encodeURIComponent(id)}/turns`)
      const items = (res.data?.data?.items || []) as { role: string; content: string }[]
      const mapped: ChatTurn[] = items.map((x) => ({
        role: x.role === 'assistant' ? 'assistant' : 'user',
        content: x.content,
        learnable: x.role === 'assistant',
      }))
      const firstQ = mapped.find((x) => x.role === 'user')?.content || ''
      const title = firstQ ? (firstQ.length > 24 ? `${firstQ.slice(0, 24)}…` : firstQ) : '对话'
      setChatTabs((prev) => {
        const next = prev.map((t) => (t.id === id ? { ...t, turns: mapped, title, loaded: true } : t))
        persistOpenTabs(next, id)
        return next
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载会话失败')
    } finally {
      setHistLoading(false)
    }
  }

  const switchTab = async (id: string) => {
    if (id === sessionId) return
    setSessionId(id)
    localStorage.setItem('copilot_session_id', id)
    const tab = chatTabs.find((t) => t.id === id)
    if (tab && !tab.loaded) {
      await fetchTurnsIntoTab(id)
    }
  }

  const openOrFocusTab = async (id: string, title?: string) => {
    setChatTabs((prev) => {
      if (prev.some((t) => t.id === id)) {
        persistOpenTabs(prev, id)
        return prev
      }
      const next = [...prev, {
        id, title: title || '对话', turns: [], draft: '', images: [], loaded: false,
      }]
      persistOpenTabs(next, id)
      return next
    })
    setSessionId(id)
    localStorage.setItem('copilot_session_id', id)
    await fetchTurnsIntoTab(id)
  }

  const closeTab = (targetId: string) => {
    if (chatTabs.length <= 1) {
      message.info('至少保留一个对话窗口')
      return
    }
    const next = chatTabs.filter((t) => t.id !== targetId)
    const active = targetId === sessionId ? next[next.length - 1].id : sessionId
    setChatTabs(next)
    setSessionId(active)
    persistOpenTabs(next, active)
    const tab = next.find((t) => t.id === active)
    if (tab && !tab.loaded) void fetchTurnsIntoTab(active)
  }

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [b, d] = await Promise.all([
        api.get('/aiops/knowledge', { params: { engine } }),
        api.get('/aiops/knowledge/db', { params: { engine, q: q || undefined, limit: 500 } }),
      ])
      setBuiltin(b.data?.data?.items || [])
      setDbRows(d.data?.data?.items || [])
    } finally {
      setLoading(false)
    }
  }, [engine, q])

  useEffect(() => { void load() }, [load])

  const runMatch = async () => {
    const res = await api.get('/aiops/knowledge/match', { params: { alertname: matchName } })
    setMatchHits(res.data?.data?.cases || [])
    message.success(`命中 ${res.data?.data?.total ?? 0} 条`)
  }

  const openChat = async () => {
    setChatOpen(true)
    try {
      const res = await api.get('/copilot/models')
      const d = res.data?.data || {}
      const models = (d.models || []).map((m: { value: string; label: string }) => ({
        value: m.value, label: m.label || m.value,
      }))
      setModelOpts(models)
      setLlmEnabled(!!d.enabled)
      setChatModel(d.default || models[0]?.value)
    } catch {
      setModelOpts([])
      setLlmEnabled(false)
    }
    await loadSessions()
    const tab = chatTabs.find((t) => t.id === sessionId)
    if (tab && !tab.loaded) {
      void fetchTurnsIntoTab(sessionId)
    }
  }

  /** 新对话：顶部新增独立 Tab（随机命名），互不影响各自历史 */
  const resetChat = () => {
    const id = `cps-${Date.now()}`
    const tab: ChatTab = {
      id, title: randomChatTitle(), turns: [], draft: '', images: [], loaded: true,
    }
    setChatTabs((prev) => {
      const next = [...prev, tab]
      persistOpenTabs(next, id)
      return next
    })
    setSessionId(id)
  }

  const lastUserQuestion = () => {
    for (let i = turns.length - 1; i >= 0; i--) {
      if (turns[i].role === 'user' && turns[i].content && turns[i].content !== '（附图识图）') {
        return turns[i].content.replace(/（强制刷新）$/, '')
      }
    }
    return ''
  }

  const ask = async (opts?: { forceRefresh?: boolean }) => {
    const forceRefresh = !!opts?.forceRefresh
    let sid = sessionId
    const imagesSnap = chatImages
    let qText = chatMsg.trim()
    if (!qText && imagesSnap.length === 0) {
      if (forceRefresh) {
        qText = lastUserQuestion()
        if (!qText) {
          message.warning('没有可刷新的上一问，请先输入问题')
          return
        }
      } else {
        message.warning('请输入问题或粘贴图片')
        return
      }
    }
    if (!qText && imagesSnap.length > 0) qText = '（附图识图）'
    const displayQ = forceRefresh ? `${qText}（强制刷新）` : qText
    updateTab(sid, (t) => {
      const nextTurns = [...t.turns, { role: 'user' as const, content: displayQ }]
      const titleFromQ = displayQ.replace(/（强制刷新）$/, '')
      const title = titleFromQ
        ? (titleFromQ.length > 24 ? `${titleFromQ.slice(0, 24)}…` : titleFromQ)
        : (t.title || '新对话')
      return { ...t, turns: nextTurns, draft: '', title, loaded: true }
    })
    setChatLoading(true)
    try {
      const res = await api.post('/copilot/chat', {
        message: qText === '（附图识图）' ? '' : qText,
        model: chatModel,
        images: toApiImages(imagesSnap),
        session_id: sid,
        force_refresh: forceRefresh,
      }, { timeout: 180000 })
      const d = res.data?.data || {}
      const ans = d.answer || ''
      const head = [
        d.model && `模型 ${d.model}`,
        d.provider && `provider=${d.provider}`,
        d.mode && `mode=${d.mode}`,
        d.cached && '命中缓存',
        forceRefresh && '强制刷新',
      ].filter(Boolean).join(' · ')
      // 后端 learnable，或 mode=llm 时展示「学习」（兼容强制刷新 / 旧后端）
      const showLearn = !!(d.learnable || (d.mode === 'llm' && !d.cached))
      const withAssistant = (t: ChatTab): ChatTab => ({
        ...t,
        images: [],
        loaded: true,
        turns: [...t.turns, {
          role: 'assistant' as const, content: ans, meta: head,
          learnable: showLearn, question: d.question || qText,
          learned: !!d.learned,
        }],
      })
      if (d.session_id && d.session_id !== sid) {
        const from = sid
        const to = d.session_id as string
        setChatTabs((prev) => {
          const next = prev.map((t) => (t.id === from ? withAssistant({ ...t, id: to }) : t))
          persistOpenTabs(next, to)
          return next
        })
        setSessionId(to)
        sid = to
      } else {
        updateTab(sid, withAssistant)
      }
      void loadSessions()
      if (d.mode === 'rag') message.success('已命中知识库直答，未调用大模型')
      else if (d.cached) message.success('命中历史问答缓存，未调用大模型')
      else if (showLearn) message.info('可点回答旁「学习」确认写入知识库')
      else if (forceRefresh && d.mode === 'llm') message.success('已强制调用大模型')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '助手调用失败')
      updateTab(sid, (t) => ({ ...t, turns: t.turns.slice(0, -1) }))
    } finally {
      setChatLoading(false)
    }
  }

  const parseAlertNames = (v?: string | string[]) => {
    if (Array.isArray(v)) return v.filter(Boolean)
    if (!v) return []
    return String(v).split(/[,;|\s]+/).map((s) => s.trim()).filter(Boolean)
  }

  const renderAlertTags = (v?: string | string[]) => {
    const names = parseAlertNames(v)
    if (names.length === 0) return '—'
    return (
      <div style={{
        display: 'flex', flexWrap: 'wrap', gap: 4,
        maxWidth: '100%', overflow: 'hidden', lineHeight: 1.4,
      }}>
        {names.map((n) => (
          <Tag key={n} style={{ margin: 0, maxWidth: '100%' }}>
            <span style={{
              display: 'inline-block', maxWidth: 160,
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', verticalAlign: 'bottom',
            }}>{n}</span>
          </Tag>
        ))}
      </div>
    )
  }

  const cellClip = { style: { overflow: 'hidden', verticalAlign: 'top' as const } }

  const builtinCols = [
    { title: '标题', dataIndex: 'title', key: 'title', ellipsis: true },
    { title: '指标', dataIndex: 'metric_key', width: 140, ellipsis: true, onCell: () => cellClip },
    { title: '级别', dataIndex: 'severity', width: 90 },
    {
      title: 'alertname', key: 'an', width: 220,
      onCell: () => cellClip,
      render: (_: unknown, r: BuiltinItem) => renderAlertTags(r.alert_names),
    },
    { title: '引擎', dataIndex: 'engine', width: 100, onCell: () => cellClip },
  ]

  const confirmLearn = (turnIdx: number) => {
    const t = turns[turnIdx]
    if (!t || t.role !== 'assistant' || !t.learnable) return
    const question = (t.question || lastUserQuestion()).replace(/（强制刷新）$/, '')
    const answer = t.content
    const relearn = !!t.learned
    let asSystem = true
    Modal.confirm({
      title: relearn ? '重新学习到平台知识库？' : '确认学习到平台知识库？',
      width: 520,
      content: (
        <div>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
            {relearn
              ? '将用当前完整回答覆盖库内同指纹学习案例（正文不截断）。'
              : '可将本次大模型分析写入知识库，供后续 RAG 直答。'}
          </Typography.Paragraph>
          <Typography.Paragraph style={{ marginBottom: 4 }}><b>问题：</b>{question}</Typography.Paragraph>
          <Typography.Paragraph
            style={{ whiteSpace: 'pre-wrap', maxHeight: 360, overflow: 'auto', background: '#fafafa', padding: 8, borderRadius: 6 }}
          >
            {answer}
          </Typography.Paragraph>
          <Checkbox defaultChecked onChange={(e) => { asSystem = e.target.checked }}>
            标记为系统案例（平台知识库，库内显示「系统」，参与检索）
          </Checkbox>
        </div>
      ),
      okText: relearn ? '确认覆盖' : '确认学习',
      cancelText: '取消',
      onOk: async () => {
        const res = await api.post('/aiops/knowledge/learn', {
          question,
          answer,
          confirm: true,
          as_system: asSystem,
          merge_system: true,
        })
        const fp = res.data?.data?.fingerprint
        message.success(fp ? `${relearn ? '已重新入库' : '已入库'}：${fp}` : (relearn ? '已重新学习' : '已学习入库'))
        setTurns((prev) => prev.map((x, i) => (i === turnIdx ? { ...x, learned: true, learnable: true } : x)))
        void load()
      },
    })
  }

  const removeLearned = async (fp: string) => {
    try {
      await api.delete('/aiops/knowledge/db', { params: { fingerprint: fp } })
      message.success('已删除错挂/学习案例')
      void load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '删除失败')
    }
  }

  const dbCols = [
    { title: '标题', dataIndex: 'title', ellipsis: true },
    { title: '指纹', dataIndex: 'fingerprint', width: 200, ellipsis: true, onCell: () => cellClip },
    { title: '指标', dataIndex: 'metric_key', width: 130, ellipsis: true, onCell: () => cellClip },
    {
      title: 'alertname', dataIndex: 'alert_names', width: 220,
      onCell: () => cellClip,
      render: (v: string) => renderAlertTags(v),
    },
    { title: '引擎', dataIndex: 'engine', width: 100, ellipsis: true, onCell: () => cellClip },
    {
      title: '类型', width: 80,
      onCell: () => cellClip,
      render: (_: unknown, r: DBItem) => r.system ? <Tag color="blue">系统</Tag> : <Tag>自定义</Tag>,
    },
    { title: '命中', dataIndex: 'hit_count', width: 70 },
    {
      title: '操作', width: 80,
      render: (_: unknown, r: DBItem) => (
        !r.system || (r.fingerprint || '').startsWith('learn|') ? (
          <Button type="link" danger size="small" onClick={() => void removeLearned(r.fingerprint)}>删除</Button>
        ) : null
      ),
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 16 }} wrap>
        <Typography.Title level={4} style={{ margin: 0 }}>AIOps 知识库</Typography.Title>
        <Select
          style={{ width: 140 }}
          value={engine}
          onChange={setEngine}
          options={[
            { value: 'postgres', label: 'PostgreSQL' },
            { value: 'mysql', label: 'MySQL' },
            { value: 'redis', label: 'Redis' },
            { value: 'starrocks', label: 'StarRocks' },
            { value: 'host', label: '主机' },
            { value: 'all', label: '全部' },
          ]}
        />
        <Input.Search allowClear placeholder="搜索库内案例" onSearch={setQ} style={{ width: 220 }} />
        {hasUrn(Perm.copilot.chat) && (
          <Button type="primary" onClick={() => void openChat()}>DBA 助手</Button>
        )}
      </Space>

      <Card size="small" style={{ marginBottom: 16 }}>
        <Space wrap>
          <Typography.Text>alertname 试算</Typography.Text>
          <Input value={matchName} onChange={(e) => setMatchName(e.target.value)} style={{ width: 200 }} />
          <Button onClick={() => void runMatch()}>匹配</Button>
          {matchHits.map((h) => (
            <Tag key={h.metric_key + h.title} color="green">{h.title}</Tag>
          ))}
        </Space>
      </Card>

      <Tabs
        items={[
          {
            key: 'builtin',
            label: `内置目录 (${builtin.length})`,
            children: (
              <Table
                rowKey="fingerprint"
                loading={loading}
                dataSource={builtin}
                columns={builtinCols}
                tableLayout="fixed"
                scroll={{ x: 900 }}
                expandable={{
                  expandedRowRender: (r) => (
                    <CaseExpandDetail root={r.root_cause_template} action={r.action_template} />
                  ),
                }}
                pagination={{ pageSize: 20 }}
              />
            ),
          },
          {
            key: 'db',
            label: `库内案例 (${dbRows.length})`,
            children: (
              <Table
                rowKey="fingerprint"
                loading={loading}
                dataSource={dbRows}
                columns={dbCols}
                tableLayout="fixed"
                scroll={{ x: 1100 }}
                expandable={{
                  expandedRowRender: (r) => (
                    <CaseExpandDetail root={r.root_cause_template} action={r.action_template} />
                  ),
                }}
                pagination={{ pageSize: 20 }}
              />
            ),
          },
        ]}
      />

      <Drawer title="DBA Copilot" open={chatOpen} onClose={() => setChatOpen(false)} width={860}>
        <style>{`
          .copilot-session-tabs .ant-tabs-content-holder { display: none; }
          .copilot-session-tabs .ant-tabs-nav { margin-bottom: 0; }
        `}</style>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, minHeight: 520 }}>
          <Tabs
            type="editable-card"
            size="small"
            hideAdd
            activeKey={sessionId}
            onChange={(key) => void switchTab(key)}
            onEdit={(targetKey, action) => {
              if (action === 'remove' && typeof targetKey === 'string') closeTab(targetKey)
            }}
            className="copilot-session-tabs"
            tabBarExtraContent={(
              <Button size="small" type="primary" onClick={resetChat} disabled={chatLoading}>
                新对话
              </Button>
            )}
            items={chatTabs.map((t) => ({
              key: t.id,
              label: (
                <span style={{
                  display: 'inline-block', maxWidth: 140, overflow: 'hidden',
                  textOverflow: 'ellipsis', whiteSpace: 'nowrap', verticalAlign: 'bottom',
                }} title={t.title}>
                  {t.title || '新对话'}
                </span>
              ),
              closable: chatTabs.length > 1,
              children: <span />,
            }))}
          />

          <div style={{ display: 'flex', gap: 12, flex: 1, minHeight: 0, width: '100%', overflow: 'hidden' }}>
            <div style={{
              flex: 1, minWidth: 0, maxWidth: '100%', overflow: 'hidden',
              display: 'flex', flexDirection: 'column',
            }}>
              <Space style={{ marginBottom: 12 }} wrap>
                <Typography.Text type="secondary">{llmEnabled ? 'LLM 已启用' : '离线模式'}</Typography.Text>
                {modelOpts.length > 0 && (
                  <Select
                    style={{ minWidth: 180 }}
                    value={chatModel}
                    onChange={setChatModel}
                    options={modelOpts}
                    placeholder="选择模型"
                  />
                )}
              </Space>
              <Typography.Text type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
                同会话可追问；顶部 Tab 各自保留历史，刷新后不丢失
              </Typography.Text>
              <div style={{ flex: 1, maxHeight: 280, overflowY: 'auto', marginBottom: 12, paddingRight: 4, opacity: histLoading ? 0.6 : 1 }}>
                {turns.length === 0 && (
                  <Typography.Text type="secondary">当前 Tab 暂无消息，输入问题开始</Typography.Text>
                )}
                {turns.map((t, i) => (
                  <div key={i} style={{
                    marginBottom: 10,
                    padding: 10,
                    borderRadius: 8,
                    background: t.role === 'user' ? '#f0f7ff' : '#fafafa',
                    border: '1px solid #eee',
                  }}>
                    <Space wrap size={8}>
                      <Typography.Text strong>{t.role === 'user' ? '我' : '助手'}</Typography.Text>
                      {t.meta && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t.meta}</Typography.Text>}
                      {t.learned && <Tag color="green">已学习</Tag>}
                      {t.role === 'assistant' && t.learnable && (
                        <Button size="small" type="primary" ghost onClick={() => confirmLearn(i)}>
                          {t.learned ? '重新学习' : '学习'}
                        </Button>
                      )}
                    </Space>
                    <Typography.Paragraph style={{ margin: '6px 0 0', whiteSpace: 'pre-wrap' }}>{t.content}</Typography.Paragraph>
                  </div>
                ))}
              </div>
              <CopilotImageInput
                value={chatImages}
                onChange={setChatImages}
                disabled={chatLoading}
                textareaProps={{
                  rows: 3,
                  value: chatMsg,
                  onChange: (e) => setChatMsg(e.target.value),
                  placeholder: '描述问题或追问；Ctrl+V 粘贴截图',
                }}
              />
              <Space style={{ marginTop: 12 }} wrap>
                <Button type="primary" loading={chatLoading} onClick={() => void ask()}>
                  {chatLoading ? '等待 Agent…' : '提问'}
                </Button>
                <Button
                  loading={chatLoading}
                  disabled={!llmEnabled}
                  onClick={() => void ask({ forceRefresh: true })}
                  title="跳过知识库直答与缓存，强制调用大模型（可沿用上一问）"
                >
                  强制刷新
                </Button>
              </Space>
              <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
                默认优先知识库直答；「强制刷新」会跳过直答/缓存并调用大模型
                {chatLoading ? '。Cursor Agent 可能需要 1–3 分钟' : ''}
              </Typography.Text>
            </div>

            <div style={{
              width: 176, flex: '0 0 176px', flexShrink: 0, borderLeft: '1px solid #f0f0f0',
              paddingLeft: 10, display: 'flex', flexDirection: 'column', gap: 6,
              background: '#fff', position: 'relative', zIndex: 1,
            }}>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>历史（点开到 Tab）</Typography.Text>
              <div style={{ flex: 1, overflowY: 'auto', maxHeight: 440 }}>
                {sessions.length === 0 && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>暂无记录</Typography.Text>
                )}
                {sessions.map((s) => (
                  <div
                    key={s.id}
                    onClick={() => void openOrFocusTab(s.id, s.title)}
                    style={{
                      padding: '7px 8px', marginBottom: 6, borderRadius: 8, cursor: 'pointer',
                      background: s.id === sessionId ? '#e6f4ff' : '#fafafa',
                      border: s.id === sessionId ? '1px solid #91caff' : '1px solid #f0f0f0',
                    }}
                    title={s.title}
                  >
                    <div style={{
                      fontSize: 12, lineHeight: 1.35, fontWeight: s.id === sessionId ? 600 : 400,
                      overflow: 'hidden', textOverflow: 'ellipsis',
                      display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' as const,
                    }}>{s.title || '对话'}</div>
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      {(s.turn_count || 0)} 轮 · {(s.updated_at || '').slice(5, 16).replace('T', ' ')}
                    </Typography.Text>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </Drawer>
    </div>
  )
}
