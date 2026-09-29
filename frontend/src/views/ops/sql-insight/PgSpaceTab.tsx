import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button,
  Drawer,
  Empty,
  InputNumber,
  Progress,
  Segmented,
  Select,
  Space,
  Tag,
  Typography,
  message,
} from 'antd'
import ResponsiveTable from '../../../components/ResponsiveTable'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'
import { CloudDownloadOutlined, ReloadOutlined } from '@ant-design/icons'
import type { EChartsOption } from 'echarts'
import ChartBox from '../../dashboard/ChartBox'
import type { SnapshotFilter } from './types'
import {
  cancelPgSpaceCollect,
  fetchPgSpaceAdvice,
  fetchPgSpaceAdviceSummary,
  fetchPgSpaceCollectJobs,
  fetchPgSpaceCollectStatus,
  fetchPgSpaceDatabases,
  fetchPgSpaceRealtime,
  fetchPgSpaceSnapshots,
  fetchPgSpaceSummary,
  fetchPgSpaceTrend,
  formatBytes,
  isCollectActive,
  pausePgSpaceCollect,
  resumePgSpaceCollect,
  startPgSpaceCollect,
} from './services/pgSpaceApi'
import { asArray } from './services/asArray'
import './PgSpaceTab.css'

type ViewKey = 'overview' | 'history' | 'trend' | 'advice' | 'adviceSummary'

function sizeCol(title: string, key: string) {
  return {
    title,
    dataIndex: key,
    width: 110,
    render: (v: any) => formatBytes(v),
  }
}

