import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Empty, Spin, Typography, message } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { EChartsOption } from 'echarts'
import ChartBox from '../../dashboard/ChartBox'
import {
  fetchActivitySummary,
  type ActivitySummary,
  type ChartNameValue,
} from './services/activitySummary'
import type { SnapshotFilter } from './types'
import './VisualSummaryTab.css'

const TOP_N = 10

const EMPTY: ActivitySummary = {
  stateDistribution: [],
  durationBuckets: [],
  topDatabases: [],
  topUsers: [],
  topInstances: [],
  trend: [],
  total: 0,
}

function barOption(data: ChartNameValue[], color: string): EChartsOption {
  const sorted = [...data].sort((a, b) => a.value - b.value)
  return {
    grid: { left: 8, right: 24, top: 16, bottom: 8, containLabel: true },
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    xAxis: { type: 'value', minInterval: 1 },
    yAxis: { type: 'category', data: sorted.map((d) => d.name) },
    series: [
      {
        type: 'bar',
        data: sorted.map((d) => d.value),
        itemStyle: { color },
        barMaxWidth: 18,
        label: { show: true, position: 'right' },
      },
    ],
  }
}

export default function VisualSummaryTab(props: { filter: SnapshotFilter; active: boolean; refreshTick?: number }) {
  const { filter, active, refreshTick = 0 } = props
  const [summary, setSummary] = useState<ActivitySummary>(EMPTY)
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [tick, setTick] = useState(0)
  const hasInstance = Boolean(filter.instanceId)

  const load = useCallback(async () => {
    if (!hasInstance) {
      setSummary(EMPTY)
      setLoaded(true)
      return
    }
    setLoading(true)
    try {
      const res = await fetchActivitySummary({
        instanceId: filter.instanceId,
        startTime: filter.startTime,
        endTime: filter.endTime,
        topN: TOP_N,
      })
      setSummary(res)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载可视汇总失败')
      setSummary(EMPTY)
    } finally {
      setLoaded(true)
      setLoading(false)
    }
  }, [hasInstance, filter.instanceId, filter.startTime, filter.endTime])

  useEffect(() => {
    if (!active) return
    void load()
  }, [active, load, tick, refreshTick])

  const stateOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: 'item' },
      legend: { type: 'scroll', bottom: 0 },
      series: [
        {
          type: 'pie',
          radius: ['40%', '68%'],
          center: ['50%', '46%'],
          data: summary.stateDistribution.map((d) => ({ name: d.name, value: d.value })),
          label: { formatter: '{b}: {c}' },
        },
      ],
    }),
    [summary.stateDistribution],
  )

  const durOption = useMemo<EChartsOption>(
    () => ({
      grid: { left: 8, right: 16, top: 24, bottom: 8, containLabel: true },
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
      xAxis: { type: 'category', data: summary.durationBuckets.map((d) => d.name) },
      yAxis: { type: 'value', minInterval: 1 },
      series: [
        {
          type: 'bar',
          data: summary.durationBuckets.map((d) => d.value),
          itemStyle: { color: '#faad14' },
          barMaxWidth: 48,
          label: { show: true, position: 'top' },
        },
      ],
    }),
    [summary.durationBuckets],
  )

  const trendOption = useMemo<EChartsOption>(
    () => ({
      grid: { left: 8, right: 24, top: 24, bottom: 8, containLabel: true },
      tooltip: { trigger: 'axis' },
      xAxis: { type: 'category', data: summary.trend.map((d) => d.time), boundaryGap: false },
      yAxis: { type: 'value', minInterval: 1 },
      series: [
        {
          type: 'line',
          smooth: true,
          areaStyle: {},
          data: summary.trend.map((d) => d.value),
          itemStyle: { color: '#1677ff' },
        },
      ],
    }),
    [summary.trend],
  )

  const dbOption = useMemo(() => barOption(summary.topDatabases, '#52c41a'), [summary.topDatabases])
  const userOption = useMemo(() => barOption(summary.topUsers, '#722ed1'), [summary.topUsers])
  const instOption = useMemo(() => barOption(summary.topInstances, '#13c2c2'), [summary.topInstances])

  if (!hasInstance) {
    return (
      <div className="visual-empty">
        <Empty description="请先选择实例后查看可视汇总" />
      </div>
    )
  }

  if (loading && !loaded) {
    return (
      <div className="visual-empty">
        <Spin tip="加载中…" />
      </div>
    )
  }

  if (loaded && summary.total === 0) {
    return (
      <div className="visual-empty">
        <Empty description="当前筛选无快照数据，请调整实例或时间范围后重试" />
      </div>
    )
  }

  return (
    <div className="visual-summary">
      <div className="visual-summary-head">
        <Typography.Text type="secondary">
          {summary.timeRange
            ? `时间窗：${summary.timeRange.start} ~ ${summary.timeRange.end} · 共 ${summary.total} 条快照`
            : `共 ${summary.total} 条快照`}
        </Typography.Text>
        <Button
          size="small"
          icon={<ReloadOutlined />}
          loading={loading}
          onClick={() => setTick((n) => n + 1)}
        >
          刷新
        </Button>
      </div>
      <div className="visual-grid">
        <Card size="small" title="会话状态分布" className="visual-card">
          <ChartBox option={stateOption} height="260px" />
        </Card>
        <Card size="small" title="事务时长分布" className="visual-card">
          <ChartBox option={durOption} height="260px" />
        </Card>
        <Card size="small" title="TOP 数据库" className="visual-card">
          <ChartBox option={dbOption} height="260px" />
        </Card>
        <Card size="small" title="TOP 用户" className="visual-card">
          <ChartBox option={userOption} height="260px" />
        </Card>
        <Card size="small" title="TOP 实例" className="visual-card">
          <ChartBox option={instOption} height="260px" />
        </Card>
        <Card size="small" title="活跃会话时间趋势" className="visual-card">
          <ChartBox option={trendOption} height="260px" />
        </Card>
      </div>
    </div>
  )
}
