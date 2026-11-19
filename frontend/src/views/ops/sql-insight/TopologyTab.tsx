import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Checkbox, Empty, Input, Table } from 'antd'
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
}

export type TopologyExportRow = {
  host: string
  database: string
  connections: number
  pod: string
  namespace: string
  node: string
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
    api.get('/sql-insight/topology', { params: { instance_id: instanceId }, timeout: 60000 })
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

  useEffect(() => {
    props.onRows?.(shown.map((row) => ({
      host: row.local || !row.host ? '本地套接字' : row.host,
      database: row.database || '',
      connections: row.connections || 0,
      pod: podText(row),
      namespace: row.namespace || '',
      node: row.node || '',
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
      render: (pod: string, row) => {
        if (pod) return pod
        if (row.local || !row.lookup) return ''
        return promNote || ''
      },
    },
    { title: '命名空间', dataIndex: 'namespace', width: 160, render: (v: string) => v || '' },
    { title: '节点', dataIndex: 'node', width: 220, render: (v: string) => v || '' },
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
        scroll={{ x: 980 }}
      />
    </div>
  )
}
