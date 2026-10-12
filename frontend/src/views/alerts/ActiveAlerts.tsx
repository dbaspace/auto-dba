import React, { useCallback, useEffect, useState } from 'react'
import {
  Button, Card, Checkbox, Col, Descriptions, Drawer, Input, List, Modal, Row, Select, Space, Statistic, Tabs, Tag,
  Timeline, Tooltip, Typography, message,
} from 'antd'
import dayjs from 'dayjs'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'
import {
  AlertEvent, AlertItem, AlertStats, HANDLE_LABEL, SEV_COLOR, durationText,
} from './types'
import { getImpact, type CmdbImpact } from '../cmdb/services/cmdbApi'
import CopilotImageInput, { CopilotImage, toApiImages } from '../../components/CopilotImageInput'

const rowBorder: Record<string, string> = {
  P0: '#dc2626', P1: '#ea580c', P2: '#2563eb', P3: '#94a3b8',
}

export default function ActiveAlerts() {
  const { hasUrn } = usePermission()
  const [tab, setTab] = useState('active')
  const [severity, setSeverity] = useState<string>()
  const [keyword, setKeyword] = useState('')
  const [rows, setRows] = useState<AlertItem[]>([])
  const [stats, setStats] = useState<AlertStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<{ alert: AlertItem; events: AlertEvent[] } | null>(null)
  const [cmdbImpact, setCmdbImpact] = useState<CmdbImpact | null>(null)
  const [cmdbLoading, setCmdbLoading] = useState(false)
  const [bundle, setBundle] = useState<{
    summary?: string
    alertname?: string
    impact?: { downstream?: number; upstream?: number; app_tags?: string[] }
    cases?: { title: string; metric_key: string; match_reason?: string }[]
    findings?: { title: string; severity: string }[]
    history?: { id: string; status: string }[]
    drifts?: { field: string }[]
    sources?: string[]
  } | null>(null)
  const [bundleLoading, setBundleLoading] = useState(false)
  const [runbookOpen, setRunbookOpen] = useState(false)
  const [runbookItems, setRunbookItems] = useState<{ action: string; title: string; description: string; executable: boolean; reason: string }[]>([])
  const [copilotOpen, setCopilotOpen] = useState(false)
  const [copilotMsg, setCopilotMsg] = useState('请根据该告警给出排查步骤')
  const [copilotAns, setCopilotAns] = useState('')
  const [copilotLoading, setCopilotLoading] = useState(false)
  const [copilotModel, setCopilotModel] = useState<string>()
  const [copilotModels, setCopilotModels] = useState<{ value: string; label: string }[]>([])
  const [copilotLlm, setCopilotLlm] = useState(false)
  const [copilotImages, setCopilotImages] = useState<CopilotImage[]>([])
  const [copilotSessionId, setCopilotSessionId] = useState(() => `cps-${Date.now()}`)
  const [copilotTurns, setCopilotTurns] = useState<{
    role: 'user' | 'assistant'
    content: string
    meta?: string
    learnable?: boolean
    question?: string
    learned?: boolean
  }[]>([])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [listRes, stRes] = await Promise.all([
        api.get('/alerts', { params: { tab, severity, keyword: keyword || undefined } }),
        api.get('/alerts/stats'),
      ])
      setRows(listRes.data?.data || [])
      setStats(stRes.data?.data || null)
    } finally {
      setLoading(false)
    }
  }, [tab, severity, keyword])

  useEffect(() => { void load() }, [load])

  const openDetail = async (id: string) => {
    const res = await api.get(`/alerts/${id}`)
    setDetail(res.data?.data || null)
    setCmdbImpact(null)
    setBundle(null)
    setBundleLoading(true)
    try {
      const bRes = await api.get(`/alerts/${id}/context-bundle`)
      setBundle(bRes.data?.data || null)
    } catch {
      setBundle(null)
    } finally {
      setBundleLoading(false)
    }
  }

  const loadCmdbImpact = async () => {
    const a = detail?.alert
    if (!a) return
    const labels = a.labels || {}
    const instanceID = labels.instance_id || labels.instanceId || ''
    const ciType = labels.ci_type || (instanceID ? 'instance' : '')
    const ciID = instanceID || labels.ci_id || ''
    if (!ciType || !ciID) {
      message.warning('告警标签中无 instance_id / ci_id，无法查 CMDB 影响')
      return
    }
    setCmdbLoading(true)
    try {
      setCmdbImpact(await getImpact(ciType, ciID, 'down', 2))
    } catch (e: any) {
      message.error(e?.response?.data?.message || 'CMDB 影响分析失败')
    } finally {
      setCmdbLoading(false)
    }
  }

  const act = async (path: string, ok: string, body?: object) => {
    const id = detail?.alert.id
    if (!id) return
    await api.post(`/alerts/${id}/${path}`, body || {})
    message.success(ok)
    await openDetail(id)
    await load()
  }

  const openRunbook = async () => {
    const id = detail?.alert.id
    if (!id) return
    const res = await api.get(`/alerts/${id}/runbook`)
    setRunbookItems(res.data?.data?.suggestions || [])
    setRunbookOpen(true)
  }

  const execRunbook = async (action: string) => {
    const id = detail?.alert.id
    if (!id) return
    if (!hasUrn(Perm.runbook.execute)) {
      message.warning('无 Runbook 执行权限')
      return
    }
    await api.post('/runbook/execute', { alert_id: id, action, confirm: true })
    message.success('已执行 ' + action)
    setRunbookOpen(false)
    await openDetail(id)
    await load()
  }

  const openCopilot = async () => {
    setCopilotOpen(true)
    try {
      const res = await api.get('/copilot/models')
      const d = res.data?.data || {}
      const models = (d.models || []).map((m: { value: string; label: string }) => ({
        value: m.value, label: m.label || m.value,
      }))
      setCopilotModels(models)
      setCopilotLlm(!!d.enabled)
      setCopilotModel(d.default || models[0]?.value)
    } catch {
      setCopilotModels([])
      setCopilotLlm(false)
    }
  }

  const resetCopilot = () => {
    setCopilotSessionId(`cps-${Date.now()}`)
    setCopilotTurns([])
    setCopilotAns('')
    setCopilotImages([])
  }

  const lastCopilotQuestion = () => {
    for (let i = copilotTurns.length - 1; i >= 0; i--) {
      if (copilotTurns[i].role === 'user' && copilotTurns[i].content && copilotTurns[i].content !== '（附图识图）') {
        return copilotTurns[i].content.replace(/（强制刷新）$/, '')
      }
    }
    return ''
  }

  const confirmCopilotLearn = (turnIdx: number) => {
    const t = copilotTurns[turnIdx]
    if (!t || t.role !== 'assistant' || !t.learnable) return
    const question = (t.question || lastCopilotQuestion()).replace(/（强制刷新）$/, '')
    const answer = t.content
    const relearn = !!t.learned
    let asSystem = true
    Modal.confirm({
      title: relearn ? '重新学习到平台知识库？' : '确认学习到平台知识库？',
      width: 520,
      content: (
        <div>
          <Typography.Paragraph type="secondary">
            {relearn ? '将用当前完整回答覆盖库内同指纹学习案例。' : '可将本次大模型分析写入知识库，供后续检索直答。'}
          </Typography.Paragraph>
          <Typography.Paragraph><b>问题：</b>{question}</Typography.Paragraph>
          <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', maxHeight: 280, overflow: 'auto' }}>
            {answer}
          </Typography.Paragraph>
          <Checkbox defaultChecked onChange={(e) => { asSystem = e.target.checked }}>
            标记为系统案例（平台知识库）
          </Checkbox>
        </div>
      ),
      okText: relearn ? '确认覆盖' : '确认学习',
      onOk: async () => {
        await api.post('/aiops/knowledge/learn', {
          question, answer, confirm: true, as_system: asSystem, merge_system: true,
        })
        message.success(relearn ? '已重新学习入库' : '已学习入库')
        setCopilotTurns((prev) => prev.map((x, i) => (i === turnIdx ? { ...x, learned: true, learnable: true } : x)))
      },
    })
  }

  const askCopilot = async (opts?: { forceRefresh?: boolean }) => {
    const id = detail?.alert.id
    if (!id) return
    const forceRefresh = !!opts?.forceRefresh
    let qText = copilotMsg.trim()
    if (!qText && copilotImages.length === 0) {
      if (forceRefresh) {
        qText = lastCopilotQuestion()
        if (!qText) {
          message.warning('没有可刷新的上一问，请先输入问题')
          return
        }
      } else {
        message.warning('请输入问题或粘贴图片')
        return
      }
    }
    if (!qText && copilotImages.length > 0) qText = '（附图识图）'
    const displayQ = forceRefresh ? `${qText}（强制刷新）` : qText
    setCopilotTurns((t) => [...t, { role: 'user', content: displayQ }])
    setCopilotMsg('')
    setCopilotAns('')
    setCopilotLoading(true)
    try {
      const res = await api.post(`/alerts/${id}/copilot`, {
        message: qText === '（附图识图）' ? '' : qText,
        alert_id: id, model: copilotModel,
        images: toApiImages(copilotImages),
        session_id: copilotSessionId,
        force_refresh: forceRefresh,
      }, { timeout: 180000 })
      const d = res.data?.data || {}
      if (d.session_id) setCopilotSessionId(d.session_id)
      const ans = d.answer || ''
      const head = [
        d.model && `模型 ${d.model}`, d.provider && `provider=${d.provider}`,
        d.mode && `mode=${d.mode}`, d.cached && '命中缓存',
        forceRefresh && '强制刷新',
      ].filter(Boolean).join(' · ')
      const showLearn = !!(d.learnable || (d.mode === 'llm' && !d.cached))
      setCopilotTurns((t) => [...t, {
        role: 'assistant', content: ans, meta: head,
        learnable: showLearn, question: d.question || qText, learned: !!d.learned,
      }])
      setCopilotAns(head ? `【${head}】\n${ans}` : ans)
      setCopilotImages([])
      if (d.mode === 'rag') message.success('已命中知识库直答，未调用大模型')
      else if (d.cached) message.success('命中历史问答缓存，未调用大模型')
      else if (showLearn) message.info('可点回答旁「学习」确认写入知识库')
      else if (forceRefresh && d.mode === 'llm') message.success('已强制调用大模型')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '助手失败')
      setCopilotTurns((t) => t.slice(0, -1))
    } finally {
      setCopilotLoading(false)
    }
  }

  const columns = [
    {
      title: '级别', dataIndex: 'severity', width: 70,
      render: (v: string) => <Tag color={SEV_COLOR[v]}>{v}</Tag>,
    },
    {
      title: '告警内容', key: 'title',
      render: (_: unknown, a: AlertItem) => (
        <div>
          <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
            {a.status === 'firing' && <span style={{ width: 7, height: 7, borderRadius: '50%', background: '#ef4444', display: 'inline-block' }} />}
            {a.title}
          </div>
          <div style={{ marginTop: 4 }}>
            {a.service && <Tag>{a.service}</Tag>}
            {a.cluster && <Tag>{a.cluster}</Tag>}
            {a.aggregate_count > 1 && <Typography.Text type="warning">聚合 {a.aggregate_count}</Typography.Text>}
          </div>
        </div>
      ),
    },
    {
      title: '当前值 / 阈值', width: 140,
      render: (_: unknown, a: AlertItem) => (
        <div>
          <Typography.Text type="danger" style={{ fontFamily: 'monospace', fontWeight: 700 }}>{a.cur_value || '—'}</Typography.Text>
          <div style={{ fontSize: 12, color: '#94a3b8' }}>阈值 {a.threshold || '—'}</div>
        </div>
      ),
    },
    { title: '持续', width: 90, render: (_: unknown, a: AlertItem) => durationText(a.started_at) },
    {
      title: '处理状态', width: 110, dataIndex: 'handle',
      render: (v: string) => {
        const h = HANDLE_LABEL[v] || { label: v, color: 'default' }
        return <Tag color={h.color}>{h.label}</Tag>
      },
    },
    { title: '负责人', width: 110, dataIndex: 'owner', render: (v: string) => v || <Typography.Text type="secondary">待认领</Typography.Text> },
    {
      title: '操作', width: 160,
      render: (_: unknown, a: AlertItem) => (
        <Space size={0} onClick={(e) => e.stopPropagation()}>
          {hasUrn(Perm.alert.claim) && a.handle === 'unclaimed' && a.status !== 'resolved' && (
            <Button type="link" size="small" onClick={async () => {
              await api.post(`/alerts/${a.id}/claim`)
              message.success('已认领')
              await load()
            }}>认领</Button>
          )}
          <Button type="link" size="small" onClick={() => void openDetail(a.id)}>详情</Button>
        </Space>
      ),
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>活跃告警</Typography.Title>
      <Row gutter={12} style={{ marginBottom: 16 }}>
        <Col xs={12} md={6}><Card size="small"><Statistic title="活跃告警" value={stats?.active ?? 0} suffix="条" valueStyle={{ color: '#dc2626' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="未认领" value={stats?.unclaimed ?? 0} suffix="条" valueStyle={{ color: '#f59e0b' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="P0 紧急" value={stats?.p0 ?? 0} suffix="条" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="今日 MTTA" value={stats?.mtta || '—'} /></Card></Col>
      </Row>

      <Card size="small">
        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            { key: 'active', label: '活跃告警' },
            { key: 'unclaimed', label: '未认领' },
            { key: 'claimed', label: '已认领' },
            { key: 'silenced', label: '已静默' },
          ]}
        />
        <Space wrap style={{ marginBottom: 12 }}>
          <Select
            allowClear placeholder="全部级别" style={{ width: 120 }}
            options={['P0', 'P1', 'P2', 'P3'].map((v) => ({ value: v, label: v }))}
            value={severity} onChange={setSeverity}
          />
          <Input.Search
            allowClear placeholder="搜索告警 / 服务 / 集群" style={{ width: 260 }}
            onSearch={setKeyword} onChange={(e) => { if (!e.target.value) setKeyword('') }}
          />
          <Button onClick={() => void load()}>刷新</Button>
        </Space>
        <ResponsiveTable
          rowKey="id"
          loading={loading}
          dataSource={rows}
          columns={columns}
          pagination={{ pageSize: 20 }}
          onRow={(a) => ({
            onClick: () => void openDetail(a.id),
            style: { cursor: 'pointer', boxShadow: `inset 3px 0 0 ${rowBorder[a.severity] || '#cbd5e1'}` },
          })}
        />
      </Card>

      <Drawer
        width={560}
        open={!!detail}
        onClose={() => setDetail(null)}
        title={detail ? (
          <Space>
            <Tag color={SEV_COLOR[detail.alert.severity]}>{detail.alert.severity}</Tag>
            <Tag color={HANDLE_LABEL[detail.alert.handle]?.color}>{HANDLE_LABEL[detail.alert.handle]?.label}</Tag>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>{detail.alert.id}</Typography.Text>
          </Space>
        ) : null}
        extra={detail && (
          <Space wrap>
            {hasUrn(Perm.alert.claim) && detail.alert.handle === 'unclaimed' && (
              <Button type="primary" onClick={() => void act('claim', '已认领')}>认领</Button>
            )}
            {hasUrn(Perm.alert.claim) && detail.alert.status !== 'resolved' && (
              <Button onClick={() => void act('processing', '已进入处理中')}>处理中</Button>
            )}
            {hasUrn(Perm.alert.silence) && detail.alert.status !== 'resolved' && (
              <Button onClick={() => void act('silence', '已静默 2 小时')}>静默</Button>
            )}
            {hasUrn(Perm.alert.resolve) && detail.alert.status !== 'resolved' && (
              <Tooltip title="标记误报并关闭">
                <Button danger onClick={() => void act('false-positive', '已标记误报')}>误报</Button>
              </Tooltip>
            )}
            {hasUrn(Perm.todo.create) && (
              <Button onClick={() => void act('todo', '已转待办')}>转待办</Button>
            )}
            <Button onClick={() => void openRunbook()}>Runbook</Button>
            {hasUrn(Perm.copilot.chat) && (
              <Button onClick={() => { setCopilotAns(''); void openCopilot() }}>助手</Button>
            )}
          </Space>
        )}
      >
        {detail && (
          <>
            <Typography.Title level={5}>{detail.alert.title}</Typography.Title>
            <Space wrap style={{ marginBottom: 16 }}>
              {detail.alert.service && <Tag>{detail.alert.service}</Tag>}
              {detail.alert.cluster && <Tag>{detail.alert.cluster}</Tag>}
              {Object.entries(detail.alert.labels || {}).slice(0, 6).map(([k, v]) => (
                <Tag key={k}>{k}={v}</Tag>
              ))}
            </Space>
            <Descriptions size="small" bordered column={2}>
              <Descriptions.Item label="负责人">{detail.alert.owner || '待认领'}</Descriptions.Item>
              <Descriptions.Item label="状态">{detail.alert.status}</Descriptions.Item>
              <Descriptions.Item label="当前值">{detail.alert.cur_value || '—'}</Descriptions.Item>
              <Descriptions.Item label="阈值">{detail.alert.threshold || '—'}</Descriptions.Item>
              <Descriptions.Item label="开始时间">{detail.alert.started_at ? dayjs(detail.alert.started_at).format('YYYY-MM-DD HH:mm:ss') : '—'}</Descriptions.Item>
              <Descriptions.Item label="持续">{durationText(detail.alert.started_at)}</Descriptions.Item>
            </Descriptions>
            <Typography.Title level={5} style={{ marginTop: 24 }}>诊断摘要</Typography.Title>
            {bundleLoading && <Typography.Text type="secondary">加载上下文包…</Typography.Text>}
            {!bundleLoading && bundle?.summary && (
              <Typography.Paragraph style={{ marginBottom: 8 }}>{bundle.summary}</Typography.Paragraph>
            )}
            {!bundleLoading && bundle && (
              <Space wrap style={{ marginBottom: 16 }}>
                {bundle.alertname && <Tag>alertname={bundle.alertname}</Tag>}
                {bundle.impact != null && (
                  <Tag color="blue">下游 {bundle.impact.downstream ?? 0} · 上游 {bundle.impact.upstream ?? 0}</Tag>
                )}
                {(bundle.cases || []).slice(0, 3).map((c) => (
                  <Tag key={c.metric_key + c.title} color="green">案例 {c.title}</Tag>
                ))}
                {(bundle.findings?.length ?? 0) > 0 && <Tag>洞察 {bundle.findings!.length}</Tag>}
                {(bundle.history?.length ?? 0) > 0 && <Tag>历史 {bundle.history!.length}</Tag>}
                {(bundle.drifts?.length ?? 0) > 0 && <Tag color="orange">漂移 {bundle.drifts!.length}</Tag>}
              </Space>
            )}
            {!bundleLoading && !bundle?.summary && (
              <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
                暂无额外诊断上下文（可检查 labels 是否含 instance_id / alertname）
              </Typography.Text>
            )}
            <Typography.Title level={5} style={{ marginTop: 24 }}>CMDB 影响</Typography.Title>
            <Space style={{ marginBottom: 8 }}>
              <Button size="small" loading={cmdbLoading} onClick={() => void loadCmdbImpact()}>
                查 CMDB 影响
              </Button>
              {cmdbImpact && (
                <Typography.Text type="secondary">
                  下游 {cmdbImpact.summary.downstream} · 上游 {cmdbImpact.summary.upstream}
                </Typography.Text>
              )}
            </Space>
            {cmdbImpact && (
              <Space wrap style={{ marginBottom: 16 }}>
                {Object.entries(cmdbImpact.summary.by_type || {}).map(([k, v]) => (
                  <Tag key={k}>{k}:{v}</Tag>
                ))}
                {(cmdbImpact.summary.app_tags || []).map((t) => (
                  <Tag key={t} color="purple">{t}</Tag>
                ))}
              </Space>
            )}
            <Typography.Title level={5} style={{ marginTop: 24 }}>处理时间线</Typography.Title>
            <Timeline
              items={(detail.events || []).map((e) => ({
                children: (
                  <div>
                    <div style={{ fontWeight: 600 }}>{e.detail || e.action}</div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {e.operator} · {e.created_at ? dayjs(e.created_at).format('MM-DD HH:mm:ss') : ''}
                    </Typography.Text>
                  </div>
                ),
              }))}
            />
          </>
        )}
      </Drawer>

      <Modal
        title="Runbook 建议"
        open={runbookOpen}
        onCancel={() => setRunbookOpen(false)}
        footer={null}
        width={560}
      >
        <List
          dataSource={runbookItems}
          renderItem={(it) => (
            <List.Item
              actions={[
                <Button
                  key="run"
                  type={it.executable ? 'primary' : 'default'}
                  size="small"
                  disabled={!hasUrn(Perm.runbook.execute)}
                  onClick={() => {
                    Modal.confirm({
                      title: `确认执行 ${it.title}？`,
                      content: it.executable ? it.description : `${it.description}（将生成草稿待办）`,
                      onOk: () => execRunbook(it.action),
                    })
                  }}
                >
                  {it.executable ? '执行' : '生成草稿'}
                </Button>,
              ]}
            >
              <List.Item.Meta title={it.title} description={`${it.description} · ${it.reason}`} />
            </List.Item>
          )}
        />
      </Modal>

      <Drawer title="DBA 助手" open={copilotOpen} onClose={() => setCopilotOpen(false)} width={480}>
        <Space style={{ marginBottom: 12 }} wrap>
          <Typography.Text type="secondary">{copilotLlm ? 'LLM 已启用' : '离线模式'}</Typography.Text>
          {copilotModels.length > 0 && (
            <Select
              style={{ minWidth: 180 }}
              value={copilotModel}
              onChange={setCopilotModel}
              options={copilotModels}
              placeholder="选择模型"
            />
          )}
          <Button size="small" onClick={resetCopilot} disabled={copilotLoading}>新对话</Button>
          <Button
            size="small"
            disabled={copilotLoading || !copilotLlm}
            onClick={() => void askCopilot({ forceRefresh: true })}
            title="跳过知识库直答与缓存，强制调用大模型"
          >
            强制刷新
          </Button>
        </Space>
        <div style={{ maxHeight: 280, overflowY: 'auto', marginBottom: 12 }}>
          {copilotTurns.map((t, i) => (
            <div key={i} style={{
              marginBottom: 8, padding: 8, borderRadius: 6,
              background: t.role === 'user' ? '#f0f7ff' : '#fafafa', border: '1px solid #eee',
            }}>
              <Space wrap size={8}>
                <Typography.Text strong>{t.role === 'user' ? '我' : '助手'}</Typography.Text>
                {t.meta && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t.meta}</Typography.Text>}
                {t.learned && <Tag color="green">已学习</Tag>}
                {t.role === 'assistant' && t.learnable && (
                  <Button size="small" type="primary" ghost onClick={() => confirmCopilotLearn(i)}>
                    {t.learned ? '重新学习' : '学习'}
                  </Button>
                )}
              </Space>
              <Typography.Paragraph style={{ margin: '4px 0 0', whiteSpace: 'pre-wrap' }}>{t.content}</Typography.Paragraph>
            </div>
          ))}
        </div>
        <CopilotImageInput
          value={copilotImages}
          onChange={setCopilotImages}
          disabled={copilotLoading}
          textareaProps={{
            rows: 3,
            value: copilotMsg,
            onChange: (e) => setCopilotMsg(e.target.value),
            placeholder: '描述问题或追问；Ctrl+V 粘贴截图',
          }}
        />
        <Space style={{ marginTop: 12 }} wrap>
          <Button type="primary" loading={copilotLoading} onClick={() => void askCopilot()}>
            {copilotLoading ? '等待 Agent…' : '提问'}
          </Button>
          <Button
            loading={copilotLoading}
            disabled={!copilotLlm}
            onClick={() => void askCopilot({ forceRefresh: true })}
            title="跳过知识库直答与缓存，强制调用大模型"
          >
            强制刷新
          </Button>
        </Space>
        {copilotLoading && (
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
            Cursor Agent 可能需要 1–3 分钟；相同问题优先走缓存
          </Typography.Text>
        )}
      </Drawer>
    </div>
  )
}
