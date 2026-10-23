import React, { useEffect, useState } from 'react'
import { Button, Select, Space } from 'antd'
import api from '../../api/request'
import { useReloadOnTabActive } from '../../hooks/useReloadOnTabActive'
import ActivityTimeline, { ActivitySummary } from './ActivityTimeline'
import './AlertActivity.css'

const RANGES = [
  { value: 24, label: '近 24 小时' },
  { value: 72, label: '近 3 天' },
  { value: 168, label: '近 7 天' },
]

const KINDS = [
  { value: '', label: '全部事件' },
  { value: 'alert', label: '告警' },
  { value: 'recover', label: '恢复' },
  { value: 'change', label: '变更' },
  { value: 'inspect', label: '巡检' },
]

export default function AlertActivity() {
  const [hours, setHours] = useState(24)
  const [kinds, setKinds] = useState('')
  const [severity, setSeverity] = useState<string>()
  const [instanceId, setInstanceId] = useState<string>()
  const [instances, setInstances] = useState<{ id: string; name: string }[]>([])
  const [summary, setSummary] = useState<ActivitySummary>({ total: 0, firing: 0 })
  const [refreshKey, setRefreshKey] = useState(0)
  const [since, setSince] = useState(() => Math.floor(Date.now() / 1000) - 24 * 3600)

  useEffect(() => {
    setSince(Math.floor(Date.now() / 1000) - hours * 3600)
  }, [hours, refreshKey])

  useEffect(() => {
    api.get('/instances').then(({ data }) => {
      setInstances((data.data || []).map((it: any) => ({ id: it.id, name: it.name || it.host })))
    }).catch(() => undefined)
  }, [])

  useReloadOnTabActive('/ops/alerts/activity', () => setRefreshKey((n) => n + 1))

  const rangeText = RANGES.find((r) => r.value === hours)?.label || '近 24 小时'
  return (
    <div className="act-page">
      <div className="act-head">
        <div>
          <h2>实例动态</h2>
          <div className="sub">{rangeText} · {summary.total} 条事件 · {summary.firing} 个告警未恢复</div>
        </div>
        <Space>
          <Select value={hours} style={{ width: 120 }} options={RANGES} onChange={setHours} />
          <Button type="primary" onClick={() => setRefreshKey((n) => n + 1)}>刷新</Button>
        </Space>
      </div>
      <div className="act-filters">
        {KINDS.map((k) => (
          <Button key={k.value || 'all'} type={kinds === k.value ? 'primary' : 'default'} shape="round"
            onClick={() => setKinds(k.value)}>{k.label}</Button>
        ))}
        <Select allowClear placeholder="级别" style={{ width: 100 }} value={severity}
          disabled={kinds === 'change' || kinds === 'inspect'}
          options={['P0', 'P1', 'P2', 'P3'].map((v) => ({ value: v, label: v }))}
          onChange={setSeverity} />
        <Select allowClear showSearch optionFilterProp="label" placeholder="实例" style={{ width: 220 }} value={instanceId}
          options={instances.map((it) => ({ value: it.id, label: it.name }))}
          onChange={setInstanceId} />
      </div>
      <ActivityTimeline
        since={since}
        kinds={kinds}
        severity={kinds === 'change' || kinds === 'inspect' ? undefined : severity}
        instanceId={instanceId}
        poll
        refreshKey={refreshKey}
        onSummary={setSummary}
      />
    </div>
  )
}
