import type { EChartsCoreOption } from 'echarts'
import type { PanelResult } from './services/dbMonitorApi'

const COLORS = [
  '#73C0DE', '#EE6666', '#FAC858', '#91CC75', '#5470C6',
  '#9A60B4', '#FC8452', '#3BA272', '#EA7CCC', '#48B8D0',
]

/** Grafana short：K / M / B，避免大整数挤满 Y 轴 */
function formatCompact(v: number): string {
  if (!Number.isFinite(v)) return '-'
  const sign = v < 0 ? '-' : ''
  const abs = Math.abs(v)
  let body: string
  if (abs >= 1e9) body = `${(abs / 1e9).toFixed(2)}B`
  else if (abs >= 1e6) body = `${(abs / 1e6).toFixed(2)}M`
  else if (abs >= 1e3) body = `${(abs / 1e3).toFixed(2)}K`
  else if (abs >= 100) body = abs.toFixed(0)
  else if (Number.isInteger(abs) || Math.abs(abs - Math.round(abs)) < 1e-9) body = String(Math.round(abs))
  else body = abs.toFixed(1)
  if (body.startsWith('.')) body = `0${body}`
  return sign + body
}

function formatLegendVal(unit: string, v?: number | null): string {
  const n = typeof v === 'number' ? v : Number(v)
  if (v == null || !Number.isFinite(n)) return '-'
  if (unit === 'ratio') return `${(n * 100).toFixed(2)}%`
  if (unit === 'percent') return `${n.toFixed(2)}%`
  if (unit === 'bytes' || unit === 'bytes/s') {
    const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
    let x = Math.abs(n)
    let i = 0
    while (x >= 1024 && i < u.length - 1) {
      x /= 1024
      i++
    }
    let body = i === 0 ? x.toFixed(0) : x.toFixed(2)
    if (body.startsWith('.')) body = `0${body}`
    const suffix = unit === 'bytes/s' ? `${u[i]}/s` : u[i]
    return `${n < 0 ? '-' : ''}${body}\u00A0${suffix}`
  }
  // num / short / rows 等计数类：与 Grafana short 一致
  return formatCompact(n)
}

export { formatLegendVal, COLORS as CHART_COLORS }

function chartTheme(mode: 'light' | 'dark') {
  if (mode === 'dark') {
    return {
      legend: '#aab',
      axis: '#889',
      axisLine: '#445',
      split: '#2a2f3a',
      tipBg: 'rgba(20,24,32,0.92)',
      tipBorder: '#333',
      tipText: '#ddd',
    }
  }
  return {
    legend: '#4b5563',
    axis: '#6b7280',
    axisLine: '#d1d5db',
    split: '#e5e7eb',
    tipBg: 'rgba(255,255,255,0.96)',
    tipBorder: '#e5e7eb',
    tipText: '#1f2937',
  }
}

function isStackKeysPanel(panel: PanelResult): boolean {
  return panel.key === 'redis_db_keys' || panel.key === 'redis_expiry'
}

/** 用分位数压掉 rate/counter 尖刺，避免 Y 轴被单点拉飞 */
function softYMax(series: PanelResult['series'], stacked = false): number | undefined {
  if (stacked) {
    // 堆叠：按时间点求和再取峰值（对齐 Grafana stack）
    const byT = new Map<number, number>()
    for (const s of series || []) {
      for (const p of s.points || []) {
        if (!Number.isFinite(p.t) || !Number.isFinite(p.v) || p.v < 0) continue
        byT.set(p.t, (byT.get(p.t) || 0) + p.v)
      }
    }
    let hardMax = 0
    for (const v of byT.values()) {
      if (v > hardMax) hardMax = v
    }
    if (hardMax <= 0) {
      // 退化：各序列 max 相加
      for (const s of series || []) {
        if (s.max != null && Number.isFinite(s.max) && s.max > 0) hardMax += s.max
      }
    }
    return hardMax > 0 ? hardMax * 1.05 : undefined
  }
  const vals: number[] = []
  for (const s of series || []) {
    if (s.max != null && Number.isFinite(s.max)) vals.push(s.max)
    for (const p of s.points || []) {
      if (Number.isFinite(p.v) && p.v >= 0) vals.push(p.v)
    }
  }
  if (vals.length === 0) return undefined
  vals.sort((a, b) => a - b)
  const idx = Math.min(vals.length - 1, Math.floor(vals.length * 0.98))
  const p98 = vals[idx]
  const hardMax = vals[vals.length - 1]
  // 若尖刺超过 P98 的 8 倍，用 P98*1.2 作为轴上限
  if (hardMax > p98 * 8 && p98 > 0) return p98 * 1.2
  if (hardMax <= 0) return undefined
  return hardMax * 1.05
}

function cleanPoints(points: { t: number; v: number }[], yCap?: number) {
  return points
    .filter((p) => Number.isFinite(p.t) && Number.isFinite(p.v) && p.v >= 0)
    .map((p) => {
      const v = yCap != null && p.v > yCap * 8 ? null : p.v
      return [p.t * 1000, v] as [number, number | null]
    })
}

export type LegendSortBy = 'max' | 'current'
export type LegendSortDir = 'asc' | 'desc'

export type LegendSort = {
  by: LegendSortBy
  dir: LegendSortDir
}

/** max/current 单列宽度（够 194.24 KiB/s / 488.40 GiB） */
export const LEGEND_VAL_COL_PX = 88
const LEGEND_VAL_GAP_PX = 8
const LEGEND_ICON_PX = 18
const LEGEND_PAD_PX = 8

