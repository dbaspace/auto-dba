import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { createPortal } from 'react-dom'
import {
  useLocation,
  useNavigate } from 'react-router-dom'
import {
  Button,
  Input,
  Drawer,
  Modal,
  Pagination,
  Popover,
  Select,
  Space,
  Spin,
  Switch,
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
  CodeOutlined,
  CopyOutlined,
  DoubleLeftOutlined,
  DoubleRightOutlined,
  DownloadOutlined,
  ExpandAltOutlined,
  SearchOutlined,
  StarOutlined,
  StopOutlined,
  TableOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'
import axios from 'axios'
import api from '../../api/request'
import { useAppSelector } from '../../store'
import { prefetchQueryCatalog, readQueryTreeCache, refreshQueryCatalog } from './queryTreeCache'
import { queryFavoriteKey, queryHistoryKey, queryWorkspaceKey } from './querySession'
import DbTypeIcon from '../../components/DbTypeIcon'
import SqlEditor from '../../components/SqlEditor'
import { useQuerySuggest, type SuggestEnv } from './useQuerySuggest'
import { newClientRequestID } from '../../utils/clientRequestId'
import { formatSqlContent } from '../../utils/formatSql'
import { applySqlLineComment, type SqlCommentMode } from '../../utils/sqlComment'
import {
  buildExportContent,
  downloadPasswordZip,
  downloadTextFile,
  formatJsonKeepBigint,
  tableNameFromSQL,
} from './exportResult'
import './CommonQuery.css'

const DQL_DRAFT_KEY = 'auto-dba-dql-draft'
const PAGE_SIZE_KEY = 'query_result_page_size'

const TYPE_ORDER = [
  'mysql', 'postgres', 'tidb', 'cockroachdb', 'oceanbase', 'cassandra',
  'oracle', 'elasticsearch', 'mongodb', 'sqlserver', 'starrocks', 'clickhouse',
]

interface QueryResult {
  columns?: string[]
  rows?: any[][]
  truncated?: boolean
  notice?: string
  elapsed_ms?: number
  limit?: number
  applied_limit?: number
  valid_sql?: string
}

interface SqlSnap {
  sql: string
  cursor: number
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
  favId?: string
  /** 未收藏时的页签自定义名；空则显示库名 */
  alias?: string
  /** 最近一次成功执行并写出结果的语句 */
  executedSql?: string
  result: QueryResult | null
  results?: QueryResult[]
  resultIdx?: number
  /** 结果区搜索词，跟页签走，切窗口不丢 */
  resultKeyword?: string
  /** 结果区当前页，跟页签走 */
  resultPage?: number
  error: string
}

interface FavItem {
  id: string
  name: string
  dbId: string
  dbName: string
  env?: string
  sql: string
  ts: number
}

interface HistItem {
  id: string
  dbId: string
  dbName: string
  env?: string
  instanceId?: string
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

const RESULT_COL_MIN = 72
const RESULT_COL_MAX = 460
let measureCanvas: HTMLCanvasElement | null = null

function measureResultText(text: string): number {
  if (typeof document === 'undefined') return text.length * 8
  if (!measureCanvas) measureCanvas = document.createElement('canvas')
  const ctx = measureCanvas.getContext('2d')
  if (!ctx) return text.length * 8
  ctx.font = '12px ui-sans-serif, system-ui, sans-serif'
  return ctx.measureText(text || '').width
}

function highlightText(text: string, keyword: string): React.ReactNode {
  const q = keyword.trim()
  if (!q) return text
  const lower = text.toLowerCase()
  const nq = q.toLowerCase()
  const parts: React.ReactNode[] = []
  let from = 0
  let n = 0
  while (from < text.length) {
    const idx = lower.indexOf(nq, from)
    if (idx < 0) {
      parts.push(text.slice(from))
      break
    }
    if (idx > from) parts.push(text.slice(from, idx))
    parts.push(<span className="result-hit" key={`${idx}-${n}`}>{text.slice(idx, idx + q.length)}</span>)
    n += 1
    from = idx + q.length
  }
  return parts
}

function CellDetailBody({
  raw, index, total, prettyOn, onPretty, onPrev, onNext,
}: {
  raw: string
  index: number
  total: number
  prettyOn: boolean
  onPretty: (v: boolean) => void
  onPrev: () => void
  onNext: () => void
}) {
  const pretty = formatJsonKeepBigint(raw)
  const shown = prettyOn && pretty ? pretty : raw
  return (
    <div className="result-detail">
      <div className="result-detail-bar">
        <span>
          <Button size="small" disabled={index <= 0} onClick={onPrev}>上一条</Button>
          <Button size="small" disabled={index >= total - 1} onClick={onNext} style={{ marginLeft: 8 }}>下一条</Button>
          <span className="result-detail-pos">{total ? index + 1 : 0} / {total} 条记录</span>
        </span>
        <span>
          <Button
            size="small"
            icon={<CopyOutlined />}
            onClick={() => {
              navigator.clipboard.writeText(shown).then(
                () => message.success('已复制'),
                () => message.error('复制失败'),
              )
            }}
          >
            拷贝
          </Button>
          {pretty ? (
            <Button size="small" icon={<CodeOutlined />} style={{ marginLeft: 8 }} onClick={() => onPretty(!prettyOn)}>
              {prettyOn ? '原文本' : '格式化'}
            </Button>
          ) : null}
        </span>
      </div>
      <pre className="result-detail-body">{shown}</pre>
    </div>
  )
}

function ResultCell({ text, keyword, onExpand }: { text: string; keyword: string; onExpand: () => void }) {
  const ref = useRef<HTMLSpanElement | null>(null)
  const [overflow, setOverflow] = useState(false)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setOverflow(el.scrollWidth - el.clientWidth > 1)
  }, [text, keyword])
  return (
    <div className="result-cell">
      <span ref={ref} className="result-cell-text" title={text}>{highlightText(text, keyword)}</span>
      {overflow ? (
        <button
          type="button"
          className="result-cell-expand"
          title="展开"
          onClick={(e) => { e.stopPropagation(); onExpand() }}
        >
          <ExpandAltOutlined />
        </button>
      ) : null}
    </div>
  )
}

