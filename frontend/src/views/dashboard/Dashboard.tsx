import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Empty, Modal, Pagination, Skeleton, Tag } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import {
  FileTextOutlined,
  FormOutlined,
  PlusOutlined,
  RocketOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import * as echarts from 'echarts'
import api from '../../api/request'
import { useAppSelector } from '../../store'
import ChartBox from './ChartBox'
import MyDraftsModal from '../../components/MyDraftsModal'
import {
  DRAFTS_CHANGED_EVENT,
  WORK_ORDER_CREATE_OPTIONS,
  WorkOrderCreateOption,
  allowOpenTypeOrRestore,
  currentDraftOwnerId,
  loadAllWorkOrderDrafts,
} from '../../utils/workOrderDraft'
import './Dashboard.css'

const TYPE_LABEL: Record<string, string> = {
  dql: '数据导出', dml: 'DML', ddl: 'DDL', dcl: '权限申请', resource_apply: '平台资源申请', cloud_pg: '云 PostgreSQL 变更', cloud_mysql: '云 MySQL 变更', cloud_redis: '云 Redis 变更', cloud_host: '云主机变更',
  kafka: 'Kafka', redis: 'Redis',
  mullang: '多语模版', cdc_sync: 'CDC同步', cdc: 'CDC同步', migration: '数据迁移',
  archive: '数据归档', flashback: '闪回', createdb: '新增数据库',
}
const STATUS_TEXT: Record<string, string> = {
  pending: '待审批', approved: '已通过', executed: '已完成', rejected: '已驳回',
  canceled: '已取消', failed: '失败', running: '执行中', init: '待审批',
}
const STATUS_COLOR: Record<string, string> = {
  pending: '#faad14', approved: '#1677ff', executed: '#52c41a', rejected: '#cf1322',
  canceled: '#8c8c8c', failed: '#eb2f96', running: '#13c2c2',
}
const STACK_COLORS = ['#1677ff', '#52c41a', '#722ed1', '#fa8c16', '#13c2c2', '#eb2f96', '#2f54eb', '#a0d911']
const createOptions = WORK_ORDER_CREATE_OPTIONS
const AUDIT_KIND_LABEL: Record<string, string> = {
  db: '数据库',
  kafka: 'Kafka',
  redis: 'Redis',
}
const AUDIT_KIND_COLOR: Record<string, string> = {
  db: '#1677ff',
  kafka: '#722ed1',
  redis: '#cf1322',
}

function auditKindLabel(k?: string) {
  const key = String(k || '').toLowerCase()
  return AUDIT_KIND_LABEL[key] || k || '-'
}

function previewAuditSQL(s?: string, max = 60) {
  const v = String(s || '').replace(/\s+/g, ' ').trim()
  if (v.length <= max) return v || '-'
  return `${v.slice(0, max)}…`
}

interface Filters {
  status: string
  issue_type: string
  idc: string
  idc_scope: string
}

function fmt(n: any) {
  return Number(n || 0).toLocaleString('en-US')
}
function typeLabel(t: any) {
  return TYPE_LABEL[String(t || '').toLowerCase()] || String(t || '').toUpperCase() || t
}
function scopeLabel(s: any) {
  const k = String(s || '').toLowerCase()
  if (k === 'prod') return '生产'
  if (k === 'test') return '测试'
  return s || '未知'
}
function statusText(s: any) {
  return STATUS_TEXT[s] || s || '-'
}
function statusColor(s: any): string {
  if (s === 'pending' || s === 'init') return 'warning'
  if (s === 'executed') return 'success'
  if (s === 'rejected' || s === 'failed') return 'error'
  return 'default'
}
function shortId(id: any) {
  const s = String(id || '')
  if (!s) return '-'
  return s.length <= 8 ? `#${s}` : `#${s.slice(0, 4)}…${s.slice(-4)}`
}
function fmtTime(at: any) {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
function fmtActTime(at: any) {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return '-'
  const now = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  if (d.toDateString() === now.toDateString()) return `${p(d.getHours())}:${p(d.getMinutes())}`
  const y = new Date(now)
  y.setDate(now.getDate() - 1)
  if (d.toDateString() === y.toDateString()) return '昨天'
  return `${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
function actTone(a: any) {
  const s = String(a.status || '')
  if (a.event_type === 'created') return 'primary'
  if (s === 'failed' || s === 'rejected' || s === 'canceled') return 'danger'
  if (s === 'executed' || s === 'approved') return 'success'
  return 'primary'
}
function canApprove(row: any) {
  return (
    row.status === 'pending' ||
    (row.source === 'migration' && (row.status === 'pending' || row.status === 'init'))
  )
}

export default function Dashboard() {
  const navigate = useNavigate()
  const user = useAppSelector((s) => s.auth.user)
  const isAdmin = String(user?.role || '').toLowerCase() === 'admin'
  const [filters, setFilters] = useState<Filters>({ status: '', issue_type: '', idc: '', idc_scope: '' })
  const [stats, setStats] = useState<any>(null)
  const [statsLoading, setStatsLoading] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [todos, setTodos] = useState<any[]>([])
  const [pendingCount, setPendingCount] = useState(0)
  const [todoPage, setTodoPage] = useState(1)
  const [todoLoading, setTodoLoading] = useState(false)
  const [activities, setActivities] = useState<any[]>([])
  const [actLoading, setActLoading] = useState(false)
  const [actPage, setActPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)
  const [draftsOpen, setDraftsOpen] = useState(false)
  const [draftCount, setDraftCount] = useState(0)
  const [idcOpen, setIdcOpen] = useState(false)
  const [creatorOpen, setCreatorOpen] = useState(false)
  const [auditStats, setAuditStats] = useState<any>(null)
  const [auditLoading, setAuditLoading] = useState(false)

  const filtersRef = useRef(filters)
  filtersRef.current = filters
  const todoPageRef = useRef(todoPage)
  todoPageRef.current = todoPage

  function sumStatus(keys: string[]) {
    const set = new Set(keys)
    return (stats?.by_status || []).reduce(
      (a: number, r: any) => (set.has(r.status) ? a + (r.count || 0) : a),
      0,
    )
  }
  const waitInWindow = sumStatus(['pending'])
  const doneCount = sumStatus(['executed'])
  const runningCount = sumStatus(['approved', 'running'])
  const rejectedCount = sumStatus(['rejected'])
  const failedCount = sumStatus(['failed'])

  function share(part: number) {
    const t = stats?.total || 0
    if (!t) return '0% 占比'
    return `${Math.round((part / t) * 100)}% 占比`
  }

  function goDetail(row: any) {
    const id = row.id || row.issue_id
    if (!id) return
    if (row.source === 'migration' || row.issue_type === 'migration') {
      navigate(id ? `/ops/migration/${id}` : '/ops/migration')
      return
    }
    navigate('/ops/tickets/' + id)
  }

  const pagedActivities = useMemo(
    () => activities.slice((actPage - 1) * 10, actPage * 10),
    [activities, actPage],
  )

  function queryParams() {
    const f = filtersRef.current
    const q: Record<string, any> = {}
    if (f.status) q.status = f.status
    if (f.issue_type) q.issue_type = f.issue_type
    if (f.idc) q.idc = f.idc
    if (f.idc_scope) q.idc_scope = f.idc_scope
    return q
  }

  async function loadStats() {
    setStatsLoading(true)
    setLoadError(false)
    try {
      const res = await api.get('/tickets/dashboard/stats', { params: queryParams() })
      setStats(res.data.data)
    } catch {
      setStats(null)
      setLoadError(true)
    } finally {
      setStatsLoading(false)
    }
  }
  async function loadTodos() {
    setTodoLoading(true)
    try {
      const res = await api.get('/tickets/todos', {
        params: { ...queryParams(), current: todoPageRef.current, page_size: 10 },
      })
      const data = res.data.data
      setTodos(data?.list || [])
      setPendingCount(data?.pending_count || 0)
    } catch {
      setTodos([])
      setPendingCount(0)
    } finally {
      setTodoLoading(false)
    }
  }
  async function loadActs() {
    setActLoading(true)
    try {
      const res = await api.get('/tickets/activities', { params: { ...queryParams(), limit: 20 } })
      setActivities(res.data.data?.list || [])
    } catch {
      setActivities([])
    } finally {
      setActLoading(false)
    }
  }
  async function loadAuditStats() {
    if (!isAdmin) {
      setAuditStats(null)
      return
    }
    setAuditLoading(true)
    try {
      const res = await api.get('/query-audits/stats', { params: { days: 7 }, silent: true })
      setAuditStats(res.data.data || null)
    } catch {
      setAuditStats(null)
    } finally {
      setAuditLoading(false)
    }
  }
  function loadAll() {
    loadStats()
    loadTodos()
    loadActs()
    loadAuditStats()
  }

  function toggle(key: keyof Filters, value?: string) {
    if (!value) return
    setFilters((prev) => {
      const next = { ...prev, [key]: prev[key] === value ? '' : value }
      filtersRef.current = next
      return next
    })
    setTodoPage(1)
    todoPageRef.current = 1
    // 用 setTimeout 让 ref 更新后再请求
    setTimeout(loadAll, 0)
  }

  function refreshDraftCount() {
    setDraftCount(loadAllWorkOrderDrafts(currentDraftOwnerId()).length)
  }

  async function goCreate(opt: WorkOrderCreateOption) {
    const go = () => {
      setCreateOpen(false)
      navigate(opt.path)
    }
    if (!opt.type) {
      go()
      return
    }
    const ok = await allowOpenTypeOrRestore(opt.type, opt.label, go)
    if (ok) go()
  }

  const hasStatus = (stats?.by_status || []).some((x: any) => x.count > 0)
  const hasType = (stats?.by_issue_type || []).some((x: any) => x.count > 0)
  const hasScope = (stats?.by_idc_scope || []).some((x: any) => x.count > 0)
  const hasIdc = (stats?.by_idc || []).some((x: any) => x.count > 0)
  const hasTrend = (stats?.trend || []).some((x: any) => x.count > 0)
  const hasAuditData = (auditStats?.total || 0) > 0
  const hasAuditKind = (auditStats?.by_kind || []).some((x: any) => x.count > 0)
  const hasAuditTrend = (auditStats?.trend || []).some((x: any) => x.total > 0)
  const hasAuditUsers = (auditStats?.top_users || []).length > 0
  const hasAuditSlow = (auditStats?.slow_top || []).length > 0

  const statusPie: echarts.EChartsCoreOption = {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, type: 'scroll' },
    series: [{
      type: 'pie', radius: ['42%', '68%'], center: ['50%', '42%'],
      label: { formatter: '{b}\n{c}' },
      data: (stats?.by_status || []).filter((x: any) => x.count > 0).map((x: any) => ({
        name: statusText(x.status), value: x.count, status: x.status,
        itemStyle: { color: STATUS_COLOR[x.status], opacity: filters.status && filters.status !== x.status ? 0.35 : 1 },
      })),
    }],
  }
  const typeBar: echarts.EChartsCoreOption = (() => {
    const rows = [...(stats?.by_issue_type || [])].sort((a, b) => (b.count || 0) - (a.count || 0))
    return {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: 8, right: 12, top: 12, bottom: 8, containLabel: true },
      xAxis: { type: 'category', data: rows.map((r) => typeLabel(r.issue_type)), axisLabel: { interval: 0, rotate: rows.length > 4 ? 28 : 0, fontSize: 11 } },
      yAxis: { type: 'value', minInterval: 1 },
      series: [{
        type: 'bar', barMaxWidth: 36,
        data: rows.map((r) => ({
          value: r.count, issue_type: r.issue_type,
          itemStyle: { color: '#1677ff', opacity: filters.issue_type && filters.issue_type !== r.issue_type ? 0.3 : 1 },
        })),
      }],
    }
  })()
  const scopePie: echarts.EChartsCoreOption = {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, type: 'scroll' },
    series: [{
      type: 'pie', radius: ['35%', '62%'], center: ['50%', '42%'],
      label: { formatter: '{b}\n{c}' },
      data: (stats?.by_idc_scope || []).filter((x: any) => x.count > 0).map((x: any) => ({
        name: scopeLabel(x.idc_scope), value: x.count, idc_scope: x.idc_scope,
        itemStyle: { opacity: filters.idc_scope && filters.idc_scope !== x.idc_scope ? 0.35 : 1 },
      })),
    }],
  }
  const creatorBar: echarts.EChartsCoreOption = (() => {
    const rows = [...(stats?.top_creators || [])].sort((a, b) => (b.count || 0) - (a.count || 0)).slice(0, 10)
    return {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: 8, right: 24, top: 8, bottom: 8, containLabel: true },
      xAxis: { type: 'value', minInterval: 1 },
      yAxis: { type: 'category', data: rows.map((r) => r.username || '-').reverse(), axisLabel: { width: 100, overflow: 'truncate', fontSize: 11 } },
      series: [{ type: 'bar', barMaxWidth: 16, itemStyle: { color: '#722ed1' }, data: [...rows].reverse().map((r) => r.count) }],
    }
  })()
  const trendLine: echarts.EChartsCoreOption = (() => {
    const points = stats?.trend || []
    return {
      tooltip: { trigger: 'axis' },
      grid: { left: 8, right: 16, top: 16, bottom: 8, containLabel: true },
      xAxis: { type: 'category', data: points.map((p: any) => p.date), axisLabel: { fontSize: 10, rotate: points.length > 14 ? 30 : 0 } },
      yAxis: { type: 'value', minInterval: 1 },
      series: [{ type: 'line', smooth: true, symbolSize: 6, areaStyle: { opacity: 0.12 }, itemStyle: { color: '#1677ff' }, data: points.map((p: any) => p.count) }],
    }
  })()
  const idcBar: echarts.EChartsCoreOption = (() => {
    const rows = [...(stats?.by_idc || [])].filter((x) => x.count > 0).sort((a, b) => (b.count || 0) - (a.count || 0))
    return {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: 8, right: 16, top: 12, bottom: 8, containLabel: true },
      xAxis: { type: 'category', data: rows.map((r) => r.idc), axisLabel: { interval: 0, rotate: rows.length > 4 ? 28 : 0, fontSize: 11 } },
      yAxis: { type: 'value', minInterval: 1 },
      series: [{
        type: 'bar', barMaxWidth: 36,
        data: rows.map((r) => ({ value: r.count, idc: r.idc, itemStyle: { color: '#13c2c2', opacity: filters.idc && filters.idc !== r.idc ? 0.35 : 1 } })),
      }],
    }
  })()
  const creatorStack: echarts.EChartsCoreOption | null = (() => {
    const top = [...(stats?.top_creators || [])].sort((a, b) => (b.count || 0) - (a.count || 0)).slice(0, 10)
    const types = new Map<string, number>()
    for (const c of top) for (const t of c.by_issue_type || []) types.set(t.issue_type, (types.get(t.issue_type) || 0) + (t.count || 0))
    const issueTypes = [...types.entries()].sort((a, b) => b[1] - a[1]).map(([t]) => t)
    if (!top.length || !issueTypes.length) return null
    return {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      legend: { type: 'scroll', bottom: 0 },
      grid: { left: 8, right: 16, top: 24, bottom: 48, containLabel: true },
      xAxis: { type: 'category', data: top.map((c) => c.username || '-'), axisLabel: { interval: 0, rotate: top.length > 5 ? 28 : 0, fontSize: 11 } },
      yAxis: { type: 'value', minInterval: 1 },
      series: issueTypes.map((issueType, idx) => ({
        name: typeLabel(issueType), type: 'bar', stack: 'issue_type', barMaxWidth: 36,
        itemStyle: { color: STACK_COLORS[idx % STACK_COLORS.length] },
        data: top.map((c) => Number((c.by_issue_type || []).find((t: any) => t.issue_type === issueType)?.count) || 0),
      })),
    }
  })()

  const hasCreators = (stats?.top_creators || []).length > 0

  function onStatusClick(p: any) { toggle('status', p?.data?.status) }
  function onTypeClick(p: any) { toggle('issue_type', p?.data?.issue_type) }
  function onScopeClick(p: any) {
    if (!p?.data?.idc_scope) return
    setFilters((prev) => ({ ...prev, idc: '' }))
    toggle('idc_scope', p.data.idc_scope)
  }
  function onIdcClick(p: any) {
    if (!p?.data?.idc) return
    setFilters((prev) => ({ ...prev, idc_scope: '' }))
    toggle('idc', p.data.idc)
  }

  const auditKindPie: echarts.EChartsCoreOption = {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, type: 'scroll' },
    series: [{
      type: 'pie', radius: ['42%', '68%'], center: ['50%', '42%'],
      label: { formatter: '{b}\n{c}' },
      data: (auditStats?.by_kind || []).filter((x: any) => x.count > 0).map((x: any) => ({
        name: auditKindLabel(x.kind),
        value: x.count,
        itemStyle: { color: AUDIT_KIND_COLOR[String(x.kind || '').toLowerCase()] },
      })),
    }],
  }
  const auditTrendLine: echarts.EChartsCoreOption = (() => {
    const points = auditStats?.trend || []
    return {
      tooltip: { trigger: 'axis' },
      legend: { bottom: 0, data: ['总量', '失败'] },
      grid: { left: 8, right: 16, top: 16, bottom: 28, containLabel: true },
      xAxis: { type: 'category', data: points.map((p: any) => p.date), axisLabel: { fontSize: 10, rotate: points.length > 7 ? 30 : 0 } },
      yAxis: { type: 'value', minInterval: 1 },
      series: [
        { name: '总量', type: 'line', smooth: true, symbolSize: 5, itemStyle: { color: '#1677ff' }, data: points.map((p: any) => p.total || 0) },
        { name: '失败', type: 'line', smooth: true, symbolSize: 5, itemStyle: { color: '#ff4d4f' }, data: points.map((p: any) => p.failed || 0) },
      ],
    }
  })()
  const auditUserBar: echarts.EChartsCoreOption = (() => {
    const rows = [...(auditStats?.top_users || [])].sort((a: any, b: any) => (b.count || 0) - (a.count || 0)).slice(0, 5)
    return {
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      grid: { left: 8, right: 24, top: 8, bottom: 8, containLabel: true },
      xAxis: { type: 'value', minInterval: 1 },
      yAxis: {
        type: 'category',
        data: rows.map((r: any) => r.display_name || r.username || '-').reverse(),
        axisLabel: { width: 90, overflow: 'truncate', fontSize: 11 },
      },
      series: [{ type: 'bar', barMaxWidth: 16, itemStyle: { color: '#13c2c2' }, data: [...rows].reverse().map((r: any) => r.count) }],
    }
  })()

  useEffect(() => {
    loadAll()
    refreshDraftCount()
    window.addEventListener(DRAFTS_CHANGED_EVENT, refreshDraftCount)
    return () => window.removeEventListener(DRAFTS_CHANGED_EVENT, refreshDraftCount)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    refreshDraftCount()
  }, [user?.id])

  const todoColumns: ColumnsType<any> = [
    {
      title: '工单',
      key: 'name',
      render: (_, row) => (
        <span>
          <span className="todo-id">{shortId(row.id)}</span>
          {row.name || '-'}
          {row.source === 'migration' && <Tag color="blue" style={{ marginLeft: 6 }}>数据迁移</Tag>}
          {row.issue_type === 'resource_apply' && (row.tenant_name || row.tenant_id) ? (
            <Tag style={{ marginLeft: 6 }}>{row.tenant_name || row.tenant_id}</Tag>
          ) : null}
        </span>
      ),
    },
    { title: '申请人', width: 100, render: (_, row) => row.creator_name || row.created_by || '-' },
    { title: '创建时间', width: 150, render: (_, row) => fmtTime(row.created_at) },
    {
      title: '状态',
      width: 100,
      render: (_, row) => <Tag color={statusColor(row.status)}>{statusText(row.status)}</Tag>,
    },
    {
      title: '操作',
      width: 120,
      render: (_, row) => (
        <>
          {canApprove(row) && (
            <Button type="link" size="small" style={{ color: '#52c41a' }} onClick={() => goDetail(row)}>通过</Button>
          )}
          <Button type="link" size="small" onClick={() => goDetail(row)}>查看</Button>
        </>
      ),
    },
  ]

  return (
    <div className="dash">
      <div className="ops-card">
        <Skeleton loading={statsLoading} active paragraph={{ rows: 3 }}>
          {loadError ? (
            <div className="empty-hint">统计接口不可用</div>
          ) : (
            <div className="kpi-grid">
              <div className="kpi">
                <div className="kpi-top"><span>总工单</span><span className="kpi-ico" style={{ background: '#1677ff' }}>▣</span></div>
                <div className="kpi-num">{fmt(stats?.total)}</div>
                {stats?.today_count ? (
                  <div className="kpi-foot up">+{stats.today_count} 今日新增</div>
                ) : (
                  <div className="kpi-foot">CDC 任务工单 {fmt(stats?.cdc_total)}</div>
                )}
              </div>
              <div className="kpi">
                <div className="kpi-top"><span>我的工单</span><span className="kpi-ico" style={{ background: '#597ef7' }}>☺</span></div>
                <div className="kpi-num">{fmt(stats?.my_total)}</div>
                <div className="kpi-foot">我创建/经办/审批</div>
              </div>
              <div className="kpi">
                <div className="kpi-top"><span>待审批</span><span className="kpi-ico" style={{ background: '#fa8c16' }}>⌛</span></div>
                <div className="kpi-num">{fmt(pendingCount ?? waitInWindow)}</div>
                <div className="kpi-foot">平台分布 {fmt(waitInWindow)}</div>
              </div>
              <div className="kpi">
                <div className="kpi-top"><span>已通过/完成</span><span className="kpi-ico" style={{ background: '#52c41a' }}>✓</span></div>
                <div className="kpi-num">{fmt(doneCount)}</div>
                <div className="kpi-foot">{share(doneCount)}</div>
              </div>
              <div className="kpi">
                <div className="kpi-top"><span>执行中</span><span className="kpi-ico" style={{ background: '#13c2c2' }}>▶</span></div>
                <div className="kpi-num">{fmt(runningCount)}</div>
                <div className="kpi-foot">{share(runningCount)}</div>
              </div>
              <div className="kpi">
                <div className="kpi-top"><span>已驳回</span><span className="kpi-ico" style={{ background: '#f5222d' }}>✕</span></div>
                <div className="kpi-num">{fmt(rejectedCount)}</div>
                <div className="kpi-foot">{share(rejectedCount)}</div>
              </div>
              <div className="kpi">
                <div className="kpi-top"><span>失败</span><span className="kpi-ico" style={{ background: '#eb2f96' }}>!</span></div>
                <div className="kpi-num">{fmt(failedCount)}</div>
                <div className="kpi-foot">{share(failedCount)}</div>
              </div>
            </div>
          )}
        </Skeleton>

        <div className="workbench">
          <div className="wb-card todo-card">
            <div className="wb-head">
              <span>待办工单 {pendingCount ? <span className="muted">（{pendingCount}）</span> : null}</span>
              <Button type="link" onClick={() => navigate('/ops/tickets')}>查看全部 &gt;</Button>
            </div>
            <div className="todo-table-wrap">
              <ResponsiveTable
                rowKey={(r, i) => String(r.id ?? i)}
                dataSource={todos}
                columns={todoColumns}
                size="small"
                loading={todoLoading}
                pagination={false}
                locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无待您审批的工单（含迁移）" /> }}
              />
            </div>
            {pendingCount > 10 && (
              <div className="pager">
                <Pagination
                  size="small"
                  total={pendingCount}
                  pageSize={10}
                  current={todoPage}
                  showTotal={(t) => `共 ${t} 条`}
                  onChange={(p) => { setTodoPage(p); todoPageRef.current = p; loadTodos() }}
                />
              </div>
            )}
          </div>

          <div className="wb-side">
            <div className="wb-card">
              <div className="wb-head">
                <span>实时动态</span>
                <Button type="link" onClick={() => navigate('/ops/tickets')}>更多</Button>
              </div>
              <div>
                {activities.length ? (
                  <>
                    <ul className="act-list">
                      {pagedActivities.map((a, i) => (
                        <li key={i} onClick={() => goDetail(a)}>
                          <span className={`act-dot ${actTone(a)}`} />
                          <span className="act-msg">
                            {a.source === 'migration' && <Tag color="blue">数据迁移</Tag>}
                            {a.message || a.issue_name || '-'}
                          </span>
                          <span className="act-time">{fmtActTime(a.at)}</span>
                        </li>
                      ))}
                    </ul>
                    {activities.length > 10 && (
                      <div className="pager">
                        <Pagination
                          size="small"
                          simple
                          total={activities.length}
                          pageSize={10}
                          current={actPage}
                          onChange={(p) => setActPage(p)}
                        />
                      </div>
                    )}
                  </>
                ) : (
                  <Empty description="近 3 天暂无动态" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                )}
                {actLoading ? null : null}
              </div>
            </div>

            <div className="wb-card">
              <div className="wb-head">
                <span className="wb-head-title">
                  <RocketOutlined className="wb-head-ico" />
                  快捷操作
                </span>
              </div>
              <div className="quick-grid">
                <button type="button" className="quick" onClick={() => setCreateOpen(true)}>
                  <span className="quick-ico"><PlusOutlined /></span>
                  <span>新建工单</span>
                </button>
                <button type="button" className="quick" onClick={() => navigate('/ops/query')}>
                  <span className="quick-ico"><SearchOutlined /></span>
                  <span>数据查询</span>
                </button>
                <button
                  type="button"
                  className="quick"
                  onClick={() => navigate('/ops/query', { state: { storeTab: 'fav' } })}
                >
                  <span className="quick-ico"><FileTextOutlined /></span>
                  <span>常用模板</span>
                </button>
                <button
                  type="button"
                  className={`quick${draftCount > 0 ? ' alert' : ''}`}
                  aria-label={draftCount > 0 ? `我的草稿（${draftCount} 条未提交本地缓存）` : '我的草稿'}
                  onClick={() => setDraftsOpen(true)}
                >
                  {draftCount ? <span className="quick-badge">{draftCount > 99 ? '99+' : draftCount}</span> : null}
                  <span className="quick-ico"><FormOutlined /></span>
                  <span>{draftCount > 0 ? `我的草稿（${draftCount}）` : '我的草稿'}</span>
                </button>
              </div>
            </div>
          </div>
        </div>

        <div className="chart-grid">
          <div className="chart-card">
            <div className="chart-title">状态分布</div>
            {hasStatus ? (
              <ChartBox option={statusPie} height="220px" onClick={onStatusClick} />
            ) : (
              <Empty description="暂无数据" imageStyle={{ height: 56 }} />
            )}
            <p className="chart-tip">点击可筛选状态（再次点击取消）</p>
          </div>
          <div className="chart-card">
            <div className="chart-title">工单类型分布</div>
            {hasType ? (
              <ChartBox option={typeBar} height="220px" onClick={onTypeClick} />
            ) : (
              <Empty description="暂无数据" imageStyle={{ height: 56 }} />
            )}
            <p className="chart-tip">点击可筛选工单类型</p>
          </div>
          <div className="chart-card">
            <div className="chart-title">
              环境概览
              <Button type="link" onClick={() => setIdcOpen(true)}>查看明细</Button>
            </div>
            {hasScope ? (
              <ChartBox option={scopePie} height="220px" onClick={onScopeClick} />
            ) : (
              <Empty description="暂无数据" imageStyle={{ height: 56 }} />
            )}
            <p className="chart-tip">测试 / 生产；点击可筛选环境范围</p>
          </div>
          {hasCreators && (
            <div className="chart-card">
              <div className="chart-title">
                申请人 Top10
                <Button type="link" onClick={() => setCreatorOpen(true)}>查看明细</Button>
              </div>
              <ChartBox option={creatorBar} height="240px" />
            </div>
          )}
        </div>

        <div className="chart-card">
          <div className="chart-title">申请趋势（近 30 天）</div>
          {hasTrend ? (
            <ChartBox option={trendLine} height="240px" />
          ) : (
            <Empty description="近 30 天无申请" imageStyle={{ height: 56 }} />
          )}
        </div>

        {isAdmin ? (
          <div className="audit-section">
            <div className="wb-head audit-section-head">
              <span>查询审计数据分析（近 7 天）</span>
              <Button type="link" onClick={() => navigate('/config/query-audits')}>查看明细 &gt;</Button>
            </div>
            <Skeleton loading={auditLoading} active paragraph={{ rows: 4 }}>
              {!hasAuditData ? (
                <div className="chart-card">
                  <Empty description="近 7 天暂无查询审计" imageStyle={{ height: 56 }} />
                </div>
              ) : (
                <>
                  <div className="kpi-grid audit-kpi">
                    <div className="kpi">
                      <div className="kpi-top"><span>总查询</span></div>
                      <div className="kpi-num">{fmt(auditStats.total)}</div>
                      <div className="kpi-foot">成功 {fmt(auditStats.success)} · 失败 {fmt(auditStats.failed)}</div>
                    </div>
                    <div className="kpi">
                      <div className="kpi-top"><span>成功率</span></div>
                      <div className="kpi-num">{`${Math.round((auditStats.success_rate || 0) * 1000) / 10}%`}</div>
                      <div className="kpi-foot">近 7 天</div>
                    </div>
                    <div className="kpi">
                      <div className="kpi-top"><span>平均耗时</span></div>
                      <div className="kpi-num">{`${Math.round(auditStats.avg_elapsed_ms || 0)}`}<span className="muted"> ms</span></div>
                      <div className="kpi-foot">含成功与失败</div>
                    </div>
                    <div className="kpi">
                      <div className="kpi-top"><span>今日查询</span></div>
                      <div className="kpi-num">{fmt(auditStats.today)}</div>
                      <div className="kpi-foot">今日</div>
                    </div>
                  </div>
                  <div className="chart-grid">
                    <div className="chart-card">
                      <div className="chart-title">类型分布</div>
                      {hasAuditKind ? (
                        <ChartBox option={auditKindPie} height="220px" />
                      ) : (
                        <Empty description="暂无数据" imageStyle={{ height: 56 }} />
                      )}
                    </div>
                    <div className="chart-card">
                      <div className="chart-title">近 7 日趋势</div>
                      {hasAuditTrend ? (
                        <ChartBox option={auditTrendLine} height="220px" />
                      ) : (
                        <Empty description="暂无数据" imageStyle={{ height: 56 }} />
                      )}
                    </div>
                    <div className="chart-card">
                      <div className="chart-title">用户 Top5</div>
                      {hasAuditUsers ? (
                        <ChartBox option={auditUserBar} height="220px" />
                      ) : (
                        <Empty description="暂无数据" imageStyle={{ height: 56 }} />
                      )}
                    </div>
                    <div className="chart-card">
                      <div className="chart-title">慢查询 Top5</div>
                      {hasAuditSlow ? (
                        <ResponsiveTable
                          size="small"
                          pagination={false}
                          rowKey={(r) => r.id || `${r.created_at}-${r.elapsed_ms}`}
                          dataSource={auditStats.slow_top || []}
                          scroll={{ x: 480 }}
                          columns={[
                            {
                              title: '耗时',
                              width: 72,
                              dataIndex: 'elapsed_ms',
                              render: (v: number) => `${v ?? 0} ms`,
                            },
                            {
                              title: '类型',
                              width: 70,
                              dataIndex: 'kind',
                              render: (v: string) => auditKindLabel(v),
                            },
                            { title: '用户', width: 90, dataIndex: 'username', ellipsis: true },
                            { title: '目标', width: 90, dataIndex: 'target', ellipsis: true, render: (v: string) => v || '-' },
                            {
                              title: '语句',
                              dataIndex: 'statement',
                              ellipsis: true,
                              render: (v: string) => previewAuditSQL(v),
                            },
                          ]}
                        />
                      ) : (
                        <Empty description="暂无数据" imageStyle={{ height: 56 }} />
                      )}
                    </div>
                  </div>
                </>
              )}
            </Skeleton>
          </div>
        ) : null}
      </div>

      <Modal
        open={createOpen}
        title="新建工单"
        width={420}
        footer={null}
        destroyOnClose
        onCancel={() => setCreateOpen(false)}
      >
        <p className="muted">请选择工单申请类型</p>
        <div className="create-list">
          {createOptions.map((opt) => (
            <button key={opt.type || opt.path} type="button" className="create-item" onClick={() => goCreate(opt)}>
              {opt.label}
            </button>
          ))}
        </div>
      </Modal>

      <MyDraftsModal open={draftsOpen} onOpenChange={setDraftsOpen} onChanged={refreshDraftCount} />

      <Modal
        open={idcOpen}
        title="机房分布"
        width={800}
        footer={null}
        destroyOnClose
        onCancel={() => setIdcOpen(false)}
      >
        {hasIdc ? (
          <ChartBox option={idcBar} height="360px" onClick={onIdcClick} />
        ) : (
          <Empty description="暂无机房分布数据" />
        )}
      </Modal>

      <Modal
        open={creatorOpen}
        title="申请人 Top10 · 工单分类"
        width={900}
        footer={null}
        destroyOnClose
        onCancel={() => setCreatorOpen(false)}
      >
        {creatorStack ? (
          <ChartBox option={creatorStack} height="400px" />
        ) : (
          <Empty description="暂无工单分类明细" />
        )}
      </Modal>
    </div>
  )
}
