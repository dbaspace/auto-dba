import React, { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Table, Tag } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import './archive.css'

const STATUS_TEXT: Record<string, string> = {
  running: '执行中',
  executed: '成功',
  success: '成功',
  failed: '失败',
  paused: '已停用',
  init: '待执行',
}

export default function RunHistory() {
  const navigate = useNavigate()
  const [runs, setRuns] = useState<any[]>([])
  const [loading, setLoading] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/archive/runs')
      const list = data.data || []
      if (list.length) {
        setRuns(
          list.map((r: any) => ({
            ...r,
            name: r.task_name || r.name,
            volume: r.volume || `${r.deleted_rows || 0} 行`,
          })),
        )
      } else {
        const st = (await api.get('/archive/stats')).data.data || {}
        setRuns(st.recent_runs || [])
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const columns: ColumnsType<any> = [
    { title: '任务名称', dataIndex: 'name', minWidth: 160 },
    {
      title: '状态',
      width: 100,
      render: (_, row) => (
        <Tag
          color={
            row.status === 'failed' ? 'error' : row.status === 'running' ? 'warning' : 'success'
          }
        >
          {STATUS_TEXT[row.status] || row.status}
        </Tag>
      ),
    },
    { title: '源库', dataIndex: 'source_db', width: 140 },
    { title: '表', dataIndex: 'table_name', width: 140 },
    { title: '归档量', dataIndex: 'volume', width: 90 },
    { title: '耗时', dataIndex: 'duration', width: 90 },
    { title: '创建时间', dataIndex: 'created_at', minWidth: 170 },
    { title: '执行时间', dataIndex: 'started_at', minWidth: 170 },
  ]

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <div className="page-head">
          <div className="toolbar" style={{ margin: 0 }}>
            <Button onClick={() => navigate('/ops/archive/dashboard')}>返回控制台</Button>
            <Button onClick={load}>刷新</Button>
          </div>
        </div>
        <Table
          rowKey={(r) => r.id ?? r.name ?? Math.random()}
          dataSource={runs}
          columns={columns}
          loading={loading}
          bordered
          pagination={false}
          scroll={{ x: true }}
          locale={{ emptyText: '暂无执行记录' }}
        />
      </div>
    </div>
  )
}
