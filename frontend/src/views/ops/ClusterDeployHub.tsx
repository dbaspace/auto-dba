import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Col, Empty, Input, Row, Segmented, Space, Table, Tag, Typography, message } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import api from '../../api/request'
import './ClusterDeployHub.css'

type EngineCard = {
  id: string
  label: string
  family: string
  family_label: string
  capability: string
  capability_label: string
  desc: string
  path: string
  available: boolean
  allow_deploy: boolean
  allow_cloud: boolean
  agent_kind?: string
  agent_label?: string
  agent_port?: number
  agent_note?: string
}

type AgentInfo = {
  kind: string
  label: string
  port: number
  artifact?: string
  where?: string
  shared_with?: string
  used_by?: string
  note?: string
}

function capabilityColor(cap: string): string {
  switch (cap) {
    case 'full':
      return 'success'
    case 'register':
      return 'processing'
    default:
      return 'default'
  }
}

function agentTagColor(kind?: string): string {
  switch (kind) {
    case 'platform':
      return 'geekblue'
    case 'node':
      return 'purple'
    case 'db':
      return 'cyan'
    default:
      return 'default'
  }
}

function actionLabel(e: EngineCard): string {
  if (!e.available) return '暂不可用'
  if (e.capability === 'register') return '进入登记台'
  return '进入工作台'
}

export default function ClusterDeployHub() {
  const nav = useNavigate()
  const [engines, setEngines] = useState<EngineCard[]>([])
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [family, setFamily] = useState('all')
  const [q, setQ] = useState('')

  useEffect(() => {
    api.get('/db-deploy/engines')
      .then((res) => {
        setEngines(res.data?.data || [])
        setAgents(res.data?.agents || [])
        setWarnings(res.data?.warnings || [])
      })
      .catch((e) => message.error(e?.response?.data?.message || String(e)))
      .finally(() => setLoading(false))
  }, [])

  const familyOptions = useMemo(() => {
    const map = new Map<string, string>()
    engines.forEach((e) => {
      if (e.family) map.set(e.family, e.family_label || e.family)
    })
    return [
      { label: '全部', value: 'all' },
      ...Array.from(map.entries()).map(([value, label]) => ({ label, value })),
    ]
  }, [engines])

  const filtered = useMemo(() => {
    const kw = q.trim().toLowerCase()
    return engines.filter((e) => {
      if (family !== 'all' && e.family !== family) return false
      if (!kw) return true
      return [e.label, e.id, e.desc, e.capability_label, e.agent_label].some((s) => (s || '').toLowerCase().includes(kw))
    })
  }, [engines, family, q])

  const agentColumns = useMemo(
    () => [
      {
        title: 'Agent',
        dataIndex: 'label',
        width: 120,
        render: (v: string, r: AgentInfo) => (
          <Space size={4} wrap>
            <span>{v}</span>
            <Tag color={agentTagColor(r.kind)}>:{r.port}</Tag>
          </Space>
        ),
      },
      {
        title: '分合',
        dataIndex: 'shared_with',
        render: (v: string) => v || '-',
      },
      {
        title: '本 Hub 用途',
        dataIndex: 'used_by',
        render: (v: string) => v || '-',
      },
      {
        title: '登记 / 下发',
        dataIndex: 'where',
        render: (v: string, r: AgentInfo) => {
          if (r.kind === 'platform') {
            return (
              <span>
                <Link to="/config/agents">配置中心 → 平台 Agent</Link>
              </span>
            )
          }
          return v || '-'
        },
      },
    ],
    [],
  )

  return (
    <div className="cluster-deploy-hub">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="集群部署"
        description={
          <>
            按引擎进入差异化工作台。
            {' '}
            <Tag color="success">可部署</Tag>
            自建装机 + 登记；部分引擎另支持云入口；
            {' '}
            <Tag color="processing">仅登记</Tag>
            先纳管已有/云。含 HAProxy / Nginx / LVS（平台 Agent 装发行版包 + keepalived）。与
            {' '}
            <Link to="/ops/ha">高可用服务维护</Link>
            、
            {' '}
            <Link to="/ops/sr">SR 控制台</Link>
            职责不同。制品预置
            {' '}
            <Link to="/config/oss">配置中心 OSS</Link>
            。
          </>
        }
      />

      <Card
        size="small"
        className="cluster-deploy-agents"
        title="现场 Agent：哪些分开、哪些共用"
        style={{ marginBottom: 16 }}
      >
        <Typography.Paragraph type="secondary" style={{ marginBottom: 12 }}>
          三种 Agent 是<strong>独立进程、端口不可混用</strong>。平台 Agent 在备份 / HA / 接入层代理间
          <strong>共用一类</strong>；DB Agent 在 MySQL / PG / Redis 等引擎工作台间共用；Node Agent 仅 StarRocks。
          卡片上的标签标明该引擎走哪一种。
        </Typography.Paragraph>
        <Table
          size="small"
          rowKey="kind"
          pagination={false}
          loading={loading && agents.length === 0}
          dataSource={agents}
          columns={agentColumns as any}
        />
        {agents.length === 0 && !loading && (
          <Typography.Text type="secondary">暂无 Agent 元数据（请升级控制面）。</Typography.Text>
        )}
      </Card>

      {warnings.length > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="注册表与实例类型交叉告警"
          description={(
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {warnings.map((w) => <li key={w}>{w}</li>)}
            </ul>
          )}
        />
      )}
      <Space wrap style={{ marginBottom: 16 }} size="middle">
        <Segmented value={family} onChange={(v) => setFamily(String(v))} options={familyOptions} />
        <Input.Search allowClear placeholder="搜索引擎 / Agent" style={{ width: 240 }} value={q} onChange={(e) => setQ(e.target.value)} />
      </Space>
      {!loading && filtered.length === 0 ? (
        <Empty description="无匹配引擎" />
      ) : (
        <Row gutter={[16, 16]}>
          {filtered.map((e) => (
            <Col key={e.id} xs={24} sm={12} lg={8} xl={6}>
              <Card
                loading={loading}
                className={e.available ? 'engine-card' : 'engine-card engine-card-disabled'}
                title={(
                  <span className="engine-card-title">
                    {e.label}
                    {' '}
                    <Tag color={capabilityColor(e.capability)}>{e.capability_label || e.capability}</Tag>
                  </span>
                )}
              >
                <p className="engine-family">{e.family_label || e.family}</p>
                <p className="engine-desc">{e.desc}</p>
                <Space wrap size="small" style={{ marginBottom: 12 }}>
                  {e.agent_label && (
                    <Tag color={agentTagColor(e.agent_kind)} title={e.agent_note}>
                      {e.agent_label}
                      {e.agent_port ? ` :${e.agent_port}` : ''}
                    </Tag>
                  )}
                  {e.allow_deploy && <Tag>自建装机</Tag>}
                  <Tag>登记已有</Tag>
                  {e.allow_cloud && <Tag color="blue">云入口</Tag>}
                </Space>
                <Button type="primary" disabled={!e.available} block onClick={() => nav(e.path)}>
                  {actionLabel(e)}
                </Button>
              </Card>
            </Col>
          ))}
        </Row>
      )}
    </div>
  )
}