export type LegendLayout = {
  legendWidth: number
  gridRight: number
  nameWidth: number
  valColWidth: number
  /** 顶部 max/current 排序条宽度 */
  hdrWidth: number
}

/** 按卡片容器宽度动态分配右侧图例；数值列优先，不够则压缩左侧绘图区 */
export function computeLegendLayout(containerWidth = 0): LegendLayout {
  const valCols = LEGEND_VAL_COL_PX * 2 + LEGEND_VAL_GAP_PX
  const minLegend = valCols + 64 + LEGEND_ICON_PX
  const cw = containerWidth > 0 ? containerWidth : 560
  // 最多占容器 48%，保证折线区仍可见；至少塞下数值列 + 短名
  const maxByContainer = Math.floor(cw * 0.48)
  const legendWidth = Math.max(minLegend, Math.min(360, maxByContainer))
  const nameWidth = Math.max(
    40,
    legendWidth - LEGEND_ICON_PX - valCols - LEGEND_PAD_PX,
  )
  return {
    legendWidth,
    gridRight: legendWidth + 12,
    nameWidth,
    valColWidth: LEGEND_VAL_COL_PX,
    hdrWidth: valCols,
  }
}

export type LegendRow = {
  name: string
  color: string
  max: string
  current: string
}

function seriesSortValue(s: NonNullable<PanelResult['series']>[number], by: LegendSortBy): number {
  const v = by === 'current' ? s.current : s.max
  return v != null && Number.isFinite(v) ? v : Number.NEGATIVE_INFINITY
}

/** 图例行（与 buildTimeseriesOption 排序/配色一致），供右侧可滚动列表使用 */
export function buildLegendRows(
  panel: PanelResult,
  sort: LegendSort = { by: 'max', dir: 'desc' },
): LegendRow[] {
  const series = panel.series || []
  const ordered = [...series].sort((a, b) => {
    const diff = seriesSortValue(a, sort.by) - seriesSortValue(b, sort.by)
    return sort.dir === 'desc' ? -diff : diff
  })
  return ordered.map((s, i) => {
    const base = s.name || `series-${i}`
    const dup = ordered.filter((x) => x.name === s.name).length > 1
    const name = dup ? `${base} #${i + 1}` : base
    return {
      name,
      color: COLORS[i % COLORS.length],
      max: formatLegendVal(panel.unit, s.max),
      current: formatLegendVal(panel.unit, s.current),
    }
  })
}

export function buildTimeseriesOption(
  panel: PanelResult,
  mode: 'light' | 'dark' = 'light',
  sort: LegendSort = { by: 'max', dir: 'desc' },
  containerWidth = 0,
  opts?: { externalLegend?: boolean; hiddenNames?: string[] },
): EChartsCoreOption {
  const series = panel.series || []
  const ordered = [...series].sort((a, b) => {
    const diff = seriesSortValue(a, sort.by) - seriesSortValue(b, sort.by)
    return sort.dir === 'desc' ? -diff : diff
  })
  const legendNames = ordered.map((s, i) => {
    // 同名时加序号，避免 ECharts 合并导致图例 max 对不上折线
    const base = s.name || `series-${i}`
    const dup = ordered.filter((x) => x.name === s.name).length > 1
    return dup ? `${base} #${i + 1}` : base
  })
  const stacked = isStackKeysPanel(panel)
  const yMax = softYMax(ordered, stacked)
  const t = chartTheme(mode)
  const layout = computeLegendLayout(containerWidth)
  const fmtAxis = (v: unknown) => formatLegendVal(panel.unit, typeof v === 'number' ? v : Number(v))
  const external = !!opts?.externalLegend
  const hidden = new Set(opts?.hiddenNames || [])
  const selected: Record<string, boolean> = {}
  for (const n of legendNames) selected[n] = !hidden.has(n)

  return {
    backgroundColor: 'transparent',
    color: COLORS,
    grid: {
      left: 56,
      right: external ? 16 : layout.gridRight,
      top: 28,
      bottom: 28,
      containLabel: false,
    },
    tooltip: {
      trigger: 'axis',
      backgroundColor: t.tipBg,
      borderColor: t.tipBorder,
      textStyle: { color: t.tipText, fontSize: 12 },
      valueFormatter: (v) => fmtAxis(v),
    },
    legend: {
      show: !external,
      type: 'scroll',
      orient: 'vertical',
      right: 4,
      top: 20,
      bottom: 20,
      width: layout.legendWidth,
      data: legendNames,
      selected,
    },
    xAxis: {
      type: 'time',
      axisLine: { lineStyle: { color: t.axisLine } },
      axisLabel: { color: t.axis, fontSize: 11 },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: yMax,
      scale: false,
      axisLine: { show: false },
      axisLabel: {
        color: t.axis,
        fontSize: 11,
        hideOverlap: true,
        formatter: fmtAxis,
      },
      splitLine: { lineStyle: { color: t.split } },
    },
    series: ordered.map((s, i) => {
      const name = legendNames[i]
      const on = !hidden.has(name)
      return {
        name,
        type: 'line',
        stack: stacked ? 'keys' : undefined,
        areaStyle: stacked && on ? { opacity: 0.85 } : undefined,
        showSymbol: false,
        smooth: false,
        connectNulls: false,
        lineStyle: { width: stacked ? 1 : 1.5, opacity: on ? 1 : 0 },
        data: on ? cleanPoints(s.points || [], stacked ? undefined : yMax) : [],
      }
    }),
  }
}
