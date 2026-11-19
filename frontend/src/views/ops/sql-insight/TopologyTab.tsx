import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Checkbox, Empty, Input, Popover, Table } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import api from '../../../api/request'

export interface TopologyRow {
  host: string
  database: string
  connections: number
  pod: string
  namespace: string
  node: string
  local?: boolean
  lookup?: boolean
  pool?: boolean
  pool_current?: number
  pool_active?: number
  pool_idle?: number
  pool_pending?: number
  pool_max?: number
  pool_min?: number
  pool_active_max_7d?: number
  pool_pending_max_7d?: number
  pool_advice?: string
}

export type TopologyExportRow = {
  host: string
  database: string
  connections: number
  pod: string
  pool_current: string
  pool_active: string
  pool_idle: string
  pool_pending: string
  pool_max: string
  pool_min: string
  pool_active_max_7d: string
  pool_pending_max_7d: string
  pool_advice: string
}

function FoldedName(props: { text: string; title: string }) {
  const folded = props.text.length > 24
  const label = (
    <span style={{ display: 'inline-block', maxWidth: folded ? 156 : 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', verticalAlign: 'bottom' }}>
      {props.text}
    </span>
  )
  if (!folded) return label
  return (
    <Popover
      trigger="click"
      title={props.title}
      content={<div style={{ maxWidth: 480, wordBreak: 'break-all' }}>{props.text}</div>}
    >
      <span style={{ display: 'inline-flex', alignItems: 'center', maxWidth: 210, gap: 4 }}>
        {label}
        <Button type="link" size="small" style={{ padding: 0, flex: 'none' }}>查看</Button>
      </span>
    </Popover>
  )
}

function poolNum(row: TopologyRow, value?: number) {
  if (!row.pool) return ''
  return value ?? 0
}

type PoolPayload = {
  pool?: boolean
  pool_current?: number
  pool_active?: number
  pool_idle?: number
  pool_pending?: number
  pool_max?: number
  pool_min?: number
  pool_active_max_7d?: number
  pool_pending_max_7d?: number
  pool_advice?: string
  pool_max_limit?: number
  idle_samples?: number
  samples?: number
}

function uniquePodNames(rows: TopologyRow[]) {
  const seen = new Set<string>()
  const out: string[] = []
  for (const row of rows) {
    for (const part of (row.pod || '').split(',')) {
      const name = part.trim()
      if (!name || seen.has(name)) continue
      seen.add(name)
      out.push(name)
    }
  }
  return out
}

function joinNote(a: string, b: string) {
  const left = a.trim()
  const right = b.trim()
  if (!left) return right
  if (!right || left === right) return left
  return `${left}；${right}`
}

function poolAdvice(u: {
  active: number
  idle: number
  current: number
  max: number
  activeMax: number
  pendingMax: number
  maxLimit: number
  idleSamples: number
  samples: number
}) {
  const limit = u.maxLimit > 0 ? u.maxLimit : u.max
  if (u.pendingMax > 0 && limit > 0 && u.activeMax >= limit) {
    return '这 7 天里池曾打满，业务线程等过连接。先查慢 SQL、未归还连接和长事务；确认数据库还能加连接后，再提高 maximumPoolSize。'
  }
  if (u.pendingMax > 0) {
    return '这 7 天有线程等过连接，池却没满。检查建连是否变慢、获取超时，以及数据库是否拒绝新连接。'
  }
  if (limit > 0 && u.activeMax * 5 >= limit * 4) {
    return '这 7 天余量不足。突发流量时容易排队，先看慢 SQL 和事务时长。'
  }
  if (u.samples > 0 && u.idleSamples * 10 >= u.samples * 9) {
    return '最近 7 天连接基本一直空闲，没有请求占用连接。这些空闲连接仍占用 PostgreSQL 连接名额。若该 Pod 应该在处理流量，先确认请求有没有打到它；若业务就是低峰，并且最小空闲等于最大连接，空闲连接不会回收，可把 minimumIdle 调低。'
  }
  if (u.active > 0 && u.idle === 0 && u.current < u.max) {
    return '此刻空闲连接已用完，池还会继续建连。关注是否很快顶到最大。'
  }
  return `最近 7 天活跃峰值 ${u.activeMax}、等待峰值 ${u.pendingMax}，未接近上限。`
}

function mergePoolRow(row: TopologyRow, byPod: Record<string, PoolPayload>): TopologyRow {
  const names = (row.pod || '').split(',').map((part) => part.trim()).filter(Boolean)
  if (!names.length) return row
  const picked = names.map((name) => byPod[name]).filter((item): item is PoolPayload => !!item?.pool)
  if (!picked.length) return row
  if (picked.length === 1 && names.length === 1) {
    const item = picked[0]
    return {
      ...row,
      pool: true,
      pool_current: item.pool_current,
      pool_active: item.pool_active,
      pool_idle: item.pool_idle,
      pool_pending: item.pool_pending,
      pool_max: item.pool_max,
      pool_min: item.pool_min,
      pool_active_max_7d: item.pool_active_max_7d,
      pool_pending_max_7d: item.pool_pending_max_7d,
      pool_advice: item.pool_advice || '',
    }
  }
  const sum = picked.reduce((acc, item) => ({
    active: acc.active + (item.pool_active || 0),
    idle: acc.idle + (item.pool_idle || 0),
    current: acc.current + (item.pool_current || 0),
    pending: acc.pending + (item.pool_pending || 0),
    max: acc.max + (item.pool_max || 0),
    min: acc.min + (item.pool_min || 0),
    activeMax: acc.activeMax + (item.pool_active_max_7d || 0),
    pendingMax: acc.pendingMax + (item.pool_pending_max_7d || 0),
    maxLimit: acc.maxLimit + (item.pool_max_limit || 0),
    idleSamples: acc.idleSamples + (item.idle_samples || 0),
    samples: acc.samples + (item.samples || 0),
  }), {
    active: 0, idle: 0, current: 0, pending: 0, max: 0, min: 0, activeMax: 0, pendingMax: 0, maxLimit: 0, idleSamples: 0, samples: 0,
  })
  return {
    ...row,
    pool: true,
    pool_current: sum.current,
    pool_active: sum.active,
    pool_idle: sum.idle,
    pool_pending: sum.pending,
    pool_max: sum.max,
    pool_min: sum.min,
    pool_active_max_7d: sum.activeMax,
    pool_pending_max_7d: sum.pendingMax,
    pool_advice: poolAdvice(sum),
  }
}

function poolPending(row: TopologyRow, loading: boolean) {
  return loading && !!row.pod && !row.pool
}

function poolColumns(loading: boolean): ColumnsType<TopologyRow> {
  const num = (title: string, dataIndex: keyof TopologyRow, width = 72): ColumnsType<TopologyRow>[number] => ({
    title,
    dataIndex,
    width,
    render: (value: number, row) => (poolPending(row, loading) ? '查询中' : poolNum(row, value)),
  })
  return [
    num('当前', 'pool_current'),
    num('活跃', 'pool_active'),
    num('空闲', 'pool_idle'),
    num('等待', 'pool_pending'),
    num('最大', 'pool_max'),
    num('最小', 'pool_min'),
    num('7日活跃峰值', 'pool_active_max_7d', 110),
    num('7日等待峰值', 'pool_pending_max_7d', 110),
    {
      title: '分析',
      dataIndex: 'pool_advice',
      width: 96,
      render: (value: string, row) => {
        if (poolPending(row, loading)) return '查询中'
        if (!row.pool || !value) return ''
        return (
          <Popover
            trigger="click"
            title="分析"
            content={<div style={{ maxWidth: 360 }}>{value}</div>}
          >
            <Button type="link" size="small" style={{ padding: 0 }}>查看分析</Button>
          </Popover>
        )
      },
    },
  ]
}

function DatabaseFilter(props: {
  names: string[]
  options: { label: string; value: string }[]
  onChange: (names: string[]) => void
}) {
  const [keyword, setKeyword] = useState('')
  const text = keyword.trim().toLowerCase()
  const matched = text
    ? props.options.filter((item) => item.label.toLowerCase().includes(text))
    : props.options
  const toggle = (name: string, checked: boolean) => {
    props.onChange(checked ? [...props.names, name] : props.names.filter((item) => item !== name))
  }
  const selectMatched = () => {
    const selected = new Set(props.names)
    for (const item of matched) selected.add(item.value)
    props.onChange([...selected])
  }
  const invertMatched = () => {
    const matchedSet = new Set(matched.map((item) => item.value))
    const selected = new Set(props.names)
    const next = props.names.filter((name) => !matchedSet.has(name))
    for (const item of matched) {
      if (!selected.has(item.value)) next.push(item.value)
    }
    props.onChange(next)
  }
  return (
    <div style={{ padding: 8, width: 280 }} onKeyDown={(e) => e.stopPropagation()} onMouseDown={(e) => e.stopPropagation()}>
      <Input
        allowClear
        autoFocus
        placeholder="搜索数据库"
        value={keyword}
        prefix={<SearchOutlined />}
        onChange={(e) => setKeyword(e.target.value)}
      />
      <div style={{ display: 'flex', alignItems: 'center', marginTop: 4 }}>
        <span style={{ color: '#8c8c8c', marginRight: 'auto' }}>已选 {props.names.length}</span>
        <Button type="link" size="small" disabled={!matched.length} onClick={selectMatched}>全选</Button>
        <Button type="link" size="small" disabled={!matched.length} onClick={invertMatched}>反选</Button>
        <Button type="link" size="small" disabled={!props.names.length} onClick={() => props.onChange([])}>清空</Button>
      </div>
      <div style={{ maxHeight: 260, overflow: 'auto', marginTop: 4 }}>
        {matched.length ? matched.map((item) => (
          <label key={item.value} style={{ display: 'flex', gap: 8, alignItems: 'center', padding: '4px 0', cursor: 'pointer' }}>
            <Checkbox checked={props.names.includes(item.value)} onChange={(e) => toggle(item.value, e.target.checked)} />
            <span>{item.label}</span>
          </label>
        )) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的数据库" />}
      </div>
    </div>
  )
}

export default function TopologyTab(props: {
  instanceId: string
  refreshTick: number
  active: boolean
  onRows?: (rows: TopologyExportRow[]) => void
}) {
  const { instanceId, refreshTick, active } = props
  const [loading, setLoading] = useState(false)
  const [poolLoading, setPoolLoading] = useState(false)
  const [rows, setRows] = useState<TopologyRow[]>([])
  const [promNote, setPromNote] = useState('')
  const [queried, setQueried] = useState(false)
  const [error, setError] = useState('')
  const [dbNames, setDbNames] = useState<string[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [sortField, setSortField] = useState('connections')
  const [sortOrder, setSortOrder] = useState<'ascend' | 'descend' | null>('descend')

  useEffect(() => {
    if (!active) return
    if (!instanceId) {
      setRows([])
      setPromNote('')
      setError('')
      setQueried(false)
      setPoolLoading(false)
      return
    }
    if (refreshTick <= 0) return
    let cancelled = false
    setLoading(true)
    setPoolLoading(false)
    setError('')
    api.get('/sql-insight/topology', { params: { instance_id: instanceId }, timeout: 30000 })
      .then(({ data }) => {
        if (cancelled) return
        const list = Array.isArray(data?.data) ? data.data : []
        const note = String(data?.prom_note || '')
        setRows(list)
        setPromNote(note)
        setQueried(true)
        const pods = uniquePodNames(list)
        if (!pods.length || note === '未配置 Prometheus') return
        setPoolLoading(true)
        api.post('/sql-insight/topology/pools', { pods }, { timeout: 45000, silent: true })
          .then(({ data: poolData }) => {
            if (cancelled) return
            const byPod = poolData?.data && typeof poolData.data === 'object' ? poolData.data : {}
            setRows((prev) => prev.map((row) => mergePoolRow(row, byPod)))
            setPromNote((prev) => joinNote(prev, String(poolData?.prom_note || '')))
          })
          .catch((e: any) => {
            if (cancelled) return
            const msg = e?.response?.data?.message || e?.message || '连接池查询失败'
            setPromNote((prev) => joinNote(prev, String(msg)))
          })
          .finally(() => {
            if (!cancelled) setPoolLoading(false)
          })
      })
      .catch((e: any) => {
        if (cancelled) return
        setRows([])
        setPromNote('')
        setQueried(true)
        setError(e?.response?.data?.message || e?.message || '查询失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [active, instanceId, refreshTick])

  useEffect(() => {
    setDbNames([])
    setPage(1)
  }, [instanceId])

  const dbOptions = useMemo(() => {
    const names = new Set<string>()
    for (const row of rows) {
      const name = (row.database || '').trim()
      if (name) names.add(name)
    }
    return [...names].sort((a, b) => a.localeCompare(b)).map((name) => ({ label: name, value: name }))
  }, [rows])

  const shown = useMemo(() => {
    const list = !dbNames.length
      ? rows
      : rows.filter((row) => dbNames.includes((row.database || '').trim()))
    if (!sortOrder) return list
    const dir = sortOrder === 'ascend' ? 1 : -1
    return [...list].sort((a, b) => {
      if (sortField === 'connections') return ((a.connections || 0) - (b.connections || 0)) * dir
      if (sortField === 'database') return (a.database || '').localeCompare(b.database || '') * dir
      return 0
    })
  }, [rows, dbNames, sortField, sortOrder])

  const podText = (row: TopologyRow) => {
    if (row.pod) return row.pod
    if (row.local || !row.lookup) return ''
    return promNote || ''
  }

  const poolText = (row: TopologyRow, value?: number) => (row.pool ? String(value ?? 0) : '')

  useEffect(() => {
    props.onRows?.(shown.map((row) => ({
      host: row.local || !row.host ? '本地套接字' : row.host,
      database: row.database || '',
      connections: row.connections || 0,
      pod: podText(row),
      pool_current: poolText(row, row.pool_current),
      pool_active: poolText(row, row.pool_active),
      pool_idle: poolText(row, row.pool_idle),
      pool_pending: poolText(row, row.pool_pending),
      pool_max: poolText(row, row.pool_max),
      pool_min: poolText(row, row.pool_min),
      pool_active_max_7d: poolText(row, row.pool_active_max_7d),
      pool_pending_max_7d: poolText(row, row.pool_pending_max_7d),
      pool_advice: row.pool ? (row.pool_advice || '') : '',
    })))
  }, [shown, promNote, props.onRows])

  useEffect(() => () => props.onRows?.([]), [props.onRows])

  const columns: ColumnsType<TopologyRow> = [
    {
      title: '客户端地址',
      dataIndex: 'host',
      width: 180,
      render: (host: string, row) => (row.local || !host ? '本地套接字' : host),
    },
    {
      title: '数据库',
      dataIndex: 'database',
      width: 180,
      sorter: (a, b) => (a.database || '').localeCompare(b.database || ''),
      filteredValue: dbNames,
      filterIcon: (filtered) => <SearchOutlined style={{ color: filtered ? '#1677ff' : undefined }} />,
      filterDropdown: () => (
        <DatabaseFilter
          names={dbNames}
          options={dbOptions}
          onChange={(value) => {
            setDbNames(value)
            setPage(1)
          }}
        />
      ),
      render: (v: string) => v || '—',
    },
    {
      title: '连接数',
      dataIndex: 'connections',
      width: 100,
      defaultSortOrder: 'descend',
      sortDirections: ['descend', 'ascend'],
      sorter: (a, b) => (a.connections || 0) - (b.connections || 0),
    },
    {
      title: 'Pod',
      dataIndex: 'pod',
      width: 220,
      render: (pod: string, row) => {
        const text = pod || (row.local || !row.lookup ? '' : promNote || '')
        if (!text) return ''
        return <FoldedName text={text} title="Pod" />
      },
    },
    ...poolColumns(poolLoading),
  ]

  return (
    <div>
      {promNote ? <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={promNote} /> : null}
      {error ? <Alert type="error" showIcon style={{ marginBottom: 12 }} message={error} /> : null}
      <Table
        rowKey={(row, index) => `${row.host}-${row.database}-${index}`}
        size="small"
        loading={loading}
        columns={columns}
        dataSource={shown}
        onChange={(pag, _filters, sorter) => {
          setPage(pag.current || 1)
          setPageSize(pag.pageSize || 20)
          const current = Array.isArray(sorter) ? sorter[0] : sorter
          setSortField(String(current?.field || ''))
          setSortOrder(current?.order || null)
        }}
        pagination={{
          current: page,
          pageSize,
          showSizeChanger: true,
          showTotal: (n) => `共 ${n} 条`,
        }}
        locale={{
          emptyText: queried
            ? (rows.length && !shown.length
              ? <Empty description="没有匹配的数据库" />
              : <Empty description="没有客户端连接" />)
            : <Empty description="选择环境与实例后查询" />,
        }}
        scroll={{ x: 1440 }}
      />
    </div>
  )
}
