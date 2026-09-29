import React, { useEffect, useState } from 'react'
import { Button, Select, Space, Tag } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './backup.css'

const statusColor: Record<string, string> = {
  success: 'success',
  failed: 'error',
  running: 'processing',
  pending: 'default',
}

function formatBytes(n: number) {
  if (n == null || !Number.isFinite(n) || n <= 0) return '-'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`
}

export default function BackupRuns() {
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [status, setStatus] = useState<string | undefined>()
  const [detail, setDetail] = useState<any>(null)

  async function load() {
    setLoading(true)
    try {
      const params: any = { limit: 200 }
      if (status) params.status = status
      const { data } = await api.get('/backup/runs', { params })
      setRows(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [status])

  async function showDetail(row: any) {
    const { data } = await api.get(`/backup/runs/${row.id}`)
    setDetail(data.data)
  }

  const columns: ColumnsType<any> = [
    { title: '策略', dataIndex: 'policy_name', minWidth: 140, ellipsis: true },
    {
      title: '状态',
      width: 100,
      render: (_, r) => <Tag color={statusColor[r.status] || 'default'}>{r.status}</Tag>,
    },
    { title: '触发', dataIndex: 'trigger', width: 80 },
    {
      title: '方式',
      dataIndex: 'method',
      width: 100,
      render: (v) => v || '-',
    },
    { title: '引擎', dataIndex: 'engine', width: 90 },
    { title: '大小', width: 100, render: (_, r) => formatBytes(r.size_bytes) },
    { title: '开始', dataIndex: 'started_at', width: 180, ellipsis: true },
    { title: '结束', dataIndex: 'finished_at', width: 180, ellipsis: true },
    {
      title: '操作',
      width: 90,
      render: (_, r) => (
        <Button type="link" size="small" onClick={() => showDetail(r)}>
          详情
        </Button>
      ),
    },
  ]

  return (
    <div className="backup-page">
      <div className="backup-toolbar">
        <h2>执行记录</h2>
        <Space>
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 140 }}
            value={status}
            onChange={setStatus}
            options={['pending', 'running', 'success', 'failed'].map((s) => ({ value: s, label: s }))}
          />
          <Button onClick={load}>刷新</Button>
        </Space>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={rows} pagination={{ pageSize: 20 }} />
      {detail && (
        <div className="backup-detail">
          <div className="backup-toolbar">
            <h3>运行详情</h3>
            <Button type="link" onClick={() => setDetail(null)}>
              关闭
            </Button>
          </div>
          <pre className="backup-pre">
            {JSON.stringify(
              {
                id: detail.id,
                status: detail.status,
                size_bytes: detail.size_bytes,
                checksum: detail.checksum,
                object_key: detail.object_key,
                local_path: detail.local_path,
                error_message: detail.error_message,
                log_summary: detail.log_summary,
              },
              null,
              2,
            )}
          </pre>
        </div>
      )}
    </div>
  )
}
