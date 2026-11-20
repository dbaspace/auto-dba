import React, { useEffect, useState } from 'react'
import { Button, Select, Space, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

interface Mail {
  id: string
  sent_at: string
  env_label: string
  tenant_label: string
  recipients: string
  subject: string
  ok: boolean
  error: string
  html?: string
}

export default function SlowSqlReport() {
  const [rows, setRows] = useState<Mail[]>([])
  const [envs, setEnvs] = useState<any[]>([])
  const [env, setEnv] = useState('')
  const [loading, setLoading] = useState(false)

  async function load(name = env) {
    setLoading(true)
    try {
      const { data } = await api.get('/sql-insight/slow-reports/mails', { params: name ? { env: name } : {} })
      setRows(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载周报失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load('')
    api.get('/environments').then(({ data }) => setEnvs(data.data || [])).catch(() => {})
  }, [])

  function open(id: string) {
    const url = `${window.location.origin}${window.location.pathname}#/ops/slow-sql-report/${id}`
    window.open(url, '_blank')
  }

  const columns: ColumnsType<Mail> = [
    { title: '时间', dataIndex: 'sent_at', width: 200 },
    { title: '环境', dataIndex: 'env_label', ellipsis: true },
    { title: '业务线', dataIndex: 'tenant_label', width: 160, ellipsis: true },
    { title: '收件人', dataIndex: 'recipients', ellipsis: true },
    { title: '结果', dataIndex: 'ok', width: 90, render: (v, row) => (v ? <Tag color="green">成功</Tag> : <Tag color="red">{row.error ? '失败' : '未发送'}</Tag>) },
    { title: '操作', width: 90, render: (_, row) => <Button type="link" onClick={() => open(row.id)}>查看</Button> },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 12 }}>
        <Select allowClear placeholder="按环境筛选" style={{ width: 220 }} value={env || undefined}
          onChange={(v) => { const name = v || ''; setEnv(name); load(name) }}
          options={envs.map((e) => ({ value: e.name || e.code, label: e.name || e.code }))} />
        <Link to="/config/sql-insight?tab=slow">周报设置</Link>
      </Space>
      <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={rows} pagination={{ pageSize: 10, showSizeChanger: true, pageSizeOptions: ['10', '20', '30', '50', '100'] }} />
    </div>
  )
}
