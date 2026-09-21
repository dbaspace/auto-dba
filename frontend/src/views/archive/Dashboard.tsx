import React, { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Col, Progress, Row, Table, Tag } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import CreateTaskDialog from './CreateTaskDialog'
import {
  decodeMeta,
  formatTime,
  runModeLabel,
  statusText,
  statusType,
  strategyLabel,
} from './archive.ts'
import './archive.css'

const tagColor: Record<string, string> = {
  success: 'success',
  danger: 'error',
  warning: 'warning',
  info: 'default',
  primary: 'blue',
}

export default function Dashboard() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [stats, setStats] = useState<any>({})
  const [instances, setInstances] = useState<any[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [createStrategy, setCreateStrategy] = useState('')
  const [lockStrategy, setLockStrategy] = useState(false)

  const runs = useMemo(() => stats.recent_runs || [], [stats])
  const health = useMemo(
    () => ({
      storage_usage: stats.storage_usage || 0,
      db_connection: stats.db_connection === 'ok' ? '正常' : stats.db_connection || '-',
      pending_approval: stats.pending_approval || 0,
      schedule_queue: stats.schedule_queue || 0,
    }),
    [stats],
  )
  const kpis = useMemo(
    () => [
      { label: '总任务数', value: stats.total_tasks ?? '-', bg: '#1677ff', mark: '☰' },
      { label: '归档数据量', value: stats.archived_volume ?? '-', bg: '#52c41a', mark: '▣' },
      { label: '成功率', value: stats.success_rate ?? '-', bg: '#fa8c16', mark: '✓' },
      { label: '待处理告警', value: stats.pending_alerts ?? 0, bg: '#f5222d', mark: '!' },
    ],
    [stats],
  )

  function openCreate(strategy = '', lock = false) {
    setCreateStrategy(strategy)
    setLockStrategy(lock)
    setCreateOpen(true)
  }

  async function load() {
    setLoading(true)
    try {
      const [st, inst] = await Promise.all([api.get('/archive/stats'), api.get('/instances')])
      setStats(st.data.data || {})
      setInstances(inst.data.data || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const columns: ColumnsType<any> = [
    {
      title: '任务名称',
      minWidth: 150,
      ellipsis: true,
      render: (_, row) => row.task_name || row.name || '-',
    },
    {
      title: '任务策略',
      width: 128,
      render: (_, row) => (
        <Tag color="blue">{strategyLabel(decodeMeta(row.condition).strategy)}</Tag>
      ),
    },
    {
      title: '状态',
      width: 100,
      render: (_, row) => (
        <Tag color={tagColor[statusType(row.status)] || 'default'}>{statusText(row.status)}</Tag>
      ),
    },
    {
      title: '执行方式',
      width: 110,
      render: (_, row) => runModeLabel(decodeMeta(row.condition).run_mode),
    },
    { title: '归档量', dataIndex: 'volume', width: 88 },
    { title: '耗时', dataIndex: 'duration', width: 80 },
    { title: '工单创建时间', width: 168, render: (_, row) => formatTime(row.created_at) },
    { title: '执行时间', width: 168, render: (_, row) => formatTime(row.started_at) },
    {
      title: '操作',
      width: 80,
      fixed: 'right',
      render: () => (
        <Button type="link" onClick={() => navigate('/ops/archive/tasks')}>
          查看
        </Button>
      ),
    },
  ]

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <Row gutter={16} className="mb">
          {kpis.map((item) => (
            <Col key={item.label} xs={24} sm={12} lg={6}>
              <div className="kpi-card">
                <div className="kpi-ico" style={{ background: item.bg }}>
                  {item.mark}
                </div>
                <div className="kpi-text">
                  <div className="kpi-num">{item.value}</div>
                  <div className="kpi-label">{item.label}</div>
                </div>
              </div>
            </Col>
          ))}
        </Row>

        <Row gutter={16} className="mb">
          <Col xs={24} lg={16}>
            <Card bordered={false} className="panel">
              <div className="card-head" style={{ marginBottom: 12 }}>
                <span className="card-title">最近执行任务</span>
                <div className="card-extra">
                  <Button type="link" onClick={load}>
                    刷新
                  </Button>
                  <Button type="link" onClick={() => navigate('/ops/archive/runs')}>
                    查看全部 →
                  </Button>
                </div>
              </div>
              <Table
                rowKey={(r) => r.id ?? r.name ?? Math.random()}
                dataSource={runs}
                columns={columns}
                size="small"
                loading={loading}
                pagination={false}
                scroll={{ x: true }}
                locale={{ emptyText: '暂无最近执行记录' }}
              />
            </Card>
          </Col>
          <Col xs={24} lg={8}>
            <Card bordered={false} className="panel">
              <div className="card-head" style={{ marginBottom: 12 }}>
                <span className="card-title">系统健康</span>
                <Button type="link" onClick={() => navigate('/ops/archive/monitor')}>
                  详情
                </Button>
              </div>
              <div className="health-row">
                <span>存储使用率</span>
                <span>{health.storage_usage > 0 ? health.storage_usage + '%' : '-'}</span>
              </div>
              {health.storage_usage > 0 && (
                <Progress percent={health.storage_usage} showInfo={false} className="mb-sm" />
              )}
              <div className="health-row">
                <span>数据库连接</span>
                <Tag color="success">{health.db_connection || '-'}</Tag>
              </div>
              <div className="health-row">
                <span>待审批任务</span>
                <span className="badge badge-warn">{health.pending_approval || 0}</span>
              </div>
              <div className="health-row">
                <span>调度队列</span>
                <span className="badge badge-info">{health.schedule_queue || 0}</span>
              </div>
              <div className="health-actions">
                <Button type="primary" onClick={() => openCreate()}>
                  新建任务
                </Button>
                <Button onClick={load}>刷新</Button>
                <Button onClick={() => navigate('/ops/archive/report')}>报表</Button>
              </div>
            </Card>
          </Col>
        </Row>

        <Card bordered={false} className="panel">
          <div style={{ marginBottom: 12 }}>
            <span className="card-title">快速开始</span>
          </div>
          <div className="quick-grid">
            <button
              type="button"
              className="quick-btn"
              onClick={() => openCreate('backup_target', true)}
            >
              按时间归档
            </button>
            <button
              type="button"
              className="quick-btn"
              onClick={() => openCreate('delete_only', true)}
            >
              清理过期数据
            </button>
            <button
              type="button"
              className="quick-btn"
              onClick={() => navigate('/ops/migration/create')}
            >
              全库迁移
            </button>
            <button
              type="button"
              className="quick-btn"
              onClick={() => navigate('/ops/archive/masking')}
            >
              敏感数据脱敏
            </button>
          </div>
        </Card>

        <CreateTaskDialog
          open={createOpen}
          onClose={() => setCreateOpen(false)}
          instances={instances}
          initialStrategy={createStrategy}
          lockStrategy={lockStrategy}
          onSaved={load}
        />
      </div>
    </div>
  )
}
