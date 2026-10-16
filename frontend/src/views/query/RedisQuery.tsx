import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Select, Spin, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { DatabaseOutlined, EnvironmentOutlined, StopOutlined } from '@ant-design/icons'
import axios from 'axios'
import api from '../../api/request'
import CollapsibleSide from '../../components/CollapsibleSide'
import DbTypeIcon from '../../components/DbTypeIcon'
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

interface DomainEntry {
  id: string
  domain: string
  env_code: string
  instance_id: string
  port?: number
  instance_name?: string
}

function domainAddr(domain: string, port?: number) {
  const h = String(domain || '').trim()
  if (!h) return ''
  if (h.includes(':')) return h
  return `${h}:${port || 6379}`
}

function normalizeDomain(s: string) {
  return String(s || '')
    .trim()
    .replace(/\.+$/, '')
    .toLowerCase()
}

export default function RedisQuery() {
  const [instLoading, setInstLoading] = useState(false)
  const [dbLoading, setDbLoading] = useState(false)
  const [running, setRunning] = useState(false)
  const queryAbortRef = useRef<AbortController | null>(null)
  const [domains, setDomains] = useState<DomainEntry[]>([])
  const [keyword, setKeyword] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [expandedEnv, setExpandedEnv] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [selectedDomain, setSelectedDomain] = useState('')
  const [databases, setDatabases] = useState<RedisDb[]>([])
  const [dbName, setDbName] = useState('')
  const [command, setCommand] = useState('SCAN 0 MATCH * COUNT 100')
  const [result, setResult] = useState<RedisResult | null>(null)
  const [history, setHistory] = useState<string[]>([])

  const selected = useMemo(
    () => domains.find((d) => d.instance_id === selectedId && normalizeDomain(d.domain) === normalizeDomain(selectedDomain)),
    [domains, selectedId, selectedDomain],
  )

  const envOptions = useMemo(() => {
    const set = new Set<string>()
    for (const d of domains) {
      if (d.env_code) set.add(d.env_code)
    }
    return [...set].sort()
  }, [domains])

  const envGroups = useMemo(() => {
    let list = domains
    if (envFilter) list = list.filter((d) => d.env_code === envFilter)
    const q = keyword.trim().toLowerCase()
    if (q) {
      list = list.filter(
        (d) =>
          d.domain.toLowerCase().includes(q) ||
          (d.instance_name || '').toLowerCase().includes(q) ||
          domainAddr(d.domain, d.port).toLowerCase().includes(q) ||
          (d.env_code || '').toLowerCase().includes(q),
      )
    }
    const map = new Map<string, DomainEntry[]>()
    for (const d of list) {
      const env = d.env_code || '未标注环境'
      if (!map.has(env)) map.set(env, [])
      map.get(env)!.push(d)
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0], 'zh-CN'))
  }, [domains, envFilter, keyword])

  const resultRows = useMemo(() => {
    const cols = result?.columns || []
    return (result?.rows || []).map((row, ri) => {
      const obj: Record<string, any> = { __k: ri }
      cols.forEach((c, i) => {
        obj[c] = row[i] == null ? 'null' : String(row[i])
      })
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

  function formatKeys(n: any) {
    if (n == null || Number(n) < 0) return '—'
    const v = Number(n)
    if (v >= 10000) return `${(v / 10000).toFixed(v >= 100000 ? 0 : 1)}w keys`
    return `${v} keys`
  }

  function toggleEnv(env: string) {
    if (expandedEnv === env) {
      setExpandedEnv('')
      return
    }
    setExpandedEnv(env)
  }

  function selectDomain(entry: DomainEntry) {
    const d = String(entry.domain || '').trim()
    if (!d || !entry.instance_id) return
    setSelectedId(entry.instance_id)
    setSelectedDomain(d)
    setDbName('')
    setDatabases([])
    setResult(null)
    void loadDBs(entry.instance_id, d)
  }

  function selectDB(name: string) {
    setDbName(name)
    setResult(null)
  }

  async function loadDBs(id: string = selectedId, domain: string = selectedDomain) {
    if (!id || !domain) return
    setDbLoading(true)
    try {
      const { data } = await api.get(`/instances/${id}/redis/databases`, { params: { domain } })
      const list: RedisDb[] = data.data || []
      setDatabases(list)
      setDbName((prev) => (list.some((x) => x.name === prev) ? prev : list[0]?.name || ''))
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
      try {
        localStorage.setItem(HIST_KEY, JSON.stringify(next))
      } catch {
        /* ignore */
      }
      return next
    })
  }

  async function run() {
    const sql = command.trim()
    if (!selectedId || !selectedDomain || !sql || !dbName) return
    queryAbortRef.current?.abort()
    const ac = new AbortController()
    queryAbortRef.current = ac
    setRunning(true)
    try {
      const { data } = await api.post(
        `/instances/${selectedId}/redis/execute`,
        { command: sql, db_name: dbName, domain: selectedDomain },
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
      void run()
    }
  }

  async function loadMeta() {
    setInstLoading(true)
    try {
      const [domRes, instRes] = await Promise.all([
        api.get('/access-domains', { params: { db_type: 'redis' } }).catch(() => ({ data: { data: [] } })),
        api.get('/instances', { params: { db_type: 'redis' } }),
      ])
      const instMap = new Map<string, any>()
      for (const i of instRes.data.data || []) {
        instMap.set(i.id, i)
      }
      const fromDomains: DomainEntry[] = (domRes.data.data || []).map((r: any) => {
        const inst = instMap.get(r.instance_id)
        return {
          id: r.id || `${r.domain}-${r.instance_id}`,
          domain: String(r.domain || '').trim(),
          env_code: String(r.env_code || inst?.idc || '').trim(),
          instance_id: String(r.instance_id || ''),
          port: inst?.port,
          instance_name: inst?.name,
        }
      }).filter((d: DomainEntry) => d.domain && d.instance_id)

      if (fromDomains.length) {
        setDomains(fromDomains)
      } else {
        // 回退：尚未同步域名表时，由实例 domains 拼装
        const fallback: DomainEntry[] = []
        const seen = new Set<string>()
        for (const i of instRes.data.data || []) {
          const list: string[] = []
          const add = (s: string) => {
            const v = String(s || '').trim()
            if (!v) return
            const key = normalizeDomain(v)
            if (seen.has(`${i.id}:${key}`)) return
            seen.add(`${i.id}:${key}`)
            list.push(v)
          }
          for (const d of i.domains || []) add(String(d))
          if (!list.length) add(i.domain_name)
          if (!list.length) add(i.host)
          for (const dom of list) {
            fallback.push({
              id: `${i.id}:${normalizeDomain(dom)}`,
              domain: dom,
              env_code: String(i.idc || '').trim(),
              instance_id: i.id,
              port: i.port,
              instance_name: i.name,
            })
          }
        }
        setDomains(fallback)
      }
      try {
        setHistory(JSON.parse(localStorage.getItem(HIST_KEY) || '[]'))
      } catch {
        setHistory([])
      }
    } finally {
      setInstLoading(false)
    }
  }

  useEffect(() => {
    void loadMeta()
  }, [])

  useEffect(
    () => () => {
      queryAbortRef.current?.abort()
    },
    [],
  )

  const canRun = Boolean(selectedId && selectedDomain && dbName && command.trim())

  return (
    <div className="ops-page redis-query">
      <div className="layout">
        <CollapsibleSide storageKey="auto-dba-redis-side-collapsed" title="Redis 域名" width={280} className="side">
          <Input
            value={keyword}
            allowClear
            placeholder="搜索域名/环境"
            className="mb"
            onChange={(e) => setKeyword(e.target.value)}
          />
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
            {envGroups.map(([env, list]) => {
              const open = expandedEnv === env
              return (
                <div key={env} className="inst-block">
                  <div className={`inst-item${open ? ' open' : ''}`} onClick={() => toggleEnv(env)}>
                    <span className={`caret${open ? ' open' : ''}`}>▸</span>
                    <span className={`node-ico env-ico${open ? ' open' : ''}`}>
                      <EnvironmentOutlined />
                    </span>
                    <span className="name">{env}</span>
                    <span className="env">{list.length}</span>
                  </div>
                  {open ? (
                    <div className="domain-list">
                      {list.map((entry) => {
                        const domActive =
                          selectedId === entry.instance_id &&
                          normalizeDomain(selectedDomain) === normalizeDomain(entry.domain)
                        return (
                          <div key={entry.id} className="domain-block">
                            <button
                              type="button"
                              className={`domain-item${domActive ? ' active' : ''}`}
                              onClick={(e) => {
                                e.stopPropagation()
                                selectDomain(entry)
                              }}
                            >
                              <span className="node-ico">
                                <DbTypeIcon type="redis" size={14} />
                              </span>
                              <span className="domain-name">{entry.domain}</span>
                            </button>
                            {domActive ? (
                              <Spin spinning={dbLoading} wrapperClassName="db-list">
                                {databases.map((d) => (
                                  <button
                                    key={d.name}
                                    type="button"
                                    className={`db-item${dbName === d.name ? ' active' : ''}`}
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      selectDB(d.name)
                                    }}
                                  >
                                    <span className="node-ico db-ico">
                                      <DatabaseOutlined />
                                    </span>
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
                        )
                      })}
                    </div>
                  ) : null}
                </div>
              )
            })}
            {!instLoading && !envGroups.length ? (
              <p className="empty">
                暂无 Redis 域名。请在「资源配置 → 域名管理」一键同步，或在实例中登记 domains。
              </p>
            ) : null}
          </Spin>
        </CollapsibleSide>
        <section className="main">
          {!selectedId || !selectedDomain ? (
            <div className="placeholder">请从左侧展开环境并选择域名与逻辑库，再执行只读命令</div>
          ) : (
            <>
              <div className="toolbar">
                <strong>{selected?.instance_name || selectedDomain}</strong>
                <span className="muted">{domainAddr(selectedDomain, selected?.port)}</span>
                <Tag color="default">{dbName || '未选库'}</Tag>
                <Button loading={dbLoading} onClick={() => void loadDBs()}>
                  刷新库
                </Button>
              </div>
              <Input.TextArea
                value={command}
                rows={4}
                placeholder={'只读命令，例如：GET mykey\nSCAN 0 MATCH * COUNT 100\nINFO server'}
                onChange={(e) => setCommand(e.target.value)}
                onKeyDown={onCmdKey}
              />
              <div className="actions">
                <Button type="primary" loading={running} disabled={!canRun} onClick={() => void run()}>
                  运行
                </Button>
                <Button danger disabled={!running} icon={<StopOutlined />} onClick={stopQuery}>
                  查询终止
                </Button>
                <Button onClick={() => setCommand('SCAN 0 MATCH * COUNT 100')} disabled={running}>
                  SCAN 示例
                </Button>
                <Button onClick={() => setCommand('INFO server')} disabled={running}>
                  INFO 示例
                </Button>
                <span className="muted">
                  仅允许 GET / INFO / SCAN · Ctrl/⌘ + Enter 运行 · 当前 {dbName || '未选库'}
                </span>
              </div>
              {result ? (
                <div className="result">
                  <div className="result-meta">
                    {result.row_count ?? 0} 行 · {result.execution_time_ms ?? 0} ms
                  </div>
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
                    <div key={i} className="hist-item" onClick={() => setCommand(h)}>
                      {h}
                    </div>
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
