import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Button,
  DatePicker,
  Drawer,
  Empty,
  Input,
  Modal,
  Pagination,
  Radio,
  Select,
  Switch,
  Table,
  Tabs,
  Tag,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import dayjs, { Dayjs } from 'dayjs'
import api from '../../api/request'
import './SqlInsight.css'

const { RangePicker } = DatePicker

function pad(n: number) {
  return String(n).padStart(2, '0')
}
function defaultRange(): [Dayjs, Dayjs] {
  const end = dayjs()
  const start = end.subtract(1, 'hour')
  return [start, end]
}
function rangeISO(r: [Dayjs, Dayjs] | null): [string, string] {
  const pair = r && r.length === 2 ? r : defaultRange()
  return [pair[0].toDate().toISOString(), pair[1].toDate().toISOString()]
}

function instLabel(i: any) {
  const host = i.host || i.domain_name || '-'
  return `${i.name || host} (${host}:${i.port})`
}
function isPG(i: any) {
  const t = String(i.db_type || '').toLowerCase()
  return t === 'postgres' || t === 'postgresql' || t === 'pg'
}
function formatTime(v: any) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function formatDuration(seconds: any) {
  if (seconds == null || !Number.isFinite(Number(seconds))) return '-'
  const s = Math.max(0, Number(seconds))
  if (s < 1) return `${s.toFixed(2)}s`
  if (s < 60) return `${s.toFixed(1)}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}
function durationClass(v: any) {
  const s = v ?? 0
  if (s >= 10) return 'dur-danger'
  if (s >= 1) return 'dur-warn'
  return ''
}
function stateColor(state: any): string {
  const s = String(state || '').toLowerCase()
  if (s.includes('active')) return 'success'
  if (s.includes('idle in transaction')) return 'warning'
  if (s.includes('abort')) return 'error'
  return 'default'
}
function truncate(text: any, n: number) {
  if (!text) return '-'
  return text.length > n ? `${text.slice(0, n)}…` : text
}

export default function SqlInsight() {
  const [instances, setInstances] = useState<any[]>([])
  const [draftInstance, setDraftInstance] = useState('')
  const [draftRange, setDraftRange] = useState<[Dayjs, Dayjs]>(defaultRange())
  const [instanceId, setInstanceId] = useState('')
  const [range, setRange] = useState<[Dayjs, Dayjs]>(defaultRange())
  const [tab, setTab] = useState('sql')
  const [slow, setSlow] = useState('all')
  const [showSql, setShowSql] = useState(true)
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [intervalSec, setIntervalSec] = useState(15)
  const [loading, setLoading] = useState(false)
  const [collecting, setCollecting] = useState(false)
  const [rows, setRows] = useState<any[]>([])
  const [total, setTotal] = useState(0)
  const [current, setCurrent] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [sqlDrawer, setSqlDrawer] = useState<any>(null)

  const [draftDb, setDraftDb] = useState('')
  const [draftUser, setDraftUser] = useState('')
  const [pDb, setPDb] = useState('')
  const [pUser, setPUser] = useState('')
  const [orderBy, setOrderBy] = useState('count')
  const [statsLoading, setStatsLoading] = useState(false)
  const [statsRows, setStatsRows] = useState<any[]>([])
  const [statsTotal, setStatsTotal] = useState(0)
  const [statsCurrent, setStatsCurrent] = useState(1)
  const [statsPageSize, setStatsPageSize] = useState(20)
  const [statDrawer, setStatDrawer] = useState<any>(null)

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const stateRef = useRef({ instanceId, range, current, pageSize, statsCurrent, statsPageSize, pDb, pUser, orderBy, tab })
  stateRef.current = { instanceId, range, current, pageSize, statsCurrent, statsPageSize, pDb, pUser, orderBy, tab }

  const displayRows = useMemo(() => {
    if (slow === 'all') return rows
    const th = Number(slow)
    return rows.filter((r) => (r.xact_duration ?? 0) > th)
  }, [slow, rows])

  async function copyText(text: any) {
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
      message.success('已复制')
    } catch {
      message.error('复制失败')
    }
  }

  async function loadInstances() {
    const { data } = await api.get('/instances')
    setInstances((data.data || []).filter(isPG))
  }

  async function loadSnapshots() {
    const s = stateRef.current
    if (!s.instanceId) {
      setRows([])
      setTotal(0)
      return
    }
    setLoading(true)
    try {
      const [start, end] = rangeISO(s.range)
      const { data } = await api.get('/pg-activity-snapshots', {
        params: {
          instance_id: s.instanceId,
          start_time: start,
          end_time: end,
          current: s.current,
          page_size: s.pageSize,
        },
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
    } finally {
      setLoading(false)
    }
  }

  async function loadStats() {
    const s = stateRef.current
    if (!s.instanceId) {
      setStatsRows([])
      setStatsTotal(0)
      return
    }
    setStatsLoading(true)
    try {
      const [start, end] = rangeISO(s.range)
      const { data } = await api.get('/pg-activity-snapshots/stats', {
        params: {
          instance_id: s.instanceId,
          start_time: start,
          end_time: end,
          p_db: s.pDb,
          p_user: s.pUser,
          order_by: s.orderBy,
          current: s.statsCurrent,
          page_size: s.statsPageSize,
        },
      })
      setStatsRows(data.data || [])
      setStatsTotal(data.total || 0)
    } finally {
      setStatsLoading(false)
    }
  }

  function applyQuery() {
    const nextRange = draftRange?.length === 2 ? draftRange : defaultRange()
    setInstanceId(draftInstance)
    setRange(nextRange)
    setCurrent(1)
    setStatsCurrent(1)
    stateRef.current = { ...stateRef.current, instanceId: draftInstance, range: nextRange, current: 1, statsCurrent: 1 }
    if (tab === 'analysis') loadStats()
    else loadSnapshots()
  }

  function resetQuery() {
    const next = defaultRange()
    setDraftInstance('')
    setDraftRange(next)
    setInstanceId('')
    setRange(next)
    setRows([])
    setTotal(0)
    setStatsRows([])
    setStatsTotal(0)
    stateRef.current = { ...stateRef.current, instanceId: '', range: next }
    loadInstances()
  }

  function applyStatsFilter() {
    setPDb(draftDb)
    setPUser(draftUser)
    setStatsCurrent(1)
    stateRef.current = { ...stateRef.current, pDb: draftDb, pUser: draftUser, statsCurrent: 1 }
    loadStats()
  }

  function onSizeChange(_: number, size: number) {
    setPageSize(size)
    setCurrent(1)
    stateRef.current = { ...stateRef.current, pageSize: size, current: 1 }
    loadSnapshots()
  }

  function onStatsSizeChange(_: number, size: number) {
    setStatsPageSize(size)
    setStatsCurrent(1)
    stateRef.current = { ...stateRef.current, statsPageSize: size, statsCurrent: 1 }
    loadStats()
  }

  function collect() {
    Modal.confirm({
      title: '手动采集',
      content: instanceId
        ? '将对当前实例立即采集一次活动会话，确定？'
        : '将对所有 PostgreSQL 实例立即采集一次活动会话，可能耗时较久，确定？',
      okText: '确定采集',
      onOk: async () => {
        setCollecting(true)
        try {
          const { data } = await api.post('/pg-activity-snapshots/collect', {
            instance_id: instanceId || undefined,
          }, { timeout: 120000 })
          message.success(`采集完成：处理 ${data.instances_processed ?? 0} 个实例，写入 ${data.rows_written ?? 0} 条`)
          if ((data.warnings || []).length) {
            message.warning(data.warnings.slice(0, 3).join('；'))
          }
          await loadSnapshots()
          if (tab === 'analysis') await loadStats()
        } finally {
          setCollecting(false)
        }
      },
    })
  }

  useEffect(() => {
    if (tab === 'analysis') loadStats()
    if (tab === 'sql') loadSnapshots()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])

  useEffect(() => {
    if (tab === 'analysis') {
      setStatsCurrent(1)
      stateRef.current = { ...stateRef.current, statsCurrent: 1, orderBy }
      loadStats()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderBy])

  useEffect(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
    if (autoRefresh && tab === 'sql') {
      timerRef.current = setInterval(loadSnapshots, intervalSec * 1000)
    }
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoRefresh, intervalSec, tab])

  useEffect(() => {
    loadInstances()
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [])

  const sqlColumns: ColumnsType<any> = [
    { title: '采集时间', dataIndex: 'collected_at', width: 170, render: (v) => formatTime(v) },
    {
      title: '实例',
      width: 180,
      render: (_, row) => (
        <div>
          <div>{row.instance_name || '-'}</div>
          <div className="sub">{row.host}:{row.port}</div>
        </div>
      ),
    },
    { title: 'PID', dataIndex: 'pid', width: 80 },
    { title: '用户', dataIndex: 'p_user', width: 110 },
    { title: '客户端', dataIndex: 'p_host', width: 130 },
    { title: '数据库', dataIndex: 'p_db', width: 120 },
    {
      title: '事务时长',
      width: 110,
      sorter: (a, b) => (a.xact_duration ?? 0) - (b.xact_duration ?? 0),
      render: (_, row) => <span className={durationClass(row.xact_duration)}>{formatDuration(row.xact_duration)}</span>,
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 150,
      render: (v) => <Tag color={stateColor(v)}>{v || '-'}</Tag>,
    },
    ...(showSql ? [{
      title: 'SQL',
      width: 240,
      ellipsis: true,
      render: (_: any, row: any) => (
        row.info
          ? <Button type="link" onClick={() => setSqlDrawer(row)}>{row.info}</Button>
          : <span>-</span>
      ),
    }] : []),
  ]

  const statsColumns: ColumnsType<any> = [
    {
      title: '归一化 SQL',
      width: 260,
      render: (_, row) => <Button type="link" onClick={() => setStatDrawer(row)}>{truncate(row.query_normalized, 80)}</Button>,
    },
    { title: '次数', dataIndex: 'count', width: 80 },
    { title: '平均时长', width: 110, render: (_, row) => formatDuration(row.avg_xact_duration) },
    { title: '最大时长', width: 110, render: (_, row) => formatDuration(row.max_xact_duration) },
    { title: '库数', dataIndex: 'distinct_dbs', width: 70 },
    { title: '首次出现', width: 170, render: (_, row) => formatTime(row.first_seen) },
    { title: '最近出现', width: 170, render: (_, row) => formatTime(row.last_seen) },
  ]

  return (
    <div className="ops-page">
      <div className="ops-filters">
        <div className="filter-item" style={{ flex: 1.2 }}>
          <span className="label">实例</span>
          <Select
            value={draftInstance || undefined}
            showSearch
            allowClear
            placeholder="选择 PostgreSQL 实例"
            style={{ flex: 1 }}
            optionFilterProp="label"
            onChange={(v) => setDraftInstance(v || '')}
            options={instances.map((i) => ({ label: instLabel(i), value: i.id }))}
          />
        </div>
        <div className="filter-item" style={{ flex: 1.6 }}>
          <span className="label">时间范围</span>
          <RangePicker
            value={draftRange}
            showTime
            format="YYYY-MM-DD HH:mm:ss"
            placeholder={['开始', '结束']}
            style={{ width: '100%' }}
            onChange={(v) => { if (v && v[0] && v[1]) setDraftRange([v[0], v[1]]) }}
          />
        </div>
        <Button type="primary" onClick={applyQuery}>查询</Button>
        <Button onClick={resetQuery}>重置</Button>
      </div>

      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          { key: 'sql', label: 'SQL 洞察' },
          { key: 'analysis', label: '洞察分析' },
          { key: 'awr', label: 'AWR' },
          { key: 'space', label: '空间分析' },
        ]}
      />

      {tab === 'sql' ? (
        <div className="toolbar extra">
          <span className="muted">慢事务</span>
          <Radio.Group size="small" value={slow} onChange={(e) => setSlow(e.target.value)}>
            <Radio.Button value="all">全部</Radio.Button>
            <Radio.Button value="1">&gt; 1s</Radio.Button>
            <Radio.Button value="10">&gt; 10s</Radio.Button>
          </Radio.Group>
          <span className="muted">显示 SQL</span>
          <Switch size="small" checked={showSql} onChange={setShowSql} />
          <span className="muted">自动刷新</span>
          <Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} />
          <Select
            value={intervalSec}
            size="small"
            disabled={!autoRefresh}
            style={{ width: 80 }}
            onChange={setIntervalSec}
            options={[15, 30, 45, 60].map((s) => ({ label: `${s}s`, value: s }))}
          />
          <Button size="small" loading={loading} onClick={loadSnapshots}>刷新</Button>
          <Button size="small" type="primary" loading={collecting} onClick={collect}>手动采集</Button>
        </div>
      ) : null}

      {tab === 'analysis' ? (
        <div className="toolbar extra">
          <Input value={draftDb} allowClear placeholder="数据库" style={{ width: 160 }} onChange={(e) => setDraftDb(e.target.value)} />
          <Input value={draftUser} allowClear placeholder="用户" style={{ width: 140 }} onChange={(e) => setDraftUser(e.target.value)} />
          <Radio.Group size="small" value={orderBy} onChange={(e) => setOrderBy(e.target.value)}>
            <Radio.Button value="count">按次数</Radio.Button>
            <Radio.Button value="avg_duration">按平均时长</Radio.Button>
            <Radio.Button value="max_duration">按最大时长</Radio.Button>
            <Radio.Button value="last_seen">按最近出现</Radio.Button>
          </Radio.Group>
          <Button size="small" type="primary" onClick={applyStatsFilter}>查询</Button>
        </div>
      ) : null}

      {tab === 'sql' ? (
        <>
          <div className="ops-table">
            <Table
              rowKey={(r, i) => `${r.pid}-${r.collected_at}-${i}`}
              loading={loading}
              dataSource={displayRows}
              columns={sqlColumns}
              bordered
              pagination={false}
              scroll={{ y: 'calc(100% - 8px)', x: 'max-content' }}
              locale={{ emptyText: '请选择实例并查询' }}
            />
          </div>
          <div className="pager">
            <Pagination
              current={current}
              pageSize={pageSize}
              total={total}
              pageSizeOptions={[10, 20, 50, 100]}
              showSizeChanger
              showTotal={(t) => `共 ${t} 条`}
              onChange={(p) => { setCurrent(p); stateRef.current = { ...stateRef.current, current: p }; loadSnapshots() }}
              onShowSizeChange={onSizeChange}
            />
          </div>
        </>
      ) : tab === 'analysis' ? (
        <>
          <div className="ops-table">
            <Table
              rowKey={(r, i) => `${r.query_normalized || ''}-${i}`}
              loading={statsLoading}
              dataSource={statsRows}
              columns={statsColumns}
              bordered
              pagination={false}
              scroll={{ y: 'calc(100% - 8px)', x: 'max-content' }}
              locale={{ emptyText: '请选择实例并查询' }}
            />
          </div>
          <div className="pager">
            <Pagination
              current={statsCurrent}
              pageSize={statsPageSize}
              total={statsTotal}
              pageSizeOptions={[10, 20, 50, 100]}
              showSizeChanger
              showTotal={(t) => `共 ${t} 条`}
              onChange={(p) => { setStatsCurrent(p); stateRef.current = { ...stateRef.current, statsCurrent: p }; loadStats() }}
              onShowSizeChange={onStatsSizeChange}
            />
          </div>
        </>
      ) : (
        <Empty description="该能力仍在开发中，当前已对齐老项目「SQL 洞察」快照采集与洞察分析" />
      )}

      <Drawer open={!!sqlDrawer} title="SQL 详情" width={520} onClose={() => setSqlDrawer(null)}>
        {sqlDrawer ? (
          <div className="drawer-meta">
            <div>采集时间：{formatTime(sqlDrawer.collected_at)}</div>
            <div>PID：{sqlDrawer.pid} · {sqlDrawer.p_user} @ {sqlDrawer.p_db}</div>
            <div>事务时长：{formatDuration(sqlDrawer.xact_duration)}</div>
          </div>
        ) : null}
        <Input.TextArea value={sqlDrawer?.info || ''} rows={16} readOnly />
        <div className="drawer-actions">
          <Button type="primary" onClick={() => copyText(sqlDrawer?.info)}>复制 SQL</Button>
        </div>
      </Drawer>

      <Drawer open={!!statDrawer} title="归一化 SQL" width={520} onClose={() => setStatDrawer(null)}>
        <Input.TextArea value={statDrawer?.query_normalized || ''} rows={10} readOnly />
        <div className="drawer-meta">样例 SQL</div>
        <Input.TextArea value={statDrawer?.sample_info || ''} rows={10} readOnly />
        <div className="drawer-actions">
          <Button type="primary" onClick={() => copyText(statDrawer?.query_normalized)}>复制模板</Button>
        </div>
      </Drawer>
    </div>
  )
}
