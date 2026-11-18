import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Button, DatePicker, Empty, Modal, Select, Space } from 'antd'
import dayjs, { type Dayjs } from 'dayjs'
import api from '../../api/request'
import { reportLabel } from './HostDiskReportBody'

type SavedReport = {
  id: string
  scope: string
  created_at: string
}

export type AnalysisHost = { host: string; domain?: string }
export type AnalysisEnv = { id: string; name?: string; code?: string }

export default function HostDiskAnalysis({
  open,
  onClose,
  hosts,
  envs,
}: {
  open: boolean
  onClose: () => void
  hosts: AnalysisHost[]
  envs: AnalysisEnv[]
}) {
  const navigate = useNavigate()
  const [domain, setDomain] = useState<string>()
  const [host, setHost] = useState<string>()
  const [env, setEnv] = useState<string>()
  const [range, setRange] = useState<[Dayjs, Dayjs]>([dayjs().subtract(7, 'day'), dayjs()])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [history, setHistory] = useState<SavedReport[]>([])
  const [historyPick, setHistoryPick] = useState(0)
  const [jobId, setJobId] = useState('')

  const loadHistory = async () => {
    try {
      const { data } = await api.get('/analysis-reports', { params: { kind: 'host_disk' } })
      setHistory((data?.data || []) as SavedReport[])
    } catch {
      setHistory([])
    }
  }

  useEffect(() => {
    if (!open) return
    void loadHistory()
  }, [open])

  useEffect(() => {
    if (!open || !jobId) return
    let stop = false
    const tick = async () => {
      try {
        const { data } = await api.get(`/instance-overview/host-disk-trend/jobs/${jobId}`)
        if (stop) return
        if (data?.status === 'done') {
          setJobId('')
          setNotice('分析完成，可在下方打开这份报告')
          void loadHistory()
          return
        }
        if (data?.status === 'error') {
          setJobId('')
          setError(data.message || '分析失败')
          return
        }
        setNotice('正在后台分析，可以关掉窗口，完成后从历史报告打开')
      } catch {
        if (!stop) setNotice('正在查询分析进度')
      }
    }
    void tick()
    const timer = window.setInterval(() => void tick(), 3000)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [open, jobId])

  const domainOptions = useMemo(() => {
    const set = new Set<string>()
    for (const h of hosts) if (h.domain) set.add(h.domain)
    return [...set].sort().map((v) => ({ value: v, label: v }))
  }, [hosts])

  const hostOptions = useMemo(() => {
    const set = new Set<string>()
    for (const h of hosts) {
      if (!h.host) continue
      if (domain && h.domain !== domain) continue
      set.add(h.host)
    }
    return [...set].sort().map((v) => ({ value: v, label: v }))
  }, [hosts, domain])

  const run = async () => {
    if (!range?.[0] || !range?.[1]) {
      setError('请选择开始和结束时间')
      return
    }
    setSubmitting(true)
    setError('')
    setNotice('')
    try {
      const { data } = await api.post('/instance-overview/host-disk-trend', {
        domain: domain || '',
        host: host || '',
        env: env || '',
        start: range[0].toISOString(),
        end: range[1].toISOString(),
      })
      if (data?.status === 'error') {
        setError(data.message || '分析失败')
        return
      }
      setJobId(data?.job_id || '')
      setNotice(data?.status === 'running' ? '已提交，正在后台分析。可以关掉窗口，完成后从历史报告打开' : '')
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message
      setError(msg || '分析失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title="智能分析：磁盘、网络、连接数、CPU、IO"
      open={open}
      onCancel={onClose}
      footer={null}
      width={1280}
      destroyOnClose={false}
    >
      <Space wrap style={{ marginBottom: 12 }}>
        <Select
          allowClear
          showSearch
          placeholder="全部域名"
          style={{ width: 260 }}
          value={domain}
          onChange={(v) => {
            setDomain(v)
            setHost(undefined)
          }}
          options={domainOptions}
        />
        <Select
          allowClear
          showSearch
          placeholder="全部地址"
          style={{ width: 180 }}
          value={host}
          onChange={setHost}
          options={hostOptions}
        />
        <Select
          allowClear
          placeholder="全部环境"
          style={{ width: 160 }}
          value={env}
          onChange={setEnv}
          options={[
            { value: 'unset', label: '未设置环境' },
            ...envs.map((e) => ({ value: e.id, label: e.name || e.code || e.id })),
          ]}
        />
        <DatePicker.RangePicker
          showTime
          allowClear={false}
          value={range}
          disabledDate={(d) => !!d && d.isAfter(dayjs())}
          presets={[
            { label: '近 7 天', value: [dayjs().subtract(7, 'day'), dayjs()] },
            { label: '近 30 天', value: [dayjs().subtract(30, 'day'), dayjs()] },
            { label: '近 90 天', value: [dayjs().subtract(90, 'day'), dayjs()] },
          ]}
          onChange={(v) => {
            if (v?.[0] && v?.[1]) setRange([v[0], v[1]])
          }}
        />
        <Space wrap={false}>
          <Button type="primary" loading={submitting} disabled={!!jobId} onClick={() => void run()}>
            开始分析
          </Button>
          <Select
            key={historyPick}
            allowClear
            showSearch
            placeholder="打开以前的报告"
            style={{ width: 156 }}
            popupMatchSelectWidth={false}
            dropdownStyle={{ minWidth: 420 }}
            onChange={(id) => {
              if (!id) return
              setHistoryPick((n) => n + 1)
              onClose()
              navigate(`/ops/instance-overview/reports/${id}`)
            }}
            options={history.map((r) => ({ value: r.id, label: reportLabel(r.created_at, r.scope) }))}
            optionFilterProp="label"
          />
        </Space>
      </Space>
      {error ? <Alert type="error" showIcon message={error} style={{ marginBottom: 12 }} /> : null}
      {notice ? <Alert type="info" showIcon message={notice} style={{ marginBottom: 12 }} /> : null}
      {!notice && !error ? <Empty description="选择时间和范围后点「开始分析」。分析在后台进行，完成后从历史报告打开" /> : null}
    </Modal>
  )
}
