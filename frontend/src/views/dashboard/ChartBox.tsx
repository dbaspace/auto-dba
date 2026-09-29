import React, { useEffect, useRef } from 'react'
import * as echarts from 'echarts'

interface Props {
  option: echarts.EChartsCoreOption
  height?: string
  onClick?: (params: any) => void
}

function legendDataNames(legend: unknown): Set<string> {
  const names = new Set<string>()
  if (!legend || typeof legend !== 'object') return names
  const data = (legend as { data?: unknown }).data
  if (!Array.isArray(data)) return names
  for (const item of data) {
    if (typeof item === 'string') names.add(item)
    else if (item && typeof item === 'object' && typeof (item as { name?: unknown }).name === 'string') {
      names.add((item as { name: string }).name)
    }
  }
  return names
}

/** 把上一轮图例选中态合并进新 option，避免 notMerge 刷新冲掉用户点击隐藏/只看 */
function withPreservedLegendSelected(
  chart: echarts.ECharts,
  option: echarts.EChartsCoreOption,
): echarts.EChartsCoreOption {
  if (!option || typeof option !== 'object') return option
  let prevSelected: Record<string, boolean> | undefined
  try {
    const prev = chart.getOption() as { legend?: Array<{ selected?: Record<string, boolean> }> }
    prevSelected = prev?.legend?.[0]?.selected
  } catch {
    return option
  }
  if (!prevSelected || Object.keys(prevSelected).length === 0) return option

  const next = { ...option }
  const rawLegend = next.legend
  if (!rawLegend) return option

  const apply = (leg: Record<string, unknown>) => {
    const names = legendDataNames(leg)
    if (names.size === 0) return leg
    const selected: Record<string, boolean> = {}
    for (const [k, v] of Object.entries(prevSelected!)) {
      if (names.has(k)) selected[k] = v
    }
    if (Object.keys(selected).length === 0) return leg
    return { ...leg, selected }
  }

  if (Array.isArray(rawLegend)) {
    next.legend = rawLegend.map((leg) =>
      leg && typeof leg === 'object' ? apply(leg as Record<string, unknown>) : leg,
    ) as echarts.EChartsCoreOption['legend']
  } else if (typeof rawLegend === 'object') {
    next.legend = apply(rawLegend as Record<string, unknown>) as echarts.EChartsCoreOption['legend']
  }
  return next
}

export default function ChartBox({ option, height = '220px', onClick }: Props) {
  const elRef = useRef<HTMLDivElement | null>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)
  const onClickRef = useRef<Props['onClick']>(onClick)
  onClickRef.current = onClick

  useEffect(() => {
    if (!elRef.current) return
    const el = elRef.current
    const chart = echarts.init(el)
    chartRef.current = chart
    chart.on('click', (p) => onClickRef.current?.(p))

    const resize = () => {
      if (!chartRef.current) return
      chartRef.current.resize()
    }
    window.addEventListener('resize', resize)

    let ro: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(() => resize())
      ro.observe(el)
    }

    // 首次可能在 display:none 的 Tab 内初始化，下一帧再尝试一次
    const raf = requestAnimationFrame(resize)

    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      ro?.disconnect()
      chart.dispose()
      chartRef.current = null
    }
  }, [])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const next = withPreservedLegendSelected(chart, option || {})
    chart.setOption(next || {}, true)
    requestAnimationFrame(() => chart.resize())
  }, [option])

  return <div ref={elRef} className="chart-box" style={{ height, width: '100%', minWidth: 0 }} />
}