function resultCellKey(index: number) {
  return `c${index}`
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

/** 与已登记库 resolveRowEnv 一致：tags 的 idc= 优先，回退实例 idc。 */
function resolveDbEnv(db: any, inst?: any): string {
  for (const raw of dbTags(db)) {
    const i = raw.indexOf('=')
    if (i <= 0) continue
    if (raw.slice(0, i).trim().toLowerCase() === 'idc') {
      const v = raw.slice(i + 1).trim()
      if (v) return v
    }
  }
  return String(inst?.idc || '').trim()
}

/** 同名库里分开相同环境名：实例 domain=、库域名、实例域名、实例名，取第一个非空值。 */
function peerMark(db: any, inst?: any): string {
  const fromTag = firstTagValue(inst, 'domain')
  if (fromTag) return fromTag
  const domains = Array.isArray(db?.domains) ? db.domains : []
  for (const raw of domains) {
    const v = String(raw || '').trim()
    if (v) return v
  }
  const domainName = String(inst?.domain_name || '').trim()
  if (domainName) return domainName
  return String(inst?.name || '').trim()
}

function firstTagValue(source: any, key: string): string {
  const want = key.toLowerCase()
  for (const raw of dbTags(source)) {
    const i = raw.indexOf('=')
    if (i <= 0) continue
    if (raw.slice(0, i).trim().toLowerCase() !== want) continue
    const v = raw.slice(i + 1).trim()
    if (v) return v
  }
  return ''
}

function peerEnvLabel(db: any, inst: any, peers: any[], instOf: (row: any) => any): { env: string; label: string; title: string } {
  const env = resolveDbEnv(db, inst) || '—'
  const mark = peerMark(db, inst)
  const instName = String(inst?.name || '').trim()
  const same = peers.filter((row) => (resolveDbEnv(row, instOf(row)) || '—') === env).length
  const label = same > 1 && mark ? `${env} · ${mark}` : env
  const extra = [mark, instName && instName !== mark ? instName : ''].filter(Boolean)
  const title = extra.length ? [env, ...extra].join(' · ') : env
  return { env, label, title }
}

/** 查询树短展示名：优先 alias=，兼容 label=；无则空（不拼接整段 tags）。 */
function dbAliasFromTags(db: any): string {
  let fromLabel = ''
  for (const raw of dbTags(db)) {
    const i = raw.indexOf('=')
    if (i <= 0) continue
    const key = raw.slice(0, i).trim().toLowerCase()
    const val = raw.slice(i + 1).trim()
    if (!val) continue
    if (key === 'alias') return val
    if (key === 'label' && !fromLabel) fromLabel = val
  }
  return fromLabel
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
    favId: t.favId || '',
    alias: t.alias || '',
    executedSql: t.executedSql || '',
    result: t.result && Array.isArray(t.result.columns) ? t.result : null,
    results: Array.isArray(t.results) ? t.results : undefined,
    resultIdx: typeof t.resultIdx === 'number' ? t.resultIdx : 0,
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

function findTreeNode(nodes: DataNode[] | undefined, key: React.Key): DataNode | undefined {
  for (const n of nodes || []) {
    if (n.key === key) return n
    const hit = findTreeNode(n.children, key)
    if (hit) return hit
  }
  return undefined
}

function reorderQueryTabs(list: TabItem[], fromKey: string, toKey: string): TabItem[] {
  if (!fromKey || !toKey || fromKey === toKey || list.length < 2) return list
  const fromIdx = list.findIndex((t) => t.key === fromKey)
  const toIdx = list.findIndex((t) => t.key === toKey)
  if (fromIdx < 0 || toIdx < 0) return list
  const next = list.slice()
  const [moved] = next.splice(fromIdx, 1)
  let insertAt = next.findIndex((t) => t.key === toKey)
  if (insertAt < 0) return list
  if (fromIdx < toIdx) insertAt += 1
  next.splice(insertAt, 0, moved)
  return next
}

function statementCount(sql: string) {
  let n = 0
  for (const part of sql.split(';')) {
    if (part.trim()) n += 1
  }
  return n
}

function queryTabKeyAt(x: number, y: number): string | null {
  const hit = document.elementFromPoint(x, y)
  if (!hit || hit.closest('.ant-tabs-tab-remove')) return null
  const tab = hit.closest('.work-tabs .ant-tabs-tab')
  const lab = tab?.querySelector('[data-tab-key]') as HTMLElement | null
  return lab?.dataset.tabKey || null
}

function loadWorkspace(key: string): Workspace {
  const empty: Workspace = { tabs: [], activeKey: '', leftWidth: 280, editorHeight: 320, histWidth: 300 }
  try {
    const raw = sessionStorage.getItem(key)
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
  const tenantId = String(
    (useAppSelector((s) => s.auth.user) as { current_tenant?: { id?: string } })?.current_tenant?.id || '',
  )
  const workspaceStoreKey = queryWorkspaceKey(tenantId)
  const favStoreKey = queryFavoriteKey(tenantId)
  const histStoreKey = queryHistoryKey(tenantId)

  const username = useMemo(() => {
    try {
      return JSON.parse(localStorage.getItem('user') || '{}').username || ''
    } catch {
      return ''
    }
  }, [])

  const [treeLoading, setTreeLoading] = useState(false)
  const [catalogReady, setCatalogReady] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [envPickOpen, setEnvPickOpen] = useState(false)
  const [envOptions, setEnvOptions] = useState<string[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [tablesMap, setTablesMap] = useState<Record<string, string[]>>({})
  const tableLoads = useRef<Record<string, Promise<{ names: string[]; error: string }>>>({})

  const saved = useMemo(() => loadWorkspace(workspaceStoreKey), [workspaceStoreKey])
  const [tabs, setTabs] = useState<TabItem[]>(saved.tabs)
  const [activeKey, setActiveKey] = useState(saved.activeKey)
  const [running, setRunning] = useState(false)
  const queryAbortRef = useRef<AbortController | null>(null)
  const sqlBoxRef = useRef<HTMLTextAreaElement | null>(null)
  const sqlUndoRef = useRef<Record<string, { undo: SqlSnap[]; redo: SqlSnap[] }>>({})
  const suggestEnv = useRef<SuggestEnv>({
    sql: '',
    dbId: '',
    dbType: '',
    tableNames: [],
    ensureTables: () => {},
    apply: () => {},
  })
  const suggest = useQuerySuggest(suggestEnv, () => sqlBoxRef.current)
  const clickSelectionRef = useRef<{ start: number; end: number; text: string; sql: string; key: string } | null>(null)
  const runPointerDownRef = useRef(false)
  const selectionRestoreRef = useRef<{ start: number; end: number; sql: string } | null>(null)
  const collapseSelectionRef = useRef(false)
  const revealDbRef = useRef('')
  const pendingStoreRef = useRef<{ item: FavItem | HistItem; source: 'fav' | 'hist' } | null>(null)
  const [selectionEpoch, setSelectionEpoch] = useState(0)
  const tabPointerRef = useRef<{ key: string; x: number; y: number; dragging: boolean; pointerId: number } | null>(null)
  const tabDraggedRef = useRef(false)
  const lastTabClickRef = useRef<{ key: string; at: number } | null>(null)
  const [dragOverKey, setDragOverKey] = useState<string | null>(null)
  const [draggingKey, setDraggingKey] = useState<string | null>(null)
  const [renamingKey, setRenamingKey] = useState<string | null>(null)
  const [renameDraft, setRenameDraft] = useState('')
  const renameLockRef = useRef(false)
  const renameCancelRef = useRef(false)
  const resultTableWrapRef = useRef<HTMLDivElement | null>(null)
  const [resultScrollY, setResultScrollY] = useState(240)
  const [resultPageSize, setResultPageSize] = useState(() => {
    const n = Number(localStorage.getItem(PAGE_SIZE_KEY) || 20)
    return [10, 20, 50, 100].includes(n) ? n : 20
  })
  const [colWidths, setColWidths] = useState<number[]>([])
  const colDragRef = useRef(false)
  const [cellDetail, setCellDetail] = useState<{ row: number; col: number } | null>(null)
  const [cellPretty, setCellPretty] = useState(false)
  const [directExportOpen, setDirectExportOpen] = useState(false)
  const [exportFormat, setExportFormat] = useState<'csv' | 'json' | 'sql'>('csv')
  const [exportEncrypt, setExportEncrypt] = useState(false)
  const [exportPassword, setExportPassword] = useState('')
  const [exporting, setExporting] = useState(false)
  const [tableInfoOpen, setTableInfoOpen] = useState(false)
  const [tableInfoLoading, setTableInfoLoading] = useState(false)
  const [tableInfoMode, setTableInfoMode] = useState<'list' | 'sql'>('list')
  const [tableInfo, setTableInfo] = useState<{
    schema?: string
    table?: string
    columns?: {
      name: string
      data_type: string
      nullable: boolean
      default?: string
      comment?: string
    }[]
    ddl?: string
  } | null>(null)
  const [tableInfoError, setTableInfoError] = useState('')
  const [leftWidth, setLeftWidth] = useState(saved.leftWidth)
  const [leftCollapsed, setLeftCollapsed] = useState(false)
  const [editorHeight, setEditorHeight] = useState(saved.editorHeight)
  const [histWidth, setHistWidth] = useState(saved.histWidth)
  const [storeTab, setStoreTab] = useState(() => {
    const tab = (location.state as { storeTab?: string } | null)?.storeTab
    return tab === 'hist' || tab === 'fav' ? tab : 'fav'
  })
  const [favorites, setFavorites] = useState<FavItem[]>(() => loadJSON<FavItem[]>(favStoreKey, []))
  const [history, setHistory] = useState<HistItem[]>(() => loadJSON<HistItem[]>(histStoreKey, []))
  const [favVisible, setFavVisible] = useState(false)
  const [favMode, setFavMode] = useState<'create' | 'saveas'>('create')
  const [favName, setFavName] = useState('')
  const [favSql, setFavSql] = useState('')
  const [favKeyword, setFavKeyword] = useState('')
  const [treeData, setTreeData] = useState<DataNode[]>([])
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([])
  const [selectedKeys, setSelectedKeys] = useState<React.Key[]>([])

  useEffect(() => {
    const tab = (location.state as { storeTab?: string } | null)?.storeTab
    if (tab === 'hist' || tab === 'fav') setStoreTab(tab)
  }, [location.state])

  const current = useMemo(() => tabs.find((t) => t.key === activeKey) || null, [tabs, activeKey])
  const resultKeyword = current?.resultKeyword || ''
  const resultPage = Math.max(current?.resultPage || 1, 1)

  useLayoutEffect(() => {
    const el = sqlBoxRef.current
    if (!el) return
    if (collapseSelectionRef.current) {
      if (el.value !== (current?.sql || '')) return
      collapseSelectionRef.current = false
      const pos = el.value.length
      el.setSelectionRange(pos, pos)
      return
    }
    const saved = selectionRestoreRef.current
    if (!saved) return
    if (saved.start < 0 || saved.end > el.value.length || el.value !== saved.sql) return
    const active = document.activeElement as HTMLElement | null
    if (active && active !== el && active !== document.body) {
      const tag = active.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || active.isContentEditable) return
    }
    el.focus()
    el.setSelectionRange(saved.start, saved.end)
  }, [selectionEpoch, current?.sql, current?.key])

  const instMap = useMemo(() => {
    const m: Record<string, any> = {}
    for (const i of instances) m[i.id] = i
    return m
  }, [instances])

  const dbTypeOf = useCallback((db: any) => normalizeType(instMap[db.instance_id]?.db_type), [instMap])
  const dbEnv = useCallback((db: any) => resolveDbEnv(db, instMap[db.instance_id]) || '未分组', [instMap])

  const instanceHint = useCallback((db: any) => {
    const inst = instMap[db?.instance_id] || {}
    return inst.name || inst.domain_name || (inst.domains && inst.domains[0]) || inst.host || ''
  }, [instMap])

  const dbTagText = useCallback((db: any) => dbAliasFromTags(db), [])

  const dbTag = useCallback((db: any) => dbTagText(db), [dbTagText])

  const sortDbs = useCallback((dbs: any[]) => {
    return [...dbs].sort((a, b) => {
      const ra = typeRank(dbTypeOf(a))
      const rb = typeRank(dbTypeOf(b))
      if (ra !== rb) return ra - rb
      const ta = dbTypeOf(a)
      const tb = dbTypeOf(b)
      if (ta !== tb) return ta.localeCompare(tb)
      const dispA = dbTagText(a) || a.db_name
      const dispB = dbTagText(b) || b.db_name
      const byDisp = cmpName(dispA, dispB)
      if (byDisp) return byDisp
      return cmpName(a.db_name, b.db_name)
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
      const t = dbTypeOf(db)
      if (t === 'kafka' || t === 'redis') continue
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
  }, [databases, envFilter, matchKeyword, dbEnv, dbTypeOf, sortDbs])

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
      const tip = data.dbName && data.dbName !== data.label
        ? `${data.label}（${data.dbName}）`
        : data.label
      return (
        <span className="tree-node" title={tip}>
          <DbTypeIcon type={data.dbType} size={16} />
          <span className="tree-label">{highlightLabel(data.label)}</span>
          {data.tag ? <span className="tree-tag">{data.tag}</span> : null}
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

  const fetchTables = useCallback((dbId: string): Promise<{ names: string[]; error: string }> => {
    if (Object.prototype.hasOwnProperty.call(tablesMap, dbId)) {
      return Promise.resolve({ names: tablesMap[dbId], error: '' })
    }
    if (tableLoads.current[dbId]) return tableLoads.current[dbId]
    const p = api.get(`/databases/${dbId}/tables`)
      .then((res) => {
        const names: string[] = res.data.data || []
        const hint = String(res.data?.message || '').trim()
        setTablesMap((m) => ({ ...m, [dbId]: names }))
        return { names, error: !names.length && hint ? hint : '' }
      })
      .catch((e: any) => {
        const hint = String(e?.response?.data?.message || e?.message || '加载表失败').trim()
        setTablesMap((m) => ({ ...m, [dbId]: [] }))
        return { names: [] as string[], error: hint }
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
        const alias = dbTagText(db)
        const dbName = String(db.db_name || '')
        // 主标题为库名；有 alias/label 时 tag 展示短名
        const label = dbName || alias
        return buildNode({
          id: db.id,
          label,
          dbName,
          type: 'db',
          dbId: db.id,
          env: resolveDbEnv(db, inst) || '未分组',
          dbType: inst.db_type,
          tag: alias && alias !== dbName ? alias : '',
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
    return fetchTables(data.dbId).then(({ names, error }) => {
      let children: DataNode[]
      if (!names.length) {
        children = [buildNode({ id: `${data.dbId}__empty`, label: error || '暂无表', type: 'empty' }, true)]
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

  useEffect(() => {
    const dbId = current?.dbId
    if (!dbId) {
      revealDbRef.current = ''
      setSelectedKeys((prev) => (prev.length ? [] : prev))
      return
    }
    const db = databases.find((d) => d.id === dbId)
    if (!db) return
    const envName = dbEnv(db)
    if (revealDbRef.current !== dbId) {
      revealDbRef.current = dbId
      const hiddenByEnv = !!(envFilter && envName !== envFilter)
      const hiddenByKw = !matchKeyword(db)
      if (hiddenByEnv || hiddenByKw) {
        if (hiddenByEnv) setEnvFilter('')
        if (hiddenByKw) setKeyword('')
        return
      }
    }
    if ((envFilter && envName !== envFilter) || !matchKeyword(db)) return
    setSelectedKeys((prev) => (prev.length === 1 && prev[0] === db.id ? prev : [db.id]))
    const envKey = `env:${envName}`
    setExpandedKeys((prev) => {
      if (prev.includes(envKey) && prev.includes(db.id)) return prev
      return Array.from(new Set([...prev, envKey, db.id]))
    })
    const node = findTreeNode(treeData, db.id)
    if (node && !node.children?.length) {
      const inst = instMap[db.instance_id] || {}
      void fetchTables(db.id).then(({ names, error }) => {
        const children = !names.length
          ? [buildNode({ id: `${db.id}__empty`, label: error || '暂无表', type: 'empty' }, true)]
          : [...names].sort(cmpName).map((name) => buildNode({
            id: `${db.id}__${name}`,
            label: name,
            type: 'table',
            dbId: db.id,
            table: name,
            env: envName,
            dbType: inst.db_type,
          }, true))
        updateTreeChildren(db.id, children)
      })
    }
  }, [current?.dbId, databases, keyword, envFilter, treeData, dbEnv, matchKeyword, fetchTables, buildNode, instMap])

  useEffect(() => {
    if (!selectedKeys.length) return
    const frame = requestAnimationFrame(() => {
      const root = document.querySelector('.query-page .tree-wrap')
      const el = root?.querySelector('.ant-tree-treenode-selected')
      if (el instanceof HTMLElement) el.scrollIntoView({ block: 'nearest' })
    })
    return () => cancelAnimationFrame(frame)
  }, [selectedKeys])

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

  const activeResult = useMemo(() => {
    if (!current) return null
    const list = current.results?.length ? current.results : current.result ? [current.result] : []
    if (!list.length) return null
    const idx = Math.min(Math.max(current.resultIdx || 0, 0), list.length - 1)
    return list[idx]
  }, [current])

  const resultRowsAll = useMemo(() => {
    const res = activeResult
    if (!res) return []
    return (res.rows || []).map((row, ri) => {
      const obj: Record<string, any> = { __k: ri }
      ;(res.columns || []).forEach((_, i) => {
        obj[resultCellKey(i)] = cellText(row[i])
      })
      return obj
    })
  }, [activeResult])

  const resultRowsFiltered = useMemo(() => {
    const q = resultKeyword.trim().toLowerCase()
    if (!q) return resultRowsAll
    return resultRowsAll.filter((row) =>
      Object.keys(row).some((k) => k !== '__k' && String(row[k] ?? '').toLowerCase().includes(q)),
    )
  }, [resultRowsAll, resultKeyword])

  const resultRows = useMemo(() => {
    const start = (Math.max(resultPage, 1) - 1) * resultPageSize
    return resultRowsFiltered.slice(start, start + resultPageSize)
  }, [resultRowsFiltered, resultPage, resultPageSize])

  const resultSig = `${current?.key || ''}|${current?.resultIdx || 0}|${(activeResult?.columns || []).join('\0')}|${activeResult?.elapsed_ms || 0}`
  useEffect(() => {
    colDragRef.current = false
    setCellDetail(null)
    setCellPretty(false)
  }, [resultSig])

  useEffect(() => {
    if (colDragRef.current) return
    const cols = activeResult?.columns || []
    if (!cols.length) {
      setColWidths([])
      return
    }
    const widths = cols.map((name, i) => {
      let w = measureResultText(name) + 36
      for (const row of resultRows) {
        w = Math.max(w, measureResultText(String(row[resultCellKey(i)] ?? '')) + 36)
        if (w >= RESULT_COL_MAX) {
          w = RESULT_COL_MAX
          break
        }
      }
      return Math.max(RESULT_COL_MIN, Math.min(RESULT_COL_MAX, Math.ceil(w)))
    })
    setColWidths(widths)
  }, [activeResult, resultRows, resultSig])

  function startColResize(e: React.MouseEvent, index: number) {
    e.preventDefault()
    e.stopPropagation()
    colDragRef.current = true
    const startX = e.clientX
    const startW = colWidths[index] || 120
    const move = (ev: MouseEvent) => {
      const next = Math.max(RESULT_COL_MIN, startW + ev.clientX - startX)
      setColWidths((list) => {
        const copy = list.slice()
        copy[index] = next
        return copy
      })
    }
    const up = () => {
      document.removeEventListener('mousemove', move)
      document.removeEventListener('mouseup', up)
    }
    document.addEventListener('mousemove', move)
    document.addEventListener('mouseup', up)
  }

  function openCellDetail(pageIndex: number, col: number) {
    const row = (Math.max(resultPage, 1) - 1) * resultPageSize + pageIndex
    setCellPretty(false)
    setCellDetail({ row, col })
  }

  const execSQLText = useMemo(() => {
    const v = String(activeResult?.valid_sql || current?.sql || '').trim()
    return v.replace(/\s+/g, ' ')
  }, [activeResult?.valid_sql, current?.sql])

  const resultTableName = useMemo(() => {
    const sql = String(activeResult?.valid_sql || current?.sql || '')
    const m = /\bfrom\s+(?:([`"\[]?[\w$]+[`"\]]?)\.)?([`"\[]?[\w$]+[`"\]]?)/i.exec(sql)
    if (!m) return current?.table || ''
    const schema = (m[1] || '').replace(/[`"[\]]/g, '')
    const table = (m[2] || '').replace(/[`"[\]]/g, '')
    return schema ? `${schema}.${table}` : table
  }, [activeResult?.valid_sql, current?.sql, current?.table])

  const openTableInfo = async () => {
    if (!current?.dbId) {
      message.warning('请先选择数据库')
      return
    }
    const sql = String(activeResult?.valid_sql || current.sql || '').trim()
    if (!sql) {
      message.warning('请先编写或执行 SQL')
      return
    }
    setTableInfoOpen(true)
    setTableInfoMode('list')
    setTableInfo(null)
    setTableInfoError('')
    setTableInfoLoading(true)
    try {
      const { data } = await api.post('/query/table-info', {
        database_id: current.dbId,
        sql,
      })
      setTableInfo(data?.data || null)
    } catch (e: any) {
      setTableInfoError(e?.response?.data?.message || e?.message || '获取表信息失败')
    } finally {
      setTableInfoLoading(false)
    }
  }

  const copyTableDDL = async () => {
    const text = String(tableInfo?.ddl || '').trim()
    if (!text) {
      message.warning('无可复制的 SQL')
      return
    }
    try {
      await navigator.clipboard.writeText(text)
      message.success('已复制')
    } catch {
      message.error('复制失败')
    }
  }

  // 结果区高度变化时用像素设置 scroll.y；需扣除表头与横向滚动条，否则滚到底最后一行被裁切
  useEffect(() => {
    const el = resultTableWrapRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const measure = () => {
      const wrapH = el.clientHeight
      const header =
        el.querySelector('.ant-table-header') ||
        el.querySelector('.ant-table-thead')
      const headerH = header instanceof HTMLElement ? header.offsetHeight : 39
      // scroll.x 时底部会出现横向滚动条，占掉一部分可视高度
      const hScrollReserve = 14
      setResultScrollY(Math.max(120, wrapH - headerH - hScrollReserve))
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    // 表头渲染后再量一次，避免初次 header 尚未挂载
    const t = window.setTimeout(measure, 0)
    return () => {
      ro.disconnect()
      window.clearTimeout(t)
    }
  }, [!!activeResult, editorHeight, leftCollapsed, resultPageSize, activeResult?.columns?.length])

  const mergeRemoteHistory = useCallback(async () => {
    try {
      const { data: histRes } = await api.get('/query/history').catch(() => ({ data: { data: [] } }))
      const remoteHist: HistItem[] = (histRes?.data || [])
        .filter((h: any) => h?.statement)
        .map((h: any) => {
          const instanceId = String(h.instance_id || '')
          const inst = instanceId ? instMap[instanceId] : undefined
          const hits = instanceId
            ? databases.filter((d) => d.db_name === (h.target || '') && d.instance_id === instanceId)
            : []
          const db = hits[0]
          const env = db ? resolveDbEnv(db, inst) : String(inst?.idc || '').trim()
          return {
            id: h.id,
            dbId: db?.id || '',
            dbName: h.target || '',
            env,
            instanceId,
            sql: h.statement,
            ts: Date.parse(h.created_at) || Date.now(),
          }
        })
      if (!remoteHist.length) return
      setHistory((local) => {
        const map = new Map<string, HistItem>()
        // 服务端优先（按当前用户审计），再补本地未同步项
        for (const h of [...remoteHist, ...local]) {
          const key = `${h.sql}|${h.dbName}|${h.env || ''}|${h.instanceId || ''}`
          if (!map.has(key)) map.set(key, h)
        }
        const next = Array.from(map.values()).sort((a, b) => b.ts - a.ts).slice(0, 100)
        saveJSON(histStoreKey, next)
        return next
      })
    } catch {
      /* ignore */
    }
  }, [databases, instMap, histStoreKey])

  useEffect(() => {
    // 合并服务端收藏 / 历史，保留本地数据，不破坏现有能力
    ;(async () => {
      try {
        const { data: favRes } = await api.get('/query/favorites').catch(() => ({ data: { data: [] } }))
        const remoteFavs: FavItem[] = (favRes?.data || []).map((f: any) => ({
          id: f.id,
          name: f.name || f.db_name || '未命名',
          dbId: f.database_id || '',
          dbName: f.db_name || '',
          env: f.env || '',
          sql: f.sql || '',
          ts: Date.parse(f.updated_at || f.created_at) || Date.now(),
        }))
        if (remoteFavs.length) {
          setFavorites((local) => {
            const map = new Map<string, FavItem>()
            for (const f of [...remoteFavs, ...local]) map.set(f.id, f)
            const next = Array.from(map.values()).sort((a, b) => b.ts - a.ts)
            saveJSON(favStoreKey, next)
            return next
          })
        }
      } catch {
        /* ignore */
      }
      await mergeRemoteHistory()
    })()
  }, [mergeRemoteHistory, favStoreKey])

  useEffect(() => {
    if (storeTab === 'hist') void mergeRemoteHistory()
  }, [storeTab, mergeRemoteHistory])

  const envPeers = useMemo(() => {
    if (!current) return []
    const rows = databases.filter((d) => d.db_name === current.dbName)
    rows.sort((a, b) => {
      const ia = instMap[a.instance_id]
      const ib = instMap[b.instance_id]
      const ea = resolveDbEnv(a, ia)
      const eb = resolveDbEnv(b, ib)
      if (!ea && eb) return 1
      if (ea && !eb) return -1
      const byEnv = cmpName(ea, eb)
      if (byEnv) return byEnv
      return cmpName(peerMark(a, ia), peerMark(b, ib))
    })
    return rows
  }, [current, databases, instMap])

  const currentEnvShown = useMemo(() => {
    if (!current) return null
    const db = databases.find((d) => d.id === current.dbId) || {
      id: current.dbId,
      db_name: current.dbName,
      instance_id: current.instanceId,
      tags: current.env ? [`idc=${current.env}`] : [],
    }
    return peerEnvLabel(db, instMap[current.instanceId], envPeers, (row) => instMap[row.instance_id])
  }, [current, databases, envPeers, instMap])

  useEffect(() => {
    setEnvPickOpen(false)
  }, [activeKey])

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
          favId: t.favId || '',
          alias: t.alias || '',
          executedSql: t.executedSql || '',
          error: t.error || '',
          result: t.result && (t.result.rows?.length || 0) <= 200 ? t.result : null,
        })),
      }
      try {
        sessionStorage.setItem(workspaceStoreKey, JSON.stringify(payload))
      } catch {
        payload.tabs = payload.tabs.map((t) => ({ ...t, result: null }))
        try { sessionStorage.setItem(workspaceStoreKey, JSON.stringify(payload)) } catch { /* ignore */ }
      }
    } catch { /* ignore */ }
  }, [tabs, activeKey, leftWidth, editorHeight, histWidth, workspaceStoreKey])

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

  function newTab(db: any, opt: { sql?: string; table?: string; favId?: string } = {}): TabItem {
    const inst = instMap[db.instance_id] || {}
    return {
      key: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      dbId: db.id,
      dbName: db.db_name,
      dbType: inst.db_type || '',
      env: resolveDbEnv(db, inst),
      instanceId: db.instance_id,
      tag: dbTag(db),
      table: opt.table || '',
      sql: opt.sql || '',
      ts: Date.now(),
      favId: opt.favId || '',
      result: null,
      error: '',
    }
  }

  function openDB(db: any, opt: { sql?: string; table?: string; run?: boolean; favId?: string } = {}): TabItem {
    const exist = tabs.find((t) => t.dbId === db.id && !t.table)
    if (exist && !opt.table && opt.sql === undefined) {
      setActiveKey(exist.key)
      return exist
    }
    const sql = opt.sql ?? (opt.table
      ? `SELECT * FROM ${quoteTable(opt.table, instMap[db.instance_id]?.db_type)} LIMIT 50;`
      : '')
    const tab = newTab(db, { sql, table: opt.table, favId: opt.favId })
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

  function switchEnv(db: any) {
    if (!db || !current || db.id === current.dbId) return
    const exist = tabs.find((t) => t.dbId === db.id && !t.table && t.key !== current.key)
    if (exist) {
      setActiveKey(exist.key)
      return
    }
    const inst = instMap[db.instance_id] || {}
    updateCurrent({
      dbId: db.id,
      dbName: db.db_name,
      instanceId: db.instance_id,
      dbType: inst.db_type || current.dbType,
      env: resolveDbEnv(db, inst),
      tag: dbTag(db),
      result: null,
      results: undefined,
      resultIdx: 0,
      error: '',
    })
  }

  function cycleEnv() {
    if (!current || envPeers.length < 2) return
    const idx = envPeers.findIndex((d) => d.id === current.dbId)
    const next = envPeers[(idx + 1) % envPeers.length]
    switchEnv(next)
  }

  function sqlUndoBucket(key: string) {
    const all = sqlUndoRef.current
    if (!all[key]) all[key] = { undo: [], redo: [] }
    return all[key]
  }

  function noteSqlEdit(key: string, snap: SqlSnap) {
    if (!key) return
    const bucket = sqlUndoBucket(key)
    const top = bucket.undo[bucket.undo.length - 1]
    if (top && top.sql === snap.sql && top.cursor === snap.cursor) return
    bucket.undo.push(snap)
    if (bucket.undo.length > 100) bucket.undo.shift()
    bucket.redo = []
  }

  function noteSqlBeforeInput(e: React.FormEvent<HTMLTextAreaElement>) {
    if (!current) return
    const el = e.currentTarget
    noteSqlEdit(current.key, { sql: el.value, cursor: el.selectionStart ?? 0 })
  }

  function placeSqlCursor(cursor: number, sql: string) {
    placeSqlRange(cursor, cursor, sql)
  }

  function placeSqlRange(start: number, end: number, sql: string) {
    setTimeout(() => {
      const el = sqlBoxRef.current
      if (!el) return
      const a = Math.max(0, Math.min(start, sql.length))
      const b = Math.max(0, Math.min(end, sql.length))
      el.focus()
      el.setSelectionRange(a, b)
    }, 0)
  }

  function undoSql() {
    if (!current) return
    const bucket = sqlUndoBucket(current.key)
    const prev = bucket.undo.pop()
    if (!prev) return
    const el = sqlBoxRef.current
    bucket.redo.push({
      sql: current.sql || '',
      cursor: el ? el.selectionStart ?? (current.sql || '').length : (current.sql || '').length,
    })
    if (bucket.redo.length > 100) bucket.redo.shift()
    suggest.close()
    updateCurrent({ sql: prev.sql })
    placeSqlCursor(prev.cursor, prev.sql)
  }

  function redoSql() {
    if (!current) return
    const bucket = sqlUndoBucket(current.key)
    const next = bucket.redo.pop()
    if (!next) return
    const el = sqlBoxRef.current
    bucket.undo.push({
      sql: current.sql || '',
      cursor: el ? el.selectionStart ?? (current.sql || '').length : (current.sql || '').length,
    })
    if (bucket.undo.length > 100) bucket.undo.shift()
    suggest.close()
    updateCurrent({ sql: next.sql })
    placeSqlCursor(next.cursor, next.sql)
  }

  function formatSQL() {
    if (!current) return
    const result = formatSqlContent(String(current.sql || ''))
    if (!result.ok) {
      message.warning(result.error || 'SQL 美化失败')
      return
    }
    if (result.unchanged) {
      message.info('SQL 已是规范格式')
      return
    }
    noteSqlEdit(current.key, { sql: current.sql || '', cursor: sqlBoxRef.current?.selectionStart ?? 0 })
    updateCurrent({ sql: result.text })
  }

  function onSqlKey(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (suggest.onKeyDown(e)) return
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'z' && !e.nativeEvent.isComposing) {
      e.preventDefault()
      if (e.shiftKey) redoSql()
      else undoSql()
      return
    }
    const el = e.currentTarget
    const plain = !e.metaKey && !e.shiftKey && !e.altKey && !e.nativeEvent.isComposing
    if (plain && e.ctrlKey && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      el.setSelectionRange(0, el.value.length)
      return
    }
    if (plain && !e.ctrlKey && e.key === 'Escape' && el.selectionStart !== el.selectionEnd) {
      e.preventDefault()
      const end = el.selectionEnd
      el.setSelectionRange(end, end)
      return
    }
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.nativeEvent.isComposing && (e.key === '/' || e.key === '?')) {
      e.preventDefault()
      applyLineComment(e.shiftKey || e.key === '?' ? 'uncomment' : 'comment')
      return
    }
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      suggest.close()
      runSQL()
    }
  }

  function applyLineComment(mode: SqlCommentMode) {
    if (!current) return
    const el = sqlBoxRef.current
    const sql = el?.value ?? current.sql ?? ''
    const start = el?.selectionStart ?? sql.length
    const end = el?.selectionEnd ?? start
    const next = applySqlLineComment(sql, start, end, mode)
    if (next.text === sql) {
      placeSqlRange(next.start, next.end, sql)
      return
    }
    noteSqlEdit(current.key, { sql, cursor: start })
    suggest.close()
    updateCurrent({ sql: next.text })
    placeSqlRange(next.start, next.end, next.text)
  }

  function pushHistory(tab: TabItem) {
    setHistory((list) => {
      const next = [
        { id: `${Date.now()}`, dbId: tab.dbId, dbName: tab.dbName, sql: tab.sql, ts: Date.now() },
        ...list.filter((h) => h.sql !== tab.sql || h.dbId !== tab.dbId),
      ].slice(0, 100)
      saveJSON(histStoreKey, next)
      return next
    })
  }

  function stopQuery() {
    queryAbortRef.current?.abort()
  }

  function liveSelection(sql: string): { start: number; end: number; text: string } | null {
    const el = sqlBoxRef.current
    if (!el || document.activeElement !== el || el.value !== sql || el.selectionEnd <= el.selectionStart) return null
    const text = sql.slice(el.selectionStart, el.selectionEnd)
    if (!text.trim()) return null
    return { start: el.selectionStart, end: el.selectionEnd, text }
  }

  function rememberSelectionRestore(picked: { start: number; end: number }, sql: string) {
    selectionRestoreRef.current = { start: picked.start, end: picked.end, sql }
    setSelectionEpoch((n) => n + 1)
  }

  function armCollapseSelection() {
    clickSelectionRef.current = null
    selectionRestoreRef.current = null
    collapseSelectionRef.current = true
    setSelectionEpoch((n) => n + 1)
  }

  async function runSQL(tab: TabItem | null = current, opt: { onlySelection?: boolean; fromClick?: boolean } = {}) {
    const fromClick = !!opt.fromClick
    const clicked = fromClick ? clickSelectionRef.current : null
    if (fromClick) clickSelectionRef.current = null
    if (!tab?.sql?.trim()) {
      message.warning('请先输入 SQL')
      return
    }
    if (!tab.dbId) {
      message.warning('请先选择数据库')
      return
    }
    if (opt.onlySelection && running) return
    const pickedNow = fromClick
      ? (clicked && clicked.key === tab.key && clicked.sql === tab.sql
        ? { start: clicked.start, end: clicked.end, text: clicked.text }
        : null)
      : liveSelection(tab.sql)
    if (opt.onlySelection && !pickedNow) {
      message.warning('请先选中要执行的 SQL')
      return
    }
    const picked = (opt.onlySelection || (statementCount(tab.sql) > 1 && pickedNow)) ? pickedNow : null
    const sql = (picked ? picked.text : tab.sql).trim()
    if (!sql) {
      message.warning('请先输入 SQL')
      return
    }
    if (picked) rememberSelectionRestore(picked, tab.sql)
    else selectionRestoreRef.current = null
    queryAbortRef.current?.abort()
    const ac = new AbortController()
    queryAbortRef.current = ac
    const clientRequestID = newClientRequestID()
    const started = Date.now()
    setRunning(true)
    if (picked) setSelectionEpoch((n) => n + 1)
    setTabs((list) => list.map((t) => (t.key === tab.key ? { ...t, error: '' } : t)))
    try {
      const { data } = await api.post(
        '/query',
        { database_id: tab.dbId, sql, client_request_id: clientRequestID },
        { signal: ac.signal, timeout: 90000 },
      )
      if (data?.canceled) {
        message.info(data.message || '查询已终止')
        return
      }
      const result = data.data || { columns: [], rows: [] }
      const results: QueryResult[] = Array.isArray(data.results) && data.results.length
        ? data.results
        : [result]
      setTabs((list) => list.map((t) => (
        t.key === tab.key ? { ...t, result: results[0], results, resultIdx: 0, error: '', executedSql: sql, resultKeyword: '', resultPage: 1 } : t
      )))
      pushHistory({ ...tab, sql })
      void mergeRemoteHistory()
    } catch (err: any) {
      if (axios.isCancel(err) || err?.code === 'ERR_CANCELED' || err?.name === 'CanceledError') {
        message.info('查询已终止')
        return
      }
      if (err?.code === 'ECONNABORTED') {
        api.post(
          '/query/client-timeout',
          { client_request_id: clientRequestID, elapsed_ms: Date.now() - started },
          { timeout: 8000, silent: true },
        ).catch(() => { /* 补记失败不影响页面 */ })
      }
      const errMsg = err.response?.data?.message || err.message || '查询失败'
      setTabs((list) => list.map((t) => (t.key === tab.key ? { ...t, result: null, results: undefined, error: errMsg, resultKeyword: '', resultPage: 1 } : t)))
    } finally {
      if (queryAbortRef.current === ac) queryAbortRef.current = null
      setRunning(false)
      if (picked) setSelectionEpoch((n) => n + 1)
    }
  }

  function filteredRawRows(): any[][] {
    const rows = activeResult?.rows || []
    const q = resultKeyword.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((row) => (row || []).some((cell) => cellText(cell).toLowerCase().includes(q)))
  }

  async function downloadDirectExport() {
    const tab = current
    const columns = activeResult?.columns || []
    if (!tab?.dbId || !columns.length) {
      message.warning('请先运行查询')
      return
    }
    if (exportEncrypt && !exportPassword.trim()) {
      message.warning('请填写压缩密码')
      return
    }
    const rows = filteredRawRows()
    const table = tableNameFromSQL(execSQLText || tab.sql || '')
    const content = buildExportContent(exportFormat, columns, rows, table)
    const stamp = new Date().toISOString().replace(/[-:]/g, '').slice(0, 15)
    const base = `${table}_${stamp}`
    setExporting(true)
    try {
      if (exportEncrypt) {
        await downloadPasswordZip(`${base}.zip`, `${base}.${exportFormat}`, content, exportPassword.trim())
      } else {
        downloadTextFile(`${base}.${exportFormat}`, content)
      }
      api.post('/query/export-record', {
        database_id: tab.dbId,
        format: exportFormat,
        row_count: rows.length,
        encrypted: exportEncrypt,
      }).catch(() => undefined)
      message.success('已导出')
      setDirectExportOpen(false)
    } catch (e: any) {
      message.error(e?.message || '导出失败')
    } finally {
      setExporting(false)
    }
  }

  async function exportDQL() {
    const tab = current
    if (!tab?.dbId || !tab.sql?.trim()) {
      message.warning('请先选择数据库并填写 SQL')
      return
    }
    try {
      const { data } = await api.get('/export-whitelist/check', { params: { database_id: tab.dbId } })
      if (data?.allowed && (activeResult?.rows || []).length) {
        setExportFormat('csv')
        setExportEncrypt(false)
        setExportPassword('')
        setDirectExportOpen(true)
        return
      }
    } catch {
      /* 判断失败时走导出工单 */
    }
    const exportSql = tab.result && tab.executedSql?.trim() ? tab.executedSql : tab.sql
    Modal.confirm({
      title: '数据导出',
      content: '导出表数据需发起导出数据工单，是否继续？',
      okText: '发起导出数据工单',
      cancelText: '取消',
      onOk: () => {
        // row_count 仅审批参考：取 SQL 中 LIMIT，勿用查询预览行数（常被 query_default_limit=1000 截断）
        const limMatch = String(exportSql || '').match(/\blimit\s+(\d+)(?:\s*,\s*(\d+))?/i)
        const sqlLimit = limMatch
          ? Number(limMatch[2] != null ? limMatch[2] : limMatch[1])
          : 0
        sessionStorage.setItem(DQL_DRAFT_KEY, JSON.stringify({
          database_id: tab.dbId,
          instance_id: tab.instanceId,
          db_name: tab.dbName,
          env: tab.env,
          db_type: tab.dbType,
          sql: exportSql,
          row_count: Number.isFinite(sqlLimit) && sqlLimit > 0 ? sqlLimit : 0,
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
    setFavMode('create')
    setFavName('')
    setFavSql(current.sql)
    setFavVisible(true)
  }

  function openSaveAs() {
    if (!current?.sql?.trim()) {
      message.warning('请先输入 SQL')
      return
    }
    setFavMode('saveas')
    setFavName('')
    setFavSql(current.sql)
    setFavVisible(true)
  }

  async function writeFavorite(name: string, sql: string) {
    const tab = current
    if (!tab) return
    const trimmed = name.trim()
    if (!trimmed) {
      message.warning('请输入名称')
      return
    }
    const sqlText = sql.trim()
    if (!sqlText) {
      message.warning('请先输入 SQL')
      return
    }
    if (favMode === 'saveas') {
      const bound = favorites.find((f) => f.id === tab.favId)
      if (bound && bound.name.trim() === trimmed) {
        message.warning('请换一个名称')
        return
      }
    }
    const body = {
      name: trimmed,
      database_id: tab.dbId,
      db_name: tab.dbName,
      env: tab.env || '',
      sql: sqlText,
    }
    const same = favorites.filter((f) => f.name.trim() === trimmed)
    const now = Date.now()
    try {
      if (same.length) {
        const keep = same[0]
        await api.put(`/query/favorites/${keep.id}`, body)
        await Promise.all(same.slice(1).map((f) => api.delete(`/query/favorites/${f.id}`).catch(() => undefined)))
        const item: FavItem = { id: keep.id, name: trimmed, dbId: tab.dbId, dbName: tab.dbName, env: tab.env || '', sql: sqlText, ts: now }
        const drop = new Set(same.map((f) => f.id))
        setFavorites((list) => {
          const next = [item, ...list.filter((f) => !drop.has(f.id))]
          saveJSON(favStoreKey, next)
          return next
        })
        updateCurrent({ favId: keep.id })
        message.success('已更新收藏')
      } else {
        let id = `${now}`
        const { data } = await api.post('/query/favorites', body)
        if (data?.data?.id) id = data.data.id
        const item: FavItem = { id, name: trimmed, dbId: tab.dbId, dbName: tab.dbName, env: tab.env || '', sql: sqlText, ts: now }
        setFavorites((list) => {
          const next = [item, ...list]
          saveJSON(favStoreKey, next)
          return next
        })
        updateCurrent({ favId: id })
        message.success('已收藏')
      }
      setFavVisible(false)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '保存失败')
    }
  }

  async function updateBoundFavorite() {
    const tab = current
    if (!tab?.favId) {
      message.warning('请先点「收藏」起名')
      return
    }
    const fav = favorites.find((f) => f.id === tab.favId)
    if (!fav) {
      message.warning('请先点「收藏」起名')
      updateCurrent({ favId: '' })
      return
    }
    const sqlText = String(tab.sql || '').trim()
    if (!sqlText) {
      message.warning('请先输入 SQL')
      return
    }
    try {
      await api.put(`/query/favorites/${fav.id}`, {
        name: fav.name,
        database_id: tab.dbId,
        db_name: tab.dbName,
        env: tab.env || '',
        sql: sqlText,
      })
      setFavorites((list) => {
        const next = list.map((f) => (f.id === fav.id
          ? { ...f, dbId: tab.dbId, dbName: tab.dbName, env: tab.env || '', sql: sqlText, ts: Date.now() }
          : f))
        saveJSON(favStoreKey, next)
        return next
      })
      message.success('已更新收藏')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '更新失败')
    }
  }

  function saveFavorite() {
    void writeFavorite(favName, favSql)
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const meta = e.metaKey || e.ctrlKey
      if (!meta) return
      if (e.key === 'Enter' && e.shiftKey) {
        if (favVisible) return
        const target = e.target as HTMLElement | null
        const inField = !!target && target !== sqlBoxRef.current && (
          target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable
        )
        if (inField) return
        e.preventDefault()
        if (running) return
        void runSQL(current, { onlySelection: true })
        return
      }
      if (e.key.toLowerCase() !== 's' || e.shiftKey || e.altKey) return
      if (favVisible) return
      const bound = current?.favId
        ? favorites.find((f) => f.id === current.favId)
        : undefined
      if (!bound) return
      e.preventDefault()
      void updateBoundFavorite()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [favVisible, current, favorites, running])

  function removeFav(id: string) {
    api.delete(`/query/favorites/${id}`).catch(() => { /* ignore */ })
    setFavorites((list) => {
      const next = list.filter((f) => f.id !== id)
      saveJSON(favStoreKey, next)
      return next
    })
  }

  function openOrSwitchDB(db: any, sql: string, favId = '') {
    armCollapseSelection()
    const exist = tabs.find((t) => t.dbId === db.id && !t.table)
    const cleared = {
      sql,
      favId,
      result: null as TabItem['result'],
      results: undefined as TabItem['results'],
      resultIdx: 0,
      error: '',
      executedSql: '',
    }
    if (exist) {
      setTabs((list) => list.map((t) => (t.key === exist.key ? { ...t, ...cleared } : t)))
      setActiveKey(exist.key)
      return
    }
    openDB(db, { sql, favId })
  }

  function markStoreMiss(text: string) {
    const key = current?.key || tabs[0]?.key
    if (!key) {
      message.warning(text)
      return
    }
    if (!current?.key) setActiveKey(key)
    setTabs((list) => list.map((t) => (t.key === key
      ? { ...t, result: null, results: undefined, resultIdx: 0, error: text, executedSql: '' }
      : t)))
  }

  function applyStore(item: FavItem | HistItem, source: 'fav' | 'hist') {
    if (!catalogReady) {
      pendingStoreRef.current = { item, source }
      return
    }
    const envOf = (db: any) => resolveDbEnv(db, instMap[db.instance_id])
    if (item.dbId) {
      const byId = databases.find((d) => d.id === item.dbId)
      if (byId) {
        openOrSwitchDB(byId, item.sql, source === 'fav' ? item.id : '')
        return
      }
    }
    const name = String(item.dbName || '').trim()
    if (!name) {
      markStoreMiss('未找到库')
      return
    }
    const same = databases.filter((d) => d.db_name === name)
    if (!same.length) {
      markStoreMiss(`未找到库 ${name}`)
      return
    }
    const env = String(item.env || '').trim()
    let pool = same
    if (env) {
      pool = same.filter((d) => envOf(d) === env)
      if (!pool.length) {
        markStoreMiss(`未找到库 ${name}`)
        return
      }
    } else if (source === 'hist') {
      const envs = new Set(same.map((d) => envOf(d)))
      if (envs.size > 1) {
        markStoreMiss(`库 ${name} 存在多个环境，无法确定要打开哪一个`)
        return
      }
    } else if (current?.env) {
      const prefer = same.filter((d) => envOf(d) === current.env)
      if (prefer.length) pool = prefer
    }
    const instanceId = 'instanceId' in item ? item.instanceId : ''
    if (instanceId) {
      const byInst = pool.filter((d) => d.instance_id === instanceId)
      if (byInst.length) pool = byInst
    }
    if (pool.length > 1) message.info('同环境有多条同名库，已打开第一条')
    openOrSwitchDB(pool[0], item.sql, source === 'fav' ? item.id : '')
  }

  useEffect(() => {
    if (!catalogReady) return
    const pending = pendingStoreRef.current
    if (!pending) return
    pendingStoreRef.current = null
    applyStore(pending.item, pending.source)
  }, [catalogReady])

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
    const hit = readQueryTreeCache(tenantId)
    if (hit) {
      setDatabases(hit.databases)
      setInstances(hit.instances)
      setEnvOptions(hit.envOptions)
      setCatalogReady(true)
    } else {
      setCatalogReady(false)
      setTreeLoading(true)
    }
    try {
      const entry = hit
        ? await refreshQueryCatalog(tenantId)
        : await prefetchQueryCatalog(tenantId)
      if (entry) {
        setDatabases(entry.databases)
        setInstances(entry.instances)
        setEnvOptions(entry.envOptions)
      }
    } finally {
      setTreeLoading(false)
      setCatalogReady(true)
    }
  }, [tenantId])

  useEffect(() => {
    loadTree()
    return () => { if (dragRef.current) dragRef.current() }
  }, [loadTree])

  function moveQueryTab(fromKey: string, toKey: string) {
    setTabs((list) => reorderQueryTabs(list, fromKey, toKey))
  }

  function onTabPointerDown(e: React.PointerEvent<HTMLElement>, key: string) {
    if (e.button !== 0 || renamingKey === key) return
    if ((e.target as HTMLElement).closest('input, textarea')) return
    tabPointerRef.current = { key, x: e.clientX, y: e.clientY, dragging: false, pointerId: e.pointerId }
  }

  function onTabPointerMove(e: React.PointerEvent<HTMLElement>) {
    const p = tabPointerRef.current
    if (!p || p.pointerId !== e.pointerId) return
    const dist = Math.hypot(e.clientX - p.x, e.clientY - p.y)
    if (!p.dragging && dist <= 4) return
    if (!p.dragging) {
      p.dragging = true
      tabDraggedRef.current = true
      setDraggingKey(p.key)
      try { e.currentTarget.setPointerCapture(e.pointerId) } catch { /* 已经松开时忽略 */ }
    }
    const over = queryTabKeyAt(e.clientX, e.clientY)
    const next = over && over !== p.key ? over : null
    setDragOverKey((cur) => (cur === next ? cur : next))
  }

  function onTabPointerUp(e: React.PointerEvent<HTMLElement>) {
    const p = tabPointerRef.current
    if (!p || p.pointerId !== e.pointerId) return
    tabPointerRef.current = null
    if (p.dragging) {
      const over = queryTabKeyAt(e.clientX, e.clientY)
      if (over && over !== p.key) moveQueryTab(p.key, over)
      setDragOverKey(null)
      setDraggingKey(null)
      window.setTimeout(() => { tabDraggedRef.current = false }, 0)
    }
  }

  function tabLabelOf(t: TabItem): { text: string; showTag: boolean } {
    const fav = t.favId ? favorites.find((f) => f.id === t.favId) : undefined
    if (fav?.name) return { text: fav.name, showTag: false }
    const alias = String(t.alias || '').trim()
    if (alias) return { text: alias, showTag: false }
    return { text: t.dbName, showTag: !!t.tag }
  }

  function beginRename(t: TabItem) {
    const label = tabLabelOf(t)
    setRenamingKey(t.key)
    setRenameDraft(label.text)
  }

  async function commitRename(t: TabItem) {
    if (renameCancelRef.current) {
      renameCancelRef.current = false
      return
    }
    if (renameLockRef.current || renamingKey !== t.key) return
    renameLockRef.current = true
    const name = renameDraft.trim()
    const fav = t.favId ? favorites.find((f) => f.id === t.favId) : undefined
    setRenamingKey(null)
    try {
      if (fav) {
        if (!name || name === fav.name) return
        await api.put(`/query/favorites/${fav.id}`, {
          name,
          database_id: t.dbId,
          db_name: t.dbName,
          env: t.env || '',
          sql: fav.sql,
        })
        setFavorites((list) => {
          const next = list.map((f) => (f.id === fav.id ? { ...f, name, ts: Date.now() } : f))
          saveJSON(favStoreKey, next)
          return next
        })
        return
      }
      setTabs((list) => list.map((tab) => (tab.key === t.key ? { ...tab, alias: name } : tab)))
    } catch (e: any) {
      message.error(e?.response?.data?.message || '改名失败')
    } finally {
      renameLockRef.current = false
    }
  }

  suggestEnv.current = {
    sql: current?.sql || '',
    dbId: current?.dbId || '',
    dbType: current?.dbType || '',
    tableNames: current?.dbId ? tablesMap[current.dbId] || [] : [],
    ensureTables: () => {
      if (current?.dbId) void fetchTables(current.dbId)
    },
    apply: (sql, cursor) => {
      if (current) {
        const el = sqlBoxRef.current
        noteSqlEdit(current.key, { sql: el?.value ?? current.sql ?? '', cursor: el?.selectionStart ?? 0 })
      }
      updateCurrent({ sql })
      setTimeout(() => {
        const el = sqlBoxRef.current
        if (!el) return
        el.focus()
        el.setSelectionRange(cursor, cursor)
      }, 0)
    },
  }

  const hasTreeData = treeData.length > 0
  const boundFav = current?.favId
    ? favorites.find((f) => f.id === current.favId)
    : undefined

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
                selectedKeys={selectedKeys}
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
                  items={tabs.map((t) => {
                    const label = tabLabelOf(t)
                    const renaming = renamingKey === t.key
                    return {
                      key: t.key,
                      label: (
                        <span
                          className={`tab-lab${dragOverKey === t.key ? ' drag-over' : ''}${renaming ? ' is-renaming' : ''}${draggingKey === t.key ? ' is-dragging' : ''}`}
                          data-tab-key={t.key}
                          onPointerDown={(ev) => onTabPointerDown(ev, t.key)}
                          onPointerMove={onTabPointerMove}
                          onPointerUp={onTabPointerUp}
                          onPointerCancel={onTabPointerUp}
                          onClick={(ev) => {
                            if (tabDraggedRef.current) {
                              ev.stopPropagation()
                              ev.preventDefault()
                              tabDraggedRef.current = false
                              lastTabClickRef.current = null
                              return
                            }
                            if (renaming || (ev.target as HTMLElement).closest('input, textarea')) return
                            const now = Date.now()
                            const prev = lastTabClickRef.current
                            if (prev && prev.key === t.key && now - prev.at <= 500) {
                              lastTabClickRef.current = null
                              ev.preventDefault()
                              ev.stopPropagation()
                              beginRename(t)
                              return
                            }
                            lastTabClickRef.current = { key: t.key, at: now }
                          }}
                          onDoubleClick={(ev) => {
                            if (renaming || tabDraggedRef.current) return
                            if ((ev.target as HTMLElement).closest('input, textarea')) return
                            ev.preventDefault()
                            ev.stopPropagation()
                            beginRename(t)
                          }}
                        >
                          <DbTypeIcon type={t.dbType} size={14} />
                          {renaming ? (
                            <Input
                              className="tab-rename-input"
                              size="small"
                              autoFocus
                              value={renameDraft}
                              onChange={(ev) => setRenameDraft(ev.target.value)}
                              onMouseDown={(ev) => ev.stopPropagation()}
                              onClick={(ev) => ev.stopPropagation()}
                              onDoubleClick={(ev) => ev.stopPropagation()}
                              onKeyDown={(ev) => {
                                ev.stopPropagation()
                                if (ev.key === 'Enter') {
                                  ev.preventDefault()
                                  void commitRename(t)
                                } else if (ev.key === 'Escape') {
                                  ev.preventDefault()
                                  renameCancelRef.current = true
                                  setRenamingKey(null)
                                }
                              }}
                              onBlur={() => { void commitRename(t) }}
                            />
                          ) : (
                            <span className="tab-name">{label.text}</span>
                          )}
                          {label.showTag && t.tag ? <span className="tab-tag">[{t.tag}]</span> : null}
                        </span>
                      ),
                    }
                  })}
                />
                <Button className="tab-add" type="text" onClick={addTab}>+</Button>
              </div>
              <div className="editor-split" style={{ height: editorHeight }}>
                <div className="editor-col">
                  <div className="tool-bar">
                    <div className="tool-left">
                      <Tooltip title={`${/Mac/i.test(navigator.userAgent) ? '⌘' : 'Ctrl'}+Enter：有多条且选中了其中一段时只执行选中，否则执行全部。${/Mac/i.test(navigator.userAgent) ? '⌘' : 'Ctrl'}+Shift+Enter 只执行选中`}>
                        <span
                          onPointerDown={(e) => {
                            runPointerDownRef.current = true
                            const tab = current
                            const sel = tab ? liveSelection(tab.sql || '') : null
                            clickSelectionRef.current = tab && sel
                              ? { ...sel, sql: tab.sql || '', key: tab.key }
                              : null
                            const span = e.currentTarget
                            const up = (ev: PointerEvent) => {
                              window.removeEventListener('pointerup', up)
                              if (!span.contains(ev.target as Node)) {
                                runPointerDownRef.current = false
                                clickSelectionRef.current = null
                              }
                            }
                            window.addEventListener('pointerup', up)
                          }}
                        >
                          <Button
                            type="primary"
                            loading={running}
                            icon={<CaretRightOutlined />}
                            onClick={() => {
                              const fromClick = runPointerDownRef.current
                              runPointerDownRef.current = false
                              runSQL(current, { fromClick })
                            }}
                          >
                            运行
                          </Button>
                        </span>
                      </Tooltip>
                      <Button
                        danger
                        disabled={!running}
                        icon={<StopOutlined />}
                        onClick={stopQuery}
                      >
                        终止
                      </Button>
                      <Button onClick={formatSQL}>格式化</Button>
                    </div>
                    <Space>
                      {boundFav ? (
                        <>
                          <Button onClick={() => void updateBoundFavorite()}>更新收藏 ({/Mac/i.test(navigator.userAgent) ? '⌘+S' : 'Ctrl+S'})</Button>
                          <Button onClick={openSaveAs}>另存为</Button>
                          <span className="fav-bound-name" title={boundFav.name}>
                            <StarOutlined />
                            <span>{boundFav.name}</span>
                          </span>
                        </>
                      ) : (
                        <Button icon={<StarOutlined />} onClick={openFavorite}>收藏</Button>
                      )}
                    </Space>
                  </div>
                  <div className="db-meta">
                    {envPeers.length > 1 ? (
                      <Popover
                        open={envPickOpen}
                        onOpenChange={setEnvPickOpen}
                        trigger="click"
                        placement="bottomLeft"
                        content={(
                          <div className="env-pick">
                            {envPeers.map((d) => {
                              const shown = peerEnvLabel(d, instMap[d.instance_id], envPeers, (row) => instMap[row.instance_id])
                              const on = d.id === current?.dbId
                              return (
                                <Tag
                                  key={d.id}
                                  color={envTagColor(shown.env)}
                                  title={shown.title}
                                  className={on ? 'env-pick-item on' : 'env-pick-item'}
                                  onClick={() => {
                                    switchEnv(d)
                                    setEnvPickOpen(false)
                                  }}
                                >
                                  {shown.label}
                                </Tag>
                              )
                            })}
                          </div>
                        )}
                      >
                        <Tag color={envTagColor(current?.env)} className="env-tag-btn" title={currentEnvShown?.title}>
                          {currentEnvShown?.label || current?.env || '—'}
                        </Tag>
                      </Popover>
                    ) : (
                      <Tag color={envTagColor(current?.env)}>{current?.env || '—'}</Tag>
                    )}
                    {envPeers.length > 1 ? (
                      <span className="env-swap" onClick={cycleEnv} title="切换到下一个环境">⇄</span>
                    ) : null}
                    <DbTypeIcon type={current?.dbType} size={16} />
                    <span className="db-name">
                      {current?.dbName}
                      {current?.tag ? <span>({current.tag})</span> : null}
                    </span>
                  </div>
                  <SqlEditor
                    ref={sqlBoxRef}
                    value={current?.sql || ''}
                    className="sql-box-hl"
                    spellCheck={false}
                    minRows={8}
                    placeholder="输入 SQL，Ctrl / ⌘ + Enter 运行；有多条且选中一段时只执行选中。Ctrl+A 全选，Esc 取消选中。Ctrl / ⌘ + / 注释，Ctrl / ⌘ + Shift + / 取消注释"
                    onChange={(v) => {
                      suggestEnv.current.sql = v
                      updateCurrent({ sql: v })
                      suggest.onInput()
                    }}
                    onKeyDown={onSqlKey}
                    onBeforeInput={noteSqlBeforeInput}
                    onKeyUp={(e) => {
                      if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) suggest.onInput()
                    }}
                    onClick={() => suggest.onInput()}
                    onScroll={() => suggest.onScroll()}
                  />
                  {suggest.open ? createPortal(
                    <div className="sql-suggest" style={{ top: suggest.pos.top, left: suggest.pos.left }}>
                      {suggest.loading ? <div className="sql-suggest-loading">读取字段</div> : null}
                      {suggest.items.map((item, i) => (
                        <div
                          key={item.group + item.insert + i}
                          className={i === suggest.active ? 'sql-suggest-item on' : 'sql-suggest-item'}
                          onMouseDown={(e) => {
                            e.preventDefault()
                            suggest.accept(i)
                          }}
                          onMouseEnter={() => suggest.setActive(i)}
                        >
                          <span>{item.label}</span>
                          {item.detail ? <span className="sql-suggest-detail">{item.detail}</span> : null}
                        </div>
                      ))}
                    </div>,
                    document.body,
                  ) : null}
                </div>
                <div className="gutter-x inner" onMouseDown={(e) => startDrag('hist', e)} />
                <div className="hist-col" style={{ width: histWidth }}>
                  <Tabs
                    activeKey={storeTab}
                    onChange={(k) => setStoreTab(k)}
                    destroyInactiveTabPane
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
                                <div key={item.id} className="store-item" onClick={() => applyStore(item, 'fav')}>
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
                                  <div className="store-sub">{item.dbName}{item.env ? ` · ${item.env}` : ''} · {fmtTime(item.ts)}</div>
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
                              <div key={item.id} className="store-item" onClick={() => applyStore(item, 'hist')}>
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
                ) : activeResult ? (
                  <>
                    {(current?.results?.length || 0) > 1 ? (
                      <Tabs
                        size="small"
                        activeKey={String(current?.resultIdx || 0)}
                        onChange={(k) => updateCurrent({ resultIdx: Number(k) || 0, resultKeyword: '', resultPage: 1 })}
                        items={(current?.results || []).map((_, i) => ({
                          key: String(i),
                          label: `查询 #${i + 1}`,
                        }))}
                      />
                    ) : null}
                    <div className="result-toolbar-1">
                      <div className="result-exec">
                        <span className="result-label">执行SQL:</span>
                        <Input
                          size="small"
                          readOnly
                          className="result-sql-input"
                          value={execSQLText}
                          title={execSQLText}
                        />
                        <span className="result-label">执行时间:</span>
                        <span className="result-elapsed">{activeResult.elapsed_ms ?? 0}ms</span>
                      </div>
                      <div className="result-actions">
                        <Button size="small" icon={<TableOutlined />} onClick={() => void openTableInfo()}>
                          表信息
                        </Button>
                        <Button type="primary" size="small" icon={<DownloadOutlined />} onClick={() => exportDQL()}>
                          导出
                        </Button>
                      </div>
                    </div>
                    <div className="result-toolbar-2">
                      <Input
                        allowClear
                        size="small"
                        className="result-search"
                        placeholder="搜索"
                        prefix={<SearchOutlined />}
                        value={resultKeyword}
                        onChange={(e) => updateCurrent({ resultKeyword: e.target.value, resultPage: 1 })}
                      />
                      <div className="result-pagination">
                        <span className="result-count">
                          共 {resultRowsFiltered.length} 条记录【最大查询{activeResult.limit || activeResult.applied_limit || '—'}条】
                          {activeResult.notice ? `（${activeResult.notice}）` : activeResult.truncated ? '（已达上限）' : ''}
                        </span>
                        <Pagination
                          size="small"
                          current={resultPage}
                          pageSize={resultPageSize}
                          total={resultRowsFiltered.length}
                          showSizeChanger={{ getPopupContainer: () => document.body }}
                          showQuickJumper={false}
                          pageSizeOptions={['10', '20', '50', '100']}
                          onChange={(page, size) => {
                            updateCurrent({ resultPage: page })
                            if (size && size !== resultPageSize) {
                              setResultPageSize(size)
                              localStorage.setItem(PAGE_SIZE_KEY, String(size))
                            }
                          }}
                        />
                      </div>
                    </div>
                    <div className="result-table-wrap" ref={resultTableWrapRef}>
                      <ResponsiveTable
                        dataSource={resultRows}
                        rowKey="__k"
                        bordered
                        size="small"
                        pagination={false}
                        scroll={{ y: resultScrollY, x: 'max-content' }}
                        columns={[
                          {
                            title: '#',
                            key: '__rowno',
                            width: 56,
                            fixed: 'left' as const,
                            className: 'result-rowno-col',
                            // 仅展示用序号；导出走 result.columns，不会带上此列
                            render: (_: unknown, __: unknown, index: number) => {
                              const start = (Math.max(resultPage, 1) - 1) * resultPageSize
                              return <span className="result-rowno">{start + index + 1}</span>
                            },
                          },
                          ...(activeResult.columns || []).map((col, i) => ({
                            title: (
                              <div className="result-col-head">
                                <span className="result-col-title">{col}</span>
                                <span
                                  className="result-col-resize"
                                  onMouseDown={(e) => startColResize(e, i)}
                                />
                              </div>
                            ),
                            dataIndex: resultCellKey(i),
                            key: resultCellKey(i),
                            width: colWidths[i] || 120,
                            render: (value: string, _: unknown, index: number) => (
                              <ResultCell
                                text={String(value ?? '')}
                                keyword={resultKeyword}
                                onExpand={() => openCellDetail(index, i)}
                              />
                            ),
                          })),
                        ]}
                      />
                    </div>
                  </>
                ) : (
                  <div className="result-placeholder">运行查询后在此展示结果</div>
                )}
              </Spin>
            </>
          )}
        </div>
      </div>

      <Drawer
        title="详情"
        width={640}
        open={!!cellDetail}
        onClose={() => { setCellDetail(null); setCellPretty(false) }}
        destroyOnClose
      >
        {cellDetail ? (
          <CellDetailBody
            raw={String(resultRowsFiltered[cellDetail.row]?.[resultCellKey(cellDetail.col)] ?? '')}
            index={cellDetail.row}
            total={resultRowsFiltered.length}
            prettyOn={cellPretty}
            onPretty={setCellPretty}
            onPrev={() => {
              setCellPretty(false)
              setCellDetail((cur) => cur && cur.row > 0 ? { ...cur, row: cur.row - 1 } : cur)
            }}
            onNext={() => {
              setCellPretty(false)
              setCellDetail((cur) => cur && cur.row < resultRowsFiltered.length - 1 ? { ...cur, row: cur.row + 1 } : cur)
            }}
          />
        ) : null}
      </Drawer>

      <Drawer
        title="导出表数据"
        width={480}
        open={directExportOpen}
        onClose={() => setDirectExportOpen(false)}
        destroyOnClose
      >
        <div className="result-export-form">
          <div>
            <div className="result-export-label">格式</div>
            <Select
              style={{ width: '100%' }}
              value={exportFormat}
              onChange={(v) => setExportFormat(v)}
              options={[
                { value: 'csv', label: 'CSV' },
                { value: 'json', label: 'JSON' },
                { value: 'sql', label: 'SQL' },
              ]}
            />
          </div>
          <div className="result-export-row">
            <span>密码压缩</span>
            <Switch checked={exportEncrypt} onChange={setExportEncrypt} />
          </div>
          {exportEncrypt ? (
            <Input.Password
              placeholder="压缩密码"
              value={exportPassword}
              onChange={(e) => setExportPassword(e.target.value)}
            />
          ) : null}
          <div className="result-export-tip">导出当前查询结果{resultKeyword.trim() ? '中符合搜索条件的行' : ''}，共 {filteredRawRows().length} 行。</div>
          <Button type="primary" icon={<DownloadOutlined />} loading={exporting} onClick={() => void downloadDirectExport()}>
            下载
          </Button>
        </div>
      </Drawer>

      <Modal
        open={favVisible}
        title={favMode === 'saveas' ? '另存为' : '创建收藏'}
        width={640}
        onCancel={() => setFavVisible(false)}
        onOk={saveFavorite}
        okText="确定"
        cancelText="取消"
        destroyOnClose
      >
        <div style={{ marginBottom: 12 }}>
          <div style={{ marginBottom: 6 }}>SQL 语句</div>
          <Input.TextArea value={favSql} readOnly autoSize={{ minRows: 4, maxRows: 10 }} />
        </div>
        <div style={{ marginBottom: 12 }}>
          <div style={{ marginBottom: 6 }}><span style={{ color: '#ff4d4f' }}>* </span>名称</div>
          <Input value={favName} placeholder="请输入名称" onChange={(e) => setFavName(e.target.value)} />
        </div>
        <div>
          <div style={{ marginBottom: 6 }}>数据库名称</div>
          <Input value={current?.dbName || ''} disabled />
        </div>
      </Modal>

      <Modal
        open={tableInfoOpen}
        width={880}
        footer={null}
        onCancel={() => setTableInfoOpen(false)}
        destroyOnClose
        className="table-info-modal"
        title={
          <div className="table-info-header">
            <div className="table-info-title">
              <TableOutlined />
              <span>
                查看表结构
                {tableInfo?.table || resultTableName
                  ? ` (${tableInfo?.table || resultTableName})`
                  : ''}
              </span>
            </div>
            {tableInfoMode === 'list' ? (
              <Button
                type="primary"
                size="small"
                icon={<CodeOutlined />}
                disabled={!tableInfo?.ddl}
                onClick={() => setTableInfoMode('sql')}
              >
                查看SQL
              </Button>
            ) : (
              <div className="table-info-actions">
                <Button
                  type="primary"
                  size="small"
                  icon={<UnorderedListOutlined />}
                  onClick={() => setTableInfoMode('list')}
                >
                  列表展示
                </Button>
                <Button type="primary" size="small" icon={<CopyOutlined />} onClick={() => void copyTableDDL()}>
                  复制
                </Button>
              </div>
            )}
          </div>
        }
      >
        <Spin spinning={tableInfoLoading}>
          {tableInfoError ? (
            <div className="table-info-error">{tableInfoError}</div>
          ) : tableInfoMode === 'sql' ? (
            <pre className="table-info-ddl">
              {String(tableInfo?.ddl || '')
                .replace(/\s+$/, '')
                .split('\n')
                .map((line, i) => (
                  <div key={i} className="table-info-ddl-line">
                    <span className="table-info-ddl-no">{i + 1}</span>
                    <span className="table-info-ddl-code">{line || ' '}</span>
                  </div>
                ))}
            </pre>
          ) : (
            <ResponsiveTable
              size="small"
              bordered
              pagination={false}
              rowKey={(r) => r.name}
              scroll={{ x: 'max-content' }}
              dataSource={tableInfo?.columns || []}
              columns={[
                { title: '字段名', dataIndex: 'name', key: 'name', width: 160 },
                {
                  title: '数据类型',
                  dataIndex: 'data_type',
                  key: 'data_type',
                  width: 160,
                  render: (v: string) => <Tag className="table-info-type">{v}</Tag>,
                },
                {
                  title: '可为空',
                  dataIndex: 'nullable',
                  key: 'nullable',
                  width: 80,
                  render: (v: boolean) =>
                    v ? <Tag>是</Tag> : <Tag color="gold">否</Tag>,
                },
                {
                  title: '默认值',
                  dataIndex: 'default',
                  key: 'default',
                  width: 220,
                  ellipsis: true,
                  render: (v: string) => v || '-',
                },
                {
                  title: '注释',
                  dataIndex: 'comment',
                  key: 'comment',
                  width: 160,
                  ellipsis: true,
                  render: (v: string) => v || '-',
                },
              ]}
            />
          )}
        </Spin>
      </Modal>
    </div>
  )
}
