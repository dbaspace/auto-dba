import React, { useEffect, useState } from 'react'
import { Button, Input, Space, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

type AuditRow = {
  id: string
  actor: string
  action: string
  ci_type: string
  ci_id: string
  source: string
  created_at: string
  before_json?: unknown
  after_json?: unknown
}

export default function CmdbAudit() {
  const [rows, setRows] = useState<AuditRow[]>([])
  const [q, setQ] = useState('')
  const [ciType, setCiType] = useState('')
  const [loading, setLoading] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/cmdb/audit', { params: { q: q || undefined, ci_type: ciType || undefined, limit: 100 } })
      setRows(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  return (
    <div className="ops-page">
      <Space style={{ marginBottom: 12 }}>
        <Input placeholder="CI 类型" style={{ width: 140 }} value={ciType} onChange={(e) => setCiType(e.target.value)} />
        <Input.Search placeholder="搜索" style={{ width: 240 }} value={q} onChange={(e) => setQ(e.target.value)} onSearch={() => void load()} />
        <Button type="primary" loading={loading} onClick={() => void load()}>
          查询
        </Button>
      </Space>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        expandable={{
          expandedRowRender: (r: AuditRow) => (
            <pre style={{ margin: 0, fontSize: 12, whiteSpace: 'pre-wrap' }}>
              before: {JSON.stringify(r.before_json, null, 2)}
              {'\n'}
              after: {JSON.stringify(r.after_json, null, 2)}
            </pre>
          ),
        }}
        columns={[
          { title: '时间', dataIndex: 'created_at', width: 180 },
          { title: '操作人', dataIndex: 'actor', width: 120 },
          { title: '动作', dataIndex: 'action', width: 90 },
          { title: '类型', dataIndex: 'ci_type', width: 110 },
          { title: 'CI ID', dataIndex: 'ci_id', ellipsis: true },
          { title: '来源', dataIndex: 'source', width: 90 },
        ]}
      />
    </div>
  )
}
