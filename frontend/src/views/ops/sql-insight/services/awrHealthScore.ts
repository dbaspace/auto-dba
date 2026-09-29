/**
 * AWR 健康分：基于 sections 六维指标（对齐 universe-fe awrHealthScore）。
 */

export type AwrPeakStatus = 'low' | 'mid' | 'high' | 'none'

export const AWR_HEALTH_BELOW = 0.5
export const AWR_HEALTH_NEAR = 0.8

export const AWR_PEAK_STATUS_TEXT: Record<AwrPeakStatus, string> = {
  low: '低于峰值',
  mid: '居中',
  high: '接近峰值',
  none: '—',
}

export const AWR_PEAK_STATUS_COLOR: Record<AwrPeakStatus, string> = {
  low: 'green',
  mid: 'default',
  high: 'orange',
  none: 'default',
}

export interface AwrHealthAxisScore {
  key: string
  label: string
  metricKey: string
  score: number | null
  last?: number
  max?: number
  unit?: string
  pressure?: number
  status: AwrPeakStatus
}

export interface AwrHealthResult {
  total: number | null
  grade: 'excellent' | 'good' | 'fair' | 'poor' | 'unknown'
  gradeText: string
  nearPeakCount: number
  axes: AwrHealthAxisScore[]
}

export const AWR_HEALTH_GRADE_TEXT: Record<AwrHealthResult['grade'], string> = {
  excellent: '优',
  good: '良',
  fair: '中',
  poor: '差',
  unknown: '未知',
}

export const AWR_HEALTH_GRADE_COLOR: Record<AwrHealthResult['grade'], string> = {
  excellent: 'green',
  good: 'blue',
  fair: 'orange',
  poor: 'red',
  unknown: 'default',
}

const AXES = [
  { key: 'cpu', metricKey: 'cpu_usage', label: 'CPU' },
  { key: 'mem', metricKey: 'mem_usage', label: '内存' },
  { key: 'disk', metricKey: 'disk_usage', label: '磁盘' },
  { key: 'conn', metricKey: 'conn_usage_ratio', label: '连接率' },
  { key: 'active', metricKey: 'active_conns', label: '活跃连接' },
  { key: 'cache', metricKey: 'cache_hit_ratio', label: '缓存命中', higherBetter: true },
]

function findMetric(sections: any, metricKey: string): any {
  const list = sections?.sections
  if (!Array.isArray(list)) return undefined
  for (const sec of list) {
    const m = sec.metrics?.find((x: any) => x.key === metricKey)
    if (m) return m
  }
  return undefined
}

function peakStatus(pressure: number | undefined): AwrPeakStatus {
  if (pressure == null || Number.isNaN(pressure)) return 'none'
  if (pressure <= AWR_HEALTH_BELOW) return 'low'
  if (pressure >= AWR_HEALTH_NEAR) return 'high'
  return 'mid'
}

export function computeAwrHealth(sections: any): AwrHealthResult {
  const axes: AwrHealthAxisScore[] = AXES.map((def) => {
    const m = findMetric(sections, def.metricKey)
    if (!m) {
      return { key: def.key, label: def.label, metricKey: def.metricKey, score: null, status: 'none' as AwrPeakStatus }
    }
    const last = Number(m.last ?? m.value ?? m.avg)
    const max = Number(m.max ?? m.peak ?? last)
    const unit = m.unit
    let pressure: number | undefined
    let score: number | null = null
    if (Number.isFinite(last)) {
      if (def.higherBetter) {
        // cache hit: higher is better; score = last if ratio-like
        const ratio = last > 1 ? last / 100 : last
        score = Math.max(0, Math.min(100, ratio * 100))
        pressure = 1 - (max > 0 ? last / max : ratio)
      } else if (max > 0 && Number.isFinite(max)) {
        pressure = Math.min(1, Math.max(0, last / max))
        score = Math.max(0, Math.min(100, (1 - pressure) * 100))
      } else {
        const ratio = last > 1 ? last / 100 : last
        pressure = Math.min(1, Math.max(0, ratio))
        score = Math.max(0, Math.min(100, (1 - pressure) * 100))
      }
    }
    return {
      key: def.key,
      label: def.label,
      metricKey: def.metricKey,
      score,
      last: Number.isFinite(last) ? last : undefined,
      max: Number.isFinite(max) ? max : undefined,
      unit,
      pressure,
      status: peakStatus(pressure),
    }
  })
  const valid = axes.filter((a) => a.score != null)
  const total = valid.length
    ? Math.round(valid.reduce((s, a) => s + (a.score as number), 0) / valid.length)
    : null
  let grade: AwrHealthResult['grade'] = 'unknown'
  if (total != null) {
    if (total >= 85) grade = 'excellent'
    else if (total >= 70) grade = 'good'
    else if (total >= 50) grade = 'fair'
    else grade = 'poor'
  }
  return {
    total,
    grade,
    gradeText: AWR_HEALTH_GRADE_TEXT[grade],
    nearPeakCount: axes.filter((a) => a.status === 'high').length,
    axes,
  }
}
