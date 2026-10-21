import React, { useCallback, useEffect, useState } from 'react'
import {
  Button, Card, Drawer, Input, Select, Space, Table, Tabs, Tag, Typography, message,
} from 'antd'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import CopilotPanel from '../../components/copilot/CopilotPanel'

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
          <Button type="primary" onClick={() => setChatOpen(true)}>DBA 助手</Button>
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

      <Drawer title="DBA Copilot" open={chatOpen} onClose={() => setChatOpen(false)} width={860} destroyOnClose={false}>
        <CopilotPanel active={chatOpen} layout="drawer" onLearned={() => void load()} />
      </Drawer>
    </div>
  )
}
