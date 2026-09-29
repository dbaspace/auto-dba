import React, { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Button, Input, Select, Space, Tag } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { navigateToWorkOrderCreate } from '../../utils/workOrderDraft'

const TYPES = [
  { value: 'ddl', label: 'DDL' },
  { value: 'dml', label: '数据库操作' },
  { value: 'dql', label: '数据导出' },
  { value: 'dcl', label: '账号权限' },
  { value: 'kafka', label: 'Kafka' },
  { value: 'cdc_sync', label: 'CDC' },
  { value: 'archive', label: '数据归档' },
  { value: 'flashback', label: '闪回' },
  { value: 'migration', label: '数据迁移' },
  { value: 'mullang', label: '多语模版' },
  { value: 'createdb', label: '新增数据库' },
  { value: 'ha', label: '高可用变更' },
  { value: 'sr_cluster', label: 'StarRocks 集群变更' },
  { value: 'db_deploy', label: '数据库集群部署' },
]
const STATUSES = [
  { value: 'pending', label: '待审批' },
  { value: 'approved', label: '已通过' },
  { value: 'running', label: '执行中' },
  { value: 'rejected', label: '已驳回' },
  { value: 'canceled', label: '已撤销' },
  { value: 'executed', label: '已执行' },
  { value: 'failed', label: '执行失败' },
]

function typeText(t: string) {
  return (
    {
      ddl: 'DDL',
      dml: 'DML',
      dql: '数据导出',
      dcl: '权限申请',
      kafka: 'Kafka',
      cdc_sync: 'CDC',
      archive: '数据归档',
      flashback: '闪回',
      migration: '数据迁移',
      mullang: '多语模版',
      createdb: '新增数据库',
      ha: '高可用变更',
      redis: 'Redis',
      sr_cluster: 'StarRocks 集群变更',
      db_deploy: '数据库集群部署',
    }[t] ||
    t ||
    '-'
  )
}
function statusText(s: string) {
  return (
    {
      pending: '待审批',
      approved: '已通过',
      running: '执行中',
      rejected: '已驳回',
      canceled: '已撤销',
      executed: '已执行',
      failed: '执行失败',
    }[s] ||
    s ||
    '-'
  )
}
function statusColor(s: string) {
  if (s === 'approved' || s === 'executed') return 'success'
  if (s === 'rejected' || s === 'failed') return 'error'
  if (s === 'pending') return 'warning'
  return 'default'
}

export default function TicketList() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [filters, setFilters] = useState({
    type: String(searchParams.get('type') || ''),
    status: '',
    q: '',
  })
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  async function load(next = filters) {
    setLoading(true)
    setPage(1)
    try {
      const { data } = await api.get('/tickets', { params: next })
      setRows(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  function reset() {
    const next = { type: '', status: '', q: '' }
    setFilters(next)
    load(next)
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="ops-page" style={{ overflow: 'auto' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'center', marginBottom: 12 }}>
        <span>工单类型：</span>
        <Select
          allowClear
          placeholder="全部"
          style={{ width: 140 }}
          value={filters.type || undefined}
          onChange={(v) => setFilters((f) => ({ ...f, type: v || '' }))}
          options={TYPES}
        />
        <span>状态：</span>
        <Select
          allowClear
          placeholder="请选择状态"
          style={{ width: 140 }}
          value={filters.status || undefined}
          onChange={(v) => setFilters((f) => ({ ...f, status: v || '' }))}
          options={STATUSES}
        />
        <span>关键词：</span>
        <Input
          placeholder="请输入单号/库名"
          style={{ width: 180 }}
          value={filters.q}
          onChange={(e) => setFilters((f) => ({ ...f, q: e.target.value }))}
          onPressEnter={() => load()}
        />
        <Space>
          <Button type="primary" onClick={() => load()}>
            查询
          </Button>
          <Button onClick={reset}>重置</Button>
          <Button onClick={() => navigateToWorkOrderCreate(navigate, 'ddl')}>DDL 提单</Button>
          <Button onClick={() => navigateToWorkOrderCreate(navigate, 'dml')}>DML 提单</Button>
          <Button onClick={() => navigate('/ops/dql')}>DQL 导出</Button>
        </Space>
      </div>
      <p style={{ margin: '0 0 10px', color: '#b45309', fontSize: 13 }}>已提交工单超过 8 小时仍未审核完成，系统将自动取消。</p>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        size="small"
        bordered
        scroll={{ x: 1100 }}
        pagination={{
          current: page,
          pageSize,
          total: rows.length,
          showSizeChanger: true,
          pageSizeOptions: ['10', '20', '50', '100'],
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
          },
        }}
        columns={[
          { title: '单号', dataIndex: 'ticket_no', width: 160 },
          { title: '类型', width: 110, render: (_, r) => typeText(r.issue_type) },
          { title: '库', dataIndex: 'db_name', ellipsis: true },
          { title: '提交人', dataIndex: 'submitter_name', width: 120 },
          { title: '审批人', dataIndex: 'assigned_to', width: 120 },
          {
            title: '状态',
            width: 110,
            render: (_, r) => <Tag color={statusColor(r.status)}>{statusText(r.status)}</Tag>,
          },
          { title: '创建时间', dataIndex: 'created_at', width: 200 },
          {
            title: '操作',
            width: 90,
            fixed: 'right',
            render: (_, r) => (
              <Button type="link" onClick={() => navigate('/ops/tickets/' + r.id)}>
                详情
              </Button>
            ),
          },
        ]}
      />
    </div>
  )
}
