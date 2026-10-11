import React, { useCallback, useEffect, useState } from 'react'
import {
  Button, Card, Col, Descriptions, Drawer, Input, Row, Select, Space, Statistic, Tabs, Tag,
  Timeline, Tooltip, Typography, message,
} from 'antd'
import dayjs from 'dayjs'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'
import {
  AlertEvent, AlertItem, AlertStats, HANDLE_LABEL, SEV_COLOR, durationText,
} from './types'
import { getImpact, type CmdbImpact } from '../cmdb/services/cmdbApi'

const rowBorder: Record<string, string> = {
  P0: '#dc2626', P1: '#ea580c', P2: '#2563eb', P3: '#94a3b8',
}

export default function ActiveAlerts() {
  const { hasUrn } = usePermission()
  const [tab, setTab] = useState('active')
  const [severity, setSeverity] = useState<string>()
  const [keyword, setKeyword] = useState('')
  const [rows, setRows] = useState<AlertItem[]>([])
  const [stats, setStats] = useState<AlertStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<{ alert: AlertItem; events: AlertEvent[] } | null>(null)
  const [cmdbImpact, setCmdbImpact] = useState<CmdbImpact | null>(null)
  const [cmdbLoading, setCmdbLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [listRes, stRes] = await Promise.all([
        api.get('/alerts', { params: { tab, severity, keyword: keyword || undefined } }),
        api.get('/alerts/stats'),
      ])
      setRows(listRes.data?.data || [])
      setStats(stRes.data?.data || null)
    } finally {
      setLoading(false)
    }
  }, [tab, severity, keyword])

  useEffect(() => { void load() }, [load])

  const openDetail = async (id: string) => {
    const res = await api.get(`/alerts/${id}`)
    setDetail(res.data?.data || null)
    setCmdbImpact(null)
  }

  const loadCmdbImpact = async () => {
    const a = detail?.alert
    if (!a) return
    const labels = a.labels || {}
    const instanceID = labels.instance_id || labels.instanceId || ''
    const ciType = labels.ci_type || (instanceID ? 'instance' : '')
    const ciID = instanceID || labels.ci_id || ''
    if (!ciType || !ciID) {
      message.warning('告警标签中无 instance_id / ci_id，无法查 CMDB 影响')
      return
    }
    setCmdbLoading(true)
    try {
      setCmdbImpact(await getImpact(ciType, ciID, 'down', 2))
    } catch (e: any) {
      message.error(e?.response?.data?.message || 'CMDB 影响分析失败')
    } finally {
      setCmdbLoading(false)
    }
  }

  const act = async (path: string, ok: string, body?: object) => {
    const id = detail?.alert.id
    if (!id) return
    await api.post(`/alerts/${id}/${path}`, body || {})
    message.success(ok)
    await openDetail(id)
    await load()
  }

  const columns = [
    {
      title: '级别', dataIndex: 'severity', width: 70,
      render: (v: string) => <Tag color={SEV_COLOR[v]}>{v}</Tag>,
    },
    {
      title: '告警内容', key: 'title',
      render: (_: unknown, a: AlertItem) => (
        <div>
          <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
            {a.status === 'firing' && <span style={{ width: 7, height: 7, borderRadius: '50%', background: '#ef4444', display: 'inline-block' }} />}
            {a.title}
          </div>
          <div style={{ marginTop: 4 }}>
            {a.service && <Tag>{a.service}</Tag>}
            {a.cluster && <Tag>{a.cluster}</Tag>}
            {a.aggregate_count > 1 && <Typography.Text type="warning">聚合 {a.aggregate_count}</Typography.Text>}
          </div>
        </div>
      ),
    },
    {
      title: '当前值 / 阈值', width: 140,
      render: (_: unknown, a: AlertItem) => (
        <div>
          <Typography.Text type="danger" style={{ fontFamily: 'monospace', fontWeight: 700 }}>{a.cur_value || '—'}</Typography.Text>
          <div style={{ fontSize: 12, color: '#94a3b8' }}>阈值 {a.threshold || '—'}</div>
        </div>
      ),
    },
    { title: '持续', width: 90, render: (_: unknown, a: AlertItem) => durationText(a.started_at) },
    {
      title: '处理状态', width: 110, dataIndex: 'handle',
      render: (v: string) => {
        const h = HANDLE_LABEL[v] || { label: v, color: 'default' }
        return <Tag color={h.color}>{h.label}</Tag>
      },
    },
    { title: '负责人', width: 110, dataIndex: 'owner', render: (v: string) => v || <Typography.Text type="secondary">待认领</Typography.Text> },
    {
      title: '操作', width: 160,
      render: (_: unknown, a: AlertItem) => (
        <Space size={0} onClick={(e) => e.stopPropagation()}>
          {hasUrn(Perm.alert.claim) && a.handle === 'unclaimed' && a.status !== 'resolved' && (
            <Button type="link" size="small" onClick={async () => {
              await api.post(`/alerts/${a.id}/claim`)
              message.success('已认领')
              await load()
            }}>认领</Button>
          )}
          <Button type="link" size="small" onClick={() => void openDetail(a.id)}>详情</Button>
        </Space>
      ),
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>活跃告警</Typography.Title>
      <Row gutter={12} style={{ marginBottom: 16 }}>
        <Col xs={12} md={6}><Card size="small"><Statistic title="活跃告警" value={stats?.active ?? 0} suffix="条" valueStyle={{ color: '#dc2626' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="未认领" value={stats?.unclaimed ?? 0} suffix="条" valueStyle={{ color: '#f59e0b' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="P0 紧急" value={stats?.p0 ?? 0} suffix="条" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="今日 MTTA" value={stats?.mtta || '—'} /></Card></Col>
      </Row>

      <Card size="small">
        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            { key: 'active', label: '活跃告警' },
            { key: 'unclaimed', label: '未认领' },
            { key: 'claimed', label: '已认领' },
            { key: 'silenced', label: '已静默' },
          ]}
        />
        <Space wrap style={{ marginBottom: 12 }}>
          <Select
            allowClear placeholder="全部级别" style={{ width: 120 }}
            options={['P0', 'P1', 'P2', 'P3'].map((v) => ({ value: v, label: v }))}
            value={severity} onChange={setSeverity}
          />
          <Input.Search
            allowClear placeholder="搜索告警 / 服务 / 集群" style={{ width: 260 }}
            onSearch={setKeyword} onChange={(e) => { if (!e.target.value) setKeyword('') }}
          />
          <Button onClick={() => void load()}>刷新</Button>
        </Space>
        <ResponsiveTable
          rowKey="id"
          loading={loading}
          dataSource={rows}
          columns={columns}
          pagination={{ pageSize: 20 }}
          onRow={(a) => ({
            onClick: () => void openDetail(a.id),
            style: { cursor: 'pointer', boxShadow: `inset 3px 0 0 ${rowBorder[a.severity] || '#cbd5e1'}` },
          })}
        />
      </Card>

      <Drawer
        width={560}
        open={!!detail}
        onClose={() => setDetail(null)}
        title={detail ? (
          <Space>
            <Tag color={SEV_COLOR[detail.alert.severity]}>{detail.alert.severity}</Tag>
            <Tag color={HANDLE_LABEL[detail.alert.handle]?.color}>{HANDLE_LABEL[detail.alert.handle]?.label}</Tag>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>{detail.alert.id}</Typography.Text>
          </Space>
        ) : null}
        extra={detail && (
          <Space wrap>
            {hasUrn(Perm.alert.claim) && detail.alert.handle === 'unclaimed' && (
              <Button type="primary" onClick={() => void act('claim', '已认领')}>认领</Button>
            )}
            {hasUrn(Perm.alert.claim) && detail.alert.status !== 'resolved' && (
              <Button onClick={() => void act('processing', '已进入处理中')}>处理中</Button>
            )}
            {hasUrn(Perm.alert.silence) && detail.alert.status !== 'resolved' && (
              <Button onClick={() => void act('silence', '已静默 2 小时')}>静默</Button>
            )}
            {hasUrn(Perm.alert.resolve) && detail.alert.status !== 'resolved' && (
              <Tooltip title="标记误报并关闭">
                <Button danger onClick={() => void act('false-positive', '已标记误报')}>误报</Button>
              </Tooltip>
            )}
            {hasUrn(Perm.todo.create) && (
              <Button onClick={() => void act('todo', '已转待办')}>转待办</Button>
            )}
          </Space>
        )}
      >
        {detail && (
          <>
            <Typography.Title level={5}>{detail.alert.title}</Typography.Title>
            <Space wrap style={{ marginBottom: 16 }}>
              {detail.alert.service && <Tag>{detail.alert.service}</Tag>}
              {detail.alert.cluster && <Tag>{detail.alert.cluster}</Tag>}
              {Object.entries(detail.alert.labels || {}).slice(0, 6).map(([k, v]) => (
                <Tag key={k}>{k}={v}</Tag>
              ))}
            </Space>
            <Descriptions size="small" bordered column={2}>
              <Descriptions.Item label="负责人">{detail.alert.owner || '待认领'}</Descriptions.Item>
              <Descriptions.Item label="状态">{detail.alert.status}</Descriptions.Item>
              <Descriptions.Item label="当前值">{detail.alert.cur_value || '—'}</Descriptions.Item>
              <Descriptions.Item label="阈值">{detail.alert.threshold || '—'}</Descriptions.Item>
              <Descriptions.Item label="开始时间">{detail.alert.started_at ? dayjs(detail.alert.started_at).format('YYYY-MM-DD HH:mm:ss') : '—'}</Descriptions.Item>
              <Descriptions.Item label="持续">{durationText(detail.alert.started_at)}</Descriptions.Item>
            </Descriptions>
            <Typography.Title level={5} style={{ marginTop: 24 }}>CMDB 影响</Typography.Title>
            <Space style={{ marginBottom: 8 }}>
              <Button size="small" loading={cmdbLoading} onClick={() => void loadCmdbImpact()}>
                查 CMDB 影响
              </Button>
              {cmdbImpact && (
                <Typography.Text type="secondary">
                  下游 {cmdbImpact.summary.downstream} · 上游 {cmdbImpact.summary.upstream}
                </Typography.Text>
              )}
            </Space>
            {cmdbImpact && (
              <Space wrap style={{ marginBottom: 16 }}>
                {Object.entries(cmdbImpact.summary.by_type || {}).map(([k, v]) => (
                  <Tag key={k}>{k}:{v}</Tag>
                ))}
                {(cmdbImpact.summary.app_tags || []).map((t) => (
                  <Tag key={t} color="purple">{t}</Tag>
                ))}
              </Space>
            )}
            <Typography.Title level={5} style={{ marginTop: 24 }}>处理时间线</Typography.Title>
            <Timeline
              items={(detail.events || []).map((e) => ({
                children: (
                  <div>
                    <div style={{ fontWeight: 600 }}>{e.detail || e.action}</div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {e.operator} · {e.created_at ? dayjs(e.created_at).format('MM-DD HH:mm:ss') : ''}
                    </Typography.Text>
                  </div>
                ),
              }))}
            />
          </>
        )}
      </Drawer>
    </div>
  )
}
