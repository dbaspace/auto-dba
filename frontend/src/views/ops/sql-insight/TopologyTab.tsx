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

function poolColumns(): ColumnsType<TopologyRow> {
  const num = (title: string, dataIndex: keyof TopologyRow, width = 72): ColumnsType<TopologyRow>[number] => ({
    title,
    dataIndex,
    width,
    render: (value: number, row) => poolNum(row, value),
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
      return
    }
    if (refreshTick <= 0) return
    let cancelled = false
    setLoading(true)
    setError('')
    api.get('/sql-insight/topology', { params: { instance_id: instanceId }, timeout: 90000 })
      .then(({ data }) => {
        if (cancelled) return
        setRows(Array.isArray(data?.data) ? data.data : [])
        setPromNote(String(data?.prom_note || ''))
        setQueried(true)
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
    ...poolColumns(),
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
