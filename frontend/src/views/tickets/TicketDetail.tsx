import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Avatar, Button, Descriptions, Input, Modal, Select, Table, Tag, message } from 'antd'
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
  const [inst, setInst] = useState<any>(null)
  const [loading, setLoading] = useState(false)
  const [acting, setActing] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [comment, setComment] = useState('')
  const [fmtVisible, setFmtVisible] = useState(false)
  const [fileFormat, setFileFormat] = useState('csv')

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

  const applyFields = useMemo<ApplyField[]>(() => {
    const it = item
    if (!it) return []
    const fields: ApplyField[] = [
      { key: 'reason', label: '原因', value: it.description || '-' },
      { key: 'db_type', label: '数据库类型', kind: 'type-tag', value: dbType || '-' },
      { key: 'db', label: '数据库', kind: 'db-tag', value: dbPath },
    ]
    if (it.sql_content) {
      fields.push({
        key: 'sql',
        label:
          it.issue_type === 'flashback' || it.issue_type === 'cdc_sync' || it.issue_type === 'migration'
            ? '任务摘要'
            : '执行SQL',
        kind: 'sql',
        value: it.sql_content,
      })
      fields.push({
        key: 'schedule',
        label:
          it.issue_type === 'flashback' || it.issue_type === 'cdc_sync' || it.issue_type === 'migration'
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
  }, [item, dbType, dbPath, createdbExtra, dqlExtra])

  const canApprove = useMemo(() => {
    if (item?.status !== 'pending') return false
    if (isPrivileged()) return true
    return String(item?.assigned_to || '').toLowerCase() === String(me.username || '').toLowerCase()
  }, [item])

  const canCancel = useMemo(() => {
    if (item?.status !== 'pending') return false
    if (isPrivileged()) return true
    return (
      String(item?.submitter_name || '').toLowerCase() === String(me.username || '').toLowerCase()
    )
  }, [item])

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

  const showExecCard = useMemo(() => {
    const s = item?.status
    if (s === 'pending' || s === 'rejected' || s === 'canceled') return false
    if (isCreateDB) {
      return s === 'failed'
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
    if (s === 'approved') return 'is-current'
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
        : '审批通过后将自动执行；若未触发可手动执行',
    [item],
  )

  const execEmptyText = useMemo(
    () =>
      item?.status === 'executed' || item?.status === 'failed'
        ? '暂无数据'
        : '暂无数据，执行后获取',
    [item],
  )

  const precheckRows = useMemo<any[]>(() => [], [])

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/tickets/' + params.id)
      setItem(data.data)
      setInst(null)
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
                    <div className="approver-line">审批人员：{item.assigned_to || '-'}</div>
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
                    {item.status === 'approved' ? (
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
                        <div className="user-sub">{execSummary || fmtTime(item.updated_at)}</div>
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

            {showSqlImpact && (
              <>
                <div className="sec-title mt">预检测SQL影响</div>
                <div className="impact-card">
                  <div className="impact-head">
                    <span className="db-pill">数据库</span>
                    <span className="db-path">{dbPath}</span>
                  </div>
                  <Table
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
                    records={Array.isArray(execObj?.records) ? execObj.records : []}
                    env={dbEnv}
                    fallbackDb={item?.db_name}
                    fallbackHost={inst?.host}
                    execMessage={execSummary}
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
    </div>
  )
}
