import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Modal, Pagination, Select, Switch, Tag } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import { message } from 'antd'
import api from '../../api/request'
import './Sessions.css'

function instLabel(i: any) {
  const host = i.host || i.domain_name || '-'
  return `${i.name || host} (${host}:${i.port})`
}
function isPG(i: any) {
  const t = String(i.db_type || '').toLowerCase()
  return t === 'postgres' || t === 'postgresql' || t === 'pg'
}
function stateColor(state: any): string {
  const s = String(state || '').toLowerCase()
  if (s.includes('active')) return 'success'
  if (s.includes('idle in transaction')) return 'warning'
  if (s.includes('idle')) return 'default'
  if (s.includes('abort')) return 'error'
  return 'default'
}

export default function Sessions() {
  const [instances, setInstances] = useState<any[]>([])
  const [instanceId, setInstanceId] = useState('')
  const [tab, setTab] = useState<'list' | 'locks'>('list')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [intervalSec, setIntervalSec] = useState(15)
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [sessions, setSessions] = useState<any[]>([])
  const [locks, setLocks] = useState<any[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const instanceIdRef = useRef(instanceId)
  const tabRef = useRef(tab)
  instanceIdRef.current = instanceId
  tabRef.current = tab

  const sessionView = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return sessions
    return sessions.filter((r) => String(r.query || '').toLowerCase().includes(q))
  }, [keyword, sessions])

  const lockView = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return locks
    return locks.filter((r) =>
      String(r.relation || '').toLowerCase().includes(q) ||
      String(r.database || '').toLowerCase().includes(q) ||
      String(r.pid).includes(q),
    )
  }, [keyword, locks])

  const pageTotal = tab === 'locks' ? lockView.length : sessionView.length

  const sessionPageRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return sessionView.slice(start, start + pageSize)
  }, [sessionView, page, pageSize])

  const lockPageRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return lockView.slice(start, start + pageSize)
  }, [lockView, page, pageSize])

  async function loadInstances() {
    const { data } = await api.get('/instances')
    setInstances((data.data || []).filter(isPG))
  }

  async function loadSessions() {
    if (!instanceIdRef.current) {
      setSessions([])
      return
    }
    setLoading(true)
    try {
      const { data } = await api.get('/pg-sessions/activities', { params: { instance_id: instanceIdRef.current } })
      setSessions(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  async function loadLocks() {
    if (!instanceIdRef.current) {
      setLocks([])
      return
    }
    setLoading(true)
    try {
      const { data } = await api.get('/pg-sessions/locks', { params: { instance_id: instanceIdRef.current } })
      setLocks(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  function reload() {
    if (tabRef.current === 'locks') return loadLocks()
    return loadSessions()
  }

  function onInstanceChange(v: string) {
    setInstanceId(v || '')
    instanceIdRef.current = v || ''
    setKeyword('')
    setPage(1)
    reload()
  }

  function cancelPid(row: any) {
    Modal.confirm({
      title: '退出执行',
      content: `确认对会话 ${row.pid} 执行 pg_cancel_backend（退出当前查询）？`,
      onOk: async () => {
        await api.post(`/pg-sessions/${row.pid}/cancel`, { instance_id: instanceId }, { params: { instance_id: instanceId } })
        message.success(`已请求退出执行：会话 ${row.pid}`)
        await loadSessions()
      },
    })
  }

  function terminatePid(row: any) {
    Modal.confirm({
      title: '关闭会话',
      content: `确认对会话 ${row.pid} 执行 pg_terminate_backend（关闭连接）？`,
      okText: '关闭会话',
      onOk: async () => {
        await api.post(`/pg-sessions/${row.pid}/terminate`, { instance_id: instanceId }, { params: { instance_id: instanceId } })
        message.success(`已请求关闭会话：${row.pid}`)
        await loadSessions()
      },
    })
  }

  useEffect(() => {
    setPage(1)
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])

  useEffect(() => { setPage(1) }, [keyword])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(pageTotal / pageSize) || 1)
    if (page > maxPage) setPage(maxPage)
  }, [pageTotal, pageSize, page])

  useEffect(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
    if (autoRefresh && instanceId) {
      timerRef.current = setInterval(reload, intervalSec * 1000)
    }
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoRefresh, intervalSec, instanceId])

  useEffect(() => {
    loadInstances()
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [])

  const sessionColumns: ColumnsType<any> = [
    { title: '会话 ID', dataIndex: 'pid', width: 90 },
    { title: '数据库', dataIndex: 'datname', width: 130 },
    { title: '用户名', dataIndex: 'usename', width: 120 },
    {
      title: '会话状态',
      dataIndex: 'state',
      width: 140,
      render: (v) => <Tag color={stateColor(v)}>{v || '-'}</Tag>,
    },
    { title: '执行中语句', dataIndex: 'query', ellipsis: true, width: 220 },
    { title: '连接程序名', dataIndex: 'application_name', width: 140, ellipsis: true },
    { title: '客户端地址', dataIndex: 'client_addr', width: 130 },
    { title: '连接启动时间', dataIndex: 'backend_start', width: 170 },
    { title: '连接持续时间', dataIndex: 'duration', width: 120 },
    { title: '等待事件类型', dataIndex: 'wait_event_type', width: 130 },
    { title: '等待事件', dataIndex: 'wait_event', width: 130 },
    {
      title: '操作',
      width: 170,
      fixed: 'right',
      render: (_, row) => (
        <>
          <Button type="link" size="small" onClick={() => cancelPid(row)}>退出执行</Button>
          <Button type="link" size="small" danger onClick={() => terminatePid(row)}>关闭会话</Button>
        </>
      ),
    },
  ]

  const lockColumns: ColumnsType<any> = [
    { title: '锁类型', dataIndex: 'locktype', width: 130 },
    { title: '数据库', dataIndex: 'database', width: 120 },
    { title: 'PID', dataIndex: 'pid', width: 90 },
    { title: '模式', dataIndex: 'mode', width: 150 },
    { title: 'Relation', dataIndex: 'relation', width: 160 },
  ]

  return (
    <div className="ops-page">
      <div className="ops-filters">
        <div className="filter-item">
          <span className="label">实例</span>
          <Select
            value={instanceId || undefined}
            showSearch
            allowClear
            placeholder="选择 PostgreSQL 实例"
            style={{ flex: 1 }}
            optionFilterProp="label"
            onChange={onInstanceChange}
            options={instances.map((i) => ({ label: instLabel(i), value: i.id }))}
          />
        </div>
      </div>

      <Tabs value={tab} onChange={setTab} />

      <div className="toolbar extra">
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
        <Button size="small" loading={loading} onClick={reload}>刷新</Button>
        <Input
          value={keyword}
          allowClear
          size="small"
          placeholder={tab === 'list' ? '模糊搜索 SQL 语句' : '搜索 PID / 库 / relation'}
          style={{ width: 260, marginLeft: 'auto' }}
          onChange={(e) => setKeyword(e.target.value)}
        />
      </div>

      {tab === 'list' ? (
        <div className="ops-table">
          <ResponsiveTable
            rowKey={(r) => String(r.pid)}
            loading={loading}
            dataSource={sessionPageRows}
            columns={sessionColumns}
            bordered
            pagination={false}
            scroll={{ y: 'calc(100% - 8px)', x: 'max-content' }}
            locale={{ emptyText: '请选择实例后查看会话' }}
          />
        </div>
      ) : (
        <div className="ops-table">
          <ResponsiveTable
            rowKey={(r, i) => `${r.pid}-${i}`}
            loading={loading}
            dataSource={lockPageRows}
            columns={lockColumns}
            bordered
            pagination={false}
            scroll={{ y: 'calc(100% - 8px)', x: 'max-content' }}
            locale={{ emptyText: '请选择实例后查看锁信息' }}
          />
        </div>
      )}

      <div className="pager">
        <Pagination
          current={page}
          pageSize={pageSize}
          total={pageTotal}
          pageSizeOptions={[10, 20, 50]}
          showSizeChanger
          showQuickJumper
          showTotal={(t) => `共 ${t} 条`}
          onChange={(p, ps) => { setPage(p); setPageSize(ps) }}
        />
      </div>
    </div>
  )
}

// 简易 Tab 组件，保持与旧项目两个页签的一致视觉
function Tabs({ value, onChange }: { value: string; onChange: (v: 'list' | 'locks') => void }) {
  const items: { key: 'list' | 'locks'; label: string }[] = [
    { key: 'list', label: '会话列表' },
    { key: 'locks', label: '锁信息' },
  ]
  return (
    <div className="sessions-tabs">
      {items.map((it) => (
        <button
          key={it.key}
          type="button"
          className={`sessions-tab${value === it.key ? ' active' : ''}`}
          onClick={() => onChange(it.key)}
        >
          {it.label}
        </button>
      ))}
    </div>
  )
}
