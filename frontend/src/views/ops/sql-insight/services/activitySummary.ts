import api from '../../../../api/request'
import type { SnapshotFilter } from '../types'

export interface ChartNameValue {
  name: string
  value: number
}

export interface ActivitySummary {
  stateDistribution: ChartNameValue[]
  durationBuckets: ChartNameValue[]
  topDatabases: ChartNameValue[]
  topUsers: ChartNameValue[]
  topInstances: ChartNameValue[]
  trend: { time: string; value: number }[]
  timeRange?: { start: string; end: string }
  total: number
}

function kvList(raw: any): ChartNameValue[] {
  if (!Array.isArray(raw)) return []
  return raw.map((it) => ({
    name: String(it?.name ?? '-'),
    value: Number(it?.count ?? 0) || 0,
  }))
}

function fmtTrendTime(v: string): string {
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v || '-'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export async function fetchActivitySummary(
  filter: SnapshotFilter & { topN?: number },
): Promise<ActivitySummary> {
  const { data } = await api.get('/pg-activity-snapshots/summary', {
    params: {
      instance_id: filter.instanceId,
      start_time: filter.startTime,
      end_time: filter.endTime,
      top_n: filter.topN ?? 10,
    },
  })
  const raw = data?.data || {}
  const stateDistribution = kvList(raw.state_distribution)
  const durationBuckets = (Array.isArray(raw.duration_buckets) ? raw.duration_buckets : []).map(
    (it: any) => ({
      name: String(it?.label ?? '-'),
      value: Number(it?.count ?? 0) || 0,
    }),
  )
  const topDatabases = kvList(raw.top_databases)
  const topUsers = kvList(raw.top_users)
  const topInstances = kvList(raw.top_instances)
  const trend = (Array.isArray(raw.trend) ? raw.trend : []).map((it: any) => ({
    time: fmtTrendTime(String(it?.time || '')),
    value: Number(it?.count ?? 0) || 0,
  }))
  const total = stateDistribution.reduce((n, d) => n + d.value, 0)
  return {
    stateDistribution,
    durationBuckets,
    topDatabases,
    topUsers,
    topInstances,
    trend,
    timeRange: raw.time_range
      ? { start: String(raw.time_range.start || ''), end: String(raw.time_range.end || '') }
      : undefined,
    total,
  }
}
