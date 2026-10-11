import React, { useEffect, useState } from 'react'
import { Button, Card, Col, Row, Select, Space, Statistic, Table, Tag, message } from 'antd'
import api from '../../api/request'
import { listCIs, listDrifts, type CmdbDrift } from './services/cmdbApi'

type MonitorCand = {
  endpoint: string
  host: string
  port: number
  domain?: string
  idc?: string
  job_name?: string
  matched_instance_id?: string
  tenant_id?: string
}

type ClusterCand = {
  source: string
  source_id: string
  source_name?: string
  tenant_id?: string
  engine?: string
  host: string
  port?: number
  role?: string
  matched_instance_id?: string
  dedup_key: string
}

type LiveDB = { name?: string; system?: boolean; registered?: boolean }

type HealthOV = {
  monitor_unmatched: number
  instance_no_db: number
  database_no_approver: number
}

export default function CmdbDiscover() {
  const [cands, setCands] = useState<MonitorCand[]>([])
  const [clusterCands, setClusterCands] = useState<ClusterCand[]>([])
  const [drifts, setDrifts] = useState<CmdbDrift[]>([])
  const [health, setHealth] = useState<HealthOV | null>(null)
  const [instances, setInstances] = useState<{ value: string; label: string }[]>([])
  const [instanceId, setInstanceId] = useState('')
  const [liveDBs, setLiveDBs] = useState<LiveDB[]>([])
  const [selectedDBs, setSelectedDBs] = useState<string[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    void (async () => {
      const list = await listCIs({ type: 'instance', limit: 500 })
      setInstances(list.map((i) => ({ value: i.id, label: `${i.name} (${i.id.slice(0, 8)})` })))
      try {
        const { data } = await api.get('/cmdb/discover/health')
        setHealth(data.data || null)
      } catch {
        /* optional */
      }
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

  async function loadClusters() {
    setLoading(true)
    try {
      const { data } = await api.get('/cmdb/discover/from-clusters')
      setClusterCands(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  async function loadDrifts() {
    setLoading(true)
    try {
      setDrifts(await listDrifts())
    } catch (e: any) {
      message.error(e?.response?.data?.message || '漂移加载失败')
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
      {health && (
        <Row gutter={12} style={{ marginBottom: 16 }}>
          <Col span={8}>
            <Card size="small">
              <Statistic title="监控有、CMDB 无" value={health.monitor_unmatched} />
            </Card>
          </Col>
          <Col span={8}>
            <Card size="small">
              <Statistic title="有实例无库登记" value={health.instance_no_db} />
            </Card>
          </Col>
          <Col span={8}>
            <Card size="small">
              <Statistic title="有库无审批人" value={health.database_no_approver} />
            </Card>
          </Col>
        </Row>
      )}

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
            render: (v: string) => (v ? <Tag color="green">{v.slice(0, 8)}…</Tag> : <Tag color="warning">未登记</Tag>),
          },
        ]}
      />

      <h3 style={{ marginTop: 24 }}>从集群 / HA 发现候选</h3>
      <Button loading={loading} onClick={() => void loadClusters()} style={{ marginBottom: 12 }}>
        扫描 SR / 部署 / HA
      </Button>
      <Table
        rowKey="dedup_key"
        size="small"
        dataSource={clusterCands}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '来源', dataIndex: 'source', width: 140 },
          { title: '名称', dataIndex: 'source_name', ellipsis: true },
          { title: '引擎', dataIndex: 'engine', width: 100 },
          { title: 'Host', dataIndex: 'host', width: 140 },
          { title: 'Port', dataIndex: 'port', width: 80 },
          { title: '角色', dataIndex: 'role', width: 100 },
          {
            title: '匹配',
            dataIndex: 'matched_instance_id',
            render: (v: string) => (v ? <Tag color="green">已登记</Tag> : <Tag color="warning">未登记</Tag>),
          },
        ]}
      />

      <h3 style={{ marginTop: 24 }}>Live 库发现并登记</h3>
      <Space style={{ marginBottom: 12 }}>
        <Select style={{ width: 360 }} showSearch optionFilterProp="label" options={instances} value={instanceId || undefined} onChange={(v) => void loadLiveDBs(v)} placeholder="选择实例（本租户）" />
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
          getCheckboxProps: (r) => ({ disabled: !!r.registered || !!r.system }),
        }}
        columns={[
          { title: '库名', dataIndex: 'name' },
          {
            title: '状态',
            render: (_: unknown, r: LiveDB) => (r.registered ? <Tag color="green">已登记</Tag> : r.system ? <Tag>系统库</Tag> : <Tag>未登记</Tag>),
          },
        ]}
        pagination={{ pageSize: 10 }}
      />

      <h3 style={{ marginTop: 24 }}>配置漂移（相对基线）</h3>
      <Button loading={loading} onClick={() => void loadDrifts()} style={{ marginBottom: 12 }}>
        扫描漂移
      </Button>
      <Table
        rowKey={(r) => `${r.ci_type}:${r.ci_id}:${r.field}`}
        size="small"
        dataSource={drifts}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '类型', dataIndex: 'ci_type', width: 100 },
          { title: '名称', dataIndex: 'name', width: 160 },
          { title: '字段', dataIndex: 'field', width: 120 },
          {
            title: '基线',
            dataIndex: 'baseline',
            ellipsis: true,
            render: (v: unknown) => JSON.stringify(v),
          },
          {
            title: '当前',
            dataIndex: 'current',
            ellipsis: true,
            render: (v: unknown) => JSON.stringify(v),
          },
        ]}
      />
    </div>
  )
}