export default function PgSpaceTab(props: {
  filter: SnapshotFilter
  active: boolean
}) {
  const { filter, active } = props
  const [view, setView] = useState<ViewKey>('overview')
  const [mode, setMode] = useState<'summary' | 'realtime'>('summary')
  const [dbScope, setDbScope] = useState<string>('__cluster__')
  const [topN, setTopN] = useState(20)
  const [tick, setTick] = useState(0)
  const [loading, setLoading] = useState(false)
  const [collecting, setCollecting] = useState(false)
  const [payload, setPayload] = useState<any>(null)
  const [history, setHistory] = useState<any[]>([])
  const [historyTotal, setHistoryTotal] = useState(0)
  const [dbOptions, setDbOptions] = useState<{ label: string; value: string }[]>([])
  const [trend, setTrend] = useState<any>(null)
  const [taskOpen, setTaskOpen] = useState(false)
  const [jobs, setJobs] = useState<any[]>([])
  const [activeJobId, setActiveJobId] = useState('')
  const [jobStatus, setJobStatus] = useState<any>(null)

  const hasInstance = Boolean(filter.instanceId)

  const loadDbs = useCallback(async () => {
    if (!filter.instanceId) {
      setDbOptions([])
      return
    }
    try {
      const res = await fetchPgSpaceDatabases(filter.instanceId)
      const list = Array.isArray(res.databases) ? res.databases : []
      setDbOptions(
        list.map((d: any) => ({
          label: `${d.db_name}${d.size_bytes != null ? ` (${formatBytes(d.size_bytes)})` : ''}`,
          value: d.db_name,
        })),
      )
    } catch {
      setDbOptions([])
    }
  }, [filter.instanceId])

  const loadOverview = useCallback(async () => {
    if (!filter.instanceId) {
      setPayload(null)
      return
    }
    setLoading(true)
    try {
      if (mode === 'realtime') {
        const dbName =
          dbScope === '__cluster__' ? '' : dbScope === '__all__' ? '__all__' : dbScope
        setPayload(await fetchPgSpaceRealtime(filter.instanceId, dbName, topN))
      } else {
        setPayload(await fetchPgSpaceSummary(filter.instanceId, topN))
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
      setPayload(null)
    } finally {
      setLoading(false)
    }
  }, [filter.instanceId, mode, dbScope, topN])

  const loadHistory = useCallback(async () => {
    if (!filter.instanceId) {
      setHistory([])
      setHistoryTotal(0)
      return
    }
    setLoading(true)
    try {
      const res = await fetchPgSpaceSnapshots({
        instance_id: filter.instanceId,
        object_type: 'database',
        only_latest: false,
        current: 1,
        page_size: 50,
      })
      setHistory(asArray(res.list ?? res))
      setHistoryTotal(Number(res.total) || asArray(res.list ?? res).length)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载历史失败')
      setHistory([])
    } finally {
      setLoading(false)
    }
  }, [filter.instanceId])

  const loadAdvice = useCallback(async () => {
    setLoading(true)
    try {
      if (view === 'adviceSummary') {
        setPayload(await fetchPgSpaceAdviceSummary())
      } else if (filter.instanceId) {
        setPayload(await fetchPgSpaceAdvice(filter.instanceId))
      } else {
        setPayload(null)
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载建议失败')
      setPayload(null)
    } finally {
      setLoading(false)
    }
  }, [view, filter.instanceId])

  const loadTrend = useCallback(async () => {
    if (!filter.instanceId) {
      setTrend(null)
      return
    }
    setLoading(true)
    try {
      setTrend(
        await fetchPgSpaceTrend({
          instance_id: filter.instanceId,
          start_time: filter.startTime,
          end_time: filter.endTime,
        }),
      )
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载趋势失败')
      setTrend(null)
    } finally {
      setLoading(false)
    }
  }, [filter.instanceId, filter.startTime, filter.endTime])

  useEffect(() => {
    if (!active) return
    void loadDbs()
  }, [active, loadDbs, tick])

  useEffect(() => {
    if (!active) return
    if (view === 'overview') void loadOverview()
    else if (view === 'history') void loadHistory()
    else if (view === 'trend') void loadTrend()
    else void loadAdvice()
  }, [active, view, loadOverview, loadHistory, loadTrend, loadAdvice, tick])

  async function pollJob(jobId: string) {
    setActiveJobId(jobId)
    for (let i = 0; i < 60; i++) {
      const st = await fetchPgSpaceCollectStatus(jobId)
      setJobStatus(st)
      const state = String(st?.state || st?.status || '').toLowerCase()
      if (['succeeded', 'success', 'done', 'completed'].includes(state)) {
        message.success('采集完成')
        setTick((n) => n + 1)
        return
      }
      if (['failed', 'cancelled', 'canceled', 'error'].includes(state)) {
        message.error(st?.error_message || '采集失败')
        return
      }
      await new Promise((r) => setTimeout(r, 2000))
    }
  }

  async function openTasks() {
    setTaskOpen(true)
    try {
      const list = await fetchPgSpaceCollectJobs(20)
      setJobs(asArray(list))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载任务失败')
    }
  }

  async function onCollect() {
    setCollecting(true)
    setTaskOpen(true)
    try {
      const start = await startPgSpaceCollect(filter.instanceId || undefined)
      const jobId = start?.job_id || start?.id
      const list = await fetchPgSpaceCollectJobs(20)
      setJobs(asArray(list))
      if (!jobId) {
        message.success('已触发采集')
        setTick((n) => n + 1)
        return
      }
      message.info('采集任务已启动…')
      await pollJob(String(jobId))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '采集失败')
    } finally {
      setCollecting(false)
    }
  }

  const trendOption = useMemo<EChartsOption>(() => {
    const points = trend?.points || trend?.series || trend?.data || []
    const list = Array.isArray(points) ? points : []
    return {
      tooltip: { trigger: 'axis' },
      grid: { left: 8, right: 16, top: 24, bottom: 8, containLabel: true },
      xAxis: {
        type: 'category',
        data: list.map((p: any) => p.time || p.collected_at || p.date || ''),
      },
      yAxis: { type: 'value' },
      series: [
        {
          type: 'line',
          smooth: true,
          data: list.map((p: any) => Number(p.size_bytes ?? p.value ?? p.total_bytes ?? 0)),
        },
      ],
    }
  }, [trend])

  const tablespaceCols = useMemo(
    () => [
      { title: '表空间', dataIndex: 'object_name', width: 160 },
      sizeCol('大小', 'size_bytes'),
    ],
    [],
  )
  const dbCols = useMemo(
    () => [
      { title: '数据库', dataIndex: 'object_name', width: 160 },
      sizeCol('大小', 'size_bytes'),
      { title: '表空间', dataIndex: 'tablespace_name', width: 120 },
    ],
    [],
  )
  const tableCols = useMemo(
    () => [
      { title: '库', dataIndex: 'db_name', width: 120 },
      { title: 'Schema', dataIndex: 'schema_name', width: 100 },
      { title: '表', dataIndex: 'object_name', width: 160 },
      sizeCol('大小', 'size_bytes'),
      sizeCol('膨胀', 'bloat_bytes'),
      {
        title: '死元组比',
        dataIndex: 'dead_ratio',
        width: 90,
        render: (v: any) => (v == null ? '-' : `${(Number(v) * 100).toFixed(1)}%`),
      },
    ],
    [],
  )

  if (!hasInstance && view !== 'adviceSummary') {
    return (
      <div className="pgspace-empty">
        <Empty description="请先选择实例后查看表空间分析" />
      </div>
    )
  }

  return (
    <div className="pgspace-tab">
      <div className="pgspace-toolbar">
        <Segmented
          value={view}
          onChange={(v) => setView(v as ViewKey)}
          options={[
            { label: '空间概览', value: 'overview' },
            { label: '历史明细', value: 'history' },
            { label: '趋势', value: 'trend' },
            { label: '分析建议', value: 'advice' },
            { label: '建议汇总', value: 'adviceSummary' },
          ]}
        />
        <Space wrap>
          {view === 'overview' ? (
            <>
              <Segmented
                size="small"
                value={mode}
                onChange={(v) => setMode(v as 'summary' | 'realtime')}
                options={[
                  { label: '快照汇总', value: 'summary' },
                  { label: '实时直连', value: 'realtime' },
                ]}
              />
              <Select
                size="small"
                style={{ width: 200 }}
                value={dbScope}
                onChange={setDbScope}
                options={[
                  { label: '仅集群级', value: '__cluster__' },
                  { label: '扫描全部库', value: '__all__' },
                  ...dbOptions,
                ]}
              />
              <span className="muted">Top-N</span>
              <InputNumber size="small" min={5} max={100} value={topN} onChange={(v) => setTopN(Number(v) || 20)} />
            </>
          ) : null}
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={() => setTick((n) => n + 1)}>
            刷新
          </Button>
          <Auth urn={Perm.sqlInsight.spaceCollect}>
            <Button
              size="small"
              type={isCollectActive(jobStatus?.state || jobStatus?.status) ? 'primary' : 'default'}
              onClick={() => void openTasks()}
            >
              采集任务
            </Button>
          </Auth>
          <Auth urn={Perm.sqlInsight.spaceCollect}>
            <Button
              size="small"
              type="primary"
              icon={<CloudDownloadOutlined />}
              loading={collecting}
              onClick={() => void onCollect()}
            >
              手动采集
            </Button>
          </Auth>
        </Space>
      </div>

      {view === 'overview' && payload ? (
        <div className="pgspace-sections">
          <Typography.Text type="secondary">
            {payload.collected_at ? `采集时间：${payload.collected_at}` : null}
            {Array.isArray(payload.warnings) && payload.warnings.length
              ? ` · 警告：${payload.warnings.slice(0, 2).join('；')}`
              : null}
          </Typography.Text>
          <ResponsiveTable
            size="small"
            title={() => '表空间'}
            rowKey={(r: any, i?: number) => `ts-${r?.object_name ?? i}`}
            columns={tablespaceCols}
            dataSource={asArray(payload.tablespaces)}
            pagination={false}
            loading={loading}
            scroll={{ x: 'max-content', y: 200 }}
          />
          <ResponsiveTable
            size="small"
            title={() => '数据库'}
            rowKey={(r: any, i?: number) => `db-${r?.object_name ?? i}`}
            columns={dbCols}
            dataSource={asArray(payload.databases)}
            pagination={false}
            loading={loading}
            scroll={{ x: 'max-content', y: 200 }}
          />
          <ResponsiveTable
            size="small"
            title={() => '大表 Top'}
            rowKey={(r: any, i?: number) => `tb-${r?.db_name}-${r?.object_name ?? i}`}
            columns={tableCols}
            dataSource={asArray(payload.tables || payload.top_tables)}
            pagination={false}
            loading={loading}
            scroll={{ x: 'max-content', y: 280 }}
          />
        </div>
      ) : null}

      {view === 'history' ? (
        <ResponsiveTable
          size="small"
          loading={loading}
          rowKey={(r) => String(r.id)}
          dataSource={history}
          pagination={{ total: historyTotal, pageSize: 50, showTotal: (t) => `共 ${t} 条` }}
          columns={[
            { title: '采集时间', dataIndex: 'collected_at', width: 180 },
            { title: '库', dataIndex: 'object_name', width: 160 },
            sizeCol('大小', 'size_bytes'),
            { title: '表空间', dataIndex: 'tablespace_name', width: 140 },
          ]}
          scroll={{ x: 'max-content', y: 420 }}
          locale={{ emptyText: '暂无快照，请先手动采集' }}
        />
      ) : null}

      {view === 'trend' ? (
        <div style={{ height: 360 }}>
          {trend ? <ChartBox option={trendOption} height="360px" /> : <Empty description={loading ? '加载中…' : '暂无趋势数据'} />}
        </div>
      ) : null}

      {view === 'advice' || view === 'adviceSummary' ? (
        <div className="pgspace-sections">
          {payload ? (
            <>
              <Space wrap>
                <Tag color={payload.severity === 'critical' ? 'error' : payload.severity === 'warning' ? 'warning' : 'default'}>
                  严重度：{payload.severity || '-'}
                </Tag>
                {payload.advice_count != null ? <Tag>建议数 {payload.advice_count}</Tag> : null}
                {payload.issue_instance_count != null ? (
                  <Tag>问题实例 {payload.issue_instance_count}</Tag>
                ) : null}
                {payload.reclaimable_bytes != null || payload.reclaimable_bytes_total != null ? (
                  <Tag>
                    可回收 {formatBytes(payload.reclaimable_bytes ?? payload.reclaimable_bytes_total)}
                  </Tag>
                ) : null}
              </Space>
              <ResponsiveTable
                size="small"
                loading={loading}
                rowKey={(_, i) => String(i)}
                dataSource={
                  view === 'adviceSummary'
                    ? asArray(payload.instances)
                    : asArray(payload.advices)
                }
                pagination={false}
                columns={
                  view === 'adviceSummary'
                    ? [
                        { title: '实例', dataIndex: 'instance_name', width: 160 },
                        { title: 'Host', render: (_: any, r: any) => `${r.host}:${r.port}`, width: 180 },
                        { title: '严重度', dataIndex: 'severity', width: 100 },
                        { title: '建议数', dataIndex: 'advice_count', width: 90 },
                        sizeCol('可回收', 'reclaimable_bytes'),
                      ]
                    : [
                        { title: '标题', dataIndex: 'title', width: 180 },
                        { title: '严重度', dataIndex: 'severity', width: 90 },
                        { title: '动作', dataIndex: 'action', width: 120 },
                        { title: '说明', dataIndex: 'message' },
                      ]
                }
                scroll={{ x: 'max-content', y: 420 }}
              />
            </>
          ) : (
            <Empty description={loading ? '加载中…' : '暂无建议数据'} />
          )}
        </div>
      ) : null}

      {view === 'overview' && !payload && !loading ? (
        <Empty description="暂无汇总数据，可切换实时直连或先手动采集" />
      ) : null}

      <Drawer open={taskOpen} title="表空间采集任务" width={560} onClose={() => setTaskOpen(false)}>
        {activeJobId ? (
          <div style={{ marginBottom: 16 }}>
            <Typography.Text>当前任务：{activeJobId}</Typography.Text>
            <Progress
              style={{ marginTop: 8 }}
              percent={(() => {
                const p = Number(jobStatus?.progress ?? jobStatus?.percent)
                if (!Number.isFinite(p)) return undefined
                return Math.round(p <= 1 ? p * 100 : p)
              })()}
              status="active"
            />
            <Typography.Text type="secondary">状态：{jobStatus?.state || jobStatus?.status || '-'}</Typography.Text>
            <Space style={{ marginTop: 8 }}>
              <Button size="small" onClick={() => void pausePgSpaceCollect(activeJobId).then(() => pollJob(activeJobId))}>
                暂停
              </Button>
              <Button size="small" onClick={() => void resumePgSpaceCollect(activeJobId).then(() => pollJob(activeJobId))}>
                恢复
              </Button>
              <Button size="small" danger onClick={() => void cancelPgSpaceCollect(activeJobId)}>
                取消
              </Button>
            </Space>
          </div>
        ) : null}
        <ResponsiveTable
          size="small"
          rowKey={(r) => String(r.id || r.job_id)}
          dataSource={jobs}
          pagination={false}
          columns={[
            { title: '任务', dataIndex: 'id', width: 120, render: (v, r) => v || r.job_id },
            { title: '状态', dataIndex: 'state', width: 100, render: (v, r) => v || r.status },
            {
              title: '操作',
              width: 80,
              render: (_: any, r: any) => (
                <Button type="link" size="small" onClick={() => void pollJob(String(r.id || r.job_id))}>
                  跟踪
                </Button>
              ),
            },
          ]}
        />
      </Drawer>
    </div>
  )
}
