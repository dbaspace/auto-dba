import React, { useEffect, useState } from 'react'
import { Button, Select, Space, Table, message } from 'antd'
import api from '../../api/request'
import { listCIs } from './services/cmdbApi'

type MonitorCand = {
  endpoint: string
  host: string
  port: number
  domain?: string
  idc?: string
  job_name?: string
  matched_instance_id?: string
}

type LiveDB = { name?: string; system?: boolean; registered?: boolean }

export default function CmdbDiscover() {
  const [cands, setCands] = useState<MonitorCand[]>([])
  const [instances, setInstances] = useState<{ value: string; label: string }[]>([])
  const [instanceId, setInstanceId] = useState('')
  const [liveDBs, setLiveDBs] = useState<LiveDB[]>([])
  const [selectedDBs, setSelectedDBs] = useState<string[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    void (async () => {
      const list = await listCIs({ type: 'instance', limit: 500 })
      setInstances(list.map((i) => ({ value: i.id, label: `${i.name} (${i.id.slice(0, 8)})` })))
    })()
  }, [])

  async function loadMonitor() {
    setLoading(true)
    try {
      const { data } = await api.get('/cmdb/discover/from-monitor')
      setCands(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  async function loadLiveDBs(id: string) {
    setInstanceId(id)
    if (!id) {
      setLiveDBs([])
      return
    }
    const { data } = await api.get(`/instances/${id}/databases`)
    const rows = (data.data || []) as LiveDB[]
    setLiveDBs(rows)
    setSelectedDBs([])
  }

  async function registerDBs() {
    if (!instanceId || !selectedDBs.length) {
      message.warning('请选择实例与库')
      return
    }
    await api.post('/cmdb/discover/databases/register', { instance_id: instanceId, db_names: selectedDBs })
    message.success('已登记')
    await loadLiveDBs(instanceId)
  }

  return (
    <div className="ops-page">
      <h3>从监控发现候选</h3>
      <Button type="primary" loading={loading} onClick={() => void loadMonitor()} style={{ marginBottom: 12 }}>
        扫描监控目标
      </Button>
      <Table
        rowKey={(r) => `${r.host}:${r.port}:${r.endpoint}`}
        size="small"
        dataSource={cands}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: 'Endpoint', dataIndex: 'endpoint', ellipsis: true },
          { title: 'Host', dataIndex: 'host', width: 140 },
          { title: 'Port', dataIndex: 'port', width: 80 },
          { title: 'Domain', dataIndex: 'domain', width: 160 },
          { title: 'Job', dataIndex: 'job_name', width: 160 },
          {
            title: '已匹配实例',
            dataIndex: 'matched_instance_id',
            render: (v: string) => (v ? v.slice(0, 8) + '…' : '未登记'),
          },
        ]}
      />

      <h3 style={{ marginTop: 24 }}>Live 库发现并登记</h3>
      <Space style={{ marginBottom: 12 }}>
        <Select style={{ width: 360 }} showSearch optionFilterProp="label" options={instances} value={instanceId || undefined} onChange={(v) => void loadLiveDBs(v)} placeholder="选择实例" />
        <Button type="primary" disabled={!selectedDBs.length} onClick={() => void registerDBs()}>
          登记所选库
        </Button>
      </Space>
      <Table
        rowKey={(r) => String(r.name)}
        size="small"
        dataSource={liveDBs}
        rowSelection={{
          selectedRowKeys: selectedDBs,
          onChange: (keys) => setSelectedDBs(keys as string[]),
          getCheckboxProps: (r) => ({ disabled: !!r.registered }),
        }}
        columns={[
          { title: '库名', dataIndex: 'name' },
          { title: '已登记', dataIndex: 'registered', render: (v: boolean) => (v ? '是' : '否') },
        ]}
        pagination={{ pageSize: 15 }}
      />
    </div>
  )
}
