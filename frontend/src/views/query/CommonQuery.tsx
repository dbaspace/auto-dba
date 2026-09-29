import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  useLocation,
  useNavigate } from 'react-router-dom'
import {
  Button,
  Input,
  Modal,
  Select,
  Spin,
  Tabs,
  Tag,
  Tooltip,
  Tree,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { DataNode } from 'antd/es/tree'
import {
  CaretRightOutlined,
  CloseOutlined,
  DoubleLeftOutlined,
  DoubleRightOutlined,
  SearchOutlined,
  StarOutlined,
} from '@ant-design/icons'
import api from '../../api/request'
import DbTypeIcon from '../../components/DbTypeIcon'
import './CommonQuery.css'

const DQL_DRAFT_KEY = 'auto-dba-dql-draft'
const WORKSPACE_KEY = 'auto-dba-query-workspace'
const FAV_KEY = 'auto-dba-query-favorites'
const HIST_KEY = 'auto-dba-query-history'

const TYPE_ORDER = [
  'mysql', 'postgres', 'tidb', 'cockroachdb', 'oceanbase', 'cassandra',
  'oracle', 'elasticsearch', 'mongodb', 'sqlserver', 'starrocks', 'clickhouse',
]

interface QueryResult {
  columns?: string[]
  rows?: any[][]
  truncated?: boolean
  elapsed_ms?: number
}

interface TabItem {
  key: string
  dbId: string
  dbName: string
  dbType: string
  env: string
  instanceId: string
  tag: string
  table: string
  sql: string
  ts: number
  result: QueryResult | null
  error: string
}

interface FavItem {
  id: string
  name: string
  dbId: string
  dbName: string
  sql: string
  ts: number
}

interface HistItem {
  id: string
  dbId: string
  dbName: string
  sql: string
  ts: number
}

const tableSvg = (
  <svg viewBox="0 0 16 16" width={16} height={16}>
    <rect x="1.5" y="2.5" width="13" height="11" rx="1.2" fill="#60a5fa" />
    <path d="M1.5 5.5h13M6.5 5.5v8" stroke="#fff" strokeWidth="1.2" />
  </svg>
)

function normalizeType(t: any): string {
  const n = String(t || '').toLowerCase().trim()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'cockroach' || n === 'cockroadchdb') return 'cockroachdb'
  if (n === 'mssql' || n === 'sql-server' || n === 'sql_server') return 'sqlserver'
  if (n === 'es' || n === 'elastic') return 'elasticsearch'
  if (n === 'mongo') return 'mongodb'
  if (n === 'ob') return 'oceanbase'
  return n || 'other'
}

function typeRank(t: any): number {
  const i = TYPE_ORDER.indexOf(normalizeType(t))
  return i < 0 ? TYPE_ORDER.length : i
}

function cmpName(a: any, b: any): number {
  return String(a || '').localeCompare(String(b || ''), 'zh-CN', { numeric: true, sensitivity: 'base' })
}

function loadJSON<T>(key: string, fallback: T): T {
  try {
    const v = JSON.parse(localStorage.getItem(key) || 'null')
    return Array.isArray(v) ? (v as unknown as T) : fallback
  } catch {
    return fallback
  }
}

function saveJSON(key: string, val: any) {
  localStorage.setItem(key, JSON.stringify(val))
}

