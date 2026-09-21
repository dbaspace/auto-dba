import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useNavigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Input,
  Modal,
  Pagination,
  Progress,
  Radio,
  Select,
  Spin,
  Table,
  Tabs,
  Tag,
  message,
} from 'antd'
import api from '../../api/request'
import './MigrationDetail.css'

function emptyPlain(v: any) {
  const s = v != null ? String(v).trim() : ''
  return s || '暂无'
}
function emptyNum(v: any) {
  if (v == null || v === '' || v === 0) return '—'
  return v
}
function fmtTime(v: any) {
  if (!v) return '暂无'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(
    d.getMinutes(),
  )}:${pad(d.getSeconds())}`
}
function formatHumanDataSize(bytes: number) {
  if (bytes === 0) return '0 B'
  if (bytes >= 1024 ** 4) return `${(bytes / 1024 ** 4).toFixed(2)} TB`
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(2)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}
function transferText(t: any) {
  return ({ direct: '直连', oss: 'OSS 中转' } as Record<string, string>)[t] || t || '暂无'
}
function statusTextOf(s: any, approvalStatus?: any) {
  if (approvalStatus === 'pending' && (s === 'pending' || s === 'init')) return '待审批'
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
      canceled: '已取消',
    } as Record<string, string>)[s] || s || '—'
  )
}
function statusTagColor(s: any) {
  if (s === 'executed' || s === 'completed') return 'success'
  if (s === 'running' || s === 'pre_checking' || s === 'scheduled' || s === 'pending') return 'warning'
  if (s === 'failed' || s === 'pre_check_failed') return 'error'
  return 'default'
}
function approvalText(s: any) {
  return (
    ({
      draft: '草稿',
      pending: '待审批',
      approved: '已通过',
      rejected: '已驳回',
      canceled: '已撤销',
    } as Record<string, string>)[s] || s || '暂无'
  )
}
function approvalTagColor(s: any) {
  if (s === 'approved') return 'success'
  if (s === 'pending') return 'warning'
  if (s === 'rejected') return 'error'
  return 'default'
}
function objStatusText(s: any) {
  return (
    ({ pending: '待迁移', running: '迁移中', completed: '已完成', failed: '失败', skipped: '跳过' } as Record<
      string,
      string
    >)[s] || s || '—'
  )
}
function objStatusTagColor(s: any) {
  if (s === 'completed') return 'success'
  if (s === 'running') return 'warning'
  if (s === 'failed') return 'error'
  return 'default'
}
function objectName(row: any, side: 'source' | 'target') {
  const db = side === 'source' ? row.source_db_name : row.target_db_name
  const schema = side === 'source' ? row.source_schema : row.target_schema
  const table = side === 'source' ? row.source_table_name : row.target_table_name
  return schema ? `${db}.${schema}.${table}` : `${db}.${table}`
}
function rowPct(row: any) {
  const t = Number(row.total_rows || 0)
  const c = Number(row.completed_rows || 0)
  return t > 0 ? Math.round((c * 100) / t) : 0
}
function precheckTagColor(s: any) {
  if (s === 'passed') return 'success'
  if (s === 'failed') return 'error'
  if (s === 'warning') return 'warning'
  return 'default'
}
function logLevelClass(lv: any) {
  if (lv === 'ERROR') return 'lv-error'
  if (lv === 'WARN') return 'lv-warn'
  return ''
}
function readSelection(taskVal: any) {
  const tags = taskVal?.tags
  const sel = tags?.selection
  if (sel && typeof sel === 'object') return sel
  return {}
}
function readNonNeg(v: any): number | undefined {
  if (v == null || v === '') return undefined
  const n = Number(v)
  if (!Number.isFinite(n) || n < 0) return undefined
  return Math.floor(n)
}
function mapVerifyRows(raw: any) {
  const result = raw?.data ?? raw
  let items: any[] = []
  if (Array.isArray(result)) items = result
  else if (Array.isArray(result?.list)) items = result.list
  else if (Array.isArray(result?.details?.objects)) items = result.details.objects
  return items.map((r) => {
    if (typeof r.row_match === 'boolean') {
      return {
        table: r.table,
        check_type: r.check_type || '行数比对',
        status: r.row_match ? '一致' : '不一致',
        detail: r.detail || `源 ${r.source_rows ?? '-'} 行 / 目标 ${r.target_rows ?? '-'} 行`,
      }
    }
    if (typeof r.schema_match === 'boolean') {
      return {
        table: r.table,
        check_type: r.check_type || '结构比对',
        status: r.schema_match ? '一致' : '不一致',
        detail: r.detail || (r.schema_match ? '列、约束、索引一致' : '结构不一致'),
      }
    }
    return { table: r.table, check_type: r.check_type, status: r.status, detail: r.detail }
  })
}

const me = (() => {
  try {
    return JSON.parse(localStorage.getItem('user') || '{}')
  } catch {
    return {}
  }
})()

export default function MigrationDetail() {
  const params = useParams()
  const navigate = useNavigate()
  const id = params.id as string

  const [loading, setLoading] = useState(false)
  const [task, setTask] = useState<any>(null)
  const [objects, setObjects] = useState<any[]>([])
  const [logs, setLogs] = useState<any[]>([])
  const [prechecks, setPrechecks] = useState<any[]>([])
  const [prog, setProg] = useState<any>(null)
  const [verifyRows, setVerifyRows] = useState<any[]>([])
  const [tab, setTab] = useState('overview')
  const [comment, setComment] = useState('')
  const [objStatus, setObjStatus] = useState('')
  const [logLevel, setLogLevel] = useState('')
  const [logKw, setLogKw] = useState('')
  const [logPage, setLogPage] = useState(1)
  const [logPageSize, setLogPageSize] = useState(10)
  const [verifying, setVerifying] = useState(false)
  const [remainSec, setRemainSec] = useState<number | null>(null)

  const taskRef = useRef<any>(task)
  taskRef.current = task
  const remainRef = useRef<number | null>(remainSec)
  remainRef.current = remainSec

  const isCreator = Boolean(task && me.username && me.username === task.created_by)
  const approvalPending = task?.approval_status === 'pending'
  const canApprove = (() => {
    if (!task || task.approval_status !== 'pending') return false
    if (isCreator) return false
    if (task.assigned_approver && task.assigned_approver !== me.username) return false
    return true
  })()
  const canStart =
    ['pending', 'scheduled'].includes(task?.status) && task?.approval_status === 'approved'
  const canCancel = ['pending', 'scheduled', 'running', 'pre_checking'].includes(task?.status)
  const canRetry = ['failed', 'pre_check_failed', 'canceled'].includes(task?.status)
  const verifyEnabled = ['completed', 'executed'].includes(task?.status)

  const estimatedBytes = useMemo(() => {
    const t = task
    if (!t) return undefined
    const fromSel = readNonNeg(readSelection(t).estimated_total_bytes)
    if (fromSel != null) return fromSel
    return readNonNeg(t.total_bytes ?? t.estimated_total_bytes ?? t.objects_total_bytes)
  }, [task])
  const selectionScale = useMemo(() => {
    const sel = readSelection(task)
    return {
      dbCount: readNonNeg(sel.db_count),
      tableCount: readNonNeg(sel.table_count),
    }
  }, [task])
  const endpointPair = useMemo(() => {
    const t = task
    if (!t) return '—'
    const src = `${t.source_host || ''}${t.source_port ? ':' + t.source_port : ''}`
    const tgt = `${t.target_host || ''}${t.target_port ? ':' + t.target_port : ''}`
    return `${src || '—'} → ${tgt || '—'}`
  }, [task])
  const overviewTotal = prog?.total_objects ?? task?.total_objects ?? 0
  const overviewDone = prog?.completed_objects ?? task?.completed_objects ?? 0
  const overviewPct = (() => {
    if (task?.status === 'completed' || task?.status === 'executed') return 100
    if (overviewTotal > 0) return Math.min(100, Math.round((overviewDone * 100) / overviewTotal))
    return 0
  })()
  const progressStatus: 'success' | 'exception' | undefined = (() => {
    const s = task?.status
    if (s === 'failed' || s === 'pre_check_failed') return 'exception'
    if (s === 'completed' || s === 'executed') return 'success'
    return undefined
  })()
  const byteTotal = prog?.total_bytes ?? estimatedBytes ?? task?.total_bytes ?? 0
  const mbps = Number(prog?.speed_bytes_per_sec || 0) / (1024 * 1024)
  const remainText = (() => {
    if (remainSec == null) return '暂无'
    if (remainSec <= 0) return '已到点'
    const h = Math.floor(remainSec / 3600)
    const m = Math.floor((remainSec % 3600) / 60)
    const s = remainSec % 60
    if (h > 0) return `${h}小时${m}分${s}秒`
    if (m > 0) return `${m}分${s}秒`
    return `${s}秒`
  })()

  const filteredObjects = useMemo(() => {
    if (!objStatus) return objects
    return objects.filter((r) => r.status === objStatus)
  }, [objects, objStatus])
  const filteredLogs = useMemo(() => {
    return logs.filter((l) => {
      if (logLevel && l.log_level !== logLevel) return false
      if (logKw.trim()) {
        const q = logKw.trim().toLowerCase()
        return (
          String(l.message || '').toLowerCase().includes(q) ||
          String(l.detail || '').toLowerCase().includes(q)
        )
      }
      return true
    })
  }, [logs, logLevel, logKw])
  const pagedLogs = useMemo(() => {
    const start = (logPage - 1) * logPageSize
    return filteredLogs.slice(start, start + logPageSize)
  }, [filteredLogs, logPage, logPageSize])

  function syncRemain(t: any) {
    if (!t) {
      setRemainSec(null)
      return
    }
    const server = t.seconds_until_scheduled
    if (server != null && server !== '') {
      setRemainSec(Math.max(0, Math.floor(Number(server))))
      return
    }
    if (t.start_mode === 'scheduled' && t.scheduled_at) {
      const diff = Math.floor((new Date(t.scheduled_at).getTime() - Date.now()) / 1000)
      setRemainSec(Number.isNaN(diff) ? null : Math.max(0, diff))
      return
    }
    setRemainSec(null)
  }

  async function load() {
    setLoading(true)
    try {
      const [t, o, l, p, g] = await Promise.all([
        api.get(`/migration/tasks/${id}`),
        api.get(`/migration/tasks/${id}/objects`),
        api.get(`/migration/tasks/${id}/logs`, { params: { since_id: 0, limit: 500 } }),
        api.get(`/migration/tasks/${id}/prechecks`),
        api.get(`/migration/tasks/${id}/progress`).catch(() => ({ data: {} })),
      ])
      const taskVal = t.data.data
      setTask(taskVal)
      setObjects(o.data.data || [])
      setLogs(l.data.data || [])
      setPrechecks(p.data.data || [])
      setProg(g.data.data || null)
      syncRemain(taskVal)
      if (['completed', 'executed'].includes(taskVal?.status)) {
        try {
          const vr = await api.get(`/migration/tasks/${id}/verify`)
          setVerifyRows(mapVerifyRows(vr.data))
        } catch {
          /* ignore */
        }
      }
    } finally {
      setLoading(false)
    }
  }

  async function act(kind: string) {
    await api.post(`/migration/tasks/${id}/${kind}`)
    message.success('已提交')
    await load()
  }
  async function approve() {
    await api.post(`/migration/tasks/${id}/approve`, { comment })
    message.success('已通过')
    await load()
  }
  function reject() {
    if (!comment.trim()) {
      message.warning('驳回原因必填')
      return
    }
    Modal.confirm({
      title: '驳回',
      content: '确认驳回该任务？',
      okText: '确定',
      cancelText: '取消',
      async onOk() {
        await api.post(`/migration/tasks/${id}/reject`, { comment })
        message.success('已驳回')
        await load()
      },
    })
  }
  async function triggerVerify() {
    setVerifying(true)
    try {
      const { data } = await api.post(`/migration/tasks/${id}/verify`, { row_count: true })
      setVerifyRows(mapVerifyRows(data))
      message.success('校验完成')
    } catch (e: any) {
      message.error(e.response?.data?.message || '校验失败')
    } finally {
      setVerifying(false)
    }
  }
  function exportLog() {
    const text = filteredLogs
      .map((l) => `${l.created_at} [${l.log_level}] ${l.message}${l.detail ? ` ${l.detail}` : ''}`)
      .join('\n')
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `migration-${id}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  useEffect(() => {
    setLogPage(1)
  }, [logLevel, logKw])

  useEffect(() => {
    load()
    const timer = window.setInterval(() => {
      if (['running', 'scheduled', 'pre_checking'].includes(taskRef.current?.status)) load()
    }, 4000)
    const remainTimer = window.setInterval(() => {
      if (remainRef.current != null && remainRef.current > 0) {
        setRemainSec((v) => (v != null ? v - 1 : v))
      }
    }, 1000)
    return () => {
      window.clearInterval(timer)
      window.clearInterval(remainTimer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <Spin spinning={loading}>
      <div className="mig-detail">
        <div className="mig-toolbar">
          <Button onClick={() => navigate('/ops/migration')}>返回列表</Button>
        </div>

        {task?.error_message && (
          <Alert type="error" showIcon className="mb" message={task.error_message} />
        )}
        {approvalPending && isCreator ? (
          <Alert
            type="info"
            showIcon
            className="mb"
            message="当前为「待审批」"
            description={`请通知指定审核人「${
              task.assigned_approver || '—'
            }」在本页完成通过或驳回。超过 8 小时未审核完成，系统将自动取消。`}
          />
        ) : approvalPending && canApprove ? (
          <Alert
            type="warning"
            showIcon
            className="mb"
            message="该任务待您审批"
            description="请在「审批操作」区域点击「通过」或「驳回」（驳回需填写意见）。"
          />
        ) : approvalPending ? (
          <Alert
            type="info"
            showIcon
            className="mb"
            message="您当前无法审批该任务"
            description={`本任务已指定审核人为「${
              task.assigned_approver || '—'
            }」，仅该账号可在本页通过/驳回。`}
          />
        ) : null}
        {task?.status === 'pre_check_failed' && (
          <Alert
            type="warning"
            showIcon
            className="mb"
            message="预检未通过"
            description="请修正配置后重新编辑并预检，或联系任务创建人处理。"
          />
        )}

        {canApprove && (
          <Card className="mb" bordered>
            <div className="sec-title">审批操作</div>
            <Input.TextArea
              className="mb"
              rows={2}
              placeholder="通过可填备注；驳回必须填写原因"
              value={comment}
              onChange={(e) => setComment(e.target.value)}
            />
            <Button type="primary" onClick={approve} style={{ marginRight: 8 }}>
              通过
            </Button>
            <Button danger onClick={reject}>
              驳回
            </Button>
          </Card>
        )}

        <Card className="mb head-card" bordered>
          <div className="head-row">
            <div className="head-main">
              <h2 className="task-name">{task?.name || task?.task_name || '任务详情'}</h2>
              <div className="head-meta">
                <span className="muted">任务 ID</span>
                <span className="mono">{task?.id}</span>
                <Tag color={statusTagColor(task?.status)}>
                  {statusTextOf(task?.status, task?.approval_status)}
                </Tag>
                <Tag color={approvalTagColor(task?.approval_status)}>
                  {approvalText(task?.approval_status)}
                </Tag>
              </div>
            </div>
            <div className="head-ops">
              {canStart && (
                <Button type="primary" onClick={() => act('start')}>
                  启动
                </Button>
              )}
              {canCancel && (
                <Button style={{ color: '#e6a23c' }} onClick={() => act('cancel')}>
                  取消
                </Button>
              )}
              {canRetry && (
                <Button type="primary" onClick={() => act('retry')}>
                  重试
                </Button>
              )}
              {task?.issue_id && (
                <Button onClick={() => navigate('/ops/tickets/' + task.issue_id)}>查看工单</Button>
              )}
            </div>
          </div>

          <div className="sec-title">任务配置</div>
          <Descriptions column={3} bordered size="small" className="mb">
            <Descriptions.Item label="数据库类型">{emptyPlain(task?.db_type)}</Descriptions.Item>
            <Descriptions.Item label="迁移模式">{emptyPlain(task?.migration_mode)}</Descriptions.Item>
            <Descriptions.Item label="数据中转方式">{transferText(task?.transfer_mode)}</Descriptions.Item>
            <Descriptions.Item label="迁移数据量（估算）">
              {estimatedBytes != null ? (
                <div>
                  <span>{formatHumanDataSize(estimatedBytes)}</span>
                  <span className="muted mono"> （{Number(estimatedBytes).toLocaleString()} B）</span>
                  <p className="field-note">
                    取值优先 tags.selection.estimated_total_bytes，否则回退任务根字段；与创建时选表汇总一致。
                  </p>
                </div>
              ) : (
                <span className="muted">
                  {task?.migration_mode === 'schema_only' ? '暂无 — 仅结构迁移时该项可为 0 或未返回' : '暂无'}
                </span>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="涉及库数">{selectionScale.dbCount ?? '暂无'}</Descriptions.Item>
            <Descriptions.Item label="选中表数">{selectionScale.tableCount ?? '暂无'}</Descriptions.Item>
            <Descriptions.Item label="启动方式">
              {task?.start_mode === 'scheduled' ? '定时启动' : '立即启动'}
            </Descriptions.Item>
            <Descriptions.Item label="定时时间">{fmtTime(task?.scheduled_at)}</Descriptions.Item>
            <Descriptions.Item label="距定时触发">{remainText}</Descriptions.Item>
            <Descriptions.Item label="源 → 目标" span={3}>
              <span className="mono">{endpointPair}</span>
            </Descriptions.Item>
          </Descriptions>

          <div className="sec-title">审批与流转</div>
          <Descriptions column={3} bordered size="small">
            <Descriptions.Item label="审批状态">
              <Tag color={approvalTagColor(task?.approval_status)}>
                {approvalText(task?.approval_status)}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="指定审核人">
              {emptyPlain(task?.assigned_approver || task?.assigned_approver_login)}
            </Descriptions.Item>
            <Descriptions.Item label="审批人">{emptyPlain(task?.approved_by)}</Descriptions.Item>
            <Descriptions.Item label="审批意见 / 驳回原因" span={3}>
              {emptyPlain(task?.approval_comment)}
            </Descriptions.Item>
            <Descriptions.Item label="提交审批时间">{fmtTime(task?.submitted_at)}</Descriptions.Item>
            <Descriptions.Item label="通过时间">{fmtTime(task?.approved_at)}</Descriptions.Item>
            <Descriptions.Item label="驳回时间">{fmtTime(task?.rejected_at)}</Descriptions.Item>
          </Descriptions>
        </Card>

        <Card bordered>
          <Tabs
            activeKey={tab}
            onChange={setTab}
            items={[
              {
                key: 'overview',
                label: '概览',
                children: (
                  <>
                    <div className="overview-block">
                      <div className="block-title">总体进度</div>
                      <Progress percent={overviewPct} strokeWidth={12} status={progressStatus} />
                      <div className="muted">
                        对象 {overviewDone} / {overviewTotal}
                      </div>
                    </div>
                    <div className="overview-block">
                      <div className="block-title">数据统计与速率</div>
                      <div className="stat-row">
                        <div className="stat">
                          <div className="stat-label">行数</div>
                          <div className="stat-value">
                            {prog?.completed_rows ?? task?.completed_rows ?? 0} /{' '}
                            {prog?.total_rows ?? task?.total_rows ?? 0}
                          </div>
                        </div>
                        <div className="stat">
                          <div className="stat-label">数据量（字节）</div>
                          <div className="stat-value">
                            {prog?.completed_bytes ?? task?.completed_bytes ?? 0} / {byteTotal}
                          </div>
                        </div>
                        <div className="stat">
                          <div className="stat-label">行/秒</div>
                          <div className="stat-value">
                            {Number(prog?.speed_rows_per_sec || 0).toFixed(1)}
                          </div>
                        </div>
                        <div className="stat">
                          <div className="stat-label">MB/s</div>
                          <div className="stat-value">{mbps.toFixed(2)}</div>
                        </div>
                      </div>
                    </div>
                    <div className="overview-block">
                      <div className="block-title">耗时与当前对象</div>
                      <Descriptions column={1} size="small">
                        <Descriptions.Item label="已用时间（秒）">
                          {emptyNum(prog?.elapsed_seconds)}
                        </Descriptions.Item>
                        <Descriptions.Item label="预估剩余（秒）">
                          {emptyNum(prog?.estimated_remaining_seconds)}
                        </Descriptions.Item>
                        <Descriptions.Item label="当前对象">
                          {emptyPlain(prog?.current_object)}
                        </Descriptions.Item>
                      </Descriptions>
                    </div>
                  </>
                ),
              },
              {
                key: 'objects',
                label: '对象明细',
                children: (
                  <>
                    <Select
                      allowClear
                      placeholder="按状态筛选"
                      style={{ width: 160, marginBottom: 12 }}
                      value={objStatus || undefined}
                      onChange={(v) => setObjStatus(v || '')}
                      options={[
                        { label: '待迁移', value: 'pending' },
                        { label: '迁移中', value: 'running' },
                        { label: '已完成', value: 'completed' },
                        { label: '失败', value: 'failed' },
                        { label: '跳过', value: 'skipped' },
                      ]}
                    />
                    <Table
                      rowKey={(_, idx) => String(idx)}
                      bordered
                      pagination={false}
                      dataSource={filteredObjects}
                      locale={{ emptyText: '暂无对象' }}
                      columns={[
                        {
                          title: '源端库/表',
                          ellipsis: true,
                          render: (_: any, row: any) => objectName(row, 'source'),
                        },
                        {
                          title: '目标端库/表',
                          ellipsis: true,
                          render: (_: any, row: any) => objectName(row, 'target'),
                        },
                        {
                          title: '状态',
                          width: 100,
                          render: (_: any, row: any) => (
                            <Tag color={objStatusTagColor(row.status)}>{objStatusText(row.status)}</Tag>
                          ),
                        },
                        {
                          title: '行数进度',
                          width: 180,
                          render: (_: any, row: any) =>
                            `${row.completed_rows || 0} / ${row.total_rows || 0} (${rowPct(row)}%)`,
                        },
                        {
                          title: '数据量',
                          width: 180,
                          render: (_: any, row: any) =>
                            `${row.completed_bytes || 0} / ${row.total_bytes || 0}`,
                        },
                        {
                          title: '错误信息',
                          dataIndex: 'error_message',
                          ellipsis: true,
                        },
                      ]}
                    />
                  </>
                ),
              },
              {
                key: 'logs',
                label: '日志',
                children: (
                  <>
                    <div className="log-tools">
                      <Radio.Group
                        size="small"
                        value={logLevel}
                        onChange={(e) => setLogLevel(e.target.value)}
                      >
                        <Radio.Button value="">全部</Radio.Button>
                        <Radio.Button value="INFO">INFO</Radio.Button>
                        <Radio.Button value="WARN">WARN</Radio.Button>
                        <Radio.Button value="ERROR">ERROR</Radio.Button>
                      </Radio.Group>
                      <Input
                        allowClear
                        placeholder="关键字过滤"
                        style={{ width: 200 }}
                        value={logKw}
                        onChange={(e) => setLogKw(e.target.value)}
                      />
                      <Button onClick={exportLog}>导出日志</Button>
                    </div>
                    <div className="log-box">
                      {pagedLogs.map((l) => (
                        <div key={l.id} className="log-line">
                          <span className="muted">{l.created_at}</span>
                          <span className={logLevelClass(l.log_level)}> [{l.log_level}] </span>
                          <span>{l.message}</span>
                          {l.detail && <span> — {l.detail}</span>}
                        </div>
                      ))}
                      {!pagedLogs.length && <div className="muted">暂无日志</div>}
                    </div>
                    <div className="log-pager">
                      <Pagination
                        size="small"
                        current={logPage}
                        pageSize={logPageSize}
                        total={filteredLogs.length}
                        showSizeChanger
                        pageSizeOptions={['10', '20', '50', '100']}
                        showTotal={(t) => `共 ${t} 条`}
                        onChange={(p, ps) => {
                          setLogPage(p)
                          setLogPageSize(ps)
                        }}
                      />
                    </div>
                  </>
                ),
              },
              {
                key: 'precheck',
                label: '预检',
                children: (
                  <Table
                    rowKey={(_, idx) => String(idx)}
                    bordered
                    pagination={false}
                    dataSource={prechecks}
                    locale={{ emptyText: '暂无预检记录' }}
                    columns={[
                      { title: '检查项', dataIndex: 'check_item', width: 180 },
                      { title: '级别', dataIndex: 'check_level', width: 100 },
                      {
                        title: '状态',
                        width: 120,
                        render: (_: any, row: any) => (
                          <Tag color={precheckTagColor(row.check_status)}>{row.check_status}</Tag>
                        ),
                      },
                      {
                        title: '详情',
                        render: (_: any, row: any) => row.message || row.detail || '—',
                      },
                    ]}
                  />
                ),
              },
              {
                key: 'verify',
                label: '校验',
                children: !verifyEnabled ? (
                  <div className="muted">任务完成后可在此触发数据校验。</div>
                ) : (
                  <>
                    <Button type="primary" loading={verifying} onClick={triggerVerify}>
                      触发校验
                    </Button>
                    <Table
                      className="mt"
                      rowKey={(_, idx) => String(idx)}
                      bordered
                      pagination={false}
                      dataSource={verifyRows}
                      locale={{ emptyText: '暂无校验结果' }}
                      columns={[
                        { title: '表', dataIndex: 'table' },
                        { title: '校验类型', dataIndex: 'check_type', width: 120 },
                        { title: '状态', dataIndex: 'status', width: 100 },
                        { title: '说明', dataIndex: 'detail', ellipsis: true },
                      ]}
                    />
                  </>
                ),
              },
            ]}
          />
        </Card>
      </div>
    </Spin>
  )
}
