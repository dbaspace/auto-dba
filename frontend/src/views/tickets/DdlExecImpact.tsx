import React, { useMemo, useState } from 'react'
import { Tag } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import {
  buildDdlExecGroups,
  isTicketExecFailed,
  type DdlExecGroup,
  type DdlExecStats,
  type DdlExecTab,
} from './ddlExecGroups'
import SqlSnippetCell from './SqlSnippetCell'

export type { DdlExecGroup, DdlExecStats, DdlExecTab }
export { buildDdlExecGroups }

const STATUS_TAG: Record<string, { color: string; text: string }> = {
  success: { color: 'success', text: '成功' },
  failed: { color: 'error', text: '失败' },
  rolled_back: { color: 'warning', text: '已回滚' },
  skipped: { color: 'default', text: '跳过' },
}

const tableColumns = [
  {
    title: 'SQL语句',
    dataIndex: 'sql',
    width: 360,
    render: (v: string) => <SqlSnippetCell sql={v} />,
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
    width: 200,
    ellipsis: true,
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
  const ticketFailed = isTicketExecFailed({
    execFailed: props.execFailed,
    execSuccess: props.execSuccess,
  })
  const { groups, stats } = useMemo(
    () =>
      buildDdlExecGroups(props.records, {
        env: props.env,
        envByInstanceId: props.envByInstanceId,
        fallbackDb: props.fallbackDb,
        fallbackHost: props.fallbackHost,
        execMessage: props.execMessage,
        execFailed: props.execFailed,
        execSuccess: props.execSuccess,
      }),
    [
      props.records,
      props.env,
      props.envByInstanceId,
      props.fallbackDb,
      props.fallbackHost,
      props.execMessage,
      props.execFailed,
      props.execSuccess,
    ],
  )
  const rolledBack = Boolean(props.rollbackSummary) || stats.rolledBackSQL > 0

  const visible = useMemo(() => {
    if (tab === 'ok') return groups.filter((g) => g.ok)
    if (tab === 'fail') return groups.filter((g) => !g.ok)
    return groups
  }, [groups, tab])

  const hasFail = stats.instFail > 0 || ticketFailed
  const allOk = stats.instTotal > 0 && stats.instFail === 0 && !ticketFailed
  const allFail = stats.instTotal > 0 && stats.instOk === 0

  const summaryLine = (() => {
    if (!stats.instTotal && !stats.totalSQL) return props.emptyText || '暂无数据，执行后获取'
    if (hasFail && rolledBack) {
      return props.rollbackSummary || props.execMessage || '执行失败，事务已回滚，0 条生效'
    }
    if (hasFail && ticketFailed && props.execMessage) return props.execMessage
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
