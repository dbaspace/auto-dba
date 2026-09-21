import React, { useEffect, useMemo, useState } from 'react'
import { Button, Checkbox, Input, Modal, Pagination, Select, Spin, Table, message } from 'antd'
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
}

export default function KafkaQuery() {
  const [instLoading, setInstLoading] = useState(false)
  const [topicsLoading, setTopicsLoading] = useState(false)
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

  function formatCount(n: any) {
    if (n == null || Number(n) === 0) return '-'
    return Number(n)
  }

  function formatSize(bytes: any) {
    if (bytes == null || Number(bytes) === 0) return '-'
    const b = Number(bytes)
    if (b >= 1073741824) return `${(b / 1073741824).toFixed(1)} GB`
    if (b >= 1048576) return `${(b / 1048576).toFixed(1)} MB`
    if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`
    return `${b} B`
  }

  function formatLastRecord(iso: any) {
    if (!iso) return '-'
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
  }

  function selectInstance(inst: any) {
    setSelectedId(inst.id)
    setPage(1)
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
        loadTopics()
      },
    })
  }

  async function loadTopics(id: string = selectedId) {
    if (!id) return
    setTopicsLoading(true)
    try {
      const { data } = await api.get(`/instances/${id}/kafka/topics`)
      setTopics(data.data || [])
    } catch {
      setTopics([])
    } finally {
      setTopicsLoading(false)
    }
  }

  async function loadMeta() {
    setInstLoading(true)
    try {
      const [instRes, envRes] = await Promise.all([
        api.get('/instances', { params: { db_type: 'kafka' } }),
        api.get('/environments'),
      ])
      setInstances(instRes.data.data || [])
      setEnvs(envRes.data.data || [])
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
                <Button loading={topicsLoading} onClick={() => loadTopics()}>刷新 Topic</Button>
                <Input value={topicSearch} allowClear placeholder="搜索 Topic" style={{ width: 200 }} onChange={(e) => setTopicSearch(e.target.value)} />
                <Checkbox checked={hideInternal} onChange={(e) => setHideInternal(e.target.checked)}>隐藏内部 Topic（__ 前缀）</Checkbox>
              </div>
              <div className="ops-table">
                <Table
                  dataSource={pagedTopics}
                  rowKey={(r) => r.name || ''}
                  size="small"
                  loading={topicsLoading}
                  pagination={false}
                  scroll={{ y: 'calc(100% - 8px)' }}
                  locale={{ emptyText: '该实例暂无 Topic' }}
                  columns={[
                    { title: 'Name', dataIndex: 'name', ellipsis: true, width: 220 },
                    { title: 'Count', width: 90, render: (_, row) => formatCount(row.message_count) },
                    { title: 'Size', width: 90, render: (_, row) => formatSize(row.size_bytes) },
                    { title: 'Last Record', width: 160, render: (_, row) => formatLastRecord(row.last_record) },
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
