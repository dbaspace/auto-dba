import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Alert, Avatar, Button, Descriptions, Input, Modal, Progress, Select, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { useAppDispatch, useAppSelector } from '../../store'
import { closeTab, openTab, tabKey } from '../../store/tabsSlice'
import CollapsibleSqlBlock from '../../components/CollapsibleSqlBlock'
import DdlExecImpact from './DdlExecImpact'
import { ddlProgressView } from './execProgress'
import DnsChangeDiff, { payloadRows } from '../dns/DnsChangeDiff'
import { countNonEmptyLines, lineVsStatementHint, shouldHintLineVsStatement } from './sqlStatementCount'
import './TicketDetail.css'

const SQL_TYPES = new Set(['ddl', 'dml'])

const me: any = (() => {
  try {
    return JSON.parse(localStorage.getItem('user') || '{}')
  } catch {
    return {}
  }
})()

function fmtTime(v: any): string {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function userInitial(name: any): string {
  const s = String(name || '').trim()
  return s ? s.slice(0, 1).toUpperCase() : '?'
}

function isPrivileged(): boolean {
  return ['admin', 'platform_admin', 'super_admin', 'operator', 'dba'].includes(
    String(me.role || '').toLowerCase(),
  )
}

function filenameFromHeader(header: any, fallback: string): string {
  const raw = String(header || '')
  const star = raw.match(/filename\*=UTF-8''([^;]+)/i)
  if (star?.[1]) {
    try {
      return decodeURIComponent(star[1])
    } catch {
      /* ignore */
    }
  }
  const plain = raw.match(/filename="?([^";]+)"?/i)
  return plain?.[1] || fallback
}

function exportMime(format: string): string {
  switch (String(format || '').toLowerCase()) {
    case 'json':
      return 'application/json;charset=utf-8'
    case 'sql':
      return 'application/sql;charset=utf-8'
    default:
      return 'text/csv;charset=utf-8'
  }
}

/** 解析导出 blob：csv/json/sql 共用；区分错误 JSON 与成功文件（含 json 成功也是 application/json） */
async function resolveExportBlob(
  data: any,
  contentType: string,
  contentDisposition: string,
  format: string,
): Promise<{ blob: Blob } | { error: string }> {
  const mime = exportMime(format)
  let blob = data instanceof Blob ? data : new Blob([data], { type: mime })
  const ctype = String(contentType || '')
  const hasFile = /filename/i.test(String(contentDisposition || ''))

  // 成功导出会带 Content-Disposition；错误体多为 JSON 且无文件名
  // json 成功也是 application/json，不能只靠 Content-Type：有文件名则直接当下载体
  // csv/sql 若误标成 application/json（无文件名或错类型）也要能读出 message
  const maybeErrorJSON = !hasFile || (ctype.includes('application/json') && format !== 'json')

  if (maybeErrorJSON) {
    const text = await blob.text()
    try {
      const parsed = JSON.parse(text)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed) && parsed.message) {
        return { error: String(parsed.message) }
      }
    } catch {
      /* csv/sql/json 文件正文 */
    }
    return { blob: new Blob([text], { type: ctype || mime }) }
  }

  // 有文件名的成功响应：统一补齐 MIME，避免浏览器把 json/sql 存成错误扩展名
  if (!blob.type || blob.type === 'application/octet-stream') {
    blob = new Blob([blob], { type: mime })
  }
  return { blob }
}

interface ApplyField {
  key: string
  label: string
  value: any
  kind?: string
  span?: number
}

function DmlSourceInfo({ value }: { value: any }) {
  const domain = String(value?.domain || '').trim()
  const name = String(value?.name || '').trim()
  const host = String(value?.host || '').trim()
  const port = Number(value?.port || 0)
  const addr = host ? (port > 0 ? `${host}:${port}` : host) : ''
  const title = domain || name || '-'
  return (
    <div className="apply-source">
      <div className="apply-source-title">{title}</div>
      {name && domain ? <div className="apply-source-sub">实例 {name}</div> : null}
      {addr ? <div className="apply-source-sub">{addr}</div> : null}
    </div>
  )
}

function DDLSourceList({ sources }: { sources: string[] }) {
  const [open, setOpen] = useState(false)
  const fold = sources.length > 2
  const shown = fold && !open ? sources.slice(0, 2) : sources
  const rest = sources.length - 2
  return (
    <div>
      {shown.map((s, i) => (
        <div key={`${s}-${i}`}>{s}</div>
      ))}
      {fold ? (
        <Button type="link" size="small" style={{ padding: 0, height: 'auto' }} onClick={() => setOpen((v) => !v)}>
          {open ? '收起' : `展开其余 ${rest} 个`}
        </Button>
      ) : null}
    </div>
  )
}

function parseDDLApplyExtra(raw: string) {
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const sources = (Array.isArray(parsed.data_sources) ? parsed.data_sources : [])
      .map((s: any) => String(s?.domain || '').trim())
      .filter(Boolean)
    return {
      sources,
      shardDB: !!parsed.shard_db,
      shardDBPrefix: String(parsed.shard_db_prefix || '').trim(),
      shardTable: !!parsed.shard_table,
      shardTablePrefix: String(parsed.shard_table_prefix || '').trim(),
      constraintCheckSkipped: !!parsed.constraint_check_skipped,
    }
  } catch {
    return null
  }
}

