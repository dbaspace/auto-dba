import React, { useEffect, useState } from 'react'
import { Alert, Empty, Table } from 'antd'
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

export default function TopologyTab(props: {
  instanceId: string
  refreshTick: number
  active: boolean
}) {
  const { instanceId, refreshTick, active } = props
  const [loading, setLoading] = useState(false)
  const [rows, setRows] = useState<TopologyRow[]>([])
  const [promNote, setPromNote] = useState('')
  const [queried, setQueried] = useState(false)
  const [error, setError] = useState('')

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

  const columns: ColumnsType<TopologyRow> = [
    {
      title: '客户端地址',
      dataIndex: 'host',
      width: 180,
      render: (host: string, row) => (row.local || !host ? '本地套接字' : host),
    },
    { title: '数据库', dataIndex: 'database', width: 160, render: (v: string) => v || '—' },
    { title: '连接数', dataIndex: 'connections', width: 100 },
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
        dataSource={rows}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (n) => `共 ${n} 条` }}
        locale={{
          emptyText: queried
            ? <Empty description="最近 5 分钟没有客户端连接" />
            : <Empty description="选择环境与实例后查询" />,
        }}
        scroll={{ x: 980 }}
      />
    </div>
  )
}
