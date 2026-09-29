import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button,
  DatePicker,
  Drawer,
  Empty,
  Progress,
  Space,
  Statistic,
  Tag,
  Typography,
  message,
} from 'antd'
import ResponsiveTable from '../../../components/ResponsiveTable'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'
import { BarChartOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import type { EChartsOption } from 'echarts'
import ChartBox from '../../dashboard/ChartBox'
import type { SnapshotFilter } from './types'
import {
  cancelAwrGenerate,
  exportAwrReport,
  fetchAwrDetail,
  fetchAwrGenerateJobs,
  fetchAwrGenerateStatus,
  fetchAwrReports,
  fetchAwrSummary,
  parseAwrSections,
  pauseAwrGenerate,
  resumeAwrGenerate,
  startAwrGenerate,
} from './services/awrApi'
import { asArray, asTotal } from './services/asArray'
import {
  AWR_HEALTH_GRADE_COLOR,
  AWR_PEAK_STATUS_COLOR,
  AWR_PEAK_STATUS_TEXT,
  computeAwrHealth,
} from './services/awrHealthScore'

const { Text } = Typography

export default function AwrReportTab(props: { filter: SnapshotFilter; active: boolean }) {
  const { filter, active } = props
  const [loading, setLoading] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [rows, setRows] = useState<any[]>([])
  const [total, setTotal] = useState(0)
  const [current, setCurrent] = useState(1)
  const [detailOpen, setDetailOpen] = useState(false)
  const [detail, setDetail] = useState<any>(null)
  const [jobOpen, setJobOpen] = useState(false)
  const [jobs, setJobs] = useState<any[]>([])
  const [activeJobId, setActiveJobId] = useState('')
  const [jobStatus, setJobStatus] = useState<any>(null)
  const [summaryOpen, setSummaryOpen] = useState(false)
  const [summaryDate, setSummaryDate] = useState(() => dayjs().subtract(1, 'day'))
  const [summary, setSummary] = useState<any>(null)
  const [summaryLoading, setSummaryLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const start = filter.startTime ? dayjs(filter.startTime).format('YYYY-MM-DD') : undefined
      const end = filter.endTime ? dayjs(filter.endTime).format('YYYY-MM-DD') : undefined
      const data = await fetchAwrReports({
        instance_id: filter.instanceId || undefined,
        start_date: start,
        end_date: end,
        current,
        page_size: 20,
      })
      const payload = data?.data
      setRows(asArray(payload))
      setTotal(asTotal(payload, Number(data?.total) || 0))
    } catch (e: any) {
      if (e?.response?.status !== 404) {
        message.error(e?.response?.data?.message || e?.message || '加载 AWR 失败')
      }
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [filter.instanceId, filter.startTime, filter.endTime, current])

  useEffect(() => {
    if (!active) return
    void load()
  }, [active, load])

  const health = useMemo(() => {
    const sections = parseAwrSections(detail?.sections || detail?.sections_json)
    return computeAwrHealth(sections)
  }, [detail])

  const radarOption = useMemo<EChartsOption>(() => {
    const valid = health.axes.filter((a) => a.score != null)
    if (!valid.length) return {}
    return {
      tooltip: {},
      radar: {
        indicator: valid.map((a) => ({ name: a.label, max: 100 })),
        radius: '62%',
      },
      series: [
        {
          type: 'radar',
          data: [{ value: valid.map((a) => a.score as number), name: '健康分', areaStyle: { opacity: 0.15 } }],
        },
      ],
    }
  }, [health])

  async function pollJob(jobId: string) {
    setActiveJobId(jobId)
    for (let i = 0; i < 90; i++) {
      const st = await fetchAwrGenerateStatus(jobId)
      setJobStatus(st)
      const state = String(st?.state || st?.status || '').toLowerCase()
      if (['succeeded', 'success', 'done', 'completed'].includes(state)) {
        message.success('生成完成')
        await load()
        return
      }
      if (['failed', 'cancelled', 'canceled', 'error'].includes(state)) {
        message.error(st?.error_message || st?.message || '生成失败')
        return
      }
      await new Promise((r) => setTimeout(r, 2000))
    }
  }

  async function onGenerate() {
    setGenerating(true)
    setJobOpen(true)
    try {
      const start = await startAwrGenerate({
        instance_id: filter.instanceId || undefined,
        report_date: filter.endTime ? dayjs(filter.endTime).format('YYYY-MM-DD') : undefined,
      })
      const jobId = start?.job_id || start?.id
      const list = await fetchAwrGenerateJobs(20)
      setJobs(asArray(list))
      if (!jobId) {
        message.success('已触发生成')
        await load()
        return
      }
      message.info('AWR 生成任务已启动…')
      await pollJob(String(jobId))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '生成失败（请先在 SQL 洞察设置中配置 Prometheus）')
    } finally {
      setGenerating(false)
    }
  }

  async function openJobs() {
    setJobOpen(true)
    try {
      const list = await fetchAwrGenerateJobs(20)
      setJobs(asArray(list))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载任务失败')
    }
  }

  async function onView(row: any) {
    try {
      const d = await fetchAwrDetail({
        id: row.id,
        instance_id: row.instance_id,
        report_date: row.report_date,
      })
      setDetail(d)
      setDetailOpen(true)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载详情失败')
    }
  }

  async function onExport(row: any) {
    try {
      const blob = await exportAwrReport({
        id: row.id,
        instance_id: row.instance_id,
        report_date: row.report_date,
        format: 'html',
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `awr_${row.report_date || row.id}.html`
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '导出失败')
    }
  }

  async function loadSummary() {
    setSummaryLoading(true)
    try {
      const data = await fetchAwrSummary(summaryDate.format('YYYY-MM-DD'))
      setSummary(data)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载汇总失败')
      setSummary(null)
    } finally {
      setSummaryLoading(false)
    }
  }

  const summaryChart = useMemo<EChartsOption>(() => {
    const items = summary?.severity_counts || summary?.by_severity || []
    const list = Array.isArray(items)
      ? items
      : Object.entries(items || {}).map(([name, value]) => ({ name, value }))
    return {
      tooltip: { trigger: 'item' },
      series: [
        {
          type: 'pie',
          radius: ['40%', '70%'],
          data: list.map((x: any) => ({
            name: x.name || x.severity || 'unknown',
            value: Number(x.value ?? x.count ?? 0),
          })),
        },
      ],
    }
  }, [summary])

  const jobPct = (() => {
    const p = Number(jobStatus?.progress ?? jobStatus?.percent)
    if (Number.isFinite(p)) return Math.min(100, Math.max(0, p <= 1 ? p * 100 : p))
    return undefined
  })()

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Space wrap>
        <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load()}>
          刷新
        </Button>
        <Auth urn={Perm.sqlInsight.awrGenerate}>
          <Button type="primary" icon={<ThunderboltOutlined />} loading={generating} onClick={() => void onGenerate()}>
            手动生成
          </Button>
        </Auth>
        <Auth urn={Perm.sqlInsight.awrGenerate}>
          <Button onClick={() => void openJobs()}>生成任务</Button>
        </Auth>
        <Button
          icon={<BarChartOutlined />}
          onClick={() => {
            setSummaryOpen(true)
            void loadSummary()
          }}
        >
          异常汇总
        </Button>
      </Space>
      <ResponsiveTable
        size="small"
        loading={loading}
        rowKey={(r) => String(r.id || `${r.instance_id}-${r.report_date}`)}
        dataSource={rows}
        pagination={{
          current,
          total,
          pageSize: 20,
          onChange: setCurrent,
          showTotal: (t) => `共 ${t} 条`,
        }}
          locale={{ emptyText: <Empty description="暂无报告。请在右上角「设置」中配置 Prometheus 后生成。" /> }}
        columns={[
          { title: '报告日', dataIndex: 'report_date', width: 120 },
          { title: '实例', dataIndex: 'instance_id', width: 180, ellipsis: true },
          { title: 'Host', render: (_: any, r: any) => (r.host ? `${r.host}:${r.port || ''}` : '-'), width: 180 },
          {
            title: '状态',
            dataIndex: 'status',
            width: 100,
            render: (v: string) => <Tag>{v || '-'}</Tag>,
          },
          {
            title: '严重度',
            dataIndex: 'severity',
            width: 100,
            render: (v: string) =>
              v ? <Tag color={v === 'critical' ? 'error' : v === 'warning' ? 'warning' : 'default'}>{v}</Tag> : '-',
          },
          {
            title: '操作',
            width: 160,
            render: (_: any, row: any) => (
              <Space>
                <Button type="link" size="small" onClick={() => void onView(row)}>
                  查看
                </Button>
                <Button type="link" size="small" onClick={() => void onExport(row)}>
                  导出
                </Button>
              </Space>
            ),
          },
        ]}
      />

      <Drawer open={detailOpen} title="AWR 报告详情" width={720} onClose={() => setDetailOpen(false)}>
        {detail ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            <Space wrap size="large">
              <Statistic title="健康分" value={health.total ?? '-'} suffix={health.total != null ? '/100' : undefined} />
              <Tag color={AWR_HEALTH_GRADE_COLOR[health.grade]}>{health.gradeText}</Tag>
              <Text type="secondary">接近峰值维度：{health.nearPeakCount}</Text>
            </Space>
            {health.axes.some((a) => a.score != null) ? (
              <div style={{ height: 260 }}>
                <ChartBox option={radarOption} />
              </div>
            ) : (
              <Empty description="报告无可用指标，无法计算健康分" />
            )}
            <ResponsiveTable
              size="small"
              pagination={false}
              rowKey="key"
              dataSource={health.axes}
              columns={[
                { title: '维度', dataIndex: 'label', width: 90 },
                { title: '最新', dataIndex: 'last', width: 100, render: (v, r) => (v == null ? '-' : `${v}${r.unit ? ` ${r.unit}` : ''}`) },
                { title: '峰值', dataIndex: 'max', width: 100, render: (v, r) => (v == null ? '-' : `${v}${r.unit ? ` ${r.unit}` : ''}`) },
                { title: '轴分', dataIndex: 'score', width: 80, render: (v) => (v == null ? '-' : Math.round(v)) },
                {
                  title: '相对峰值',
                  dataIndex: 'status',
                  width: 100,
                  render: (v: keyof typeof AWR_PEAK_STATUS_TEXT) => (
                    <Tag color={AWR_PEAK_STATUS_COLOR[v]}>{AWR_PEAK_STATUS_TEXT[v]}</Tag>
                  ),
                },
              ]}
            />
            <Text type="secondary">原始 sections（节选）</Text>
            <pre style={{ maxHeight: 220, overflow: 'auto', background: '#fafafa', padding: 12, fontSize: 12 }}>
              {JSON.stringify(parseAwrSections(detail.sections || detail.sections_json) || detail, null, 2).slice(0, 8000)}
            </pre>
          </div>
        ) : null}
      </Drawer>

      <Drawer open={jobOpen} title="AWR 生成任务" width={560} onClose={() => setJobOpen(false)}>
        {activeJobId ? (
          <div style={{ marginBottom: 16 }}>
            <Text>当前任务：{activeJobId}</Text>
            <div style={{ marginTop: 8 }}>
              <Progress percent={jobPct != null ? Math.round(jobPct) : undefined} status="active" />
            </div>
            <Text type="secondary">状态：{jobStatus?.state || jobStatus?.status || '-'}</Text>
            <Space style={{ marginTop: 8 }}>
              <Button size="small" onClick={() => void pauseAwrGenerate(activeJobId).then(() => pollJob(activeJobId))}>
                暂停
              </Button>
              <Button size="small" onClick={() => void resumeAwrGenerate(activeJobId).then(() => pollJob(activeJobId))}>
                恢复
              </Button>
              <Button size="small" danger onClick={() => void cancelAwrGenerate(activeJobId)}>
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
            { title: '报告日', dataIndex: 'report_date', width: 120 },
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

      <Drawer open={summaryOpen} title="AWR 异常汇总" width={640} onClose={() => setSummaryOpen(false)}>
        <Space style={{ marginBottom: 12 }}>
          <DatePicker value={summaryDate} onChange={(d) => d && setSummaryDate(d)} />
          <Button type="primary" loading={summaryLoading} onClick={() => void loadSummary()}>
            查询
          </Button>
        </Space>
        {summary ? (
          <>
            <Space wrap style={{ marginBottom: 12 }}>
              <Statistic title="报告数" value={summary.total ?? summary.report_count ?? '-'} />
              <Statistic title="异常实例" value={summary.issue_count ?? summary.anomaly_instance_count ?? '-'} />
            </Space>
            <div style={{ height: 260, marginBottom: 12 }}>
              <ChartBox option={summaryChart} />
            </div>
            <ResponsiveTable
              size="small"
              rowKey={(_, i) => String(i)}
              dataSource={asArray(summary.top_instances || summary.instances)}
              pagination={false}
              columns={[
                { title: '实例', dataIndex: 'instance_id', width: 160 },
                { title: 'Host', render: (_: any, r: any) => (r.host ? `${r.host}:${r.port || ''}` : '-'), width: 160 },
                { title: '严重度', dataIndex: 'severity', width: 100 },
              ]}
            />
          </>
        ) : (
          <Empty description={summaryLoading ? '加载中…' : '暂无汇总'} />
        )}
      </Drawer>
    </div>
  )
}
