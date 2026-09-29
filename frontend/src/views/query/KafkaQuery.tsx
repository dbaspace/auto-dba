import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Checkbox, Input, Modal, Pagination, Select, Spin, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import KafkaTopicQueryModal from './KafkaTopicQueryModal'
import './KafkaQuery.css'

interface Topic {
  name?: string
  message_count?: any
  size_bytes?: any
  last_record?: any
  partition_count?: any
  replication_factor?: any
  stats?: boolean
}

export default function KafkaQuery() {
  const [instLoading, setInstLoading] = useState(false)
  const [topicsLoading, setTopicsLoading] = useState(false)
  const loadSeq = useRef(0)
  const statsSeq = useRef(0)
  const topicCache = useRef<Map<string, Topic[]>>(new Map())
  const liteInflight = useRef<Map<string, Promise<Topic[]>>>(new Map())
  const liteGen = useRef<Map<string, number>>(new Map())
  const statsAsked = useRef<Set<string>>(new Set())
  const [instances, setInstances] = useState<any[]>([])
  const [envs, setEnvs] = useState<any[]>([])
  const [keyword, setKeyword] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [topics, setTopics] = useState<Topic[]>([])
  const [topicSearch, setTopicSearch] = useState('')
  const [hideInternal, setHideInternal] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [modalOpen, setModalOpen] = useState(false)
  const [modalTopic, setModalTopic] = useState('')
  const [modalTab, setModalTab] = useState('data')

  const selected = useMemo(() => instances.find((i) => i.id === selectedId), [instances, selectedId])

  const envOptions = useMemo(() => {
    const set = new Set<string>()
    for (const e of envs) {
      if (e.code) set.add(e.code)
      if (e.name) set.add(e.name)
    }
    for (const i of instances) {
      if (i.idc) set.add(i.idc)
    }
    return [...set]
  }, [envs, instances])

  function instEnv(i: any) {
    return i?.idc || i?.env_id || ''
  }

  function instAddr(i: any) {
    if (!i) return ''
    const host = i.host || i.domain_name || ''
    return host ? `${host}:${i.port || 9092}` : ''
  }

  const filteredInstances = useMemo(() => {
    let list = instances
    if (envFilter) list = list.filter((i) => instEnv(i) === envFilter)
    const q = keyword.trim().toLowerCase()
    if (q) list = list.filter((i) => (i.name || '').toLowerCase().includes(q) || instAddr(i).toLowerCase().includes(q))
    return list
  }, [instances, envFilter, keyword])

  const filteredTopics = useMemo(() => {
    let list = topics
    if (hideInternal) list = list.filter((t) => !(t.name || '').startsWith('__'))
    const q = topicSearch.trim().toLowerCase()
    if (q) list = list.filter((t) => (t.name || '').toLowerCase().includes(q))
    return list
  }, [topics, hideInternal, topicSearch])

  const pagedTopics = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredTopics.slice(start, start + pageSize)
  }, [filteredTopics, page, pageSize])

  function formatCount(row: Topic) {
    if (!row.stats || row.message_count == null) return '-'
    return Number(row.message_count)
  }

  function formatSize(row: Topic) {
    if (!row.stats || row.size_bytes == null) return '-'
    const b = Number(row.size_bytes)
    if (b === 0) return '0 B'
    if (b >= 1073741824) return `${(b / 1073741824).toFixed(1)} GB`
    if (b >= 1048576) return `${(b / 1048576).toFixed(1)} MB`
    if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`
    return `${b} B`
  }

  function formatLastRecord(row: Topic) {
    if (!row.stats || !row.last_record) return '-'
    const d = new Date(row.last_record)
    return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
  }

  function topicSkeleton(list: Topic[]): Topic[] {
    return (list || []).map((t) => ({
      name: t.name,
      partition_count: t.partition_count,
      replication_factor: t.replication_factor,
    }))
  }

  function fetchLite(id: string, refresh = false): Promise<Topic[]> {
    if (refresh) {
      liteGen.current.set(id, (liteGen.current.get(id) || 0) + 1)
      topicCache.current.delete(id)
    } else {
      const hit = topicCache.current.get(id)
      if (hit) return Promise.resolve(hit)
      const pending = liteInflight.current.get(id)
      if (pending) return pending
    }
    const gen = liteGen.current.get(id) || 0
    const p = api.get(`/instances/${id}/kafka/topics`, {
      params: refresh ? { lite: 1, refresh: 1 } : { lite: 1 },
      timeout: 15000,
      silent: !refresh,
    }).then(({ data }) => {
      const list = topicSkeleton(data.data || [])
      if ((liteGen.current.get(id) || 0) !== gen) return list
      topicCache.current.set(id, list)
      return list
    }).finally(() => {
      if (liteInflight.current.get(id) === p) liteInflight.current.delete(id)
    })
    if (!refresh) liteInflight.current.set(id, p)
    return p
  }

  function selectInstance(inst: any) {
    setSelectedId(inst.id)
    setPage(1)
    statsSeq.current += 1
    statsAsked.current = new Set()
    setTopics(topicCache.current.get(inst.id) || [])
    loadTopics(inst.id)
  }

  function openTopic(name: string, tab: string) {
    setModalTopic(name)
    setModalTab(tab)
    setModalOpen(true)
  }

  function confirmDelete(row: Topic) {
    Modal.confirm({
      title: '删除 Topic',
      content: `确定删除 Topic「${row.name}」？此操作不可恢复。`,
      okType: 'danger',
      onOk: async () => {
        await api.delete(`/instances/${selectedId}/kafka/topics/${encodeURIComponent(row.name || '')}`)
        message.success('已删除')
        loadTopics(selectedId, true)
      },
    })
  }

  async function loadTopics(id: string = selectedId, refresh = false) {
    if (!id) return
    const seq = ++loadSeq.current
    const cached = !refresh ? topicCache.current.get(id) : undefined
    if (refresh) {
      statsSeq.current += 1
      statsAsked.current = new Set()
      setTopics([])
    }
    if (!cached) setTopicsLoading(true)
    try {
      const list = await fetchLite(id, refresh)
      if (seq !== loadSeq.current) return
      setTopics((prev) => {
        if (refresh) return list
        const prevStats = new Map(prev.filter((t) => t.stats).map((t) => [t.name || '', t]))
        return list.map((t) => {
          const old = prevStats.get(t.name || '')
          if (!old) return t
          return {
            ...t,
            message_count: old.message_count,
            size_bytes: old.size_bytes,
            last_record: old.last_record,
            stats: true,
          }
        })
      })
    } catch {
      if (seq !== loadSeq.current) return
      if (!cached) setTopics([])
    } finally {
      if (seq === loadSeq.current) setTopicsLoading(false)
    }
  }

  const pendingStatsKey = useMemo(() => {
    return pagedTopics
      .filter((t) => t.name && !t.stats && !statsAsked.current.has(t.name))
      .map((t) => t.name)
      .join('\n')
  }, [pagedTopics])

  useEffect(() => {
    if (!selectedId || !pendingStatsKey) return
    const names = pendingStatsKey.split('\n').filter(Boolean)
    names.forEach((n) => statsAsked.current.add(n))
    const seq = ++statsSeq.current
    const id = selectedId
    api.post(`/instances/${id}/kafka/topics/stats`, { names }, { timeout: 25000, silent: true })
      .then(({ data }) => {
        if (seq !== statsSeq.current) return
        const rows: Topic[] = data?.data || []
        const byName = new Map(rows.map((r) => [r.name || '', r]))
        setTopics((prev) => prev.map((t) => {
          const name = t.name || ''
          if (!names.includes(name)) return t
          const s = byName.get(name)
          if (!s) return { ...t, stats: true, message_count: 0, size_bytes: 0 }
          return {
            ...t,
            message_count: s.message_count ?? 0,
            size_bytes: s.size_bytes ?? 0,
            last_record: s.last_record || '',
            partition_count: s.partition_count ?? t.partition_count,
            replication_factor: s.replication_factor ?? t.replication_factor,
            stats: true,
          }
        }))
      })
      .catch(() => {
        names.forEach((n) => statsAsked.current.delete(n))
      })
  }, [selectedId, pendingStatsKey])

  async function loadMeta() {
    setInstLoading(true)
    try {
      const [instRes, envRes] = await Promise.all([
        api.get('/instances', { params: { db_type: 'kafka' } }),
        api.get('/environments'),
      ])
      setInstances(instRes.data.data || [])
      setEnvs(envRes.data.data || [])
      for (const inst of instRes.data.data || []) {
        if (inst?.id) fetchLite(inst.id).catch(() => {})
      }
    } finally {
      setInstLoading(false)
    }
  }

  useEffect(() => { setPage(1) }, [topicSearch, hideInternal, pageSize])
  useEffect(() => { loadMeta() }, [])

  return (
    <div className="ops-page kafka-query">
      <div className="layout">
        <aside className="side">
          <div className="side-head">Kafka 实例</div>
          <Input value={keyword} allowClear placeholder="搜索实例" className="mb" onChange={(e) => setKeyword(e.target.value)} />
          <Select
            value={envFilter || undefined}
            allowClear
            placeholder="全部环境"
            className="mb"
            style={{ width: '100%' }}
            onChange={(v) => setEnvFilter(v || '')}
            options={envOptions.map((e) => ({ label: e, value: e }))}
          />
          <Spin spinning={instLoading} wrapperClassName="inst-list">
            {filteredInstances.map((i) => (
              <div
                key={i.id}
                className={`inst-item${selectedId === i.id ? ' active' : ''}`}
                onClick={() => selectInstance(i)}
              >
                <span className="name">{i.name || instAddr(i)}</span>
                <span className="env">{instEnv(i)}</span>
              </div>
            ))}
            {!instLoading && !filteredInstances.length ? (
              <p className="empty">暂无 Kafka 实例，请先在「配置 → 实例」登记 db_type=kafka</p>
            ) : null}
          </Spin>
        </aside>
        <section className="main">
          {!selectedId ? (
            <div className="placeholder">请从左侧选择 Kafka 实例查看 Topic 列表</div>
          ) : (
            <>
              <div className="toolbar">
                <strong>{selected?.name || instAddr(selected)}</strong>
                <span className="muted">{instAddr(selected)}</span>
                <Button loading={topicsLoading} onClick={() => loadTopics(selectedId, true)}>刷新 Topic</Button>
                <Input value={topicSearch} allowClear placeholder="搜索 Topic" style={{ width: 200 }} onChange={(e) => setTopicSearch(e.target.value)} />
                <Checkbox checked={hideInternal} onChange={(e) => setHideInternal(e.target.checked)}>隐藏内部 Topic（__ 前缀）</Checkbox>
              </div>
              <div className="ops-table">
                <ResponsiveTable
                  dataSource={pagedTopics}
                  rowKey={(r) => r.name || ''}
                  size="small"
                  pagination={false}
                  scroll={{ y: 'calc(100% - 8px)' }}
                  locale={{ emptyText: topicsLoading ? '加载中' : '该实例暂无 Topic' }}
                  columns={[
                    { title: 'Name', dataIndex: 'name', ellipsis: true, width: 220 },
                    { title: 'Count', width: 90, render: (_, row) => formatCount(row) },
                    { title: 'Size', width: 90, render: (_, row) => formatSize(row) },
                    { title: 'Last Record', width: 160, render: (_, row) => formatLastRecord(row) },
                    { title: 'Partitions', dataIndex: 'partition_count', width: 110 },
                    { title: 'Replications', dataIndex: 'replication_factor', width: 120 },
                    {
                      title: '操作',
                      width: 160,
                      fixed: 'right',
                      render: (_, row) => (
                        <>
                          <Button type="link" size="small" onClick={() => openTopic(row.name || '', 'data')}>查看</Button>
                          <Button type="link" size="small" onClick={() => openTopic(row.name || '', 'configs')}>配置</Button>
                          <Button type="link" size="small" danger onClick={() => confirmDelete(row)}>删除</Button>
                        </>
                      ),
                    },
                  ]}
                />
              </div>
              <div className="pager">
                <Pagination
                  current={page}
                  pageSize={pageSize}
                  total={filteredTopics.length}
                  pageSizeOptions={[10, 25, 50, 100]}
                  showSizeChanger
                  showTotal={(t) => `共 ${t} 条`}
                  size="small"
                  onChange={(p, ps) => { setPage(p); setPageSize(ps) }}
                />
              </div>
            </>
          )}
        </section>
      </div>
      <KafkaTopicQueryModal
        visible={modalOpen}
        instanceId={selectedId}
        topicName={modalTopic}
        initialTab={modalTab}
        onClose={() => setModalOpen(false)}
      />
    </div>
  )
}
