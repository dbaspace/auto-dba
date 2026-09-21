import React, { useEffect, useRef } from 'react'
import * as echarts from 'echarts'

interface Props {
  option: echarts.EChartsCoreOption
  height?: string
  onClick?: (params: any) => void
}

export default function ChartBox({ option, height = '220px', onClick }: Props) {
  const elRef = useRef<HTMLDivElement | null>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)
  const onClickRef = useRef<Props['onClick']>(onClick)
  onClickRef.current = onClick

  useEffect(() => {
    if (!elRef.current) return
    const chart = echarts.init(elRef.current)
    chartRef.current = chart
    chart.on('click', (p) => onClickRef.current?.(p))
    const resize = () => chart.resize()
    window.addEventListener('resize', resize)
    return () => {
      window.removeEventListener('resize', resize)
      chart.dispose()
      chartRef.current = null
    }
  }, [])

  useEffect(() => {
    chartRef.current?.setOption(option || {}, true)
  }, [option])

  return <div ref={elRef} className="chart-box" style={{ height, width: '100%', minWidth: 0 }} />
}
