import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Button,
  DatePicker,
  Form,
  Input,
  Modal,
  Pagination,
  Progress,
  Select,
  Space,
  Table,
  Tag,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Dayjs } from 'dayjs'
import api from '../../api/request'
import { navigateToWorkOrderCreate } from '../../utils/workOrderDraft'
import './Migration.css'

const STATUS_OPTS = [
  { label: '待执行', value: 'pending' },
  { label: '待定时触发', value: 'scheduled' },
  { label: '预检中', value: 'pre_checking' },
  { label: '预检失败', value: 'pre_check_failed' },
  { label: '迁移中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已暂停', value: 'paused' },
  { label: '已取消', value: 'canceled' },
]

function dbTypeLabel(t: any) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgres' || n === 'postgresql') return 'PostgreSQL'
  if (n === 'mysql' || n === 'tidb' || n === 'oceanbase') return 'MySQL'
  return t || '—'
}
function endpointText(row: any) {
  const src = [row.source_host, row.source_port].filter(Boolean).join(':') || row.source_db || '—'
  const tgt = [row.target_host, row.target_port].filter(Boolean).join(':') || row.target_db || '—'
  return `${src} / ${row.source_db || '—'} → ${tgt} / ${row.target_db || '—'}`
}
function modeText(m: any) {
  return ({ full_data: '全量迁移', schema_only: '仅结构迁移' } as Record<string, string>)[m] || m || '—'
}
function transferText(t: any) {
  return ({ direct: '直连', oss: 'OSS 中转' } as Record<string, string>)[t] || t || '—'
}
function statusText(s: any, row?: any) {
  if (row?.approval_status === 'pending' && (s === 'pending' || s === 'init')) return '待审批'
  return (
    ({
      pending: '待执行',
      init: '待执行',
      scheduled: '待定时触发',
      pre_checking: '预检中',
      pre_check_failed: '预检失败',
      running: '迁移中',
      executed: '已完成',
      completed: '已完成',
      failed: '失败',
      paused: '已暂停',
      canceled: '已取消',
    } as Record<string, string>)[s] || s || '—'
  )
}
// Element type → AntD Tag color
function statusTagColor(s: any, row?: any) {
  if (row?.approval_status === 'pending' && (s === 'pending' || s === 'init')) return 'warning'
  if (s === 'executed' || s === 'completed') return 'success'
  if (s === 'running' || s === 'pre_checking' || s === 'scheduled') return 'warning'
  if (s === 'failed' || s === 'pre_check_failed') return 'error'
  return 'default'
}
function approvalText(s: any) {
  return (
    ({ draft: '草稿', pending: '待审批', approved: '已通过', rejected: '已驳回' } as Record<string, string>)[s] ||
    s ||
    '—'
  )
}
function approvalTagColor(s: any) {
  if (s === 'approved') return 'success'
  if (s === 'pending') return 'warning'
  if (s === 'rejected') return 'error'
  return 'default'
}
function progress(row: any) {
  if (row.status === 'executed' || row.status === 'completed') return 100
  const total = Number(row.total_objects || 0)
  const done = Number(row.completed_objects || 0)
  if (total > 0) return Math.min(100, Math.round((done * 100) / total))
  if (row.status === 'running' || row.status === 'pre_checking') return 5
  return 0
}
function progressStatus(row: any): 'success' | 'exception' | undefined {
  if (row.status === 'failed' || row.status === 'pre_check_failed') return 'exception'
  if (row.status === 'completed' || row.status === 'executed') return 'success'
  return undefined
}
function canStart(row: any) {
  return ['pending', 'scheduled'].includes(row.status) && row.approval_status === 'approved'
}
function canPause(row: any) {
  return ['running', 'pre_checking'].includes(row.status)
}
function canResume(row: any) {
  return row.status === 'paused'
}
function canCancel(row: any) {
  return ['pending', 'scheduled', 'running', 'pre_checking', 'paused'].includes(row.status)
}
function canRetry(row: any) {
  return ['failed', 'pre_check_failed', 'canceled'].includes(row.status)
}
function canDelete(row: any) {
  return !['running', 'pre_checking'].includes(row.status)
}

interface Filters {
  status: string[]
  approval_status: string
  db_type: string
  env: string
  range: [Dayjs, Dayjs] | null
  q: string
}

