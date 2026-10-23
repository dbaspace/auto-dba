import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Select, Spin, Tabs, Tag, message } from 'antd'
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

function domainKey(instanceId: string, domain: string) {
  return `${instanceId}:${normalizeDomain(domain)}`
}

const DEFAULT_COMMAND = 'SCAN 0 MATCH * COUNT 100'

interface RedisTab {
  key: string
  instanceId: string
  domain: string
  port?: number
  instanceName?: string
  dbName: string
  command: string
  result: RedisResult | null
  running: boolean
}

export default function RedisQuery() {
  const [instLoading, setInstLoading] = useState(false)
  const [dbLoadingKeys, setDbLoadingKeys] = useState<string[]>([])
  const queryAbortRef = useRef<Map<string, AbortController>>(new Map())
  const [domains, setDomains] = useState<DomainEntry[]>([])
  const [keyword, setKeyword] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [expandedEnv, setExpandedEnv] = useState('')
  const [expandedKeys, setExpandedKeys] = useState<string[]>([])
  const [dbMap, setDbMap] = useState<Record<string, RedisDb[]>>({})
  const [tabs, setTabs] = useState<RedisTab[]>([])
  const [activeKey, setActiveKey] = useState('')
  const [history, setHistory] = useState<string[]>([])

  const current = useMemo(() => tabs.find((t) => t.key === activeKey), [tabs, activeKey])

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
    const result = current?.result
    const cols = result?.columns || []
    return (result?.rows || []).map((row, ri) => {
      const obj: Record<string, any> = { __k: ri }
      cols.forEach((c, i) => {
        obj[c] = row[i] == null ? 'null' : String(row[i])
      })
      return obj
    })
  }, [current?.result])

  const textBlob = useMemo(() => {
    const result = current?.result
    const cols = result?.columns || []
    const rows = result?.rows || []
    if (cols.length === 1 && rows.length === 1 && typeof rows[0]?.[0] === 'string' && /[\r\n]/.test(rows[0][0])) {
      return rows[0][0]
    }
    return ''
  }, [current?.result])

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

  function patchTab(key: string, patch: Partial<RedisTab>) {
    setTabs((list) => list.map((t) => (t.key === key ? { ...t, ...patch } : t)))
  }

  function ensureTab(entry: DomainEntry) {
    const d = String(entry.domain || '').trim()
    const key = domainKey(entry.instance_id, d)
    setTabs((list) => {
      if (list.some((t) => t.key === key)) return list
      return [
        ...list,
        {
          key,
          instanceId: entry.instance_id,
          domain: d,
          port: entry.port,
          instanceName: entry.instance_name,
          dbName: '',
          command: DEFAULT_COMMAND,
          result: null,
          running: false,
        },
      ]
    })
    setActiveKey(key)
    return key
  }

  function onDomainClick(entry: DomainEntry) {
    const d = String(entry.domain || '').trim()
    if (!d || !entry.instance_id) return
    const key = domainKey(entry.instance_id, d)
    if (expandedKeys.includes(key)) {
      setExpandedKeys((list) => list.filter((k) => k !== key))
      return
    }
    setExpandedKeys((list) => (list.includes(key) ? list : [...list, key]))
    ensureTab(entry)
    void loadDBs(entry)
  }

  function selectDB(entry: DomainEntry, name: string) {
    const d = String(entry.domain || '').trim()
    const key = domainKey(entry.instance_id, d)
    setTabs((list) => {
      const exists = list.some((t) => t.key === key)
      const base = exists
        ? list
        : [
            ...list,
            {
              key,
              instanceId: entry.instance_id,
              domain: d,
              port: entry.port,
              instanceName: entry.instance_name,
              dbName: '',
              command: DEFAULT_COMMAND,
              result: null,
              running: false,
            },
          ]
      return base.map((t) => (t.key === key ? { ...t, dbName: name, result: t.dbName === name ? t.result : null } : t))
    })
    setActiveKey(key)
  }

  async function loadDBs(entry: DomainEntry) {
    const d = String(entry.domain || '').trim()
    const id = entry.instance_id
    if (!id || !d) return
    const key = domainKey(id, d)
    setDbLoadingKeys((list) => (list.includes(key) ? list : [...list, key]))
    try {
      const { data } = await api.get(`/instances/${id}/redis/databases`, { params: { domain: d } })
      const list: RedisDb[] = data.data || []
      setDbMap((m) => ({ ...m, [key]: list }))
      setTabs((tabsNow) =>
        tabsNow.map((t) => {
          if (t.key !== key) return t
          const dbName = list.some((x) => x.name === t.dbName) ? t.dbName : list[0]?.name || ''
          return { ...t, dbName }
        }),
      )
    } catch {
      setDbMap((m) => ({ ...m, [key]: [] }))
      setTabs((tabsNow) => tabsNow.map((t) => (t.key === key ? { ...t, dbName: '' } : t)))
    } finally {
      setDbLoadingKeys((list) => list.filter((k) => k !== key))
    }
  }

  function closeTab(key: string) {
    queryAbortRef.current.get(key)?.abort()
    queryAbortRef.current.delete(key)
    setTabs((list) => {
      const idx = list.findIndex((t) => t.key === key)
      const next = list.filter((t) => t.key !== key)
      if (activeKey === key) {
        const fallback = next[idx] || next[idx - 1]
        setActiveKey(fallback?.key || '')
      }
      return next
    })
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
    const tab = tabs.find((t) => t.key === activeKey)
    const sql = tab?.command.trim() || ''
    if (!tab || !sql || !tab.dbName) return
    const key = tab.key
    queryAbortRef.current.get(key)?.abort()
    const ac = new AbortController()
    queryAbortRef.current.set(key, ac)
    patchTab(key, { running: true })
    try {
      const { data } = await api.post(
        `/instances/${tab.instanceId}/redis/execute`,
        { command: sql, db_name: tab.dbName, domain: tab.domain },
        { signal: ac.signal },
      )
      if (data?.canceled) {
        message.info(data.message || '查询已终止')
        return
      }
      patchTab(key, { result: data })
      pushHistory(sql)
    } catch (err: any) {
      if (axios.isCancel(err) || err?.code === 'ERR_CANCELED' || err?.name === 'CanceledError') {
        message.info('查询已终止')
        return
      }
      patchTab(key, { result: null })
    } finally {
      if (queryAbortRef.current.get(key) === ac) queryAbortRef.current.delete(key)
      patchTab(key, { running: false })
    }
  }

  function stopQuery() {
    if (!activeKey) return
    queryAbortRef.current.get(activeKey)?.abort()
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
      queryAbortRef.current.forEach((ac) => ac.abort())
      queryAbortRef.current.clear()
    },
    [],
  )

  const canRun = Boolean(current && current.dbName && current.command.trim() && !current.running)

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
                        const key = domainKey(entry.instance_id, entry.domain)
                        const expanded = expandedKeys.includes(key)
                        const tab = tabs.find((t) => t.key === key)
                        const databases = dbMap[key] || []
                        const dbLoading = dbLoadingKeys.includes(key)
                        return (
                          <div key={entry.id} className="domain-block">
                            <button
                              type="button"
                              className={`domain-item${activeKey === key ? ' active' : ''}${expanded ? ' open' : ''}`}
                              onClick={(e) => {
                                e.stopPropagation()
                                onDomainClick(entry)
                              }}
                            >
                              <span className={`caret${expanded ? ' open' : ''}`}>▸</span>
                              <span className="node-ico">
                                <DbTypeIcon type="redis" size={14} />
                              </span>
                              <span className="domain-name">{entry.domain}</span>
                            </button>
                            {expanded ? (
                              <Spin spinning={dbLoading} wrapperClassName="db-list">
                                {databases.map((d) => (
                                  <button
                                    key={d.name}
                                    type="button"
                                    className={`db-item${tab?.dbName === d.name ? ' active' : ''}`}
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      selectDB(entry, d.name)
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
          {tabs.length ? (
            <Tabs
              className="work-tabs"
              type="editable-card"
              hideAdd
              activeKey={activeKey}
              onChange={setActiveKey}
              onEdit={(targetKey, action) => {
                if (action === 'remove') closeTab(String(targetKey))
              }}
              items={tabs.map((t) => ({
                key: t.key,
                label: <span className="tab-label">{t.domain}</span>,
              }))}
            />
          ) : null}
          {!current ? (
            <div className="placeholder">请从左侧展开环境并选择域名与逻辑库，再执行只读命令</div>
          ) : (
            <>
              <div className="toolbar">
                <strong>{current.instanceName || current.domain}</strong>
                <span className="muted">{domainAddr(current.domain, current.port)}</span>
                <Tag color="default">{current.dbName || '未选库'}</Tag>
                <Button
                  loading={dbLoadingKeys.includes(current.key)}
                  onClick={() => {
                    const entry = domains.find((d) => domainKey(d.instance_id, d.domain) === current.key)
                    if (entry) void loadDBs(entry)
                  }}
                >
                  刷新库
                </Button>
              </div>
              <Input.TextArea
                value={current.command}
                rows={4}
                placeholder={'只读命令，例如：GET mykey\nSCAN 0 MATCH * COUNT 100\nINFO server'}
                onChange={(e) => patchTab(current.key, { command: e.target.value })}
                onKeyDown={onCmdKey}
              />
              <div className="actions">
                <Button type="primary" loading={current.running} disabled={!canRun} onClick={() => void run()}>
                  运行
                </Button>
                <Button danger disabled={!current.running} icon={<StopOutlined />} onClick={stopQuery}>
                  查询终止
                </Button>
                <Button onClick={() => patchTab(current.key, { command: DEFAULT_COMMAND })} disabled={current.running}>
                  SCAN 示例
                </Button>
                <Button onClick={() => patchTab(current.key, { command: 'INFO server' })} disabled={current.running}>
                  INFO 示例
                </Button>
                <span className="muted">
                  仅允许 GET / INFO / SCAN · Ctrl/⌘ + Enter 运行 · 当前 {current.dbName || '未选库'}
                </span>
              </div>
              {current.result ? (
                <div className="result">
                  <div className="result-meta">
                    {current.result.row_count ?? 0} 行 · {current.result.execution_time_ms ?? 0} ms
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
                      columns={(current.result.columns || []).map((col) => ({
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
                    <div key={i} className="hist-item" onClick={() => patchTab(current.key, { command: h })}>
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
