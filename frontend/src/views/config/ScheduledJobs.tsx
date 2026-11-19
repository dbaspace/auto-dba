import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Drawer,
  Form,
  Input,
  Modal,
  Radio,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  message,
} from 'antd'
import dayjs from 'dayjs'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import Auth from '../../components/Auth'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'

/** 统一展示为本地时间 YYYY-MM-DD HH:mm:ss（兼容 UTC Z / 带偏移 RFC3339） */
function formatJobTime(raw?: string): string {
  if (!raw || !String(raw).trim()) return '—'
  const d = dayjs(raw)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : String(raw)
}

type JobView = {
  key: string
  name: string
  group: string
  description?: string
  schedule_kind: string
  schedule_spec: string
  schedule_human?: string
  enabled: boolean
  mutable?: boolean
  triggerable?: boolean
  pauseable?: boolean
  risk_level?: string
  misfire_policy?: string
  next_fire_at?: string
  last_status?: string
  last_started_at?: string
  last_ended_at?: string
  last_error?: string
  last_duration_ms?: number
  run_count_24h?: number
  fail_count_24h?: number
  lease_owner_host?: string
  lease_expires_at?: string
  lease_held?: boolean
  fencing_token?: number
  local_owner?: boolean
}

type JobRun = {
  id: string
  trigger: string
  status: string
  fire_key?: string
  owner_host?: string
  error?: string
  started_at: string
  ended_at?: string
  duration_ms?: number
}

type BizSchedule = {
  kind: string
  id: string
  name: string
  schedule_expr?: string
  enabled: boolean
  status?: string
  last_run_at?: string
  link: string
  extra?: string
}

const GROUP_ORDER = ['scanner', 'ticket', 'collect', 'alert', 'cloud', 'cdc', 'dns', 'session', 'security']

const GROUP_LABEL: Record<string, string> = {
  scanner: '调度扫描',
  ticket: '工单',
  collect: '采集',
  alert: '告警',
  cloud: '云资源',
  cdc: 'CDC',
  dns: 'DNS',
  session: '会话',
  security: '安全',
}

function groupLabel(g?: string) {
  if (!g) return '未分组'
  return GROUP_LABEL[g] || g
}

function groupRank(g?: string) {
  const i = GROUP_ORDER.indexOf(g || '')
  return i < 0 ? GROUP_ORDER.length : i
}

function statusTag(s?: string) {
  if (!s) return <Tag>—</Tag>
  const color =
    s === 'success' ? 'success' : s === 'failed' || s === 'interrupted' ? 'error' : s === 'running' ? 'processing' : 'default'
  return <Tag color={color}>{s}</Tag>
}

