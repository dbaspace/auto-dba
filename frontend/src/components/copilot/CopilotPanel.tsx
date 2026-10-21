import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Button, Checkbox, Input, Modal, Select, Space, Tabs, Tag, Typography, message } from 'antd'
import api, { streamNdjson } from '../../api/request'
import CopilotImageInput, { CopilotImage, toApiImages } from '../CopilotImageInput'
import CopilotAnswer from './CopilotAnswer'
import { conclusionFromSteps, streamFailureText } from './richText'

type TaskStep = { action: string; status: string; detail?: string; output?: string }

type ChatTurn = {
  role: 'user' | 'assistant'
  content: string
  meta?: string
  learnable?: boolean
  question?: string
  learned?: boolean
  steps?: TaskStep[]
}

type QueuedAsk = {
  id: string
  text: string
  display: string
  images: CopilotImage[]
  forceRefresh: boolean
  model?: string
}

type ChatTab = {
  id: string
  title: string
  turns: ChatTurn[]
  draft: string
  images: CopilotImage[]
  queue: QueuedAsk[]
  loaded?: boolean
}

type SessionItem = { id: string; title: string; updated_at: string; turn_count: number }

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
          turns: [], draft: '', images: [], queue: [], loaded: false,
        }))
      }
    }
  } catch { /* ignore */ }
  const id = localStorage.getItem('copilot_session_id') || `cps-${Date.now()}`
  return [{ id, title: randomChatTitle(), turns: [], draft: '', images: [], queue: [], loaded: true }]
}

export type CopilotPanelProps = {
  /** 从 false 变为 true 时加载模型与会话（抽屉打开 / 整页挂载） */
  active: boolean
  layout?: 'drawer' | 'page'
  /** 学习入库成功后回调（知识库页用于刷新案例表） */
  onLearned?: () => void
}

