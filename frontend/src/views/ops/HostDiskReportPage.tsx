import { useEffect, useState } from 'react'
import { useDispatch } from 'react-redux'
import { useLocation, useParams } from 'react-router-dom'
import { Alert, Spin } from 'antd'
import api from '../../api/request'
import { tabKey, updateTabMeta } from '../../store/tabsSlice'
import HostDiskReportBody, { parseReportScope, reportLabel, type ReportScope, type TrendResp } from './HostDiskReportBody'
import PgStorageReportBody, { type PGTrendResp } from './PgStorageReportBody'
import './InstanceOverview.css'

type ReportDetail = {
  id: string
  kind?: string
  title?: string
  scope?: string | ReportScope
  payload?: TrendResp | PGTrendResp
  created_at?: string
}

function clock(d: Date) {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function analysisRange(report: ReportDetail) {
  const scope = parseReportScope(report.scope)
  const endRaw = scope.end || report.payload?.generated_at || report.created_at || ''
  const end = new Date(endRaw)
  if (!endRaw || Number.isNaN(end.getTime())) return '分析时间范围：—'
  const startRaw = scope.start
  const start = startRaw ? new Date(startRaw) : new Date(end.getTime() - (report.payload?.days || scope.days || 7) * 24 * 60 * 60 * 1000)
  if (Number.isNaN(start.getTime())) return `分析时间范围：结束 ${clock(end)}`
  return `分析时间范围：开始 ${clock(start)}　结束 ${clock(end)}`
}

export default function HostDiskReportPage() {
  const { id } = useParams()
  const location = useLocation()
  const dispatch = useDispatch()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [report, setReport] = useState<ReportDetail | null>(null)

  useEffect(() => {
    if (!id) return
    let cancelled = false
    setLoading(true)
    setError('')
    api
      .get(`/analysis-reports/${id}`)
      .then(({ data }) => {
        if (!cancelled) setReport(data as ReportDetail)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message
        setError(msg || '报告不存在')
        setReport(null)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [id])

  const pg = report?.kind === 'pg_storage'
  useEffect(() => {
    if (!pg) return
    dispatch(updateTabMeta({ key: tabKey(location.pathname, location.search), title: '智能分析报告' }))
  }, [pg, dispatch, location.pathname, location.search])
  return (
    <div className="ops-page host-disk-report">
      <div className="ops-title">
        <h1>{pg ? '智能分析报告' : '磁盘分析报告'}</h1>
      </div>
      {loading ? <Spin /> : null}
      {error ? <Alert type="error" showIcon message={error} /> : null}
      {!loading && report?.payload ? (
        <>
          <div className="host-disk-report-meta">{reportLabel(report.created_at || '', report.scope)}</div>
          <div className="host-disk-report-range">{analysisRange(report)}</div>
          {pg ? (
            <PgStorageReportBody resp={report.payload as PGTrendResp} showNote />
          ) : (
            <HostDiskReportBody resp={report.payload as TrendResp} showNote={false} />
          )}
        </>
      ) : null}
    </div>
  )
}