export default function ScheduledJobs() {
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.scheduledJobs.edit)
  const canTrigger = hasUrn(Perm.scheduledJobs.trigger)
  const [loading, setLoading] = useState(false)
  const [jobs, setJobs] = useState<JobView[]>([])
  const [summary, setSummary] = useState<Record<string, any>>({})
  const [biz, setBiz] = useState<BizSchedule[]>([])
  const [editOpen, setEditOpen] = useState(false)
  const [editJob, setEditJob] = useState<JobView | null>(null)
  const [form] = Form.useForm()
  const [runsOpen, setRunsOpen] = useState(false)
  const [runs, setRuns] = useState<JobRun[]>([])
  const [runsKey, setRunsKey] = useState('')
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsTotal, setRunsTotal] = useState(0)
  const [runsPage, setRunsPage] = useState(1)
  const [runsPageSize, setRunsPageSize] = useState(10)
  const [groupFilter, setGroupFilter] = useState('all')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [j, b] = await Promise.all([
        api.get('/jobs'),
        api.get('/jobs/business-schedules'),
      ])
      setJobs(j.data?.items || [])
      setSummary(j.data?.summary || {})
      setBiz(b.data?.items || [])
    } catch (e: any) {
      message.error(e?.response?.data?.error || e?.response?.data?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const grouped = useMemo(() => {
    const map = new Map<string, JobView[]>()
    for (const job of jobs) {
      const g = job.group || ''
      const list = map.get(g) || []
      list.push(job)
      map.set(g, list)
    }
    return [...map.entries()].sort((a, b) => groupRank(a[0]) - groupRank(b[0]) || a[0].localeCompare(b[0]))
  }, [jobs])

  const groupAlive = groupFilter === 'all' || grouped.some(([g]) => g === groupFilter)
  const effectiveFilter = groupAlive ? groupFilter : 'all'
  const visibleGroups = effectiveFilter === 'all' ? grouped : grouped.filter(([g]) => g === effectiveFilter)

  const onToggle = async (row: JobView, enabled: boolean) => {
    if (row.group === 'scanner' && !enabled) {
      Modal.confirm({
        title: '确认暂停调度扫描？',
        content: `暂停「${row.name}」后，整站将不再扫描到期的业务任务，直到恢复。`,
        okType: 'danger',
        onOk: async () => {
          await api.put(`/jobs/${row.key}`, { enabled })
          message.success('已更新')
          load()
        },
      })
      return
    }
    await api.put(`/jobs/${row.key}`, { enabled })
    message.success('已更新')
    load()
  }

  const onTrigger = async (row: JobView) => {
    const run = async () => {
      try {
        const res = await api.post(`/jobs/${row.key}/trigger`)
        if (res.data?.skipped) {
          message.warning(res.data.reason || '已跳过')
        } else {
          message.success('已触发')
        }
        load()
      } catch (e: any) {
        if (e?.response?.status === 409) {
          message.error(`租约被占用：${e.response.data?.holder || ''} ${e.response.data?.expires_at || ''}`)
        } else {
          message.error(e?.response?.data?.error || '触发失败')
        }
      }
    }
    if (row.risk_level === 'high') {
      Modal.confirm({ title: `确认立即执行「${row.name}」？`, onOk: run })
      return
    }
    await run()
  }

  const openEdit = (row: JobView) => {
    setEditJob(row)
    form.setFieldsValue({ schedule_spec: row.schedule_spec })
    setEditOpen(true)
  }

  const saveEdit = async () => {
    if (!editJob) return
    const v = await form.validateFields()
    await api.put(`/jobs/${editJob.key}`, { schedule_spec: v.schedule_spec })
    message.success('调度已更新')
    setEditOpen(false)
    load()
  }

  const loadRuns = async (key: string, page: number, pageSize: number) => {
    setRunsLoading(true)
    try {
      const res = await api.get(`/jobs/${key}/runs`, {
        params: { limit: pageSize, offset: (page - 1) * pageSize },
      })
      setRuns(res.data?.items || [])
      setRunsTotal(Number(res.data?.total) || 0)
    } catch (e: any) {
      message.error(e?.response?.data?.error || '加载历史失败')
    } finally {
      setRunsLoading(false)
    }
  }

  const openRuns = (row: JobView) => {
    setRunsKey(row.key)
    setRunsPage(1)
    setRunsPageSize(10)
    setRunsOpen(true)
    loadRuns(row.key, 1, 10)
  }

  const columns = useMemo(
    () => [
      { title: '名称', dataIndex: 'name', width: 160 },
      {
        title: '分组',
        dataIndex: 'group',
        width: 100,
        render: (g: string) => groupLabel(g),
      },
      {
        title: '调度',
        key: 'sched',
        width: 160,
        render: (_: any, r: JobView) => (
          <span>
            {r.schedule_human || r.schedule_spec}
            <div style={{ color: '#888', fontSize: 12 }}>{r.schedule_spec}</div>
          </span>
        ),
      },
      {
        title: '启用',
        dataIndex: 'enabled',
        width: 80,
        render: (v: boolean, r: JobView) => (
          <Switch
            checked={v}
            disabled={!r.pauseable || !canEdit}
            onChange={(c) => onToggle(r, c)}
            size="small"
          />
        ),
      },
      {
        title: '租约',
        key: 'lease',
        width: 140,
        render: (_: any, r: JobView) =>
          r.lease_held ? (
            <Tag color={r.local_owner ? 'blue' : 'orange'}>
              {r.lease_owner_host || '持有中'}
              {r.local_owner ? ' (本机)' : ''}
            </Tag>
          ) : (
            <Tag>—</Tag>
          ),
      },
      {
        title: '上次',
        key: 'last',
        width: 160,
        render: (_: any, r: JobView) => (
          <Space direction="vertical" size={0}>
            {statusTag(r.last_status)}
            <span style={{ fontSize: 12, color: '#888' }}>{formatJobTime(r.last_started_at)}</span>
          </Space>
        ),
      },
      {
        title: '下次',
        dataIndex: 'next_fire_at',
        width: 160,
        render: (v: string) => formatJobTime(v),
      },
      {
        title: '24h',
        key: 'c24',
        width: 90,
        render: (_: any, r: JobView) => (
          <span>
            {r.run_count_24h || 0}
            {(r.fail_count_24h || 0) > 0 ? (
              <Tag color="error" style={{ marginLeft: 4 }}>
                失败 {r.fail_count_24h}
              </Tag>
            ) : null}
          </span>
        ),
      },
      {
        title: '操作',
        key: 'op',
        width: 220,
        fixed: 'right' as const,
        render: (_: any, r: JobView) => (
          <Space>
            {r.triggerable ? (
              <Auth urn={Perm.scheduledJobs.trigger}>
                <Button type="link" size="small" onClick={() => onTrigger(r)}>
                  立即执行
                </Button>
              </Auth>
            ) : null}
            {r.mutable ? (
              <Auth urn={Perm.scheduledJobs.edit}>
                <Button type="link" size="small" onClick={() => openEdit(r)}>
                  编辑调度
                </Button>
              </Auth>
            ) : null}
            <Button type="link" size="small" onClick={() => openRuns(r)}>
              历史
            </Button>
          </Space>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [jobs, canEdit, canTrigger],
  )

  const bizColumns = [
    { title: '类型', dataIndex: 'kind', width: 120 },
    { title: '名称', dataIndex: 'name' },
    { title: '调度', dataIndex: 'schedule_expr', width: 160 },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="success">是</Tag> : <Tag>否</Tag>),
    },
    { title: '状态', dataIndex: 'status', width: 100 },
    { title: '最近执行', dataIndex: 'last_run_at', width: 180, render: (v: string) => formatJobTime(v) },
    {
      title: '操作',
      key: 'go',
      width: 100,
      render: (_: any, r: BizSchedule) => <Link to={r.link}>去配置</Link>,
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 12 }} wrap>
        <Tag color="blue">启用 {summary.enabled ?? 0}</Tag>
        <Tag>暂停 {summary.paused ?? 0}</Tag>
        <Tag color={(summary.fail_24h || 0) > 0 ? 'error' : 'default'}>24h 失败 {summary.fail_24h ?? 0}</Tag>
        <Tag color="processing">租约中 {summary.leased ?? 0}</Tag>
        <Tag>
          本机 {summary.local_host || '-'} / {(summary.local_owner_id || '').slice(0, 8)}
        </Tag>
        {summary.metrics ? (
          <Tag>
            本进程成功 {summary.metrics.run_success ?? 0} / 失败 {summary.metrics.run_failed ?? 0}
          </Tag>
        ) : null}
        <Button onClick={load} loading={loading}>
          刷新
        </Button>
      </Space>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="平台任务由元库持久化调度（settings / fire_ledger / lease），多副本靠租约互斥；业务策略请在「业务调度」Tab 深链配置。工作流请用海豚。"
      />
      <Tabs
        items={[
          {
            key: 'platform',
            label: '平台任务',
            children: (
              <>
                <Radio.Group
                  value={effectiveFilter}
                  onChange={(e) => setGroupFilter(e.target.value)}
                  style={{ marginBottom: 12, display: 'flex', flexWrap: 'wrap', rowGap: 8 }}
                >
                  <Radio.Button value="all">全部 {jobs.length}</Radio.Button>
                  {grouped.map(([g, list]) => (
                    <Radio.Button key={g || 'none'} value={g}>
                      {groupLabel(g)} {list.length}
                    </Radio.Button>
                  ))}
                </Radio.Group>
                {visibleGroups.map(([g, list]) => (
                  <div key={g || 'none'} style={{ marginBottom: 16 }}>
                    {effectiveFilter === 'all' ? (
                      <div style={{ marginBottom: 8, fontWeight: 600 }}>
                        {groupLabel(g)} <Tag>{list.length}</Tag>
                      </div>
                    ) : null}
                    <ResponsiveTable
                      rowKey="key"
                      loading={loading}
                      dataSource={list}
                      columns={columns}
                      scroll={{ x: 1200 }}
                      pagination={list.length > 20 ? { pageSize: 20 } : false}
                    />
                  </div>
                ))}
              </>
            ),
          },
          {
            key: 'biz',
            label: '业务调度',
            children: (
              <ResponsiveTable
                rowKey={(r) => `${r.kind}-${r.id}`}
                loading={loading}
                dataSource={biz}
                columns={bizColumns}
                pagination={{ pageSize: 20 }}
              />
            ),
          },
        ]}
      />

      <Modal
        title={editJob ? `编辑调度 · ${editJob.name}` : '编辑调度'}
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={saveEdit}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="schedule_spec"
            label={editJob?.schedule_kind === 'interval' ? '间隔（如 30s / 5m）' : 'Cron 表达式'}
            rules={[{ required: true }]}
            extra={
              editJob?.schedule_kind === 'cron6'
                ? '6 段：秒 分 时 * * *（日定点）'
                : editJob?.misfire_policy
                  ? `漏跑策略：${editJob.misfire_policy}`
                  : undefined
            }
          >
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer title={`执行历史 · ${runsKey}`} open={runsOpen} onClose={() => setRunsOpen(false)} width={960}>
        <Table
          rowKey="id"
          loading={runsLoading}
          dataSource={runs}
          tableLayout="fixed"
          pagination={{
            current: runsPage,
            pageSize: runsPageSize,
            total: runsTotal,
            showSizeChanger: true,
            pageSizeOptions: ['10', '20', '30', '50', '100'],
            showTotal: (t) => `共 ${t} 条`,
            onChange: (page, pageSize) => {
              const size = pageSize || runsPageSize
              const nextPage = size !== runsPageSize ? 1 : page
              setRunsPage(nextPage)
              setRunsPageSize(size)
              if (runsKey) loadRuns(runsKey, nextPage, size)
            },
          }}
          columns={[
            { title: '触发', dataIndex: 'trigger', width: 72 },
            { title: '状态', dataIndex: 'status', width: 96, render: (s: string) => statusTag(s) },
            { title: '开始', dataIndex: 'started_at', width: 188, render: (v: string) => formatJobTime(v) },
            { title: '结束', dataIndex: 'ended_at', width: 188, render: (v: string) => formatJobTime(v) },
            { title: '耗时ms', dataIndex: 'duration_ms', width: 88 },
            { title: '节点', dataIndex: 'owner_host', width: 110, ellipsis: true },
            { title: '错误', dataIndex: 'error', ellipsis: true },
          ]}
        />
      </Drawer>
    </div>
  )
}
