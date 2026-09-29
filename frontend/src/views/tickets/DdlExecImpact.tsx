import React, { useMemo, useState } from 'react'
import { Tag } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'

export type DdlExecTab = 'all' | 'ok' | 'fail'

const NO_DETAIL_ERROR = '执行失败但未记录错误详情，请联系管理员查看后端日志'

const STATUS_TAG: Record<string, { color: string; text: string }> = {
  success: { color: 'success', text: '成功' },
  failed: { color: 'error', text: '失败' },
  rolled_back: { color: 'warning', text: '已回滚' },
  skipped: { color: 'default', text: '跳过' },
}

function fmtTimeMs(v: any): string {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n: number) => String(n).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${ms}`
}

function costMs(start: string, end: string): string {
  if (!start || !end) return '-'
  const a = Date.parse(start)
  const b = Date.parse(end)
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return '-'
  return String(b - a)
}

export interface DdlExecGroup {
  key: string
  instanceId: string
  label: string
  path: string
  ok: boolean
  error: string
  rolledBack: number
  rows: {
    key: string
    sql: string
    status: string
    error: string
    start: string
    end: string
    cost: string
    affected: string | number
  }[]
}

export interface DdlExecStats {
  totalSQL: number
  successSQL: number
  rolledBackSQL: number
  instTotal: number
  instOk: number
  instFail: number
}

export function buildDdlExecGroups(
  records: any[] | undefined,
  opts: {
    env?: string
    envByInstanceId?: Record<string, string>
    fallbackDb?: string
    fallbackHost?: string
    execMessage?: string
    execFailed?: boolean
  },
): { groups: DdlExecGroup[]; stats: DdlExecStats } {
  const recs = Array.isArray(records) ? records : []
  const fallbackEnv = String(opts.env || '-').trim() || '-'
  const envById = opts.envByInstanceId || {}
  const map = new Map<string, any[]>()
  const order: string[] = []

  for (const r of recs) {
    const iid = String(r.instance_id || '').trim()
    const host = String(r.host || opts.fallbackHost || '').trim()
    const db = String(r.db || opts.fallbackDb || '').trim()
    const key = iid || `${host}|${db}` || 'unknown'
    if (!map.has(key)) {
      map.set(key, [])
      order.push(key)
    }
    map.get(key)!.push(r)
  }

  // 无 records 但有整单消息或已失败时，仍展示一个占位块，避免失败却只显示「暂无数据」
  if (!order.length && (opts.execMessage || opts.execFailed)) {
    order.push('gate')
    map.set('gate', [
      {
        sql: '(执行)',
        status: 'failed',
        error_message: opts.execMessage || NO_DETAIL_ERROR,
        host: opts.fallbackHost || '',
        db: opts.fallbackDb || '',
      },
    ])
  }

  const groups: DdlExecGroup[] = order.map((key, idx) => {
    const list = map.get(key) || []
    const first = list[0] || {}
    const host = String(first.host || opts.fallbackHost || '').trim()
    const port = first.port ? String(first.port) : ''
    const db = String(first.db || opts.fallbackDb || '').trim() || '-'
    const name = String(first.instance_name || '').trim()
    const hostPort = host ? (port ? `${host}:${port}` : host) : name || key
    const label = db && hostPort ? `${db} - ${hostPort}` : hostPort || db || key
    const iid = String(first.instance_id || '').trim()
    const env = (iid && String(envById[iid] || '').trim()) || fallbackEnv
    const path = `${env} / ${db}${hostPort ? ` · ${hostPort}` : ''}`
    const failed = list.filter((r) => String(r.status || '').toLowerCase() === 'failed')
    const rolledBack = list.filter((r) => String(r.status || '').toLowerCase() === 'rolled_back').length
    const ok = failed.length === 0 && list.length > 0
    const error =
      (failed[0] && String(failed[0].error_message || '').trim()) ||
      (!ok ? String(opts.execMessage || '').trim() || NO_DETAIL_ERROR : '') ||
      ''

    const rows = list.map((r, i) => {
      const start = r.started_at || ''
      const end = r.ended_at || ''
      return {
        key: `${key}-${i}`,
        sql: r.sql || '-',
        status: String(r.status || '').toLowerCase(),
        error: r.error_message ? String(r.error_message) : '-',
        start: fmtTimeMs(start),
        end: fmtTimeMs(end),
        cost: costMs(start, end),
        affected: r.affected_row ?? 0,
      }
    })

    return {
      key,
      instanceId: String(first.instance_id || ''),
      label,
      path,
      ok,
      error,
      rolledBack,
      rows,
    }
  })

  let successSQL = 0
  const totalSQL = recs.length
  successSQL = recs.filter((r) => String(r.status || '').toLowerCase() === 'success').length
  if (!recs.length && groups.length) {
    // gate-only placeholder
    successSQL = 0
  }

  const instOk = groups.filter((g) => g.ok).length
  const instFail = groups.filter((g) => !g.ok).length

  return {
    groups,
    stats: {
      totalSQL: totalSQL || groups.reduce((n, g) => n + g.rows.length, 0),
      successSQL,
      rolledBackSQL: groups.reduce((n, g) => n + g.rolledBack, 0),
      instTotal: groups.length,
      instOk,
      instFail,
    },
  }
}

const tableColumns = [
  {
    title: 'SQL语句',
    dataIndex: 'sql',
    ellipsis: { showTitle: true },
    onCell: () => ({ style: { minWidth: 220 } }),
  },
  {
    title: '状态',
    dataIndex: 'status',
    width: 90,
    render: (v: string) => {
      const t = STATUS_TAG[v]
      return t ? <Tag color={t.color}>{t.text}</Tag> : v || '-'
    },
  },
  {
    title: '错误信息',
    dataIndex: 'error',
    onCell: () => ({ style: { minWidth: 160 } }),
    render: (v: string) => <span className="exec-cell-error">{v}</span>,
  },
  { title: '开始执行时间', dataIndex: 'start', width: 190 },
  { title: '结束执行时间', dataIndex: 'end', width: 190 },
  { title: '执行时间(ms)', dataIndex: 'cost', width: 120 },
  { title: '影响行数', dataIndex: 'affected', width: 100 },
]

export default function DdlExecImpact(props: {
  records?: any[]
  env?: string
  envByInstanceId?: Record<string, string>
  fallbackDb?: string
  fallbackHost?: string
  execMessage?: string
  execSuccess?: boolean
  execFailed?: boolean
  /** 事务回滚时后端给出的摘要（第 N 条失败、均未生效） */
  rollbackSummary?: string
  emptyText?: string
}) {
  const [tab, setTab] = useState<DdlExecTab>('all')
  const { groups, stats } = useMemo(
    () =>
      buildDdlExecGroups(props.records, {
        env: props.env,
        envByInstanceId: props.envByInstanceId,
        fallbackDb: props.fallbackDb,
        fallbackHost: props.fallbackHost,
        execMessage: props.execMessage,
        execFailed: props.execFailed,
      }),
    [
      props.records,
      props.env,
      props.envByInstanceId,
      props.fallbackDb,
      props.fallbackHost,
      props.execMessage,
      props.execFailed,
    ],
  )
  const rolledBack = Boolean(props.rollbackSummary) || stats.rolledBackSQL > 0

  const visible = useMemo(() => {
    if (tab === 'ok') return groups.filter((g) => g.ok)
    if (tab === 'fail') return groups.filter((g) => !g.ok)
    return groups
  }, [groups, tab])

  const hasFail = stats.instFail > 0
  const allOk = stats.instTotal > 0 && stats.instFail === 0
  const allFail = stats.instTotal > 0 && stats.instOk === 0

  const summaryLine = (() => {
    if (!stats.instTotal && !stats.totalSQL) return props.emptyText || '暂无数据，执行后获取'
    if (hasFail && rolledBack) return props.rollbackSummary || '执行失败，事务已回滚，0 条生效'
    if (hasFail) return `部分失败，已成功 ${stats.successSQL} 条 SQL`
    return `成功 ${stats.successSQL || stats.totalSQL} 条 SQL`
  })()

  const bannerClass = allOk ? 'is-success' : allFail ? 'is-fail' : hasFail ? 'is-partial' : 'is-success'
  const bannerTitle = (() => {
    if (!stats.instTotal) return '暂无实例执行结果'
    if (allOk) return `共 ${stats.instTotal} 个实例，全部执行成功`
    if (allFail) return `共 ${stats.instTotal} 个实例，全部执行失败`
    return `共 ${stats.instTotal} 个实例，部分执行失败`
  })()

  const okLabels = groups.filter((g) => g.ok).map((g) => g.label)
  const failLabels = groups.filter((g) => !g.ok).map((g) => g.label)

  return (
    <div className="ddl-exec-impact">
      <div className="exec-summary-line">{summaryLine}</div>

      {stats.instTotal > 0 && (
        <div className={`exec-banner ${bannerClass}`}>
          <div className="exec-banner-title">
            <span className="exec-banner-icon" aria-hidden>
              {allOk ? '✓' : '✕'}
            </span>
            <span>{bannerTitle}</span>
          </div>
          {allOk && okLabels.length > 0 && (
            <div className="exec-banner-row">
              <span className="exec-banner-label">成功：</span>
              <div className="exec-banner-tags">
                {okLabels.map((t) => (
                  <span key={t} className="exec-chip">
                    {t}
                  </span>
                ))}
              </div>
            </div>
          )}
          {failLabels.length > 0 && (
            <div className="exec-banner-row">
              <span className="exec-banner-label">失败：</span>
              <div className="exec-banner-tags">
                {failLabels.map((t) => (
                  <span key={t} className="exec-chip is-fail">
                    {t}
                  </span>
                ))}
              </div>
            </div>
          )}
          {!allOk && !allFail && okLabels.length > 0 && (
            <div className="exec-banner-row">
              <span className="exec-banner-label">成功：</span>
              <div className="exec-banner-tags">
                {okLabels.map((t) => (
                  <span key={t} className="exec-chip">
                    {t}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {stats.instTotal > 0 && (
        <div className="exec-tabs">
          <button type="button" className={tab === 'all' ? 'is-active' : ''} onClick={() => setTab('all')}>
            全部
          </button>
          <button type="button" className={tab === 'ok' ? 'is-active' : ''} onClick={() => setTab('ok')}>
            成功 ({stats.instOk})
          </button>
          <button type="button" className={tab === 'fail' ? 'is-active' : ''} onClick={() => setTab('fail')}>
            失败 ({stats.instFail})
          </button>
        </div>
      )}

      {visible.length === 0 ? (
        <div className="exec-empty">{props.emptyText || '暂无数据'}</div>
      ) : (
        visible.map((g) => {
          const ord = groups.findIndex((x) => x.key === g.key) + 1
          return (
            <div key={g.key} className={`exec-inst-card ${g.ok ? '' : 'is-fail'}`}>
              <div className="impact-head">
                <span className={`db-pill ${g.ok ? '' : 'is-fail'}`}>数据库 {ord}</span>
                <span className="db-path">{g.path}</span>
                <Tag color={g.ok ? 'success' : 'error'}>{g.ok ? '执行成功' : '执行失败'}</Tag>
              </div>
              {!g.ok && g.error ? <div className="exec-inst-error">{g.error}</div> : null}
              {!g.ok && g.rolledBack > 0 ? (
                <div className="exec-inst-note">事务已回滚，本库 SQL 均未生效</div>
              ) : null}
              <ResponsiveTable
                dataSource={g.rows}
                bordered
                size="small"
                rowKey="key"
                pagination={false}
                scroll={{ x: 'max-content' }}
                columns={tableColumns as any}
              />
            </div>
          )
        })
      )}
    </div>
  )
}
