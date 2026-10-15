import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Alert, Avatar, Button, Descriptions, Input, Modal, Select, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import DdlExecImpact from './DdlExecImpact'
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

interface ApplyField {
  key: string
  label: string
  value: any
  kind?: string
}

export default function TicketDetail() {
  const navigate = useNavigate()
  const params = useParams()

  const [item, setItem] = useState<any>(null)
  const [canApproveFlag, setCanApproveFlag] = useState(false)
  const [canTransferFlag, setCanTransferFlag] = useState(false)
  const [inst, setInst] = useState<any>(null)
  const [envByInstanceId, setEnvByInstanceId] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
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
    const isResourceApply = it.issue_type === 'resource_apply'
    const fields: ApplyField[] = [
      { key: 'reason', label: '原因', value: it.description || resourceApplyExtra?.reason || '-' },
    ]
    if (!isResourceApply) {
      fields.push(
        { key: 'db_type', label: '数据库类型', kind: 'type-tag', value: dbType || '-' },
        { key: 'db', label: '数据库', kind: 'db-tag', value: dbPath },
      )
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
      fields.push({ key: 'rows', label: '预估行数', value: String(dqlExtra.row_count ?? '-') })
      if (dqlExtra.dba_assigned_to)
        fields.push({ key: 'dba', label: 'DBA审批人', value: dqlExtra.dba_assigned_to })
      if (dqlExtra.security_assigned_to)
        fields.push({ key: 'sec', label: '安全审批人', value: dqlExtra.security_assigned_to })
    }
    fields.push({ key: 'approver', label: '指定审批人', value: it.assigned_to || '-' })
    fields.push({ key: 'created', label: '申请时间', value: fmtTime(it.created_at) })
    return fields
  }, [item, dbType, dbPath, createdbExtra, dqlExtra, resourceApplyExtra])

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
  const execFailed = item?.status === 'failed'
  const execSuccess = execObj?.success === true ? true : execObj?.success === false ? false : undefined
  const isExecRunning = item?.status === 'running'
  const execRecords = useMemo(
    () => (Array.isArray(execObj?.records) ? execObj.records : []),
    [execObj],
  )
  // running 时隐藏上轮明细，避免「后台执行中」与旧失败/成功结果并读
  const displayExecRecords = useMemo(
    () => (isExecRunning ? [] : execRecords),
    [isExecRunning, execRecords],
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
    if (item?.issue_type === 'resource_apply') return [] as string[]
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

  const approveCardClass = useMemo(() => {
    const s = item?.status
    if (s === 'pending') return 'is-active'
    if (s === 'rejected') return 'is-danger'
    return 'is-done'
  }, [item])

  const execCardClass = useMemo(() => {
    const s = item?.status
    if (s === 'executed') return 'is-done'
    if (s === 'failed') return 'is-danger'
    if (s === 'running') return 'is-active'
    return 'is-wait'
  }, [item])

  const wfApproveClass = useMemo(() => {
    const s = item?.status
    if (s === 'pending') return 'is-current'
    if (s === 'rejected' || s === 'canceled') return 'is-fail'
    return 'is-ok'
  }, [item])

  const wfExecClass = useMemo(() => {
    const s = item?.status
    if (s === 'approved' || s === 'running') return 'is-current'
    if (s === 'executed') return 'is-ok'
    if (s === 'failed') return 'is-fail'
    return 'is-idle'
  }, [item])

  const wfApproveSub = useMemo(() => {
    const s = item?.status
    if (s === 'pending') return '待审批'
    if (s === 'rejected') return '已驳回'
    if (s === 'canceled') return '已取消'
    return '已通过'
  }, [item])

  const wfExecSub = useMemo(() => {
    const s = item?.status
    if (s === 'executed') return isDQL ? '已导出' : execSkipped ? '已跳过连库' : '已执行'
    if (s === 'failed') return '执行失败'
    if (s === 'running') return '后台执行中'
    if (s === 'approved')
      return isDQL
        ? '点击下载'
        : isFlashback || isCDC || isMigration || isCreateDB
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

  const execEmptyText = useMemo(
    () =>
      item?.status === 'executed' || item?.status === 'failed'
        ? '暂无数据'
        : item?.status === 'running'
          ? '正在后台执行，完成后自动刷新结果'
          : '暂无数据，执行后获取',
    [item],
  )

  const precheckRows = useMemo<any[]>(() => [], [])

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/tickets/' + params.id)
      setItem(data.data)
      setCanApproveFlag(Boolean(data.can_approve))
      setCanTransferFlag(Boolean(data.can_transfer))
      setInst(null)
      setEnvByInstanceId({})
      envFetchedRef.current = new Set()
      if (data.data?.instance_id) {
        try {
          const instRes = await api.get('/instances/' + data.data.instance_id)
          setInst(instRes.data.data || null)
        } catch {
          /* keep empty */
        }
      }
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
      const res = await api.post(
        `/tickets/${it.id}/export`,
        { file_format: fileFormat },
        { responseType: 'blob', timeout: 120000 },
      )
      const blob = res.data instanceof Blob ? res.data : new Blob([res.data])
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
      await load()
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
      } else if (kind === 'approve') {
        await api.put(`/tickets/${it.id}/approve`)
      } else if (kind === 'cancel') {
        const ok = await confirmCancel()
        if (!ok) {
          return
        }
        await api.put(`/tickets/${it.id}/cancel`)
      } else if (kind === 'execute') {
        await api.post(`/tickets/${it.id}/execute`)
      }
      message.success('已处理')
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
        const { data } = await api.get('/tickets/' + params.id)
        const next = data.data
        if (!next) return
        setItem(next)
        if (typeof data.can_approve === 'boolean') setCanApproveFlag(data.can_approve)
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
            const res = await api.get('/instances/' + id)
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
            <div className={`flow-card ${approveCardClass}`}>
              <div className="flow-card-head">指定人员审批</div>
              <div className="flow-card-body">
                {item.status === 'pending' ? (
                  <>
                    <div className="approver-line">
                      审批人员：{item.assigned_to || '-'}
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
                        <Avatar size={28}>{userInitial(item.assigned_to)}</Avatar>
                        <span>{item.assigned_to || '-'}</span>
                      </div>
                      {item.status === 'rejected' ? (
                        <span className="status-text danger">已驳回</span>
                      ) : item.status === 'canceled' ? (
                        <span className="status-text muted">已取消</span>
                      ) : (
                        <span className="status-text success">已通过</span>
                      )}
                    </div>
                    {item.reject_reason && (
                      <div className="remark-text">附言：{item.reject_reason}</div>
                    )}
                  </>
                )}
              </div>
            </div>

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
                      <span className="exec-wait">点击下载</span>
                      <Button
                        type="primary"
                        disabled={!canDownload}
                        loading={downloading}
                        onClick={openExport}
                      >
                        下载
                      </Button>
                    </div>
                    <div className="download-hint">仅限工单发起人可下载</div>
                  </div>
                ) : (
                  <div className="flow-card-body exec-body">
                    {item.status === 'running' && isClusterChange ? (
                      <>
                        <div className="exec-wait">集群变更执行中…</div>
                        <div className="user-sub">{execObj?.summary || '等待变更结束后同步最终状态'}</div>
                      </>
                    ) : item.status === 'running' ? (
                      <>
                        <div className="exec-wait">正在后台执行大 SQL，请勿重复点击</div>
                        <div className="user-sub">页面将自动刷新执行结果</div>
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
              <div className={`wf-node ${wfApproveClass}`}>
                <div className="wf-dot" />
                <div>
                  <div className="wf-name">指定人员审批</div>
                  <div className="wf-sub">{wfApproveSub}</div>
                </div>
              </div>
              <div className="wf-line" />
              <div className={`wf-node ${wfExecClass}`}>
                <div className="wf-dot" />
                <div>
                  <div className="wf-name">执行操作</div>
                  <div className="wf-sub">{wfExecSub}</div>
                </div>
              </div>
            </div>
          </aside>

          <section className="review-main">
            <div className="sec-title">申请信息</div>
            <Descriptions column={1} bordered className="apply-desc">
              {applyFields.map((f) => (
                <Descriptions.Item key={f.key} label={f.label}>
                  {f.kind === 'type-tag' ? (
                    <Tag color="processing">{f.value}</Tag>
                  ) : f.kind === 'db-tag' ? (
                    <Tag color="blue">{f.value}</Tag>
                  ) : f.kind === 'sql' ? (
                    <pre className="sql-pre">{f.value}</pre>
                  ) : f.kind === 'schedule' ? (
                    <Tag color="warning">{f.value}</Tag>
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
                <div className="sec-title mt">预检测SQL影响</div>
                <div className="impact-card">
                  <div className="impact-head">
                    <span className="db-pill">数据库</span>
                    <span className="db-path">{dbPath}</span>
                  </div>
                  <ResponsiveTable
                    dataSource={precheckRows}
                    bordered
                    size="small"
                    rowKey={(_r, i) => String(i)}
                    pagination={false}
                    locale={{ emptyText: '暂无数据' }}
                    columns={[
                      {
                        title: 'SQL语句',
                        dataIndex: 'sql',
                        width: undefined,
                        ellipsis: { showTitle: true },
                        onCell: () => ({ style: { minWidth: 280 } }),
                      },
                      { title: '影响行数', dataIndex: 'affected', width: 120 },
                    ]}
                  />
                </div>

                <div className="sec-title mt">执行SQL影响</div>
                <div className="impact-card">
                  <DdlExecImpact
                    records={displayExecRecords}
                    env={dbEnv}
                    envByInstanceId={envByInstanceId}
                    fallbackDb={item?.db_name}
                    fallbackHost={inst?.host}
                    execMessage={isExecRunning ? '' : execErrorText}
                    execSuccess={isExecRunning ? undefined : execSuccess}
                    execFailed={isExecRunning ? false : execFailed}
                    rollbackSummary={
                      isExecRunning || !execObj?.rolled_back ? '' : String(execObj?.summary || '')
                    }
                    emptyText={execEmptyText}
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
