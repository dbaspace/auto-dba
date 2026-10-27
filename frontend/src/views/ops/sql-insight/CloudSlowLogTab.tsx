import React, { useEffect, useState } from 'react'
import { Button, Input, Space, Table, message } from 'antd'
import { Link } from 'react-router-dom'
import api from '../../../api/request'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'
import type { SnapshotFilter } from './types'

export default function CloudSlowLogTab({ filter, active }: { filter: SnapshotFilter; active: boolean }) {
  const [rows, setRows] = useState<any[]>([])
  const [cursor, setCursor] = useState<any>(null)
  const [note, setNote] = useState('')
  const [db, setDb] = useState('')
  const [q, setQ] = useState('')
  const [minMs, setMinMs] = useState('')

  async function load() {
    if (!filter.instanceId) {
      setRows([])
      setNote('请先选择 PostgreSQL 实例')
      return
    }
    const { data } = await api.get('/cloud/slow-logs', {
      params: { instance_id: filter.instanceId, from: filter.startTime, to: filter.endTime, db, q, min_ms: minMs || undefined },
    })
    setRows(data.data || [])
    setCursor(data.cursor || null)
    setNote(data.message || '')
  }

  useEffect(() => {
    if (active) void load().catch((e) => message.error(e?.response?.data?.message || '加载失败'))
  }, [active, filter.instanceId, filter.startTime, filter.endTime])

  return (
    <div>
      <Space style={{ marginBottom: 12 }} wrap>
        <span>上次同步 {cursor?.cursor_at || '—'} {cursor?.status || ''} {cursor?.error || ''}</span>
        <Input placeholder="库名" value={db} onChange={(e) => setDb(e.target.value)} style={{ width: 120 }} />
        <Input placeholder="SQL" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 180 }} />
        <Input placeholder="最小耗时 ms" value={minMs} onChange={(e) => setMinMs(e.target.value)} style={{ width: 120 }} />
        <Button onClick={() => void load()}>查询</Button>
        <Auth urn={Perm.sqlInsight.cloudSlowSync}>
          <Button type="primary" onClick={() => api.post('/cloud/slow-logs/sync', { instance_id: filter.instanceId || '' }).then(() => message.success('已入队')).catch((e) => message.error(e?.response?.data?.message || '同步失败'))}>立即同步</Button>
        </Auth>
        <Link to="/config/sql-insight?tab=cloud">同步设置</Link>
      </Space>
      {note && <p>{note}</p>}
      <Table
        rowKey="id"
        dataSource={rows}
        size="small"
        columns={[
          { title: '时间', dataIndex: 'log_time', width: 220 },
          { title: '库', dataIndex: 'db_name', width: 120 },
          { title: '用户', dataIndex: 'db_user', width: 100 },
          { title: '耗时 ms', dataIndex: 'duration_ms', width: 100 },
          { title: 'SQL', dataIndex: 'sql_text' },
        ]}
      />
    </div>
  )
}