function cellText(v: any): string {
  if (v === null || v === undefined) return 'NULL'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

function fmtTime(ts: any): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function envTagColor(env: any): string {
  const e = String(env || '').toLowerCase()
  if (['prod', 'production', 'prd', '线上'].includes(e)) return 'red'
  return 'blue'
}

function dbTags(db: any): string[] {
  const raw = db?.tags
  if (Array.isArray(raw)) return raw.map((t) => String(t || '').trim()).filter(Boolean)
  if (typeof raw === 'string') {
    const s = raw.trim()
    if (!s) return []
    try {
      const parsed = JSON.parse(s)
      if (Array.isArray(parsed)) return dbTags({ tags: parsed })
    } catch { /* ignore */ }
    return s.split(/[,，]/).map((t) => t.trim()).filter(Boolean)
  }
  return []
}

function normalizeSavedTab(t: any): TabItem | null {
  if (!t || !t.key || !t.dbId) return null
  return {
    key: t.key,
    dbId: t.dbId,
    dbName: t.dbName || '',
    dbType: t.dbType || '',
    env: t.env || '',
    instanceId: t.instanceId || '',
    tag: t.tag || '',
    table: t.table || '',
    sql: t.sql || '',
    ts: t.ts || Date.now(),
    result: t.result && Array.isArray(t.result.columns) ? t.result : null,
    error: t.error || '',
  }
}

interface Workspace {
  tabs: TabItem[]
  activeKey: string
  leftWidth: number
  editorHeight: number
  histWidth: number
}

function loadWorkspace(): Workspace {
  const empty: Workspace = { tabs: [], activeKey: '', leftWidth: 280, editorHeight: 320, histWidth: 300 }
  try {
    const raw = sessionStorage.getItem(WORKSPACE_KEY)
    if (!raw) return empty
    const d = JSON.parse(raw)
    const tabs = Array.isArray(d.tabs) ? d.tabs.map(normalizeSavedTab).filter(Boolean) as TabItem[] : []
    const activeKey = tabs.some((t) => t.key === d.activeKey) ? d.activeKey : (tabs[0]?.key || '')
    return {
      tabs,
      activeKey,
      leftWidth: Number(d.leftWidth) || 280,
      editorHeight: Number(d.editorHeight) || 320,
      histWidth: Number(d.histWidth) || 300,
    }
  } catch {
    return empty
  }
}

export default function CommonQuery() {
  const navigate = useNavigate()
  const location = useLocation()

  const username = useMemo(() => {
    try {
      return JSON.parse(localStorage.getItem('user') || '{}').username || ''
    } catch {
      return ''
    }
  }, [])

  const [treeLoading, setTreeLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [envOptions, setEnvOptions] = useState<string[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [tablesMap, setTablesMap] = useState<Record<string, string[]>>({})
  const tableLoads = useRef<Record<string, Promise<string[]>>>({})

  const saved = useMemo(() => loadWorkspace(), [])
  const [tabs, setTabs] = useState<TabItem[]>(saved.tabs)
  const [activeKey, setActiveKey] = useState(saved.activeKey)
  const [running, setRunning] = useState(false)
  const [leftWidth, setLeftWidth] = useState(saved.leftWidth)
  const [leftCollapsed, setLeftCollapsed] = useState(false)
  const [editorHeight, setEditorHeight] = useState(saved.editorHeight)
  const [histWidth, setHistWidth] = useState(saved.histWidth)
  const [storeTab, setStoreTab] = useState(() => {
    const tab = (location.state as { storeTab?: string } | null)?.storeTab
    return tab === 'hist' || tab === 'fav' ? tab : 'fav'
  })
  const [favorites, setFavorites] = useState<FavItem[]>(() => loadJSON<FavItem[]>(FAV_KEY, []))
  const [history, setHistory] = useState<HistItem[]>(() => loadJSON<HistItem[]>(HIST_KEY, []))
  const [favVisible, setFavVisible] = useState(false)
  const [favName, setFavName] = useState('')
  const [favKeyword, setFavKeyword] = useState('')
  const [treeData, setTreeData] = useState<DataNode[]>([])
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([])

  useEffect(() => {
    const tab = (location.state as { storeTab?: string } | null)?.storeTab
    if (tab === 'hist' || tab === 'fav') setStoreTab(tab)
  }, [location.state])

  const current = useMemo(() => tabs.find((t) => t.key === activeKey) || null, [tabs, activeKey])

  const instMap = useMemo(() => {
    const m: Record<string, any> = {}
    for (const i of instances) m[i.id] = i
    return m
  }, [instances])

  const dbTypeOf = useCallback((db: any) => normalizeType(instMap[db.instance_id]?.db_type), [instMap])
  const dbEnv = useCallback((db: any) => instMap[db.instance_id]?.idc || '未分组', [instMap])

  const instanceHint = useCallback((db: any) => {
    const inst = instMap[db?.instance_id] || {}
    return inst.name || inst.domain_name || (inst.domains && inst.domains[0]) || inst.host || ''
  }, [instMap])

  const dbTagText = useCallback((db: any, siblings?: any[]) => {
    const tags = dbTags(db)
    if (tags.length) return tags.join(',')
    const name = db?.db_name
    const same = (siblings || databases || []).filter((d) => d.db_name === name)
    if (same.length > 1) return instanceHint(db)
    return ''
  }, [databases, instanceHint])

  const dbTag = useCallback((db: any) => dbTagText(db, databases), [dbTagText, databases])

  const sortDbs = useCallback((dbs: any[]) => {
    return [...dbs].sort((a, b) => {
      const ra = typeRank(dbTypeOf(a))
      const rb = typeRank(dbTypeOf(b))
      if (ra !== rb) return ra - rb
      const ta = dbTypeOf(a)
      const tb = dbTypeOf(b)
      if (ta !== tb) return ta.localeCompare(tb)
      const byName = cmpName(a.db_name, b.db_name)
      if (byName) return byName
      return cmpName(dbTagText(a).split(','), dbTagText(b).split(','))
    })
  }, [dbTypeOf, dbTagText])

  const matchKeyword = useCallback((db: any) => {
    const kw = keyword.trim().toLowerCase()
    if (!kw) return true
    if (String(db.db_name || '').toLowerCase().includes(kw)) return true
    if (dbTags(db).join(' ').toLowerCase().includes(kw)) return true
    if (instanceHint(db).toLowerCase().includes(kw)) return true
    return false
  }, [keyword, instanceHint])

  const groupedDbs = useCallback(() => {
    const groups = new Map<string, any[]>()
    for (const db of databases) {
      const envName = dbEnv(db)
      if (envFilter && envName !== envFilter) continue
      if (!matchKeyword(db)) continue
      if (!groups.has(envName)) groups.set(envName, [])
      groups.get(envName)!.push(db)
    }
    for (const [name, list] of groups) {
      groups.set(name, sortDbs(list))
    }
    return groups
  }, [databases, envFilter, matchKeyword, dbEnv, sortDbs])

  function highlightLabel(text: any): React.ReactNode {
    const kw = keyword.trim()
    const raw = String(text || '')
    if (!kw) return raw
    const idx = raw.toLowerCase().indexOf(kw.toLowerCase())
    if (idx < 0) return raw
    return (
      <>
        {raw.slice(0, idx)}
        <span className="hit">{raw.slice(idx, idx + kw.length)}</span>
        {raw.slice(idx + kw.length)}
      </>
    )
  }

  const renderNodeTitle = useCallback((node: any): React.ReactNode => {
    const data = node.raw || {}
    if (data.type === 'db') {
      return (
        <span className="tree-node" title={data.tag ? `${data.label} [${data.tag}]` : data.label}>
          <DbTypeIcon type={data.dbType} size={16} />
          <span className="tree-label">{highlightLabel(data.label)}</span>
          {data.tag ? <span className="tree-tag">[{data.tag}]</span> : null}
        </span>
      )
    }
    if (data.type === 'table') {
      return (
        <span className="tree-node" title={data.label}>
          <span className="table-ico">{tableSvg}</span>
          <span className="tree-label">{highlightLabel(data.label)}</span>
        </span>
      )
    }
    if (data.type === 'empty') {
      return <span className="tree-node"><span className="tree-label muted">{data.label}</span></span>
    }
    return <span className="tree-node"><span className="tree-label">{highlightLabel(data.label)}</span></span>
  }, [keyword])

  const buildNode = useCallback((raw: any, isLeaf: boolean, children?: DataNode[]): DataNode => {
    const node: DataNode = {
      key: raw.id,
      isLeaf,
      // @ts-ignore keep raw payload for click / loadData
      raw,
      title: undefined,
    }
    if (children) node.children = children
    return node
  }, [])

  const fetchTables = useCallback((dbId: string): Promise<string[]> => {
    if (Object.prototype.hasOwnProperty.call(tablesMap, dbId)) {
      return Promise.resolve(tablesMap[dbId])
    }
    if (tableLoads.current[dbId]) return tableLoads.current[dbId]
    const p = api.get(`/databases/${dbId}/tables`)
      .then((res) => {
        const names: string[] = res.data.data || []
        setTablesMap((m) => ({ ...m, [dbId]: names }))
        return names
      })
      .catch(() => {
        setTablesMap((m) => ({ ...m, [dbId]: [] }))
        return [] as string[]
      })
      .finally(() => {
        delete tableLoads.current[dbId]
      })
    tableLoads.current[dbId] = p
    return p
  }, [tablesMap])

  /** 环境节点直接带上库子节点（库列表已在本地）；表节点仍懒加载 */
  const rootNodes = useCallback((): DataNode[] => {
    const entries = [...groupedDbs().entries()].sort((a, b) => {
      if (a[0] === '未分组') return 1
      if (b[0] === '未分组') return -1
      return cmpName(a[0], b[0])
    })
    return entries.map(([name, dbs]) => {
      const dbChildren = dbs.map((db) => {
        const inst = instMap[db.instance_id] || {}
        return buildNode({
          id: db.id,
          label: db.db_name,
          type: 'db',
          dbId: db.id,
          env: inst.idc || '未分组',
          dbType: inst.db_type,
          tag: dbTagText(db, dbs),
        }, false)
      })
      return buildNode(
        { id: `env:${name}`, label: name, type: 'env', env: name },
        dbChildren.length === 0,
        dbChildren,
      )
    })
  }, [groupedDbs, buildNode, instMap, dbTagText])

  const rebuildTree = useCallback(() => {
    const roots = rootNodes()
    setTreeData((prev) => {
      // 保留已懒加载的表 children，避免 tablesMap 无关更新冲掉展开内容
      const prevTables = new Map<React.Key, DataNode[]>()
      const collect = (nodes: DataNode[] | undefined) => {
        for (const n of nodes || []) {
          const raw = (n as any).raw
          if (raw?.type === 'db' && n.children?.length) prevTables.set(n.key, n.children)
          if (n.children) collect(n.children)
        }
      }
      collect(prev)
      if (!prevTables.size) return roots
      return roots.map((env) => ({
        ...env,
        children: (env.children || []).map((db) => {
          const kept = prevTables.get(db.key)
          return kept ? { ...db, children: kept } : db
        }),
      }))
    })
    setExpandedKeys((prev) => {
      const envKeys = roots.map((r) => r.key)
      const keep = prev.filter((k) => !String(k).startsWith('env:'))
      return Array.from(new Set([...envKeys, ...keep]))
    })
  }, [rootNodes])

  useEffect(() => {
    if (databases.length) rebuildTree()
  }, [databases, envFilter, keyword, rebuildTree])

  const onLoadData = useCallback((node: any): Promise<void> => {
    const data = node.raw || {}
    // 环境节点已预置库列表，无需再 load
    if (data.type !== 'db') return Promise.resolve()
    // 已有子节点则跳过
    if (node.children?.length) return Promise.resolve()
    return fetchTables(data.dbId).then((names) => {
      let children: DataNode[]
      if (!names.length) {
        children = [buildNode({ id: `${data.dbId}__empty`, label: '暂无表', type: 'empty' }, true)]
      } else {
        children = [...names].sort(cmpName).map((name) => buildNode({
          id: `${data.dbId}__${name}`,
          label: name,
          type: 'table',
          dbId: data.dbId,
          table: name,
          env: data.env,
          dbType: data.dbType,
        }, true))
      }
      updateTreeChildren(data.id, children)
    })
  }, [buildNode, fetchTables])

  function updateTreeChildren(key: React.Key, children: DataNode[]) {
    setTreeData((list) => {
      const walk = (nodes: DataNode[]): DataNode[] => nodes.map((n) => {
        if (n.key === key) return { ...n, children, isLeaf: false }
        if (n.children) return { ...n, children: walk(n.children) }
        return n
      })
      return walk(list)
    })
  }

  const filteredFavs = useMemo(() => {
    const q = favKeyword.trim().toLowerCase()
    if (!q) return favorites
    return favorites.filter((f) => `${f.name} ${f.sql}`.toLowerCase().includes(q))
  }, [favKeyword, favorites])

  const resultRows = useMemo(() => {
    const res = current?.result
    if (!res) return []
    return (res.rows || []).map((row, ri) => {
      const obj: Record<string, any> = { __k: ri }
      ;(res.columns || []).forEach((c, i) => {
        obj[c] = cellText(row[i])
      })
      return obj
    })
  }, [current])

  const envPeers = useMemo(() => {
    if (!current) return []
    return databases.filter((d) => d.db_name === current.dbName)
  }, [current, databases])

  // persist
  useEffect(() => {
    try {
      const payload = {
        activeKey,
        leftWidth,
        editorHeight,
        histWidth,
        tabs: tabs.map((t) => ({
          key: t.key,
          dbId: t.dbId,
          dbName: t.dbName,
          dbType: t.dbType,
          env: t.env,
          instanceId: t.instanceId,
          tag: t.tag,
          table: t.table,
          sql: t.sql,
          ts: t.ts,
          error: t.error || '',
          result: t.result && (t.result.rows?.length || 0) <= 200 ? t.result : null,
        })),
      }
      try {
        sessionStorage.setItem(WORKSPACE_KEY, JSON.stringify(payload))
      } catch {
        payload.tabs = payload.tabs.map((t) => ({ ...t, result: null }))
        try { sessionStorage.setItem(WORKSPACE_KEY, JSON.stringify(payload)) } catch { /* ignore */ }
      }
    } catch { /* ignore */ }
  }, [tabs, activeKey, leftWidth, editorHeight, histWidth])

  function updateCurrent(patch: Partial<TabItem>) {
    setTabs((list) => list.map((t) => (t.key === activeKey ? { ...t, ...patch } : t)))
  }

  function quoteTable(name: string, dbType: any): string {
    if (!name) return name
    if (name.includes('`') || name.includes('"') || name.includes('[')) return name
    const t = normalizeType(dbType)
    if (t === 'postgres' || t === 'cockroachdb' || t === 'oracle') {
      return name.split('.').map((p) => (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(p) ? p : `"${p}"`)).join('.')
    }
    if (t === 'sqlserver') {
      return name.split('.').map((p) => (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(p) ? p : `[${p}]`)).join('.')
    }
    if (name.includes('-') || /[^a-zA-Z0-9_.]/.test(name)) return `\`${name}\``
    return name
  }

  function newTab(db: any, opt: { sql?: string; table?: string } = {}): TabItem {
    const inst = instMap[db.instance_id] || {}
    return {
      key: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      dbId: db.id,
      dbName: db.db_name,
      dbType: inst.db_type || '',
      env: inst.idc || '',
      instanceId: db.instance_id,
      tag: dbTag(db),
      table: opt.table || '',
      sql: opt.sql || '',
      ts: Date.now(),
      result: null,
      error: '',
    }
  }

  function openDB(db: any, opt: { sql?: string; table?: string; run?: boolean } = {}): TabItem {
    const exist = tabs.find((t) => t.dbId === db.id && !t.table)
    if (exist && !opt.table && opt.sql === undefined) {
      setActiveKey(exist.key)
      return exist
    }
    const sql = opt.sql ?? (opt.table
      ? `SELECT * FROM ${quoteTable(opt.table, instMap[db.instance_id]?.db_type)} LIMIT 50;`
      : '')
    const tab = newTab(db, { sql, table: opt.table })
    setTabs((list) => [...list, tab])
    setActiveKey(tab.key)
    if (opt.run) runSQL(tab)
    return tab
  }

  function onTreeSelect(_: React.Key[], info: any) {
    const data = info?.node?.raw || {}
    if (!data || data.type === 'env' || data.type === 'empty') return
    const db = databases.find((d) => d.id === data.dbId || d.id === data.id)
    if (!db) return
    if (data.type === 'table') {
      const existTab = tabs.find((t) => t.key === activeKey && t.dbId === db.id)
      const targetKey = existTab ? existTab.key : openDB(db).key
      const sql = `SELECT * FROM ${quoteTable(data.table, data.dbType)} LIMIT 50;`
      setTabs((list) => list.map((t) => (t.key === targetKey ? { ...t, sql, table: data.table } : t)))
      setActiveKey(targetKey)
      return
    }
    openDB(db)
  }

  function closeTab(key: string) {
    setTabs((list) => {
      const idx = list.findIndex((t) => t.key === key)
      if (idx < 0) return list
      const next = list.filter((t) => t.key !== key)
      if (activeKey === key) {
        const fallback = next[idx] || next[idx - 1]
        setActiveKey(fallback?.key || '')
      }
      return next
    })
  }

  function addTab() {
    const db = databases.find((d) => d.id === current?.dbId)
    if (!db) return
    const tab = newTab(db, { sql: '' })
    setTabs((list) => [...list, tab])
    setActiveKey(tab.key)
  }

  function cycleEnv() {
    if (!current || envPeers.length < 2) return
    const idx = envPeers.findIndex((d) => d.id === current.dbId)
    const next = envPeers[(idx + 1) % envPeers.length]
    const inst = instMap[next.instance_id] || {}
    updateCurrent({
      dbId: next.id,
      dbName: next.db_name,
      instanceId: next.instance_id,
      dbType: inst.db_type || current.dbType,
      env: inst.idc || '',
      tag: dbTag(next),
      result: null,
      error: '',
    })
  }

  function formatSQL() {
    if (!current) return
    const sql = String(current.sql || '')
      .replace(/\s+/g, ' ')
      .replace(/\s*;\s*/g, ';\n')
      .replace(/\b(SELECT|FROM|WHERE|AND|OR|JOIN|LEFT|RIGHT|INNER|GROUP BY|ORDER BY|LIMIT|UPDATE|SET|INSERT INTO|VALUES|ALTER TABLE|ADD|DROP|CREATE|GRANT|TO|SHOW|EXPLAIN)\b/gi, '\n$1')
      .replace(/\n+/g, '\n')
      .trim()
    updateCurrent({ sql })
  }

  function onSqlKey(e: React.KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      runSQL()
    }
  }

  function pushHistory(tab: TabItem) {
    setHistory((list) => {
      const next = [
        { id: `${Date.now()}`, dbId: tab.dbId, dbName: tab.dbName, sql: tab.sql, ts: Date.now() },
        ...list.filter((h) => h.sql !== tab.sql || h.dbId !== tab.dbId),
      ].slice(0, 50)
      saveJSON(HIST_KEY, next)
      return next
    })
  }

  async function runSQL(tab: TabItem | null = current) {
    if (!tab?.sql?.trim() || !tab.dbId) return
    setRunning(true)
    setTabs((list) => list.map((t) => (t.key === tab.key ? { ...t, error: '' } : t)))
    try {
      const { data } = await api.post('/query', { database_id: tab.dbId, sql: tab.sql, limit: 200 })
      const result = data.data || { columns: [], rows: [] }
      setTabs((list) => list.map((t) => (t.key === tab.key ? { ...t, result, error: '' } : t)))
      pushHistory(tab)
    } catch (err: any) {
      const errMsg = err.response?.data?.message || err.message || '查询失败'
      setTabs((list) => list.map((t) => (t.key === tab.key ? { ...t, result: null, error: errMsg } : t)))
    } finally {
      setRunning(false)
    }
  }

  async function exportDQL() {
    const tab = current
    if (!tab?.dbId || !tab.sql?.trim()) {
      message.warning('请先选择数据库并填写 SQL')
      return
    }
    Modal.confirm({
      title: '数据导出',
      content: '导出表数据需发起导出数据工单，是否继续？',
      okText: '发起导出数据工单',
      cancelText: '取消',
      onOk: () => {
        sessionStorage.setItem(DQL_DRAFT_KEY, JSON.stringify({
          database_id: tab.dbId,
          instance_id: tab.instanceId,
          db_name: tab.dbName,
          env: tab.env,
          db_type: tab.dbType,
          sql: tab.sql,
          row_count: tab.result?.rows?.length || 0,
          columns: tab.result?.columns || [],
        }))
        navigate('/ops/dql')
      },
    })
  }

  function openFavorite() {
    if (!current?.sql?.trim()) {
      message.warning('请先输入 SQL')
      return
    }
    setFavName(current.dbName || '未命名')
    setFavVisible(true)
  }

  function saveFavorite() {
    const tab = current
    if (!tab) return
    setFavorites((list) => {
      const next = [
        {
          id: `${Date.now()}`,
          name: favName.trim() || tab.dbName,
          dbId: tab.dbId,
          dbName: tab.dbName,
          sql: tab.sql,
          ts: Date.now(),
        },
        ...list,
      ]
      saveJSON(FAV_KEY, next)
      return next
    })
    setFavVisible(false)
    message.success('已收藏')
  }

  function removeFav(id: string) {
    setFavorites((list) => {
      const next = list.filter((f) => f.id !== id)
      saveJSON(FAV_KEY, next)
      return next
    })
  }

  function applyStore(item: FavItem | HistItem) {
    const db = databases.find((d) => d.id === item.dbId)
    if (db) {
      const tab = openDB(db, { sql: item.sql })
      setTabs((list) => list.map((t) => (t.key === tab.key ? { ...t, sql: item.sql } : t)))
      return
    }
    if (current) updateCurrent({ sql: item.sql })
  }

  // drag
  const dragRef = useRef<(() => void) | null>(null)
  function startDrag(kind: 'left' | 'hist' | 'editor', e: React.MouseEvent) {
    e.preventDefault()
    if (dragRef.current) dragRef.current()
    const sx = e.clientX
    const sy = e.clientY
    const startLeft = leftWidth
    const startHist = histWidth
    const startEditor = editorHeight
    const move = (ev: MouseEvent) => {
      if (kind === 'left') setLeftWidth(Math.min(520, Math.max(200, startLeft + ev.clientX - sx)))
      if (kind === 'hist') setHistWidth(Math.min(480, Math.max(180, startHist - (ev.clientX - sx))))
      if (kind === 'editor') setEditorHeight(Math.min(640, Math.max(200, startEditor + ev.clientY - sy)))
    }
    const up = () => { if (dragRef.current) dragRef.current() }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
    window.addEventListener('blur', up)
    dragRef.current = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      window.removeEventListener('blur', up)
      dragRef.current = null
    }
  }

  const loadTree = useCallback(async () => {
    setTreeLoading(true)
    try {
      const [dbRes, instRes] = await Promise.all([api.get('/databases'), api.get('/instances')])
      setDatabases(dbRes.data.data || [])
      setInstances(instRes.data.data || [])
      const envs = [...new Set((instRes.data.data || []).map((i: any) => i.idc).filter(Boolean))].sort(cmpName) as string[]
      setEnvOptions(envs)
    } finally {
      setTreeLoading(false)
    }
  }, [])

  useEffect(() => {
    loadTree()
    return () => { if (dragRef.current) dragRef.current() }
  }, [loadTree])

  const hasTreeData = treeData.length > 0

  return (
    <div className="query-page">
      <div className="query-body">
        <div className="query-wm" aria-hidden="true">
          {Array.from({ length: 30 }).map((_, n) => <span key={n}>{username}</span>)}
        </div>
        <div
          className={`pane-left${leftCollapsed ? ' is-collapsed' : ''}`}
          style={{ width: leftCollapsed ? 0 : leftWidth }}
        >
          <div className="tree-search">
            <Input
              value={keyword}
              allowClear
              placeholder="搜索数据库"
              prefix={<SearchOutlined />}
              onChange={(e) => setKeyword(e.target.value)}
              addonAfter={
                <Select
                  value={envFilter}
                  style={{ width: 118 }}
                  onChange={setEnvFilter}
                  options={[{ label: '全部环境', value: '' }, ...envOptions.map((e) => ({ label: e, value: e }))]}
                  placeholder="筛选环境"
                />
              }
            />
          </div>
          <Spin spinning={treeLoading} wrapperClassName="tree-wrap">
            {hasTreeData ? (
              <Tree
                treeData={treeData}
                loadData={onLoadData}
                expandedKeys={expandedKeys}
                onExpand={(keys) => setExpandedKeys(keys)}
                expandAction="click"
                titleRender={renderNodeTitle}
                onSelect={onTreeSelect}
                blockNode
              />
            ) : (!treeLoading ? <div className="tree-empty">暂无数据</div> : null)}
          </Spin>
        </div>
        <div
          className={`gutter-x${leftCollapsed ? ' is-collapsed' : ''}`}
          onMouseDown={(e) => { if (!leftCollapsed) startDrag('left', e) }}
        >
          <Tooltip title={leftCollapsed ? '展开库树' : '收起库树'} placement="right">
            <button
              type="button"
              className="pane-collapse-btn"
              aria-label={leftCollapsed ? '展开库树' : '收起库树'}
              onMouseDown={(e) => e.stopPropagation()}
              onClick={() => setLeftCollapsed((v) => !v)}
            >
              {leftCollapsed ? <DoubleRightOutlined /> : <DoubleLeftOutlined />}
            </button>
          </Tooltip>
        </div>
        <div className="pane-right">
          {!tabs.length ? (
            <div className="empty-main">
              <svg className="bolt" viewBox="0 0 24 24" fill="none">
                <path d="M13 2 4 14h7l-1 8 10-14h-7l0-6Z" fill="#2563eb" />
              </svg>
              <span>先从左侧选择一个数据库进行操作</span>
            </div>
          ) : (
            <>
              <div className="work-tabs">
                <Tabs
                  type="editable-card"
                  hideAdd
                  activeKey={activeKey}
                  onChange={setActiveKey}
                  onEdit={(targetKey, action) => { if (action === 'remove') closeTab(String(targetKey)) }}
                  items={tabs.map((t) => ({
                    key: t.key,
                    label: (
                      <span className="tab-lab">
                        <DbTypeIcon type={t.dbType} size={14} />
                        <span>{t.dbName}</span>
                        {t.tag ? <span className="tab-tag">[{t.tag}]</span> : null}
                        <span className="tab-time">{fmtTime(t.ts)}</span>
                      </span>
                    ),
                  }))}
                />
                <Button className="tab-add" type="text" onClick={addTab}>+</Button>
              </div>
              <div className="editor-split" style={{ height: editorHeight }}>
                <div className="editor-col">
                  <div className="tool-bar">
                    <div className="tool-left">
                      <Button
                        type="primary"
                        disabled={!current?.sql?.trim()}
                        loading={running}
                        icon={<CaretRightOutlined />}
                        onClick={() => runSQL()}
                      >
                        运行
                      </Button>
                      <Button onClick={formatSQL}>格式化</Button>
                    </div>
                    <Button icon={<StarOutlined />} onClick={openFavorite}>收藏</Button>
                  </div>
                  <div className="db-meta">
                    <Tag color={envTagColor(current?.env)}>{current?.env || '—'}</Tag>
                    {envPeers.length > 1 ? (
                      <span className="env-swap" onClick={cycleEnv} title="切换同名库环境">⇄</span>
                    ) : null}
                    <DbTypeIcon type={current?.dbType} size={16} />
                    <span className="db-name">
                      {current?.dbName}
                      {current?.tag ? <span>({current.tag})</span> : null}
                    </span>
                  </div>
                  <textarea
                    value={current?.sql || ''}
                    className="sql-box"
                    spellCheck={false}
                    placeholder="输入 SQL，Ctrl / ⌘ + Enter 运行"
                    onChange={(e) => updateCurrent({ sql: e.target.value })}
                    onKeyDown={onSqlKey}
                  />
                </div>
                <div className="gutter-x inner" onMouseDown={(e) => startDrag('hist', e)} />
                <div className="hist-col" style={{ width: histWidth }}>
                  <Tabs
                    activeKey={storeTab}
                    onChange={setStoreTab}
                    className="store-tabs"
                    items={[
                      {
                        key: 'fav',
                        label: '我的收藏',
                        children: (
                          <>
                            <Input
                              value={favKeyword}
                              size="small"
                              allowClear
                              placeholder="搜索收藏（收藏名称、SQL内容）"
                              onChange={(e) => setFavKeyword(e.target.value)}
                            />
                            <div className="store-list">
                              {filteredFavs.map((item) => (
                                <div key={item.id} className="store-item" onClick={() => applyStore(item)}>
                                  <div className="store-title">
                                    <span>{item.name}</span>
                                    <Button
                                      type="text"
                                      danger
                                      size="small"
                                      onClick={(e) => { e.stopPropagation(); removeFav(item.id) }}
                                    >
                                      删除
                                    </Button>
                                  </div>
                                  <div className="store-sub">{item.dbName} · {fmtTime(item.ts)}</div>
                                  <div className="store-sql">{item.sql}</div>
                                </div>
                              ))}
                              {!filteredFavs.length ? <div className="store-empty">暂无收藏</div> : null}
                            </div>
                          </>
                        ),
                      },
                      {
                        key: 'hist',
                        label: '查询历史',
                        children: (
                          <div className="store-list">
                            {history.map((item) => (
                              <div key={item.id} className="store-item" onClick={() => applyStore(item)}>
                                <div className="store-title">
                                  <span>{item.dbName}</span>
                                  <span className="store-sub">{fmtTime(item.ts)}</span>
                                </div>
                                <div className="store-sql">{item.sql}</div>
                              </div>
                            ))}
                            {!history.length ? <div className="store-empty">暂无历史</div> : null}
                          </div>
                        ),
                      },
                    ]}
                  />
                </div>
              </div>
              <div className="gutter-y" onMouseDown={(e) => startDrag('editor', e)} />
              <Spin spinning={running} wrapperClassName="result-col">
                {current?.error ? (
                  <div className="result-err">{current.error}</div>
                ) : current?.result ? (
                  <>
                    <div className="result-meta">
                      <span>
                        {current.result.rows?.length || 0} 行
                        {current.result.truncated ? '（已截断）' : ''}
                        {' · '}{current.result.elapsed_ms || 0} ms
                      </span>
                      <Button type="primary" size="small" onClick={exportDQL}>导出</Button>
                    </div>
                    <ResponsiveTable
                      dataSource={resultRows}
                      rowKey="__k"
                      bordered
                      size="small"
                      pagination={false}
                      scroll={{ y: 'calc(100% - 40px)', x: 'max-content' }}
                      columns={(current.result.columns || []).map((col) => ({
                        title: col,
                        dataIndex: col,
                        key: col,
                        width: 120,
                        ellipsis: true,
                      }))}
                    />
                  </>
                ) : (
                  <div className="result-placeholder">运行查询后在此展示结果</div>
                )}
              </Spin>
            </>
          )}
        </div>
      </div>

      <Modal
        open={favVisible}
        title="收藏 SQL"
        width={420}
        onCancel={() => setFavVisible(false)}
        onOk={saveFavorite}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Input value={favName} placeholder="收藏名称" onChange={(e) => setFavName(e.target.value)} />
      </Modal>
    </div>
  )
}
