import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Modal, Select, Table, Tabs, Tag, Typography, message } from 'antd'
import { StopOutlined } from '@ant-design/icons'
import axios from 'axios'
import api from '../../api/request'
import './KafkaTopicQueryModal.css'

interface CellDetail {
  title: string
  content: string
}

interface Props {
  visible: boolean
  instanceId: string
  topicName: string
  initialTab?: string
  /** 列表 Last Record 所在分区，优先作为 Data 默认 Partition */
  initialPartition?: number
  onClose: () => void
}

interface SearchField {
  key: string
  label: string
  value: string
  operator: string
}

interface KafkaMessage {
  key?: string
  value?: string
  timestamp?: any
  partition?: any
  offset?: any
  headers?: Record<string, any>
}

interface PartitionRow {
  id: number
  leader?: number
  replicas?: number[]
  offsets_text?: string
  size_text?: string
}

interface ConfigRow {
  name: string
  value: string
  value_hint?: string
  source?: string
}

interface LogRow {
  broker: number
  topic: string
  partition: number
  size_text?: string
  offset_lag?: number
}

function sourceTagColor(source?: string) {
  if (!source) return 'default'
  if (source.includes('DYNAMIC')) return 'gold'
  if (source.includes('STATIC')) return 'orange'
  return 'default'
}

