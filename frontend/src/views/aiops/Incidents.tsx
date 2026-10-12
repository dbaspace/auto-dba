import React, { useCallback, useEffect, useState } from 'react'
import { Select, Space, Table, Tag, Typography } from 'antd'
import api from '../../api/request'

type Incident = {
  key: string
  instance_id?: string
  name?: string
  host?: string
  alert_count: number
  max_severity: string
  summary: string
  alerts: { id: string; title: string; severity: string; alertname?: string }[]
  related_todos?: { id: string; title: string }[]
  related_drifts?: { field: string; name?: string }[]
}

const SEV: Record<string, string> = { P0: 'red', P1: 'orange', P2: 'blue', P3: 'default' }

export default function AIOpsIncidents() {
  const [window, setWindow] = useState('30m')
  const [rows, setRows] = useState<Incident[]>([])
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/aiops/incidents', { params: { window } })
      setRows(res.data?.data?.items || [])
    } finally {
      setLoading(false)
    }
  }, [window])

  useEffect(() => { void load() }, [load])

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>AIOps 告警关联</Typography.Title>
        <Select
          value={window}
          style={{ width: 120 }}
          onChange={setWindow}
          options={[
            { value: '15m', label: '15 分钟' },
            { value: '30m', label: '30 分钟' },
            { value: '1h', label: '1 小时' },
            { value: '6h', label: '6 小时' },
          ]}
        />
      </Space>
      <Table
        rowKey="key"
        loading={loading}
        dataSource={rows}
        pagination={{ pageSize: 20 }}
        expandable={{
          expandedRowRender: (r) => (
            <div>
              {(r.alerts || []).map((a) => (
                <div key={a.id}><Tag color={SEV[a.severity]}>{a.severity}</Tag> {a.title}</div>
              ))}
              {(r.related_todos?.length ?? 0) > 0 && (
                <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
                  待办：{r.related_todos!.map((t) => t.title).join('；')}
                </Typography.Paragraph>
              )}
              {(r.related_drifts?.length ?? 0) > 0 && (
                <Typography.Paragraph type="secondary">
                  漂移：{r.related_drifts!.map((d) => d.field).join(', ')}
                </Typography.Paragraph>
              )}
            </div>
          ),
        }}
        columns={[
          {
            title: '级别', dataIndex: 'max_severity', width: 80,
            render: (v: string) => <Tag color={SEV[v]}>{v}</Tag>,
          },
          { title: '摘要', dataIndex: 'summary' },
          { title: '实例', dataIndex: 'name', width: 180, render: (v: string, r) => v || r.instance_id || r.host || '—' },
          { title: '告警数', dataIndex: 'alert_count', width: 90 },
        ]}
      />
    </div>
  )
}