export default function Migration() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [allRows, setAllRows] = useState<any[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [filters, setFilters] = useState<Filters>({
    status: [],
    approval_status: '',
    db_type: '',
    env: '',
    range: null,
    q: '',
  })
  const allRowsRef = useRef<any[]>(allRows)
  allRowsRef.current = allRows

  const filtered = useMemo(() => {
    return allRows.filter((r) => {
      if (
        filters.status.length &&
        !filters.status.includes(r.status) &&
        !(filters.status.includes('pending') && r.status === 'init')
      )
        return false
      if (filters.approval_status && r.approval_status !== filters.approval_status) return false
      if (filters.db_type) {
        const n = String(r.db_type || '').toLowerCase()
        const want = filters.db_type
        if (want === 'mysql' && !['mysql', 'tidb', 'oceanbase'].includes(n)) return false
        if (want === 'postgres' && !['postgres', 'postgresql', 'pg'].includes(n)) return false
      }
      if (filters.env && r.env !== filters.env) return false
      if (filters.q && !String(r.name || '').toLowerCase().includes(filters.q.trim().toLowerCase())) return false
      if (filters.range && filters.range.length === 2) {
        const t = new Date(r.created_at).getTime()
        if (t < filters.range[0].valueOf() || t > filters.range[1].valueOf()) return false
      }
      return true
    })
  }, [allRows, filters])

  const pageRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filtered.slice(start, start + pageSize)
  }, [filtered, page, pageSize])

  function onQuery() {
    setPage(1)
  }
  function goDetail(row: any) {
    navigate(`/ops/migration/${row.id}`)
  }
  function goCreate() {
    navigateToWorkOrderCreate(navigate, 'migration')
  }

  async function load() {
    setLoading(true)
    try {
      const res = await api.get('/migration/tasks')
      setAllRows(res.data.data || [])
    } finally {
      setLoading(false)
    }
  }

  async function act(row: any, kind: string) {
    await api.post(`/migration/tasks/${row.id}/${kind}`)
    message.success('已提交')
    await load()
  }

  function remove(row: any) {
    Modal.confirm({
      title: '删除',
      icon: undefined,
      content: `确认删除任务「${row.name}」？`,
      okText: '确定',
      cancelText: '取消',
      okButtonProps: { danger: true },
      async onOk() {
        await api.delete(`/migration/tasks/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  useEffect(() => {
    ;(async () => {
      try {
        await api.get('/migration/settings')
      } catch {
        /* ignore */
      }
      await load()
    })()
    const timer = window.setInterval(() => {
      const busy = allRowsRef.current.some((r) =>
        ['running', 'scheduled', 'pre_checking'].includes(r.status),
      )
      if (busy) load()
    }, 5000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const columns: ColumnsType<any> = [
    {
      title: '任务名称',
      key: 'name',
      minWidth: 160,
      ellipsis: true,
      render: (_: any, row: any) => (
        <a className="name-link" onClick={() => goDetail(row)}>
          {row.name}
        </a>
      ),
    },
    {
      title: '数据库类型',
      key: 'db_type',
      width: 110,
      render: (_: any, row: any) => dbTypeLabel(row.db_type),
    },
    {
      title: '源端 → 目标端',
      key: 'endpoint',
      minWidth: 240,
      ellipsis: true,
      render: (_: any, row: any) => endpointText(row),
    },
    {
      title: '迁移模式',
      key: 'migration_mode',
      width: 110,
      render: (_: any, row: any) => modeText(row.migration_mode),
    },
    {
      title: '数据中转方式',
      key: 'transfer_mode',
      width: 120,
      render: (_: any, row: any) => transferText(row.transfer_mode),
    },
    {
      title: '状态',
      key: 'status',
      width: 120,
      render: (_: any, row: any) => (
        <Tag color={statusTagColor(row.status, row)}>{statusText(row.status, row)}</Tag>
      ),
    },
    {
      title: '审批',
      key: 'approval',
      width: 100,
      render: (_: any, row: any) =>
        row.issue_id ? (
          <Button type="link" size="small" onClick={() => navigate('/ops/tickets/' + row.issue_id)}>
            {approvalText(row.approval_status)}
          </Button>
        ) : (
          <Tag color={approvalTagColor(row.approval_status)}>{approvalText(row.approval_status)}</Tag>
        ),
    },
    {
      title: '进度',
      key: 'progress',
      width: 170,
      render: (_: any, row: any) => (
        <Progress percent={progress(row)} strokeWidth={10} status={progressStatus(row)} />
      ),
    },
    { title: '创建人', dataIndex: 'created_by', width: 100 },
    { title: '创建时间', dataIndex: 'created_at', width: 180 },
    {
      title: '操作',
      key: 'op',
      width: 200,
      fixed: 'right',
      render: (_: any, row: any) => (
        <div className="mig-ops">
          <Button type="link" size="small" onClick={() => goDetail(row)}>
            详情
          </Button>
          {canStart(row) && (
            <Button type="link" size="small" onClick={() => act(row, 'start')}>
              启动
            </Button>
          )}
          {canPause(row) && (
            <Button type="link" size="small" style={{ color: '#e6a23c' }} onClick={() => act(row, 'pause')}>
              暂停
            </Button>
          )}
          {canResume(row) && (
            <Button type="link" size="small" onClick={() => act(row, 'resume')}>
              恢复
            </Button>
          )}
          {canCancel(row) && (
            <Button type="link" size="small" style={{ color: '#e6a23c' }} onClick={() => act(row, 'cancel')}>
              取消
            </Button>
          )}
          {canRetry(row) && (
            <Button type="link" size="small" onClick={() => act(row, 'retry')}>
              重试
            </Button>
          )}
          {canDelete(row) && (
            <Button type="link" size="small" danger onClick={() => remove(row)}>
              删除
            </Button>
          )}
        </div>
      ),
    },
  ]

  const scrollX = columns.reduce((sum, c: any) => sum + (Number(c.width) || Number(c.minWidth) || 120), 0)

  return (
    <div className="mig-page">
      <div className="mig-head">
        <div className="mig-actions">
          <Button onClick={() => navigate('/ops/migration/settings')}>迁移设置</Button>
          <Button type="primary" onClick={goCreate}>
            新建迁移任务
          </Button>
        </div>
      </div>

      <Form
        layout="inline"
        className="mig-filters"
        onSubmitCapture={(e) => {
          e.preventDefault()
          onQuery()
        }}
      >
        <Form.Item label="任务状态">
          <Select
            mode="multiple"
            allowClear
            maxTagCount="responsive"
            placeholder="多选"
            style={{ minWidth: 200 }}
            value={filters.status}
            onChange={(v) => setFilters((f) => ({ ...f, status: v }))}
            options={STATUS_OPTS}
          />
        </Form.Item>
        <Form.Item label="审批状态">
          <Select
            allowClear
            placeholder="全部"
            style={{ width: 120 }}
            value={filters.approval_status || undefined}
            onChange={(v) => setFilters((f) => ({ ...f, approval_status: v || '' }))}
            options={[
              { label: '草稿', value: 'draft' },
              { label: '待审批', value: 'pending' },
              { label: '已通过', value: 'approved' },
              { label: '已驳回', value: 'rejected' },
            ]}
          />
        </Form.Item>
        <Form.Item label="数据库类型">
          <Select
            allowClear
            placeholder="全部"
            style={{ width: 140 }}
            value={filters.db_type || undefined}
            onChange={(v) => setFilters((f) => ({ ...f, db_type: v || '' }))}
            options={[
              { label: 'MySQL', value: 'mysql' },
              { label: 'PostgreSQL', value: 'postgres' },
            ]}
          />
        </Form.Item>
        <Form.Item label="环境">
          <Select
            allowClear
            placeholder="全部"
            style={{ width: 100 }}
            value={filters.env || undefined}
            onChange={(v) => setFilters((f) => ({ ...f, env: v || '' }))}
            options={[
              { label: 'test', value: 'test' },
              { label: 'prod', value: 'prod' },
            ]}
          />
        </Form.Item>
        <Form.Item label="创建时间">
          <DatePicker.RangePicker
            showTime
            placeholder={['开始', '结束']}
            value={filters.range}
            onChange={(v) => setFilters((f) => ({ ...f, range: (v as [Dayjs, Dayjs]) || null }))}
          />
        </Form.Item>
        <Form.Item label="关键字">
          <Input
            allowClear
            placeholder="任务名称"
            style={{ width: 140 }}
            value={filters.q}
            onChange={(e) => setFilters((f) => ({ ...f, q: e.target.value }))}
          />
        </Form.Item>
        <Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">
              查询
            </Button>
            <Button onClick={load}>刷新</Button>
          </Space>
        </Form.Item>
      </Form>

      <Table
        rowKey="id"
        loading={loading}
        bordered
        columns={columns}
        dataSource={pageRows}
        scroll={{ x: scrollX }}
        locale={{ emptyText: '暂无迁移任务' }}
        pagination={false}
      />
      <div className="mig-pager">
        <Pagination
          current={page}
          pageSize={pageSize}
          total={filtered.length}
          showSizeChanger
          pageSizeOptions={['10', '20', '50']}
          showTotal={(t) => `共 ${t} 条`}
          onChange={(p, ps) => {
            setPage(p)
            setPageSize(ps)
          }}
        />
      </div>
    </div>
  )
}