export default function KafkaTopicQueryModal({
  visible,
  instanceId,
  topicName,
  initialTab = 'data',
  initialPartition,
  onClose,
}: Props) {
  const [activeTab, setActiveTab] = useState('data')
  const [sortOrder, setSortOrder] = useState('newest')
  const [partition, setPartition] = useState('0')
  const [partitionOptions, setPartitionOptions] = useState<PartitionRow[]>([])
  const [offsetInput, setOffsetInput] = useState('-1')
  const [timeFormat, setTimeFormat] = useState('RELATIVE')
  const [showSearch, setShowSearch] = useState(false)
  const [loading, setLoading] = useState(false)
  const [metaLoading, setMetaLoading] = useState(false)
  const queryAbortRef = useRef<AbortController | null>(null)
  const [messages, setMessages] = useState<KafkaMessage[]>([])
  const [partitionRows, setPartitionRows] = useState<PartitionRow[]>([])
  const [configRows, setConfigRows] = useState<ConfigRow[]>([])
  const [logRows, setLogRows] = useState<LogRow[]>([])
  const [cellDetail, setCellDetail] = useState<CellDetail | null>(null)
  const [searchFields, setSearchFields] = useState<SearchField[]>([
    { key: 'key', label: 'Key', value: '', operator: 'Contains' },
    { key: 'value', label: 'Value', value: '', operator: 'Contains' },
    { key: 'headerKey', label: 'Header Key', value: '', operator: 'Contains' },
    { key: 'headerValue', label: 'Header Value', value: '', operator: 'Contains' },
  ])

  function topicPath() {
    return encodeURIComponent(topicName)
  }

  function applySearchFilter(list: KafkaMessage[]) {
    return list.filter((m) => searchFields.every((row) => {
      const v = (row.value || '').trim().toLowerCase()
      if (!v) return true
      let text = ''
      if (row.key === 'key') text = (m.key || '').toLowerCase()
      else if (row.key === 'value') text = (m.value || '').toLowerCase()
      else if (row.key === 'headerKey') text = Object.keys(m.headers || {}).join(' ').toLowerCase()
      else if (row.key === 'headerValue') text = Object.values(m.headers || {}).join(' ').toLowerCase()
      else return true
      if (row.operator === 'Equals') return text === v
      if (row.operator === 'Starts With') return text.startsWith(v)
      if (row.operator === 'Ends With') return text.endsWith(v)
      if (row.operator === 'Not Contains') return !text.includes(v)
      return text.includes(v)
    }))
  }

  const sortedMessages = useMemo(() => applySearchFilter(messages), [messages, searchFields])

  async function loadPartitions() {
    if (!instanceId || !topicName) return
    setMetaLoading(true)
    try {
      const { data } = await api.get(`/instances/${instanceId}/kafka/topics/${topicPath()}/partitions`)
      const list: PartitionRow[] = data.data || []
      setPartitionOptions(list)
      setPartitionRows(list)
      setPartition((prev) => {
        const preferred =
          initialPartition != null && Number(initialPartition) >= 0
            ? String(initialPartition)
            : ''
        if (preferred && list.some((p) => String(p.id) === preferred)) return preferred
        if (prev === '') return ''
        if (list.some((p) => String(p.id) === prev)) return prev
        return list.length ? String(list[0].id) : '0'
      })
    } catch {
      setPartitionOptions([])
      setPartitionRows([])
    } finally {
      setMetaLoading(false)
    }
  }

  async function loadMessages(partitionOverride?: string) {
    if (!instanceId || !topicName) return
    queryAbortRef.current?.abort()
    const ac = new AbortController()
    queryAbortRef.current = ac
    setLoading(true)
    const part = partitionOverride !== undefined ? partitionOverride : partition
    try {
      const params: Record<string, any> = { limit: 100, sort: sortOrder }
      if (offsetInput !== '' && offsetInput !== '-1') params.offset = offsetInput
      if (part !== '') params.partition = part
      const { data } = await api.get(
        `/instances/${instanceId}/kafka/topics/${topicPath()}/messages`,
        { params, signal: ac.signal },
      )
      if (data?.canceled) {
        message.info(data.message || '查询已终止')
        return
      }
      setMessages(data.data || [])
    } catch (err: any) {
      if (axios.isCancel(err) || err?.code === 'ERR_CANCELED' || err?.name === 'CanceledError') {
        message.info('查询已终止')
        return
      }
      setMessages([])
    } finally {
      if (queryAbortRef.current === ac) queryAbortRef.current = null
      setLoading(false)
    }
  }

  async function loadConfigs() {
    if (!instanceId || !topicName) return
    setMetaLoading(true)
    try {
      const { data } = await api.get(`/instances/${instanceId}/kafka/topics/${topicPath()}/configs`)
      setConfigRows(data.data || [])
    } catch {
      setConfigRows([])
      message.error('加载 Configs 失败')
    } finally {
      setMetaLoading(false)
    }
  }

  async function loadLogs() {
    if (!instanceId || !topicName) return
    setMetaLoading(true)
    try {
      const { data } = await api.get(`/instances/${instanceId}/kafka/topics/${topicPath()}/logs`)
      setLogRows(data.data || [])
    } catch {
      setLogRows([])
      message.error('加载 Logs 失败')
    } finally {
      setMetaLoading(false)
    }
  }

  function stopQuery() {
    queryAbortRef.current?.abort()
  }

  function clearSearch() {
    setSearchFields((list) => list.map((r) => ({ ...r, value: '', operator: 'Contains' })))
  }

  function updateSearchField(idx: number, patch: Partial<SearchField>) {
    setSearchFields((list) => list.map((r, i) => (i === idx ? { ...r, ...patch } : r)))
  }

  function formatTime(ts: any) {
    if (!ts) return '-'
    const d = new Date(ts)
    if (Number.isNaN(d.getTime())) return '-'
    if (timeFormat === 'RELATIVE') {
      const diff = Date.now() - d.getTime()
      if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`
      if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
      if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
      return `${Math.floor(diff / 86400000)}d ago`
    }
    return d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
  }

  function formatHeaders(h: any) {
    if (!h || !Object.keys(h).length) return '-'
    return Object.entries(h).map(([k, v]) => `${k}: ${v}`).join('; ')
  }

  function prettyText(raw?: string) {
    const s = (raw || '').trim()
    if (!s) return '-'
    try {
      return JSON.stringify(JSON.parse(s), null, 2)
    } catch {
      return raw || '-'
    }
  }

  function renderClickable(text: string, title: string) {
    if (!text || text === '-') return '-'
    return (
      <Typography.Link
        className="cell-link"
        title="点击查看全文"
        onClick={() => setCellDetail({ title, content: prettyText(text) })}
      >
        {text}
      </Typography.Link>
    )
  }

  function downloadResult() {
    const list = sortedMessages
    if (!list.length) {
      message.info('没有可下载的数据')
      return
    }
    const blob = new Blob([JSON.stringify(list, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `kafka-${topicName}-${Date.now()}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  function loadTabData(tab: string) {
    if (tab === 'data') loadMessages()
    else if (tab === 'partitions') loadPartitions()
    else if (tab === 'configs') loadConfigs()
    else if (tab === 'logs') loadLogs()
  }

  useEffect(() => {
    if (visible && instanceId && topicName) {
      const tab = initialTab || 'data'
      setActiveTab(tab)
      const preferred =
        initialPartition != null && Number(initialPartition) >= 0
          ? String(initialPartition)
          : undefined
      if (preferred !== undefined) setPartition(preferred)
      loadPartitions()
      if (tab === 'data') loadMessages(preferred)
      else loadTabData(tab)
    } else {
      queryAbortRef.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible])

  useEffect(() => {
    if (visible && instanceId && topicName && activeTab === 'data') loadMessages()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sortOrder, partition])

  useEffect(() => () => {
    queryAbortRef.current?.abort()
  }, [])

  function renderBadgeList(ids?: number[], color = 'blue') {
    if (!ids?.length) return '-'
    return (
      <span className="badge-list">
        {ids.map((id) => (
          <Tag color={color} key={id}>{id}</Tag>
        ))}
      </span>
    )
  }

  function renderMetaTable() {
    if (activeTab === 'partitions') {
      return (
        <Table
          dataSource={partitionRows}
          rowKey={(r) => String(r.id)}
          size="small"
          loading={metaLoading}
          pagination={false}
          scroll={{ y: 420 }}
          locale={{ emptyText: metaLoading ? '查询中…' : '暂无数据' }}
          columns={[
            { title: 'Id', dataIndex: 'id', width: 80 },
            {
              title: 'Leader',
              dataIndex: 'leader',
              width: 100,
              render: (v: number) => (v == null ? '-' : <Tag color="blue">{v}</Tag>),
            },
            {
              title: 'Replicas',
              dataIndex: 'replicas',
              width: 160,
              render: (v: number[]) => renderBadgeList(v, 'green'),
            },
            { title: 'Offsets', dataIndex: 'offsets_text', width: 160 },
            { title: 'Size', dataIndex: 'size_text' },
          ]}
        />
      )
    }
    if (activeTab === 'configs') {
      return (
        <Table
          dataSource={configRows}
          rowKey={(r) => r.name}
          size="small"
          loading={metaLoading}
          pagination={false}
          scroll={{ y: 420 }}
          locale={{ emptyText: metaLoading ? '查询中…' : '暂无数据' }}
          columns={[
            { title: 'Name', dataIndex: 'name', width: 280, ellipsis: true },
            {
              title: 'Value',
              dataIndex: 'value',
              render: (_: string, row: ConfigRow) => (
                <div>
                  <div>{row.value || '-'}</div>
                  {row.value_hint ? <div className="value-hint">{row.value_hint}</div> : null}
                </div>
              ),
            },
            {
              title: 'Type',
              dataIndex: 'source',
              width: 220,
              render: (v: string) => (
                <Tag color={sourceTagColor(v)}>{v || '-'}</Tag>
              ),
            },
          ]}
        />
      )
    }
    if (activeTab === 'logs') {
      return (
        <Table
          dataSource={logRows}
          rowKey={(r) => `${r.broker}-${r.partition}-${r.topic}`}
          size="small"
          loading={metaLoading}
          pagination={false}
          scroll={{ y: 420 }}
          locale={{ emptyText: metaLoading ? '查询中…' : '暂无数据' }}
          columns={[
            { title: 'Broker', dataIndex: 'broker', width: 100 },
            { title: 'Topic', dataIndex: 'topic', ellipsis: true },
            { title: 'Partition', dataIndex: 'partition', width: 100 },
            { title: 'Size', dataIndex: 'size_text', width: 140 },
            { title: 'OffsetLag', dataIndex: 'offset_lag', width: 110 },
          ]}
        />
      )
    }
    return <div className="placeholder">Consumer Groups 功能开发中</div>
  }

  return (
    <Modal
      open={visible}
      title={`Topic: ${topicName}`}
      width={1100}
      style={{ top: '6vh' }}
      footer={null}
      destroyOnClose
      onCancel={onClose}
    >
      <Tabs
        activeKey={activeTab}
        onChange={(key) => {
          setActiveTab(key)
          loadTabData(key)
        }}
        items={[
          { key: 'data', label: 'Data' },
          { key: 'partitions', label: 'Partitions' },
          { key: 'consumerGroups', label: 'Consumer Groups' },
          { key: 'configs', label: 'Configs' },
          { key: 'logs', label: 'Logs' },
        ]}
      />

      {activeTab === 'data' ? (
        <>
          <div className="toolbar">
            <span className="lab">Sort</span>
            <Select
              value={sortOrder}
              style={{ width: 110 }}
              onChange={setSortOrder}
              options={[{ label: 'Oldest', value: 'oldest' }, { label: 'Newest', value: 'newest' }]}
            />
            <span className="lab">Partition</span>
            <Select
              value={partition}
              style={{ width: 110 }}
              onChange={setPartition}
              options={[
                { label: 'All', value: '' },
                ...partitionOptions.map((p) => ({ label: String(p.id), value: String(p.id) })),
              ]}
            />
            <span className="lab">Offset</span>
            <Input value={offsetInput} style={{ width: 90 }} placeholder="-1" onChange={(e) => setOffsetInput(e.target.value)} />
            <span className="lab">Time</span>
            <Select
              value={timeFormat}
              style={{ width: 120 }}
              onChange={setTimeFormat}
              options={[{ label: 'RELATIVE', value: 'RELATIVE' }, { label: 'ABSOLUTE', value: 'ABSOLUTE' }]}
            />
            <Button onClick={() => setShowSearch((v) => !v)}>Search</Button>
            <Button type="primary" loading={loading} onClick={loadMessages}>查询</Button>
            <Button danger disabled={!loading} icon={<StopOutlined />} onClick={stopQuery}>查询终止</Button>
            <Button onClick={downloadResult} disabled={loading}>下载 JSON</Button>
            <span className="muted">默认近 7 天 · 自动定位最近有数据的分区 · 大 Topic 勿用 All</span>
          </div>

          {showSearch ? (
            <div className="search-panel">
              {searchFields.map((row, idx) => (
                <div key={row.key} className="search-row">
                  <span className="lab">{row.label}</span>
                  <Input value={row.value} style={{ flex: 1 }} onChange={(e) => updateSearchField(idx, { value: e.target.value })} />
                  <Select
                    value={row.operator}
                    style={{ width: 140 }}
                    onChange={(v) => updateSearchField(idx, { operator: v })}
                    options={[
                      { label: 'Contains', value: 'Contains' },
                      { label: 'Equals', value: 'Equals' },
                      { label: 'Starts With', value: 'Starts With' },
                      { label: 'Ends With', value: 'Ends With' },
                      { label: 'Not Contains', value: 'Not Contains' },
                    ]}
                  />
                </div>
              ))}
              <div className="search-actions">
                <Button type="primary" onClick={() => setShowSearch(false)}>应用过滤</Button>
                <Button onClick={clearSearch}>清空</Button>
              </div>
            </div>
          ) : null}

          <Table
            dataSource={sortedMessages}
            rowKey={(_, i) => String(i)}
            size="small"
            loading={loading}
            scroll={{ y: 380 }}
            pagination={false}
            locale={{ emptyText: loading ? '查询中…' : '暂无数据' }}
            rowSelection={{ type: 'checkbox' }}
            columns={[
              {
                title: 'Key',
                dataIndex: 'key',
                width: 140,
                ellipsis: true,
                render: (v: string) => renderClickable(v || '', 'Key'),
              },
              {
                title: 'Value',
                dataIndex: 'value',
                ellipsis: true,
                render: (v: string) => renderClickable(v || '', 'Value'),
              },
              { title: 'Timestamp', dataIndex: 'timestamp', width: 160, render: (v) => formatTime(v) },
              { title: 'Partition', dataIndex: 'partition', width: 90 },
              { title: 'Offset', dataIndex: 'offset', width: 100 },
              {
                title: 'Headers',
                dataIndex: 'headers',
                ellipsis: true,
                render: (v) => {
                  const text = formatHeaders(v)
                  return renderClickable(text === '-' ? '' : text, 'Headers')
                },
              },
            ]}
          />
          <Modal
            title={cellDetail?.title || '详情'}
            open={!!cellDetail}
            onCancel={() => setCellDetail(null)}
            footer={null}
            width={720}
            destroyOnClose
          >
            <pre className="cell-detail-pre">{cellDetail?.content || '-'}</pre>
          </Modal>
        </>
      ) : (
        renderMetaTable()
      )}
    </Modal>
  )
}