export default function CopilotPanel({ active, layout = 'drawer', onLearned }: CopilotPanelProps) {
  const [chatTabs, setChatTabs] = useState<ChatTab[]>(initTabs)
  const [sessionId, setSessionId] = useState(() => {
    const tabs = initTabs()
    const saved = localStorage.getItem('copilot_session_id')
    if (saved && tabs.some((t) => t.id === saved)) return saved
    return tabs[0].id
  })
  const [busy, setBusy] = useState<Record<string, boolean>>({})
  const [queueEdit, setQueueEdit] = useState<{ id: string; text: string } | null>(null)
  const [queueOpen, setQueueOpen] = useState(true)
  const queueRef = useRef<Record<string, QueuedAsk[]>>({})
  const runningRef = useRef<Record<string, boolean>>({})
  const abortRef = useRef<Record<string, AbortController>>({})
  const [chatModel, setChatModel] = useState<string>()
  const [modelOpts, setModelOpts] = useState<{ value: string; label: string }[]>([])
  const [llmEnabled, setLlmEnabled] = useState(false)
  const [histLoading, setHistLoading] = useState(false)
  const [sessions, setSessions] = useState<SessionItem[]>([])
  const booted = useRef(false)

  const page = layout === 'page'
  const activeTab = chatTabs.find((t) => t.id === sessionId) || chatTabs[0]
  const turns = activeTab?.turns || []
  const chatMsg = activeTab?.draft ?? ''
  const chatImages = activeTab?.images || []

  const persistOpenTabs = (tabs: ChatTab[], activeId: string) => {
    localStorage.setItem('copilot_open_tabs', JSON.stringify(tabs.map((t) => ({ id: t.id, title: t.title }))))
    localStorage.setItem('copilot_session_id', activeId)
  }

  const updateTab = (id: string, updater: (tab: ChatTab) => ChatTab) => {
    setChatTabs((prev) => {
      const next = prev.map((t) => (t.id === id ? updater(t) : t))
      const current = localStorage.getItem('copilot_session_id') || id
      persistOpenTabs(next, current)
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
      const items = (res.data?.data?.items || []) as SessionItem[]
      const seen = new Set<string>()
      const deduped: SessionItem[] = []
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
        id, title: title || '对话', turns: [], draft: '', images: [], queue: [], loaded: false,
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
    const nextActive = targetId === sessionId ? next[next.length - 1].id : sessionId
    setChatTabs(next)
    setSessionId(nextActive)
    persistOpenTabs(next, nextActive)
    const tab = next.find((t) => t.id === nextActive)
    if (tab && !tab.loaded) void fetchTurnsIntoTab(nextActive)
  }

  const resetChat = () => {
    const id = `cps-${Date.now()}`
    const tab: ChatTab = {
      id, title: randomChatTitle(), turns: [], draft: '', images: [], queue: [], loaded: true,
    }
    setChatTabs((prev) => {
      const next = [...prev, tab]
      persistOpenTabs(next, id)
      return next
    })
    setSessionId(id)
  }

  useEffect(() => {
    if (!active || booted.current) return
    booted.current = true
    ;(async () => {
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
    })()
    // 仅在首次激活时初始化，与原 openChat 一致
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active])

  const lastUserQuestion = () => {
    for (let i = turns.length - 1; i >= 0; i--) {
      if (turns[i].role === 'user' && turns[i].content && turns[i].content !== '（附图识图）') {
        return turns[i].content.replace(/（强制刷新）$/, '')
      }
    }
    return ''
  }

  const mirrorQueue = (tabId: string) => {
    const q = queueRef.current[tabId] || []
    setChatTabs((prev) => {
      const next = prev.map((t) => (t.id === tabId ? { ...t, queue: q } : t))
      persistOpenTabs(next, localStorage.getItem('copilot_session_id') || tabId)
      return next
    })
  }

  const moveTabRuntime = (from: string, to: string) => {
    if (from === to) return
    queueRef.current[to] = queueRef.current[from] || queueRef.current[to] || []
    delete queueRef.current[from]
    if (abortRef.current[from]) {
      abortRef.current[to] = abortRef.current[from]
      delete abortRef.current[from]
    }
  }

  const pump = (tabId: string) => {
    if (runningRef.current[tabId]) return
    const queued = queueRef.current[tabId] || []
    if (!queued.length) return
    const next = queued[0]
    queueRef.current[tabId] = queued.slice(1)
    mirrorQueue(tabId)
    runningRef.current[tabId] = true
    setBusy((b) => ({ ...b, [tabId]: true }))
    void (async () => {
      let finalId = tabId
      try {
        finalId = await runOne(tabId, next)
      } finally {
        runningRef.current[tabId] = false
        setBusy((b) => ({ ...b, [tabId]: false, [finalId]: false }))
        pump(finalId)
      }
    })()
  }

  const runOne = async (tabId: string, item: QueuedAsk): Promise<string> => {
    let sid = tabId
    const ctrl = new AbortController()
    abortRef.current[sid] = ctrl
    const liveSteps: TaskStep[] = [{ action: 'chat', status: 'running', output: '已提交，开始执行' }]
    updateTab(sid, (t) => {
      const displayQ = item.display
      const titleFromQ = displayQ.replace(/（强制刷新）$/, '')
      const title = titleFromQ
        ? (titleFromQ.length > 24 ? `${titleFromQ.slice(0, 24)}…` : titleFromQ)
        : (t.title || '新对话')
      return {
        ...t,
        title,
        loaded: true,
        turns: [
          ...t.turns,
          { role: 'user' as const, content: displayQ },
          { role: 'assistant' as const, content: '', meta: '执行中', steps: liveSteps.slice() },
        ],
      }
    })
    try {
      let finalData: any = null
      let streamErr = ''
      await streamNdjson('/copilot/chat', {
        message: item.text === '（附图识图）' ? '' : item.text,
        model: item.model,
        images: toApiImages(item.images),
        session_id: sid,
        force_refresh: item.forceRefresh,
        stream: true,
      }, (ev) => {
        if (ev.type === 'step' && ev.step) {
          liveSteps.push(ev.step)
          updateTab(sid, (t) => {
            const nextTurns = t.turns.slice()
            const last = nextTurns[nextTurns.length - 1]
            if (!last || last.role !== 'assistant') return t
            nextTurns[nextTurns.length - 1] = { ...last, steps: liveSteps.slice() }
            return { ...t, turns: nextTurns }
          })
        } else if (ev.type === 'error') {
          streamErr = ev.message || '助手调用失败'
        } else if (ev.type === 'done') {
          finalData = ev.data || {}
        }
      }, 360000, ctrl.signal)
      if (streamErr || !finalData) throw new Error(streamErr || '助手没有返回结果')
      const d = finalData
      const ans = d.answer || ''
      const head = [
        d.model && `模型 ${d.model}`,
        d.provider && `provider=${d.provider}`,
        d.mode && `mode=${d.mode}`,
        d.cached && '命中缓存',
        item.forceRefresh && '强制刷新',
      ].filter(Boolean).join(' · ')
      const showLearn = !!(d.learnable || (d.mode === 'llm' && !d.cached))
      const withAssistant = (t: ChatTab): ChatTab => {
        const nextTurns = t.turns.slice()
        const last = nextTurns[nextTurns.length - 1]
        const next = {
          role: 'assistant' as const, content: ans, meta: head,
          learnable: showLearn, question: d.question || item.text,
          learned: !!d.learned,
          steps: Array.isArray(d.steps) && d.steps.length ? d.steps : last?.steps,
        }
        if (last?.role === 'assistant') nextTurns[nextTurns.length - 1] = next
        else nextTurns.push(next)
        return { ...t, loaded: true, turns: nextTurns, queue: queueRef.current[t.id] || t.queue || [] }
      }
      if (d.session_id && d.session_id !== sid) {
        const from = sid
        const to = d.session_id as string
        moveTabRuntime(from, to)
        setChatTabs((prev) => {
          const next = prev.map((t) => (t.id === from ? withAssistant({ ...t, id: to, queue: queueRef.current[to] || [] }) : t))
          persistOpenTabs(next, to)
          return next
        })
        setSessionId((cur) => (cur === from ? to : cur))
        sid = to
      } else {
        updateTab(sid, withAssistant)
      }
      void loadSessions()
      if (d.mode === 'rag') message.success('已命中知识库直答，未调用大模型')
      else if (d.cached) message.success('命中历史问答缓存，未调用大模型')
      else if (showLearn) message.info('可点回答旁「学习」确认写入知识库')
      else if (item.forceRefresh && d.mode === 'llm') message.success('已强制调用大模型')
    } catch (e: any) {
      const raw = e?.message || e?.response?.data?.message || '助手调用失败'
      const content = streamFailureText(raw, liveSteps)
      const aborted = raw === '已停止' || raw === '回答生成超时' || /abort/i.test(raw)
      if (!aborted) message.error(content)
      else if (!conclusionFromSteps(liveSteps) && raw !== '已停止') message.warning('回答生成超时')
      const meta = content === '已停止' ? '已停止' : (aborted && !conclusionFromSteps(liveSteps) ? '超时' : (aborted ? '完成' : '失败'))
      updateTab(sid, (t) => {
        const nextTurns = t.turns.slice()
        const last = nextTurns[nextTurns.length - 1]
        if (last?.role === 'assistant') {
          nextTurns[nextTurns.length - 1] = { ...last, content, meta, steps: liveSteps.slice() }
        }
        return { ...t, turns: nextTurns }
      })
    } finally {
      if (abortRef.current[sid] === ctrl) delete abortRef.current[sid]
    }
    return sid
  }

  const enqueue = (tabId: string, item: QueuedAsk) => {
    queueRef.current[tabId] = [...(queueRef.current[tabId] || []), item]
    mirrorQueue(tabId)
    pump(tabId)
  }

  const ask = (opts?: { forceRefresh?: boolean }) => {
    const forceRefresh = !!opts?.forceRefresh
    const tabId = sessionId
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
    const display = forceRefresh ? `${qText}（强制刷新）` : qText
    updateTab(tabId, (t) => ({ ...t, draft: '', images: [] }))
    enqueue(tabId, {
      id: `q-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
      text: qText,
      display,
      images: imagesSnap,
      forceRefresh,
      model: chatModel,
    })
  }

  const stopTab = (tabId: string) => {
    abortRef.current[tabId]?.abort(new DOMException('已停止', 'AbortError'))
  }

  const saveQueueEdit = (tabId: string) => {
    if (!queueEdit) return
    const text = queueEdit.text.trim()
    queueRef.current[tabId] = (queueRef.current[tabId] || []).flatMap((item) => {
      if (item.id !== queueEdit.id) return [item]
      const nextText = text || (item.images.length ? '（附图识图）' : '')
      if (!nextText) return []
      const display = item.forceRefresh && nextText !== '（附图识图）' ? `${nextText}（强制刷新）` : nextText
      return [{ ...item, text: nextText, display }]
    })
    mirrorQueue(tabId)
    setQueueEdit(null)
  }

  const deleteQueued = (tabId: string, id: string) => {
    queueRef.current[tabId] = (queueRef.current[tabId] || []).filter((item) => item.id !== id)
    mirrorQueue(tabId)
    if (queueEdit?.id === id) setQueueEdit(null)
  }

  const tabQueue = activeTab?.queue || []
  const tabBusy = !!busy[sessionId]

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
        onLearned?.()
      },
    })
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, minHeight: page ? 0 : 520, height: page ? '100%' : undefined }}>
      <style>{`
        .copilot-session-tabs .ant-tabs-content-holder { display: none; }
        .copilot-session-tabs .ant-tabs-nav { margin-bottom: 0; }
      `}</style>
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
          <Button size="small" type="primary" onClick={resetChat}>
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
          <div style={{
            flex: 1,
            maxHeight: page ? undefined : 280,
            overflowY: 'auto',
            marginBottom: 12,
            paddingRight: 4,
            opacity: histLoading ? 0.6 : 1,
          }}>
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
                  {t.role === 'assistant' && t.meta === '执行中' && tabBusy && (
                    <Button size="small" danger onClick={() => stopTab(sessionId)}>停止</Button>
                  )}
                </Space>
                {t.role === 'assistant' && t.steps && t.steps.length > 0 && (
                  <div style={{ marginTop: 8 }}>
                    {t.steps.map((s, j) => (
                      <div key={j} style={{
                        marginBottom: 6, padding: 8, background: '#fff',
                        border: '1px solid #eee', borderRadius: 6,
                      }}>
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          {s.status === 'ok' ? '完成' : s.status === 'running' ? '进行中' : '失败'} · {s.action}{s.detail ? ` · ${s.detail}` : ''}
                        </Typography.Text>
                        {s.output && (
                          <div style={{ marginTop: 4, fontSize: 12 }}>
                            <CopilotAnswer text={s.output} />
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
                {t.content && (
                  <div style={{ marginTop: 6 }}>
                    <CopilotAnswer text={t.content} />
                  </div>
                )}
              </div>
            ))}
          </div>
          {tabQueue.length > 0 && (
            <div style={{ marginBottom: 8, border: '1px solid #f0f0f0', borderRadius: 8, background: '#fafafa' }}>
              <button
                type="button"
                onClick={() => setQueueOpen((v) => !v)}
                style={{
                  width: '100%', textAlign: 'left', background: 'transparent', border: 0,
                  padding: '8px 10px', cursor: 'pointer', color: '#595959',
                }}
              >
                {queueOpen ? '▾' : '▸'} {tabQueue.length} 条排队
              </button>
              {queueOpen && tabQueue.map((item) => (
                <div key={item.id} style={{ padding: '0 10px 8px', display: 'flex', gap: 8, alignItems: 'flex-start' }}>
                  {queueEdit?.id === item.id ? (
                    <Input.TextArea
                      autoSize={{ minRows: 1, maxRows: 4 }}
                      value={queueEdit.text}
                      onChange={(e) => setQueueEdit({ id: item.id, text: e.target.value })}
                    />
                  ) : (
                    <Typography.Paragraph style={{ margin: 0, flex: 1, whiteSpace: 'pre-wrap' }}>
                      {item.display}{item.images.length ? `（${item.images.length} 张图）` : ''}
                    </Typography.Paragraph>
                  )}
                  <Space size={4}>
                    {queueEdit?.id === item.id ? (
                      <Button size="small" type="primary" onClick={() => saveQueueEdit(sessionId)}>保存</Button>
                    ) : (
                      <Button size="small" onClick={() => setQueueEdit({ id: item.id, text: item.text })}>编辑</Button>
                    )}
                    <Button size="small" danger onClick={() => deleteQueued(sessionId, item.id)}>删除</Button>
                  </Space>
                </div>
              ))}
            </div>
          )}
          <CopilotImageInput
            value={chatImages}
            onChange={setChatImages}
            textareaProps={{
              rows: 3,
              value: chatMsg,
              onChange: (e) => setChatMsg(e.target.value),
              placeholder: '描述问题或追问；Ctrl+V 粘贴截图',
            }}
          />
          <Space style={{ marginTop: 12 }} wrap>
            <Button type="primary" onClick={() => ask()}>提问</Button>
            {tabBusy && (
              <Button danger onClick={() => stopTab(sessionId)}>停止</Button>
            )}
            <Button
              disabled={!llmEnabled}
              onClick={() => ask({ forceRefresh: true })}
              title="跳过知识库直答与缓存，强制调用大模型（可沿用上一问）"
            >
              强制刷新
            </Button>
          </Space>
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
            默认优先知识库直答；「强制刷新」会跳过直答/缓存并调用大模型。已有任务时，新问题进入当前对话队列，结束后自动执行下一条。
          </Typography.Text>
        </div>

        <div style={{
          width: page ? 220 : 176,
          flex: page ? '0 0 220px' : '0 0 176px',
          flexShrink: 0,
          borderLeft: '1px solid #f0f0f0',
          paddingLeft: 10,
          display: 'flex',
          flexDirection: 'column',
          gap: 6,
          background: '#fff',
          position: 'relative',
          zIndex: 1,
        }}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>历史（点开到 Tab）</Typography.Text>
          <div style={{ flex: 1, overflowY: 'auto', maxHeight: page ? undefined : 440 }}>
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
  )
}