export default function TicketDetail() {
  const navigate = useNavigate()
  const location = useLocation()
  const params = useParams()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)

  const backToTicketList = () => {
    const detailKey = tabKey(location.pathname, location.search)
    dispatch(openTab({ path: '/ops/tickets', menus, title: '工单列表' }))
    dispatch(closeTab(detailKey))
    navigate('/ops/tickets')
  }

  const goCdcApprovalResult = () => {
    const detailKey = tabKey(location.pathname, location.search)
    dispatch(openTab({ path: '/ops/cdc', menus, title: 'CDC 同步任务' }))
    dispatch(closeTab(detailKey))
    navigate('/ops/cdc?tab=approval_result')
  }

  const [item, setItem] = useState<any>(null)
  const [canApproveFlag, setCanApproveFlag] = useState(false)
  const [canStop, setCanStop] = useState(false)
  const [canTransferFlag, setCanTransferFlag] = useState(false)
  const [inst, setInst] = useState<any>(null)
  const [envByInstanceId, setEnvByInstanceId] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [acting, setActing] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [comment, setComment] = useState('')
  const [fmtVisible, setFmtVisible] = useState(false)
  const [fileFormat, setFileFormat] = useState('csv')
  const [transferOpen, setTransferOpen] = useState(false)
  const [transferBusy, setTransferBusy] = useState(false)
  const [transferTo, setTransferTo] = useState<string | undefined>()
  const [approverOptions, setApproverOptions] = useState<{ value: string; label: string }[]>([])
  const [transferLoading, setTransferLoading] = useState(false)
  const [transferLoadFailed, setTransferLoadFailed] = useState(false)
  const envFetchedRef = useRef<Set<string>>(new Set())

  // 使用 ref 存放最新 item，供导出等异步逻辑读取，避免闭包过期
  const itemRef = useRef<any>(null)
  itemRef.current = item

  const workflowName = useMemo(() => {
    const it = item
    if (!it) return '-'
    const prefix = String(it.issue_type || 'TK').toUpperCase()
    return `${prefix}-${it.ticket_no || it.id}`
  }, [item])

  const dbType = useMemo(() => String(inst?.db_type || '').toLowerCase(), [inst])
  const dbEnv = useMemo(() => inst?.idc || '', [inst])
  const dbPath = useMemo(() => {
    const env = dbEnv || '-'
    const name = item?.db_name || '-'
    return `${env} / ${name}`
  }, [dbEnv, item])

  const showSqlImpact = useMemo(() => SQL_TYPES.has(item?.issue_type), [item])

  const dqlExtra = useMemo(() => {
    const raw = item?.need_rollback
    if (!raw || item?.issue_type !== 'dql') return null
    try {
      const parsed = JSON.parse(raw)
      return parsed && typeof parsed === 'object' ? parsed : null
    } catch {
      return null
    }
  }, [item])

  const createdbExtra = useMemo(() => {
    const raw = item?.need_rollback
    if (!raw || item?.issue_type !== 'createdb') return null
    try {
      const parsed = JSON.parse(raw)
      return parsed && typeof parsed === 'object' ? parsed : null
    } catch {
      return null
    }
  }, [item])

  const resourceApplyExtra = useMemo(() => {
    const raw = item?.sql_content
    if (!raw || item?.issue_type !== 'resource_apply') return null
    try {
      const parsed = JSON.parse(raw)
      return parsed && typeof parsed === 'object' ? parsed : null
    } catch {
      return null
    }
  }, [item])

  const applyFields = useMemo<ApplyField[]>(() => {
    const it = item
    if (!it) return []
    if (it.issue_type === 'dml') {
      const domain = String(it.domain || inst?.domain_name || '').trim()
      const name = String(inst?.name || '').trim()
      const host = String(inst?.host || '').trim()
      const port = Number(inst?.port || 0)
      return [
        { key: 'reason', label: '原因', value: it.description || '-' },
        { key: 'env', label: '环境', value: dbEnv || '-' },
        { key: 'db_type', label: '数据库类型', kind: 'type-tag', value: dbType || '-' },
        {
          key: 'source',
          label: '数据源',
          kind: 'dml-source',
          value: { domain, name, host, port },
        },
        { key: 'db', label: '数据库', kind: 'db-tag', value: it.db_name || '-' },
        { key: 'sql', label: '执行SQL', kind: 'sql', value: it.sql_content || '' },
        {
          key: 'schedule',
          label: 'SQL执行时间',
          kind: 'schedule',
          value: it.scheduled_at ? `定时 ${fmtTime(it.scheduled_at)}` : '审批完立即执行',
        },
        { key: 'created', label: '申请时间', value: fmtTime(it.created_at) },
      ]
    }
    const isResourceApply = it.issue_type === 'resource_apply'
    const fields: ApplyField[] = [
      { key: 'reason', label: '原因', value: it.description || resourceApplyExtra?.reason || '-' },
    ]
    if (it.issue_type === 'dns_change') {
      const p = payloadRows(it.sql_content || '')
      if (p.kind === 'rollback') fields.push({ key: 'dns_batch', label: '回滚批次', value: p.batchId || '-' })
      fields.push({ key: 'dns_diff', label: '变更内容', kind: 'dns-diff', value: it.sql_content || '' })
      fields.push({
        key: 'schedule',
        label: '执行方式',
        kind: 'schedule',
        value: '审批通过后自动执行 DNS 变更（执行前重新校验并检查冲突）',
      })
      return fields
    }
    if (it.issue_type === 'cloud_pg' || it.issue_type === 'cloud_mysql' || it.issue_type === 'cloud_redis' || it.issue_type === 'cloud_host') {
      let p: any = {}
      try { p = JSON.parse(it.sql_content || '{}') } catch { p = {} }
      fields.push({ key: 'action', label: '动作', value: p.action || '-' })
      fields.push({ key: 'before', label: '变更前', value: `${p.before_cpu || 0}核 / ${p.before_memory_mb || 0}MB / ${p.before_disk_gb || 0}GB` })
      fields.push({ key: 'after', label: '变更后', value: `${p.cpu || 0}核 / ${p.memory_mb || 0}MB / ${p.disk_gb || 0}GB` })
      fields.push({ key: 'impact', label: '影响', value: `${p.impact || '-'}；${p.blip ? '会造成闪断' : '默认不闪断'}` })
      fields.push({ key: 'quote', label: '询价差额', value: p.quote?.amount ?? '-' })
      fields.push({ key: 'task', label: '云上任务', value: p.task_id || '尚未提交' })
      return fields
    }
    if (!isResourceApply) {
      fields.push(
        { key: 'db_type', label: '数据库类型', kind: 'type-tag', value: dbType || '-' },
        { key: 'db', label: '数据库', kind: 'db-tag', value: dbPath },
      )
      if (it.issue_type === 'ddl') {
        const ddlExtra = parseDDLApplyExtra(it.need_rollback)
        if (ddlExtra && ddlExtra.sources.length >= 2) {
          fields.push({
            key: 'ddl_sources',
            label: '数据源',
            kind: 'ddl-sources',
            value: ddlExtra.sources,
          })
        }
        if (ddlExtra && (ddlExtra.shardDB || ddlExtra.shardTable)) {
          const lines: string[] = []
          if (ddlExtra.shardDB) {
            lines.push(ddlExtra.shardDBPrefix ? `分库，前缀 ${ddlExtra.shardDBPrefix}` : '分库')
          }
          if (ddlExtra.shardTable) {
            lines.push(ddlExtra.shardTablePrefix ? `分表，前缀 ${ddlExtra.shardTablePrefix}` : '分表')
          }
          fields.push({ key: 'ddl_shard', label: '分库/分表', kind: 'pre', value: lines.join('\n') })
        }
        if (ddlExtra?.constraintCheckSkipped) {
          fields.push({ key: 'ddl_constraint_skipped', label: '约束检查', value: '提交时已关闭 DDL 约束检查' })
        }
      }
    } else if (resourceApplyExtra?.items?.length) {
      const actionMap: Record<string, string> = {
        acl: '同租ACL',
        grant: '跨租授权',
        assign: '划拨',
        approver: '加入库审核人',
      }
      const typeMap: Record<string, string> = {
        database: '库',
        domain: '域名',
        host: '主机',
        sr_cluster: 'SR集群',
        db_deploy_cluster: '部署集群',
      }
      const intentLabel =
        resourceApplyExtra.intent === 'approver' ? '库审核人' : resourceApplyExtra.intent === 'visibility' ? '资源可见' : ''
      if (intentLabel) {
        fields.push({ key: 'ra_intent', label: '申请意图', value: intentLabel })
      }
      const lines = resourceApplyExtra.items
        .map(
          (x: any) =>
            `${typeMap[x.resource_type] || x.resource_type} ${x.display_name || x.resource_id} → ${actionMap[x.action] || x.action}`,
        )
        .join('\n')
      fields.push({ key: 'ra_items', label: '申请资源', kind: 'sql', value: lines })
      const hasApprover = resourceApplyExtra.items.some((x: any) => x.action === 'approver')
      if (hasApprover) {
        const users = Array.from(
          new Set(
            (resourceApplyExtra.items || []).flatMap((x: any) =>
              Array.isArray(x.target_users) ? x.target_users.map(String).filter(Boolean) : [],
            ),
          ),
        )
        fields.push({
          key: 'ra_target_users',
          label: '库审核人',
          value: users.length
            ? users.join('、')
            : '（历史工单未记录目标用户；旧逻辑仅写入提交人）',
        })
      }
      fields.push({
        key: 'schedule',
        label: '执行方式',
        kind: 'schedule',
        value: hasApprover
          ? '审批通过后将指定用户写入库审核人名单（私有库同步开通可见）'
          : '审批通过后自动写入 ACL / grant / 划拨',
      })
    }
    if (it.sql_content && !isResourceApply) {
      const clusterOp =
        it.issue_type === 'sr_cluster' || it.issue_type === 'db_deploy' || it.issue_type === 'ha'
      fields.push({
        key: 'sql',
        label:
          it.issue_type === 'flashback' || it.issue_type === 'cdc_sync' || it.issue_type === 'migration' || clusterOp
            ? '任务摘要'
            : '执行SQL',
        kind: 'sql',
        value: it.sql_content,
      })
      fields.push({
        key: 'schedule',
        label:
          it.issue_type === 'flashback' || it.issue_type === 'cdc_sync' || it.issue_type === 'migration' || clusterOp
            ? '执行方式'
            : 'SQL执行时间',
        kind: 'schedule',
        value:
          it.issue_type === 'flashback'
            ? '审批通过后自动执行闪回解析'
            : it.issue_type === 'cdc_sync'
              ? '审批通过后自动执行 CDC 同步'
              : it.issue_type === 'migration'
                ? '审批通过后由后端按启动方式自动执行'
                : it.issue_type === 'sr_cluster'
                  ? '审批通过后自动执行 StarRocks 集群变更'
                  : it.issue_type === 'db_deploy'
                    ? '审批通过后自动执行数据库集群部署变更'
                    : it.issue_type === 'ha'
                      ? '审批通过后自动执行高可用变更'
                      : it.scheduled_at
                        ? `定时 ${fmtTime(it.scheduled_at)}`
                        : '审批完立即执行',
      })
    }
    if (it.issue_type === 'createdb' && createdbExtra) {
      if (createdbExtra.username)
        fields.push({ key: 'acct', label: '数据库账号', value: createdbExtra.username })
      if (createdbExtra.permissions?.length) {
        const map: Record<string, string> = { r: '读操作', c: '写操作', u: '更新操作', d: '删除操作' }
        fields.push({
          key: 'perms',
          label: '账号权限',
          value: createdbExtra.permissions.map((p: string) => map[p] || p).join('， '),
        })
      }
    }
    if (it.issue_type === 'dql' && dqlExtra) {
      fields.push({
        key: 'sensitive',
        label: '原文导出敏感字段',
        value: dqlExtra.need_sensitive_data ? '开启' : '关闭',
      })
      fields.push({
        key: 'rows',
        label: '预估行数（审批参考）',
        value: String(dqlExtra.row_count ?? '-'),
      })
      if (dqlExtra.dba_assigned_to)
        fields.push({ key: 'dba', label: 'DBA审批人', value: dqlExtra.dba_assigned_to })
      if (dqlExtra.security_assigned_to)
        fields.push({ key: 'sec', label: '安全审批人', value: dqlExtra.security_assigned_to })
      fields.push({
        key: 'approver',
        label: '指定审批人',
        value: dqlExtra.export_assigned_to || it.assigned_to || '-',
      })
    } else {
      fields.push({ key: 'approver', label: '指定审批人', value: it.assigned_to || '-' })
    }
    fields.push({ key: 'created', label: '申请时间', value: fmtTime(it.created_at) })
    return fields
  }, [item, inst, dbType, dbEnv, dbPath, createdbExtra, dqlExtra, resourceApplyExtra])

  const canApprove = useMemo(() => {
    if (item?.status !== 'pending') return false
    if (canApproveFlag) return true
    if (isPrivileged()) return true
    return String(item?.assigned_to || '').toLowerCase() === String(me.username || '').toLowerCase()
  }, [item, canApproveFlag])

  const canCancel = useMemo(() => {
    if (item?.status !== 'pending') return false
    if (isPrivileged()) return true
    return (
      String(item?.submitter_name || '').toLowerCase() === String(me.username || '').toLowerCase()
    )
  }, [item])

  const canTransfer = useMemo(() => {
    if (item?.status !== 'pending') return false
    if (canTransferFlag) return true
    return String(item?.submitter_name || '').toLowerCase() === String(me.username || '').toLowerCase()
  }, [item, canTransferFlag])

  const isDQL = useMemo(() => item?.issue_type === 'dql', [item])
  const dqlMultiStep = useMemo(
    () =>
      !!(
        isDQL &&
        dqlExtra?.dba_assigned_to &&
        dqlExtra?.security_assigned_to
      ),
    [isDQL, dqlExtra],
  )
  const dqlPhase = useMemo(() => {
    if (!dqlMultiStep) return 'done'
    const s = String(item?.status || '')
    // 终态已通过/执行：各审批关均视为完成
    if (s && s !== 'pending' && s !== 'rejected' && s !== 'canceled') return 'done'
    // 驳回/取消：优先用记录的驳回关，避免误标到最后一关
    const rejectedPhase = String(dqlExtra?.rejected_phase || '').toLowerCase()
    if ((s === 'rejected' || s === 'canceled') &&
      (rejectedPhase === 'assignee' || rejectedPhase === 'dba' || rejectedPhase === 'security')) {
      return rejectedPhase
    }
    const p = String(dqlExtra?.dql_approval_phase || '').toLowerCase()
    if (p === 'assignee' || p === 'dba' || p === 'security') return p
    if (p === 'done') return 'done'
    // 多人相同时不能靠 assigned_to 推断；有 approved_by 则推到下一关
    if (dqlExtra?.security_approved_by) return s === 'pending' ? 'done' : 'security'
    if (dqlExtra?.dba_approved_by) return s === 'pending' ? 'security' : 'dba'
    if (dqlExtra?.assignee_approved_by) return s === 'pending' ? 'dba' : 'assignee'
    return 'assignee'
  }, [dqlMultiStep, dqlExtra, item])
  const dqlApprovalSteps = useMemo(() => {
    type Step = { key: string; title: string; user: string; approvedBy: string; rejectedBy?: string }
    if (!isDQL) return [] as Step[]
    const rejectedBy = String(dqlExtra?.rejected_by || '')
    const steps: Step[] = [
      {
        key: 'assignee',
        title: '指定人员审批',
        user: String(dqlExtra?.export_assigned_to || item?.assigned_to || ''),
        approvedBy: String(dqlExtra?.assignee_approved_by || ''),
        rejectedBy: dqlPhase === 'assignee' ? rejectedBy : '',
      },
    ]
    if (dqlMultiStep) {
      steps.push(
        {
          key: 'dba',
          title: 'DBA审批',
          user: String(dqlExtra?.dba_assigned_to || ''),
          approvedBy: String(dqlExtra?.dba_approved_by || ''),
          rejectedBy: dqlPhase === 'dba' ? rejectedBy : '',
        },
        {
          key: 'security',
          title: '安全审批',
          user: String(dqlExtra?.security_assigned_to || ''),
          approvedBy: String(dqlExtra?.security_approved_by || ''),
          rejectedBy: dqlPhase === 'security' ? rejectedBy : '',
        },
      )
    }
    return steps
  }, [isDQL, dqlMultiStep, dqlExtra, item, dqlPhase])
  const dqlStepState = (key: string): 'done' | 'active' | 'wait' | 'danger' => {
    const order = ['assignee', 'dba', 'security']
    const s = item?.status
    if (s === 'rejected' || s === 'canceled') {
      const phase = dqlPhase === 'done' ? 'assignee' : dqlPhase
      const pi = order.indexOf(phase)
      const ki = order.indexOf(key)
      if (ki === pi) return 'danger'
      if (ki >= 0 && pi >= 0 && ki < pi) return 'done'
      return 'wait'
    }
    if (s && s !== 'pending') return 'done'
    if (!dqlMultiStep) return s === 'pending' ? 'active' : 'done'
    if (dqlPhase === 'done') return 'done'
    const ki = order.indexOf(key)
    const pi = order.indexOf(dqlPhase)
    if (ki < pi) return 'done'
    if (ki === pi) return 'active'
    return 'wait'
  }
  const isFlashback = useMemo(() => item?.issue_type === 'flashback', [item])
  const flashbackTaskID = useMemo(() => {
    const m = String(item?.sql_content || '').match(/^flashback\s+([0-9a-fA-F-]{36})/i)
    return m?.[1] || ''
  }, [item])
  const flashbackExecText = useMemo(() => {
    const s = item?.status
    if (s === 'rejected') return '审批已驳回，任务不会执行'
    if (s === 'canceled') return '审批已撤销，任务不会执行'
    return '审批通过后自动执行闪回解析，可在任务页查看进度'
  }, [item])
  const isCDC = useMemo(() => item?.issue_type === 'cdc_sync', [item])
  const cdcTaskID = useMemo(() => {
    const m = String(item?.sql_content || '').match(/^cdc_sync\s+\S+\s+([0-9a-fA-F-]{36})/i)
    return m?.[1] || ''
  }, [item])
  const cdcExecText = useMemo(() => {
    const s = item?.status
    if (s === 'rejected') return '审批已驳回，任务不会执行'
    if (s === 'canceled') return '审批已撤销，任务不会执行'
    return '审批通过后自动执行 CDC 同步，可在任务页查看进度'
  }, [item])
  const isMigration = useMemo(() => item?.issue_type === 'migration', [item])
  const migrationTaskID = useMemo(() => {
    const m = String(item?.sql_content || '').match(/^migration\s+([0-9a-fA-F-]{36})/i)
    return m?.[1] || ''
  }, [item])
  const migrationExecText = useMemo(() => {
    const s = item?.status
    if (s === 'rejected') return '审批已驳回，任务不会执行'
    if (s === 'canceled') return '审批已撤销，任务不会执行'
    return '审批通过后由后端按启动方式自动执行，可在任务页查看进度'
  }, [item])
  const isSubmitter = useMemo(
    () =>
      String(item?.submitter_name || '').toLowerCase() === String(me.username || '').toLowerCase(),
    [item],
  )
  const isCreateDB = useMemo(() => item?.issue_type === 'createdb', [item])

  const execObj = useMemo(() => item?.exec || {}, [item])
  const execSkipped = useMemo(() => Boolean(execObj?.skipped), [execObj])
  const execSummary = useMemo(() => execObj?.summary || execObj?.message || '', [execObj])
  // 失败时优先展示具体错误（message），摘要只是「部分执行后失败」之类的概括
  const execErrorText = useMemo(
    () => String(execObj?.message || execObj?.summary || '').trim(),
    [execObj],
  )
  const sqlStatementCount = useMemo(() => {
    const n = Number(execObj?.statement_count)
    return Number.isFinite(n) && n > 0 ? n : 0
  }, [execObj])
  const sqlLineApprox = useMemo(
    () => (SQL_TYPES.has(item?.issue_type) ? countNonEmptyLines(String(item?.sql_content || '')) : 0),
    [item],
  )
  const execFailed = item?.status === 'failed'
  const execStopped = item?.status === 'stopped'
  const execSuccess = execObj?.success === true ? true : execObj?.success === false ? false : undefined
  const isExecRunning = item?.status === 'running'
  const execRecords = useMemo(
    () => (Array.isArray(execObj?.records) ? execObj.records : []),
    [execObj],
  )
  // running 时隐藏上轮明细，避免「后台执行中」与旧失败/成功结果并读
  const displayExecRecords = useMemo(
    () => (isExecRunning && item?.issue_type !== 'ddl' ? [] : execRecords),
    [isExecRunning, item?.issue_type, execRecords],
  )
  const ddlProgress = useMemo(
    () => (isExecRunning && item?.issue_type === 'ddl' ? ddlProgressView(item?.exec_progress) : null),
    [isExecRunning, item],
  )

  // SR / 通用 DB 集群变更：工单等待变更结束，步骤写在 exec.records
  const isClusterChange = item?.issue_type === 'sr_cluster' || item?.issue_type === 'db_deploy'
  const clusterChangeLink = useMemo(() => {
    const changeId = String(execObj?.change_id || '').trim()
    if (!isClusterChange || !changeId) return ''
    let payload: any = {}
    try {
      payload = JSON.parse(item?.sql_content || '{}') || {}
    } catch {
      /* keep empty */
    }
    const engine = item?.issue_type === 'sr_cluster' ? 'starrocks' : String(payload.engine || '').trim()
    if (!engine) return ''
    const q = new URLSearchParams({ change: changeId })
    if (payload.cluster_id) q.set('cluster', String(payload.cluster_id))
    return `/ops/cluster-deploy/${encodeURIComponent(engine)}?${q.toString()}`
  }, [isClusterChange, execObj, item])

  const ddlDataSourceIds = useMemo(() => {
    const raw = item?.need_rollback
    if (!raw || !SQL_TYPES.has(item?.issue_type)) return [] as string[]
    try {
      const parsed = JSON.parse(raw)
      const ids = Array.isArray(parsed?.data_source_ids) ? parsed.data_source_ids : []
      return ids.map((id: any) => String(id || '').trim()).filter(Boolean)
    } catch {
      return [] as string[]
    }
  }, [item])

  const neededInstanceIds = useMemo(() => {
    if (item?.issue_type === 'resource_apply' || item?.issue_type === 'dns_change') return [] as string[]
    const set = new Set<string>()
    const primary = String(item?.instance_id || '').trim()
    if (primary) set.add(primary)
    for (const id of ddlDataSourceIds) set.add(id)
    for (const r of execRecords) {
      const id = String(r?.instance_id || '').trim()
      if (id) set.add(id)
    }
    return [...set]
  }, [item, ddlDataSourceIds, execRecords])

  const showExecCard = useMemo(() => {
    const s = item?.status
    if (s === 'pending' || s === 'rejected' || s === 'canceled') return false
    if (isCreateDB) {
      // 建库：执行中 / 失败 / 成功均展示执行区（running 时轮询结果）
      return s === 'running' || s === 'failed' || s === 'executed' || s === 'approved'
    }
    return true
  }, [item, isCreateDB])
  const canDownload = useMemo(
    () =>
      isDQL &&
      isSubmitter &&
      (item?.status === 'approved' || item?.status === 'executed'),
    [isDQL, isSubmitter, item],
  )

  const execCardClass = useMemo(() => {
    const s = item?.status
    if (s === 'executed') return 'is-done'
    if (s === 'failed') return 'is-danger'
    if (s === 'stopped') return 'is-danger'
    if (s === 'running') return 'is-active'
    return 'is-wait'
  }, [item])

  const wfExecClass = useMemo(() => {
    const s = item?.status
    if (s === 'approved' || s === 'running') return 'is-current'
    if (s === 'executed') return 'is-ok'
    if (s === 'failed' || s === 'stopped') return 'is-fail'
    return 'is-idle'
  }, [item])

  const wfExecSub = useMemo(() => {
    const s = item?.status
    if (s === 'executed') return isDQL ? '已下载' : execSkipped ? '已跳过连库' : '已执行'
    if (s === 'failed') return '执行失败'
    if (s === 'stopped') return '已终止'
    if (s === 'running') return '后台执行中'
    if (s === 'approved')
      return isDQL
        ? '点击下载'
        : isFlashback || isCDC || isMigration || isCreateDB || item?.issue_type === 'redis'
          ? item?.scheduled_at
            ? '等待定时执行'
            : '已自动执行'
          : '等待执行'
    return '未开始'
  }, [item, isDQL, execSkipped, isFlashback, isCDC, isMigration, isCreateDB])

  const scheduledWaitText = useMemo(
    () =>
      item?.scheduled_at
        ? '已预约，等待定时或手动执行'
        : '审批通过后将自动后台执行；若未触发可手动执行',
    [item],
  )

  const execEmptyText = useMemo(() => {
    if (item?.status === 'executed' || item?.status === 'failed' || item?.status === 'stopped') return '暂无数据'
    if (item?.status === 'running') {
      const base = item?.issue_type === 'ddl' ? '已结束的库会显示在这里' : '正在后台执行，完成后自动刷新结果'
      const hint = String(item?.exec_progress?.last_error || '').trim()
      if (!hint) return base
      return `${base}（${hint.length > 180 ? hint.slice(0, 180) + '…' : hint}）`
    }
    return '暂无数据，执行后获取'
  }, [item])

  async function load() {
    setLoading(true)
    setLoadError('')
    try {
      const { data } = await api.get('/tickets/' + params.id, { silent: true })
      setItem(data.data)
      setCanApproveFlag(Boolean(data.can_approve))
      setCanStop(Boolean(data.can_stop))
      setCanTransferFlag(Boolean(data.can_transfer))
      setInst(null)
      setEnvByInstanceId({})
      envFetchedRef.current = new Set()
      if (data.data?.instance_id) {
        try {
          const instRes = await api.get('/instances/' + data.data.instance_id, { silent: true })
          setInst(instRes.data.data || null)
        } catch {
          /* keep empty */
        }
      }
    } catch (err: any) {
      setItem(null)
      const status = err?.response?.status
      const msg = err?.response?.data?.message || err?.message || '加载失败'
      setLoadError(status === 404 ? '工单不存在或已删除' : msg)
    } finally {
      setLoading(false)
    }
  }

  async function openTransfer() {
    if (!item) return
    setTransferTo(undefined)
    setApproverOptions([])
    setTransferLoadFailed(false)
    setTransferLoading(true)
    setTransferOpen(true)
    try {
      const res = item.issue_type === 'createdb'
        ? await api.get('/create-db/approvers')
        : await api.get('/tickets/target-approvers', {
          params: {
            instance_id: item.instance_id || '',
            db_name: item.db_name || '',
            issue_type: item.issue_type || '',
          },
        })
      const payload = res.data
      const list = Array.isArray(payload) ? payload : payload?.data
      if (!Array.isArray(list)) {
        setTransferLoadFailed(true)
        return
      }
      const names: string[] = list
        .map((x: any) => (typeof x === 'string' ? x : x?.name || x?.username))
        .filter(Boolean)
      // 拆分逗号拼接脏数据后并集去重
      const seen = new Set<string>()
      const uniq: string[] = []
      for (const raw of names) {
        for (const part of String(raw).split(/[,，\s]+/)) {
          const n = part.trim()
          if (!n) continue
          const key = n.toLowerCase()
          if (seen.has(key)) continue
          seen.add(key)
          uniq.push(n)
        }
      }
      const excluded = new Set([
        String(item.assigned_to || '').toLowerCase(),
        String(item.submitter_name || '').toLowerCase(),
      ])
      setApproverOptions(
        uniq
          .filter((n) => !excluded.has(n.toLowerCase()))
          .map((n) => ({ value: n, label: n })),
      )
    } catch (e: any) {
      setTransferLoadFailed(true)
      message.error(e?.response?.data?.message || e.message || '加载候选人失败')
    } finally {
      setTransferLoading(false)
    }
  }

  async function submitTransfer() {
    if (!item || !transferTo) {
      message.warning('请选择新审核人')
      return
    }
    setTransferBusy(true)
    try {
      await api.put(`/tickets/${item.id}/assignee`, { assigned_to: transferTo })
      message.success('已更换审核人')
      setTransferOpen(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message)
    } finally {
      setTransferBusy(false)
    }
  }

  function openExport() {
    if (!canDownload) {
      message.warning('仅限工单发起人可下载')
      return
    }
    setFileFormat('csv')
    setFmtVisible(true)
  }

  async function doExport() {
    setDownloading(true)
    try {
      const it = itemRef.current
      if (!it?.id) {
        message.error('工单不存在')
        return
      }
      const res = await api.post(
        `/tickets/${it.id}/export`,
        { file_format: fileFormat },
        { responseType: 'blob', timeout: 120000 },
      )
      const resolved = await resolveExportBlob(
        res.data,
        String(res.headers['content-type'] || ''),
        String(res.headers['content-disposition'] || ''),
        fileFormat,
      )
      if ('error' in resolved) {
        message.error(resolved.error)
        return
      }
      const { blob } = resolved
      const name = filenameFromHeader(
        res.headers['content-disposition'],
        `dql_export.${fileFormat}`,
      )
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = name
      a.click()
      URL.revokeObjectURL(url)
      setFmtVisible(false)
      message.success('开始下载')
      setDownloading(false)
      void load()
    } catch (e: any) {
      /* axios interceptor already toasts most errors */
      if (!e?.response) {
        message.error(e?.message || '下载失败')
      }
    } finally {
      setDownloading(false)
    }
  }

  function promptRejectReason(): Promise<string | null> {
    return new Promise((resolve) => {
      let val = ''
      Modal.confirm({
        title: '驳回',
        content: (
          <div>
            <div style={{ marginBottom: 8 }}>驳回原因</div>
            <Input placeholder="原因" onChange={(e) => (val = e.target.value)} />
          </div>
        ),
        onOk: () => resolve(val || ''),
        onCancel: () => resolve(null),
      })
    })
  }

  function confirmCancel(): Promise<boolean> {
    return new Promise((resolve) => {
      Modal.confirm({
        title: '取消流程',
        content: '确定取消该流程？',
        type: 'warning' as any,
        onOk: () => resolve(true),
        onCancel: () => resolve(false),
      })
    })
  }

  async function act(kind: string) {
    setActing(true)
    try {
      const it = itemRef.current
      let currentComment = comment
      let okMsg = '已处理'
      if (kind === 'reject') {
        if (!currentComment.trim()) {
          const value = await promptRejectReason()
          if (value === null) {
            return
          }
          currentComment = value || ''
          setComment(currentComment)
        }
        await api.put(`/tickets/${it.id}/reject`, { reason: currentComment })
        if (it.issue_type === 'cdc_sync') {
          message.success('操作成功')
          setComment('')
          goCdcApprovalResult()
          return
        }
      } else if (kind === 'approve') {
        const { data } = await api.put(`/tickets/${it.id}/approve`)
        if (data?.message) okMsg = String(data.message)
        if (it.issue_type === 'cdc_sync') {
          const { data: fresh } = await api.get('/tickets/' + it.id)
          const st = String(fresh?.data?.status || '')
          if (st && st !== 'pending') {
            message.success(okMsg)
            setComment('')
            goCdcApprovalResult()
            return
          }
        }
      } else if (kind === 'cancel') {
        const ok = await confirmCancel()
        if (!ok) {
          return
        }
        await api.put(`/tickets/${it.id}/cancel`)
      } else if (kind === 'execute') {
        await api.post(`/tickets/${it.id}/execute`)
      } else if (kind === 'stop') {
        okMsg = '已终止执行'
        await new Promise<void>((resolve, reject) => {
          Modal.confirm({
            title: '终止执行',
            content: '已完成的语句不会回滚，将按数据库类型终止当前会话，未开始的步骤不再执行。',
            okText: '终止执行',
            okButtonProps: { danger: true },
            cancelText: '取消',
            onOk: () => resolve(),
            onCancel: () => reject('cancel'),
          })
        })
        await api.post(`/tickets/${it.id}/stop`)
      }
      message.success(okMsg)
      setComment('')
      await load()
    } catch (err: any) {
      if (err !== 'cancel' && err !== 'close') {
        message.error(err.response?.data?.message || err.message || '操作失败')
      }
    } finally {
      setActing(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 后台执行中轮询详情，直到成功/失败
  useEffect(() => {
    if (item?.status !== 'running' || !params.id) return
    const timer = window.setInterval(async () => {
      try {
        const { data } = await api.get('/tickets/' + params.id, { silent: true })
        const next = data.data
        if (!next) return
        setItem(next)
        if (typeof data.can_approve === 'boolean') setCanApproveFlag(data.can_approve)
        if (typeof data.can_stop === 'boolean') setCanStop(data.can_stop)
        if (typeof data.can_transfer === 'boolean') setCanTransferFlag(data.can_transfer)
        if (next.status !== 'running') {
          window.clearInterval(timer)
        }
      } catch {
        /* ignore */
      }
    }, 3000)
    return () => window.clearInterval(timer)
  }, [item?.status, params.id])

  // 多实例执行结果：按 instance_id 解析各自 idc，避免全部套用工单主实例环境
  useEffect(() => {
    let cancelled = false
    async function fillEnvByInstance() {
      // 平台资源申请执行记录里的 id 可能是库/资源 ID，勿按实例去拉
      if (item?.issue_type === 'resource_apply') return
      if (!neededInstanceIds.length) return

      const nextSeed: Record<string, string> = {}
      if (inst?.id && inst?.idc) {
        const id = String(inst.id).trim()
        const idc = String(inst.idc).trim()
        if (id && idc) {
          nextSeed[id] = idc
          envFetchedRef.current.add(id)
        }
      }

      const missing = neededInstanceIds.filter((id) => !envFetchedRef.current.has(id))
      for (const id of missing) envFetchedRef.current.add(id)

      const fetched = await Promise.all(
        missing.map(async (id) => {
          try {
            const res = await api.get('/instances/' + id, { silent: true })
            const idc = String(res.data?.data?.idc || '').trim()
            return idc ? ([id, idc] as const) : null
          } catch {
            return null
          }
        }),
      )
      if (cancelled) return

      setEnvByInstanceId((prev) => {
        let changed = false
        const next = { ...prev }
        for (const [id, idc] of Object.entries(nextSeed)) {
          if (next[id] !== idc) {
            next[id] = idc
            changed = true
          }
        }
        for (const pair of fetched) {
          if (!pair) continue
          const [id, idc] = pair
          if (next[id] !== idc) {
            next[id] = idc
            changed = true
          }
        }
        return changed ? next : prev
      })
    }
    fillEnvByInstance()
    return () => {
      cancelled = true
    }
  }, [neededInstanceIds, inst])

  return (
    <div className="review-page">
      <div className="back-row">
        <button type="button" className="back-link" onClick={backToTicketList}>
          ← 返回工单列表
        </button>
      </div>
      {!item && !loading && loadError ? (
        <Alert
          type="warning"
          showIcon
          style={{ margin: '16px 0' }}
          message={loadError}
          action={
            <Button size="small" type="link" onClick={backToTicketList}>
              关闭并返回列表
            </Button>
          }
        />
      ) : null}
      {item && (
        <div className="review-split">
          <aside className="review-aside">
            <div className="meta-head">
              <div>
                <div className="meta-label">工作流名称：</div>
                <div className="meta-value">{workflowName}</div>
              </div>
              <Button type="primary" size="small" disabled={!canCancel} onClick={() => act('cancel')}>
                取消流程
              </Button>
            </div>
            <div className="meta-block">
              <div className="meta-label">发起时间：</div>
              <div className="meta-value">{fmtTime(item.created_at)}</div>
            </div>
            <div className="meta-block">
              <div className="meta-label">发起人</div>
              <div className="user-card">
                <Avatar size={36} className="user-avatar">
                  {userInitial(item.submitter_name)}
                </Avatar>
                <div>
                  <div className="user-name">{item.submitter_name || '-'}</div>
                  <div className="user-sub">提交人</div>
                </div>
              </div>
            </div>

            <div className="meta-label">执行流程</div>
            {(dqlApprovalSteps.length > 0 ? dqlApprovalSteps : [{
              key: 'assignee',
              title: '指定人员审批',
              user: String(item.assigned_to || ''),
              approvedBy: '',
            }]).map((step) => {
              const state = isDQL ? dqlStepState(step.key) : (
                item.status === 'pending' ? 'active' : item.status === 'rejected' || item.status === 'canceled' ? 'danger' : 'done'
              )
              const cardClass =
                state === 'active' ? 'is-active' : state === 'danger' ? 'is-danger' : state === 'done' ? 'is-done' : 'is-wait'
              const isCurrent = state === 'active' && item.status === 'pending'
              const displayUser = isCurrent
                ? (item.assigned_to || step.user)
                : (step.rejectedBy || step.approvedBy || step.user)
              return (
                <div key={step.key} className={`flow-card ${cardClass}`}>
                  <div className="flow-card-head">{step.title}</div>
                  <div className="flow-card-body">
                    {isCurrent ? (
                      <>
                        <div className="approver-line">
                          审批人员：{item.assigned_to || step.user || '-'}
                          {canTransfer && (
                            <Button type="link" size="small" style={{ padding: '0 0 0 8px' }} onClick={() => { void openTransfer() }}>
                              更换审核人
                            </Button>
                          )}
                        </div>
                        <div className="timeout-hint">提交后超过 8 小时未审核完成，系统将自动取消</div>
                        <div className="remark-box">
                          <div className="remark-label">附言：</div>
                          <Input.TextArea
                            value={comment}
                            onChange={(e) => setComment(e.target.value)}
                            rows={3}
                            placeholder="请输入附言"
                          />
                          <div className="remark-actions">
                            <Button
                              danger
                              size="small"
                              disabled={!canApprove}
                              loading={acting}
                              onClick={() => act('reject')}
                            >
                              驳回
                            </Button>
                            <Button
                              type="primary"
                              size="small"
                              disabled={!canApprove}
                              loading={acting}
                              onClick={() => act('approve')}
                            >
                              通过
                            </Button>
                          </div>
                        </div>
                      </>
                    ) : (
                      <>
                        <div className="approve-result">
                          <div className="user-card compact">
                            <Avatar size={28}>{userInitial(displayUser)}</Avatar>
                            <span>{displayUser || '-'}</span>
                          </div>
                          {state === 'danger' ? (
                            <span className="status-text danger">
                              {item.status === 'canceled' ? '已取消' : '已驳回'}
                            </span>
                          ) : state === 'done' ? (
                            <span className="status-text success">已通过</span>
                          ) : (
                            <span className="status-text muted">待审批</span>
                          )}
                        </div>
                        {state === 'danger' && item.reject_reason && (
                          <div className="remark-text">附言：{item.reject_reason}</div>
                        )}
                      </>
                    )}
                  </div>
                </div>
              )
            })}

            {showExecCard && (
              <div className={`flow-card ${execCardClass}`}>
                <div className="flow-card-head">执行操作</div>
                {isFlashback ? (
                  <div className="flow-card-body exec-body">
                    <div className="exec-wait">{flashbackExecText}</div>
                    {flashbackTaskID && (
                      <Button
                        type="primary"
                        size="small"
                        onClick={() => navigate('/ops/flashback/history/' + flashbackTaskID)}
                      >
                        查看闪回任务
                      </Button>
                    )}
                  </div>
                ) : isCDC ? (
                  <div className="flow-card-body exec-body">
                    <div className="exec-wait">{cdcExecText}</div>
                    {cdcTaskID && (
                      <Button
                        type="primary"
                        size="small"
                        onClick={() => navigate('/ops/cdc/' + cdcTaskID)}
                      >
                        查看 CDC 任务
                      </Button>
                    )}
                  </div>
                ) : isMigration ? (
                  <div className="flow-card-body exec-body">
                    <div className="exec-wait">{migrationExecText}</div>
                    {migrationTaskID && (
                      <Button
                        type="primary"
                        size="small"
                        onClick={() => navigate('/ops/migration/' + migrationTaskID)}
                      >
                        查看迁移任务
                      </Button>
                    )}
                  </div>
                ) : isDQL ? (
                  <div className="flow-card-body download-body">
                    <div className="download-row">
                      <span className={item.status === 'executed' ? 'status-text success' : 'exec-wait'}>
                        {item.status === 'executed' ? '已下载' : '点击下载'}
                      </span>
                      <Button
                        type="primary"
                        disabled={!canDownload}
                        loading={downloading}
                        onClick={openExport}
                      >
                        {item.status === 'executed' ? '再次下载' : '下载'}
                      </Button>
                    </div>
                    {item.status === 'executed' ? (
                      <div className="download-hint">{fmtTime(item.updated_at) || '可再次下载'}</div>
                    ) : (
                      <div className="download-hint">仅限工单发起人可下载</div>
                    )}
                  </div>
                ) : (
                  <div className="flow-card-body exec-body">
                    {item.status === 'running' && isClusterChange ? (
                      <>
                        <div className="exec-wait">集群变更执行中…</div>
                        <div className="user-sub">{execObj?.summary || '等待变更结束后同步最终状态'}</div>
                        {canStop ? (
                          <Button danger size="small" loading={acting} onClick={() => act('stop')}>
                            终止执行
                          </Button>
                        ) : null}
                      </>
                    ) : item.status === 'running' ? (
                      <>
                        <div className="exec-wait">{ddlProgress?.title || '正在后台执行，请勿重复点击'}</div>
                        {ddlProgress ? (
                          <Progress
                            percent={ddlProgress.percent}
                            status="active"
                            showInfo={!ddlProgress.indeterminate}
                            style={{ margin: '8px 0' }}
                          />
                        ) : null}
                        {ddlProgress?.sub ? <div className="user-sub">{ddlProgress.sub}</div> : <div className="user-sub">页面将自动刷新执行结果</div>}
                        {canStop ? (
                          <Button danger size="small" loading={acting} onClick={() => act('stop')}>
                            终止执行
                          </Button>
                        ) : null}
                      </>
                    ) : item.status === 'stopped' ? (
                      <>
                        <div className="status-text danger">已终止</div>
                        <div className="user-sub">{execSummary || fmtTime(item.updated_at)}</div>
                        <Button type="primary" size="small" loading={acting} onClick={() => act('execute')}>
                          重试执行
                        </Button>
                      </>
                    ) : item.status === 'approved' ? (
                      <>
                        <div className="exec-wait">{scheduledWaitText}</div>
                        <Button
                          type="primary"
                          size="small"
                          loading={acting}
                          onClick={() => act('execute')}
                        >
                          立即执行
                        </Button>
                      </>
                    ) : item.status === 'failed' ? (
                      <>
                        <div className="status-text danger">执行失败</div>
                        {execObj?.summary && execObj.summary !== execErrorText ? (
                          <div className="user-sub">{execObj.summary}</div>
                        ) : null}
                        <div className="user-sub exec-fail-msg" title={execErrorText}>
                          {execErrorText || fmtTime(item.updated_at)}
                        </div>
                        <Button
                          type="primary"
                          size="small"
                          loading={acting}
                          onClick={() => act('execute')}
                        >
                          重试执行
                        </Button>
                      </>
                    ) : item.status === 'executed' ? (
                      <>
                        <div className="status-text success">
                          {execSkipped ? '已跳过连库执行' : '执行完成'}
                        </div>
                        <div className="user-sub">{execSummary || fmtTime(item.updated_at)}</div>
                      </>
                    ) : null}
                    {clusterChangeLink && (
                      <Button size="small" onClick={() => navigate(clusterChangeLink)}>
                        查看变更日志
                      </Button>
                    )}
                  </div>
                )}
              </div>
            )}

            <div className="wf-title">工作流</div>
            <div className="wf-graph">
              {(dqlApprovalSteps.length > 0 ? dqlApprovalSteps : [{
                key: 'assignee',
                title: '指定人员审批',
                user: '',
                approvedBy: '',
              }]).map((step, idx, arr) => {
                const state = isDQL ? dqlStepState(step.key) : (
                  item.status === 'pending' ? 'active' : item.status === 'rejected' || item.status === 'canceled' ? 'danger' : 'done'
                )
                const nodeClass =
                  state === 'active' ? 'is-current' : state === 'danger' ? 'is-fail' : state === 'done' ? 'is-ok' : 'is-idle'
                const sub =
                  state === 'active' ? '待审批' : state === 'danger'
                    ? (item.status === 'canceled' ? '已取消' : '已驳回')
                    : state === 'done' ? '已通过' : '未开始'
                return (
                  <React.Fragment key={step.key}>
                    {idx > 0 ? <div className="wf-line" /> : null}
                    <div className={`wf-node ${nodeClass}`}>
                      <div className="wf-dot" />
                      <div>
                        <div className="wf-name">{step.title}</div>
                        <div className="wf-sub">{sub}</div>
                      </div>
                    </div>
                    {idx === arr.length - 1 ? (
                      <>
                        <div className="wf-line" />
                        <div className={`wf-node ${wfExecClass}`}>
                          <div className="wf-dot" />
                          <div>
                            <div className="wf-name">执行操作</div>
                            <div className="wf-sub">{wfExecSub}</div>
                          </div>
                        </div>
                      </>
                    ) : null}
                  </React.Fragment>
                )
              })}
            </div>
          </aside>

          <section className="review-main">
            <div className="sec-title">申请信息</div>
            <Descriptions column={1} bordered className="apply-desc">
              {applyFields.map((f) => (
                <Descriptions.Item key={f.key} label={f.label} span={f.span}>
                  {f.kind === 'type-tag' ? (
                    <Tag color="processing">{f.value}</Tag>
                  ) : f.kind === 'db-tag' ? (
                    <Tag color="blue">{f.value}</Tag>
                  ) : f.kind === 'sql' ? (
                    <div>
                      {sqlStatementCount > 0 ? (
                        <div className="user-sub" style={{ marginBottom: 8 }}>
                          共 {sqlStatementCount} 条语句（按分号拆分）
                          {shouldHintLineVsStatement(sqlLineApprox, sqlStatementCount)
                            ? `；${lineVsStatementHint(sqlLineApprox, sqlStatementCount)}`
                            : ''}
                        </div>
                      ) : null}
                      <CollapsibleSqlBlock value={f.value} />
                    </div>
                  ) : f.kind === 'dns-diff' ? (
                    <DnsChangeDiff rows={payloadRows(f.value).rows} />
                  ) : f.kind === 'schedule' ? (
                    <Tag color="warning">{f.value}</Tag>
                  ) : f.kind === 'dml-source' ? (
                    <DmlSourceInfo value={f.value} />
                  ) : f.kind === 'ddl-sources' ? (
                    <DDLSourceList sources={Array.isArray(f.value) ? f.value.map(String) : []} />
                  ) : f.kind === 'pre' ? (
                    <div style={{ whiteSpace: 'pre-wrap' }}>{f.value}</div>
                  ) : (
                    <span>{f.value}</span>
                  )}
                </Descriptions.Item>
              ))}
            </Descriptions>

            {isClusterChange && showExecCard && (
              <>
                <div className="sec-title mt">
                  集群变更步骤
                  {execObj?.change_id ? (
                    <span className="user-sub" style={{ marginLeft: 8, fontWeight: 400 }}>
                      change_id {String(execObj.change_id).slice(0, 8)}
                    </span>
                  ) : null}
                </div>
                <div className="impact-card">
                  {execFailed && execErrorText ? (
                    <Alert type="error" showIcon style={{ marginBottom: 8 }} message={execErrorText} />
                  ) : null}
                  <ResponsiveTable
                    dataSource={execRecords}
                    bordered
                    size="small"
                    rowKey={(r: any, i) => String(r?.order ?? i)}
                    pagination={false}
                    locale={{ emptyText: execEmptyText }}
                    columns={[
                      { title: '#', dataIndex: 'order', width: 56 },
                      { title: '步骤', dataIndex: 'sql', onCell: () => ({ style: { minWidth: 220 } }) },
                      {
                        title: '状态',
                        dataIndex: 'status',
                        width: 100,
                        render: (s: string) => (
                          <Tag color={s === 'success' ? 'success' : s === 'failed' ? 'error' : s === 'running' ? 'processing' : 'default'}>
                            {s || '-'}
                          </Tag>
                        ),
                      },
                      { title: '开始', dataIndex: 'started_at', width: 170, render: (v: string) => fmtTime(v) },
                      { title: '结束', dataIndex: 'ended_at', width: 170, render: (v: string) => fmtTime(v) },
                      {
                        title: '错误',
                        dataIndex: 'error_message',
                        render: (v: string) => (v ? <pre className="cluster-step-err">{v}</pre> : '-'),
                      },
                    ]}
                  />
                </div>
              </>
            )}

            {showSqlImpact && (
              <>
                <div className="sec-title mt">执行SQL影响</div>
                <div className="impact-card">
                  <DdlExecImpact
                    records={displayExecRecords}
                    env={dbEnv}
                    envByInstanceId={envByInstanceId}
                    fallbackDb={item?.db_name}
                    fallbackHost={inst?.host}
                    execMessage={isExecRunning ? '' : execErrorText}
                    execSuccess={isExecRunning || execStopped ? undefined : execSuccess}
                    execFailed={isExecRunning || execStopped ? false : execFailed}
                    bannerTitle={
                      isExecRunning && ddlProgress
                        ? Number(item?.exec_progress?.total || 0) > 1
                          ? `执行中，已完成 ${item.exec_progress?.done || 0}/${item.exec_progress?.total} 个库`
                          : ddlProgress.title
                        : undefined
                    }
                    rollbackSummary={
                      isExecRunning || !execObj?.rolled_back ? '' : String(execObj?.summary || '')
                    }
                    emptyText={execEmptyText}
                    successCount={
                      isExecRunning ? undefined : Number(execObj?.success_count)
                    }
                    statementCount={
                      isExecRunning ? undefined : Number(execObj?.statement_count)
                    }
                    compacted={isExecRunning ? false : Boolean(execObj?.compacted)}
                  />
                </div>
              </>
            )}
          </section>
        </div>
      )}

      <Modal
        open={fmtVisible}
        title="选择下载格式"
        width={420}
        destroyOnClose
        onCancel={() => setFmtVisible(false)}
        footer={[
          <Button key="cancel" onClick={() => setFmtVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={downloading} onClick={doExport}>
            确认
          </Button>,
        ]}
      >
        <div className="fmt-label">请选择导出格式：</div>
        <Select
          value={fileFormat}
          onChange={(v) => setFileFormat(v)}
          style={{ width: '100%' }}
          options={[
            { label: 'CSV', value: 'csv' },
            { label: 'JSON', value: 'json' },
            { label: 'SQL', value: 'sql' },
          ]}
        />
      </Modal>

      <Modal
        open={transferOpen}
        title="更换审核人"
        destroyOnClose
        confirmLoading={transferBusy}
        onCancel={() => setTransferOpen(false)}
        onOk={() => { void submitTransfer() }}
        okText="确认更换"
        okButtonProps={{ disabled: transferLoading || approverOptions.length === 0 }}
      >
        <div style={{ marginBottom: 8 }}>当前审核人：{item?.assigned_to || '-'}</div>
        {!transferLoading && transferLoadFailed && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 8 }}
            message="候选人加载失败，请重试"
            action={<Button size="small" type="link" onClick={() => { void openTransfer() }}>重试</Button>}
          />
        )}
        {!transferLoading && !transferLoadFailed && approverOptions.length === 0 && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 8 }}
            message="暂无其他候选人，请联系管理员在实例/库上配置审批人"
          />
        )}
        <Select
          showSearch
          allowClear
          loading={transferLoading}
          disabled={transferLoading || approverOptions.length === 0}
          placeholder="选择新审核人"
          style={{ width: '100%' }}
          options={approverOptions}
          value={transferTo}
          onChange={(v) => setTransferTo(v)}
          filterOption={(input, opt) => String(opt?.label || '').toLowerCase().includes(input.toLowerCase())}
        />
      </Modal>
    </div>
  )
}
