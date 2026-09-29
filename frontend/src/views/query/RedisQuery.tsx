import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Select, Spin, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { StopOutlined } from '@ant-design/icons'
import axios from 'axios'
import api from '../../api/request'
import CollapsibleSide from '../../components/CollapsibleSide'
import './RedisQuery.css'

const HIST_KEY = 'auto-dba-redis-history'

interface RedisResult {
  columns?: string[]
  rows?: any[][]
  row_count?: number
  execution_time_ms?: number
}

interface RedisDb {
  name: string
  keys?: any
}

export default function RedisQuery() {
  const [instLoading, setInstLoading] = useState(false)
  const [dbLoading, setDbLoading] = useState(false)
  const [running, setRunning] = useState(false)
  const queryAbortRef = useRef<AbortController | null>(null)
  const [instances, setInstances] = useState<any[]>([])
  const [envs, setEnvs] = useState<any[]>([])
  const [keyword, setKeyword] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [databases, setDatabases] = useState<RedisDb[]>([])
  const [dbName, setDbName] = useState('')
  const [command, setCommand] = useState('SCAN 0 MATCH * COUNT 100')
  const [result, setResult] = useState<RedisResult | null>(null)
  const [history, setHistory] = useState<string[]>([])

  const selected = useMemo(() => instances.find((i) => i.id === selectedId), [instances, selectedId])

  const envOptions = useMemo(() => {
    const set = new Set<string>()
    for (const e of envs) {
      if (e.code) set.add(e.code)
      if (e.name) set.add(e.name)
    }
    for (const i of instances) {
      if (i.idc) set.add(i.idc)
    }
    return [...set]
  }, [envs, instances])

  const resultRows = useMemo(() => {
    const cols = result?.columns || []
    return (result?.rows || []).map((row, ri) => {
      const obj: Record<string, any> = { __k: ri }
      cols.forEach((c, i) => { obj[c] = row[i] == null ? 'null' : String(row[i]) })
      return obj
    })
  }, [result])

  const textBlob = useMemo(() => {
    const cols = result?.columns || []
    const rows = result?.rows || []
    if (cols.length === 1 && rows.length === 1 && typeof rows[0]?.[0] === 'string' && /[\r\n]/.test(rows[0][0])) {
      return rows[0][0]
    }
    return ''
  }, [result])

  function colWidth(col: string) {
    if (col === 'section') return 120
    if (col === 'key') return 220
    if (col === 'value') return 280
    return 160
  }

  function instEnv(i: any) {
    return i?.idc || i?.env_id || ''
  }
  function instAddr(i: any) {
    if (!i) return ''
    const host = i.host || i.domain_name || ''
    return host ? `${host}:${i.port || 6379}` : ''
  }
  function formatKeys(n: any) {
    if (n == null || Number(n) < 0) return '—'
    const v = Number(n)
    if (v >= 10000) return `${(v / 10000).toFixed(v >= 100000 ? 0 : 1)}w keys`
    return `${v} keys`
  }

  const filteredInstances = useMemo(() => {
    let list = instances
    if (envFilter) list = list.filter((i) => instEnv(i) === envFilter)
    const q = keyword.trim().toLowerCase()
    if (q) list = list.filter((i) => (i.name || '').toLowerCase().includes(q) || instAddr(i).toLowerCase().includes(q))
    return list
  }, [instances, envFilter, keyword])

  function selectInstance(inst: any) {
    if (selectedId === inst.id) return
    setSelectedId(inst.id)
    setDbName('')
    setDatabases([])
    setResult(null)
    loadDBs(inst.id)
  }

  function selectDB(name: string) {
    setDbName(name)
    setResult(null)
  }

  async function loadDBs(id: string = selectedId) {
    if (!id) return
    setDbLoading(true)
    try {
      const { data } = await api.get(`/instances/${id}/redis/databases`)
      const list: RedisDb[] = data.data || []
      setDatabases(list)
      setDbName((prev) => (list.some((d) => d.name === prev) ? prev : (list[0]?.name || '')))
    } catch {
      setDatabases([])
      setDbName('')
    } finally {
      setDbLoading(false)
    }
  }

  function pushHistory(sql: string) {
    setHistory((list) => {
      const next = [sql, ...list.filter((x) => x !== sql)].slice(0, 12)
      try { localStorage.setItem(HIST_KEY, JSON.stringify(next)) } catch { /* ignore */ }
      return next
    })
  }

  async function run() {
    const sql = command.trim()
    if (!selectedId || !sql || !dbName) return
    queryAbortRef.current?.abort()
    const ac = new AbortController()
    queryAbortRef.current = ac
    setRunning(true)
    try {
      const { data } = await api.post(
        `/instances/${selectedId}/redis/execute`,
        { command: sql, db_name: dbName },
        { signal: ac.signal },
      )
      if (data?.canceled) {
        message.info(data.message || '查询已终止')
        return
      }
      setResult(data)
      pushHistory(sql)
    } catch (err: any) {
      if (axios.isCancel(err) || err?.code === 'ERR_CANCELED' || err?.name === 'CanceledError') {
        message.info('查询已终止')
        return
      }
      setResult(null)
    } finally {
      if (queryAbortRef.current === ac) queryAbortRef.current = null
      setRunning(false)
    }
  }

  function stopQuery() {
    queryAbortRef.current?.abort()
  }

  function onCmdKey(e: React.KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      run()
    }
  }

  async function loadMeta() {
    setInstLoading(true)
    try {
      const [instRes, envRes] = await Promise.all([
        api.get('/instances', { params: { db_type: 'redis' } }),
        api.get('/environments'),
      ])
      setInstances(instRes.data.data || [])
      setEnvs(envRes.data.data || [])
      try { setHistory(JSON.parse(localStorage.getItem(HIST_KEY) || '[]')) } catch { setHistory([]) }
    } finally {
      setInstLoading(false)
    }
  }

  useEffect(() => { loadMeta() }, [])

  useEffect(() => () => {
    queryAbortRef.current?.abort()
  }, [])

  return (
    <div className="ops-page redis-query">
      <div className="layout">
        <CollapsibleSide
          storageKey="auto-dba-redis-side-collapsed"
          title="Redis 实例"
          width={280}
          className="side"
        >
          <Input value={keyword} allowClear placeholder="搜索实例" className="mb" onChange={(e) => setKeyword(e.target.value)} />
          <Select
            value={envFilter || undefined}
            allowClear
            placeholder="全部环境"
            className="mb"
            style={{ width: '100%' }}
            onChange={(v) => setEnvFilter(v || '')}
            options={envOptions.map((e) => ({ label: e, value: e }))}
          />
          <Spin spinning={instLoading} wrapperClassName="inst-list">
            {filteredInstances.map((i) => (
              <div key={i.id} className="inst-block">
                <div className={`inst-item${selectedId === i.id ? ' active' : ''}`} onClick={() => selectInstance(i)}>
                  <span className={`caret${selectedId === i.id ? ' open' : ''}`}>▸</span>
                  <span className="name">{i.name || instAddr(i)}</span>
                  <span className="env">{instEnv(i)}</span>
                </div>
                {selectedId === i.id ? (
                  <Spin spinning={dbLoading} wrapperClassName="db-list">
                    {databases.map((d) => (
                      <button
                        key={d.name}
                        type="button"
                        className={`db-item${dbName === d.name ? ' active' : ''}`}
                        onClick={(e) => { e.stopPropagation(); selectDB(d.name) }}
                      >
                        <span className="db-name">{d.name}</span>
                        <span className="keys">{formatKeys(d.keys)}</span>
                      </button>
                    ))}
                    {!dbLoading && !databases.length ? (
                      <p className="empty inner">未拉取到逻辑库，请检查实例账号或点右侧「刷新库」</p>
                    ) : null}
                  </Spin>
                ) : null}
              </div>
            ))}
            {!instLoading && !filteredInstances.length ? (
              <p className="empty">暂无 Redis 实例，请先在「配置 → 实例」登记 db_type=redis</p>
            ) : null}
          </Spin>
        </CollapsibleSide>
        <section className="main">
          {!selectedId ? (
            <div className="placeholder">请从左侧选择 Redis 实例和逻辑库，再执行只读命令</div>
          ) : (
            <>
              <div className="toolbar">
                <strong>{selected?.name || instAddr(selected)}</strong>
                <span className="muted">{instAddr(selected)}</span>
                <Tag color="default">{dbName || '未选库'}</Tag>
                <Button loading={dbLoading} onClick={() => loadDBs()}>刷新库</Button>
              </div>
              <Input.TextArea
                value={command}
                rows={4}
                placeholder={'只读命令，例如：GET mykey\nSCAN 0 MATCH * COUNT 100\nINFO server'}
                onChange={(e) => setCommand(e.target.value)}
                onKeyDown={onCmdKey}
              />
              <div className="actions">
                <Button type="primary" loading={running} disabled={!command.trim() || !dbName} onClick={run}>运行</Button>
                <Button danger disabled={!running} icon={<StopOutlined />} onClick={stopQuery}>查询终止</Button>
                <Button onClick={() => setCommand('SCAN 0 MATCH * COUNT 100')} disabled={running}>SCAN 示例</Button>
                <Button onClick={() => setCommand('INFO server')} disabled={running}>INFO 示例</Button>
                <span className="muted">仅允许 GET / INFO / SCAN · Ctrl/⌘ + Enter 运行 · 当前 {dbName || '未选库'}</span>
              </div>
              {result ? (
                <div className="result">
                  <div className="result-meta">{result.row_count ?? 0} 行 · {result.execution_time_ms ?? 0} ms</div>
                  {textBlob ? (
                    <pre className="result-pre">{textBlob}</pre>
                  ) : (
                    <ResponsiveTable
                      dataSource={resultRows}
                      rowKey="__k"
                      size="small"
                      scroll={{ y: 360 }}
                      pagination={false}
                      locale={{ emptyText: '无数据' }}
                      columns={(result.columns || []).map((col) => ({
                        title: col,
                        dataIndex: col,
                        key: col,
                        width: colWidth(col),
                        render: (v: any) => <span className="cell-text">{v}</span>,
                      }))}
                    />
                  )}
                </div>
              ) : null}
              {history.length ? (
                <div className="history">
                  <div className="side-head">最近命令</div>
                  {history.map((h, i) => (
                    <div key={i} className="hist-item" onClick={() => setCommand(h)}>{h}</div>
                  ))}
                </div>
              ) : null}
            </>
          )}
        </section>
      </div>
    </div>
  )
}
