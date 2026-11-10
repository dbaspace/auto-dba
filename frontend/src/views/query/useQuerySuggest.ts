import { useRef, useState } from 'react'
import api from '../../api/request'
import {
  applySuggestion,
  buildSuggestions,
  dialectOf,
  extendsPrefix,
  sqlContext,
  suggestAnchor,
  type HintCol,
  type SqlContext,
  type SuggestItem,
} from '../../utils/sqlComplete'

export type SuggestEnv = {
  sql: string
  dbId: string
  dbType: string
  tableNames: string[]
  ensureTables: () => void
  apply: (sql: string, cursor: number) => void
}

type Pos = { top: number; left: number }

export function useQuerySuggest(envRef: { current: SuggestEnv }, getBox: () => HTMLTextAreaElement | null) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<SuggestItem[]>([])
  const [active, setActive] = useState(0)
  const [loading, setLoading] = useState(false)
  const [pos, setPos] = useState<Pos>({ top: 0, left: 0 })
  const ctxRef = useRef<SqlContext | null>(null)
  const cacheRef = useRef(new Map<string, HintCol[]>())
  const pendingRef = useRef(new Set<string>())
  const forcedRef = useRef(false)

  function columnsRecord(): Record<string, HintCol[]> {
    const out: Record<string, HintCol[]> = {}
    const dbId = envRef.current.dbId
    cacheRef.current.forEach((cols, key) => {
      const nl = key.indexOf('\n')
      if (nl < 0 || key.slice(0, nl) !== dbId) return
      const table = key.slice(nl + 1)
      if (table) out[table] = cols
    })
    return out
  }

  function place() {
    const el = getBox()
    if (!el) return
    const caret = caretViewport(el)
    const boxWidth = 280
    const boxHeight = 240
    const margin = 8
    let left = caret.left
    let top = caret.top + 18
    if (left + boxWidth > window.innerWidth - margin) left = Math.max(margin, window.innerWidth - boxWidth - margin)
    if (top + boxHeight > window.innerHeight - margin) top = Math.max(margin, window.innerHeight - boxHeight - margin)
    setPos({ top, left })
  }

  function close() {
    setOpen(false)
    setItems([])
    forcedRef.current = false
    ctxRef.current = null
  }

  function loadColumns(dbId: string, table: string) {
    const key = dbId + '\n' + table.toLowerCase()
    if (!dbId || !table || cacheRef.current.has(key) || pendingRef.current.has(key)) return
    pendingRef.current.add(key)
    setLoading(true)
    api.get('/databases/' + dbId + '/columns', { params: { table }, silent: true, timeout: 6000 })
      .then((res) => {
        cacheRef.current.set(key, res.data?.data?.columns || [])
      })
      .catch(() => {
        cacheRef.current.set(key, [])
      })
      .finally(() => {
        pendingRef.current.delete(key)
        setLoading(pendingRef.current.size > 0)
        refresh('cache')
      })
  }

  function refresh(reason: 'type' | 'force' | 'cache') {
    const env = envRef.current
    const el = getBox()
    if (!env.dbId || !el) {
      close()
      return
    }
    const sql = el.value
    const cursor = el.selectionStart ?? sql.length
    const ctx = sqlContext(sql, cursor)
    const anchor = suggestAnchor(sql, cursor, ctx)
    if (ctx.kind === 'none' || (reason === 'type' && anchor === 'none')) {
      close()
      return
    }
    if (ctx.kind === 'table') env.ensureTables()
    if (ctx.kind === 'column' || ctx.kind === 'qual') {
      const names = ctx.kind === 'qual'
        ? ctx.tables.filter((t) => {
          const q = ctx.qualifier.toLowerCase()
          return t.alias.toLowerCase() === q || t.name.toLowerCase() === q || (t.name.split('.').pop() || '').toLowerCase() === q
        })
        : ctx.tables
      for (const ref of names) loadColumns(env.dbId, ref.name)
    }
    const useCtx = anchor === 'semi' ? { ...ctx, prefix: '', from: cursor, to: cursor } : ctx
    const next = buildSuggestions({
      ctx: useCtx,
      tableNames: env.tableNames,
      columns: columnsRecord(),
      dialect: dialectOf(env.dbType),
      loading: pendingRef.current.size > 0,
    })
    if (!next.length && pendingRef.current.size === 0 && reason !== 'force') {
      close()
      return
    }
    if (reason === 'type' && anchor === 'type' && !extendsPrefix(next, ctx.prefix)) {
      close()
      return
    }
    ctxRef.current = useCtx
    setItems(next)
    if (reason !== 'cache') setActive(0)
    setOpen(true)
    place()
  }

  function accept(index = active) {
    const env = envRef.current
    const ctx = ctxRef.current
    const item = items[index]
    const el = getBox()
    if (!ctx || !item || !el) return
    const next = applySuggestion(el.value, ctx, item.insert)
    env.apply(next.sql, next.cursor)
    close()
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === ' ' && !e.shiftKey) {
      e.preventDefault()
      forcedRef.current = true
      refresh('force')
      return true
    }
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') return false
    if (!open) return false
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
      return true
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (!items.length) return true
      setActive((i) => Math.min(items.length - 1, i + 1))
      return true
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(0, i - 1))
      return true
    }
    if (e.key === 'Tab' || e.key === 'Enter') {
      if (e.nativeEvent.isComposing) return false
      if (e.key === 'Enter' && (e.shiftKey || e.altKey || e.ctrlKey || e.metaKey)) return false
      if (e.key === 'Enter' && !items[active]) {
        close()
        return false
      }
      e.preventDefault()
      accept()
      return true
    }
    return false
  }

  function onInput() {
    refresh('type')
  }

  function onScroll() {
    if (open) place()
  }

  return { open, items, active, loading, pos, onKeyDown, onInput, onScroll, close, accept, setActive }
}

function caretViewport(el: HTMLTextAreaElement): { top: number; left: number } {
  const div = document.createElement('div')
  const style = getComputedStyle(el)
  const props = ['fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing', 'lineHeight', 'textTransform', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth', 'boxSizing', 'tabSize'] as const
  div.style.position = 'fixed'
  div.style.visibility = 'hidden'
  div.style.whiteSpace = 'pre-wrap'
  div.style.wordWrap = 'break-word'
  div.style.overflow = 'hidden'
  div.style.width = el.clientWidth + 'px'
  for (const prop of props) div.style[prop] = style[prop]
  div.textContent = el.value.slice(0, el.selectionStart || 0)
  const span = document.createElement('span')
  span.textContent = el.value.slice(el.selectionStart || 0) || '.'
  div.appendChild(span)
  document.body.appendChild(div)
  const host = el.getBoundingClientRect()
  const top = host.top + span.offsetTop - el.scrollTop
  const left = host.left + span.offsetLeft - el.scrollLeft
  document.body.removeChild(div)
  return { top, left }
}
