export type SqlDialect = 'mysql' | 'pg'

export type SuggestGroup = 'keyword' | 'snippet' | 'table' | 'column'

export type SuggestItem = {
  group: SuggestGroup
  label: string
  detail?: string
  insert: string
}

export type SqlTableRef = {
  name: string
  alias: string
}

export type SqlContext = {
  kind: 'none' | 'keyword' | 'table' | 'column' | 'qual'
  prefix: string
  from: number
  to: number
  tables: SqlTableRef[]
  qualifier: string
  clause: 'select' | 'from' | 'where' | 'on' | 'other'
}

export type HintCol = {
  name: string
  data_type?: string
  nullable?: boolean
  comment?: string
}

const MYSQL_KEYWORDS = [
  'SELECT', 'DISTINCT', 'ALL', 'FROM', 'WHERE', 'AND', 'OR', 'NOT', 'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER', 'FULL', 'CROSS',
  'ON', 'USING', 'GROUP', 'BY', 'ORDER', 'HAVING', 'LIMIT', 'OFFSET', 'AS', 'ASC', 'DESC', 'UNION', 'WITH', 'IN', 'IS', 'NULL',
  'LIKE', 'BETWEEN', 'EXISTS', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END',
]
const PG_KEYWORDS = MYSQL_KEYWORDS
const MYSQL_FUNCS = ['COUNT', 'SUM', 'AVG', 'MIN', 'MAX', 'COALESCE', 'IFNULL', 'NULLIF', 'CAST', 'CONCAT', 'SUBSTRING', 'NOW', 'CURRENT_DATE', 'DATE_FORMAT', 'IF', 'LENGTH', 'TRIM', 'ROUND', 'ABS']
const PG_FUNCS = ['COUNT', 'SUM', 'AVG', 'MIN', 'MAX', 'COALESCE', 'NULLIF', 'CAST', 'CONCAT', 'SUBSTRING', 'NOW', 'CURRENT_DATE', 'DATE_TRUNC', 'LENGTH', 'TRIM', 'ROUND', 'ABS', 'STRING_AGG']

const CLAUSE = new Set([
  'select', 'from', 'where', 'and', 'or', 'not', 'join', 'left', 'right', 'inner', 'outer', 'full', 'cross',
  'on', 'using', 'group', 'order', 'having', 'limit', 'offset', 'union', 'as', 'asc', 'desc', 'with', 'by',
  'in', 'is', 'like', 'between', 'exists', 'case', 'when', 'then', 'else', 'end', 'all', 'distinct',
])

export function dialectOf(dbType: string): SqlDialect {
  const t = String(dbType || '').toLowerCase()
  if (t === 'postgres' || t === 'postgresql' || t === 'cockroachdb' || t === 'pg') return 'pg'
  return 'mysql'
}

export function keywordsOf(dialect: SqlDialect): string[] {
  return dialect === 'pg' ? PG_KEYWORDS : MYSQL_KEYWORDS
}

export function functionsOf(dialect: SqlDialect): string[] {
  return dialect === 'pg' ? PG_FUNCS : MYSQL_FUNCS
}

export function quoteIdent(name: string, dialect: SqlDialect): string {
  const raw = String(name || '')
  const kw = new Set(keywordsOf(dialect).map((k) => k.toLowerCase()))
  const parts = raw.split('.')
  return parts.map((part) => quotePart(part, dialect, kw)).join('.')
}

function quotePart(name: string, dialect: SqlDialect, kw: Set<string>): string {
  if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) && !kw.has(name.toLowerCase())) return name
  if (dialect === 'mysql') return '`' + name.replace(/`/g, '``') + '`'
  return '"' + name.replace(/"/g, '""') + '"'
}

type Tok = { text: string; start: number; end: number; kind: 'ident' | 'sym' }

export function sqlContext(sql: string, cursor: number): SqlContext {
  const empty: SqlContext = { kind: 'none', prefix: '', from: cursor, to: cursor, tables: [], qualifier: '', clause: 'other' }
  if (cursor < 0) cursor = 0
  if (cursor > sql.length) cursor = sql.length
  if (inLiteral(sql, cursor)) return empty
  const range = statementRange(sql, cursor)
  let text = sql.slice(range.start, range.end)
  let origin = range.start
  let local = cursor - origin
  const inner = innermostSelect(text, local)
  if (inner) {
    text = text.slice(inner.start, inner.end)
    origin += inner.start
    local -= inner.start
  }
  const tokens = tokenize(text, origin)
  const tables = collectTables(tokens)
  const edit = editToken(tokens, cursor)
  const prefix = edit ? sql.slice(edit.start, cursor) : ''
  const from = edit ? edit.start : cursor
  const to = edit ? edit.end : cursor
  const prev = previousToken(tokens, edit ? edit.start : cursor)
  if (prev && prev.text === '.') {
    const qual = previousToken(tokens, prev.start)
    return {
      kind: 'qual',
      prefix,
      from,
      to,
      tables,
      qualifier: qual && qual.kind === 'ident' ? qual.text : '',
      clause: 'other',
    }
  }
  const mode = modeAt(tokens, edit ? edit.start : cursor)
  return { kind: mode.kind, prefix, from, to, tables, qualifier: '', clause: mode.clause }
}

function inLiteral(sql: string, cursor: number): boolean {
  let i = 0
  let quote = ''
  let line = false
  let block = false
  while (i < cursor) {
    const c = sql[i]
    const n = sql[i + 1]
    if (line) {
      if (c === '\n') line = false
      i++
      continue
    }
    if (block) {
      if (c === '*' && n === '/') {
        block = false
        i += 2
        continue
      }
      i++
      continue
    }
    if (quote) {
      if (c === quote) {
        if (n === quote) { i += 2; continue }
        quote = ''
      }
      i++
      continue
    }
    if (c === '-' && n === '-') { line = true; i += 2; continue }
    if (c === '/' && n === '*') { block = true; i += 2; continue }
    if (c === "'" || c === '"' || c === '`') { quote = c; i++; continue }
    i++
  }
  return quote !== '' || line || block
}

function statementRange(sql: string, cursor: number): { start: number; end: number } {
  let start = 0
  let depth = 0
  let quote = ''
  let i = 0
  while (i < sql.length) {
    if (i === cursor && !quote) break
    const c = sql[i]
    const n = sql[i + 1]
    if (quote) {
      if (c === quote && n === quote) { i += 2; continue }
      if (c === quote) quote = ''
      i++
      continue
    }
    if (c === '-' && n === '-') {
      while (i < sql.length && sql[i] !== '\n') i++
      continue
    }
    if (c === '/' && n === '*') {
      i += 2
      while (i < sql.length && !(sql[i] === '*' && sql[i + 1] === '/')) i++
      i += 2
      continue
    }
    if (c === "'" || c === '"' || c === '`') { quote = c; i++; continue }
    if (c === '(') depth++
    else if (c === ')') depth = Math.max(0, depth - 1)
    else if (c === ';' && depth === 0) {
      if (i < cursor) start = i + 1
      else return { start, end: i }
    }
    i++
  }
  return { start, end: sql.length }
}

function innermostSelect(text: string, local: number): { start: number; end: number } | null {
  const tokens = tokenize(text, 0)
  let best: { start: number; end: number } | null = null
  const stack: number[] = []
  for (let i = 0; i < tokens.length; i++) {
    const tok = tokens[i]
    if (tok.text === '(') stack.push(i)
    else if (tok.text === ')' && stack.length) {
      const open = stack.pop() as number
      const close = tok
      if (tokens[open].start < local && local <= close.end && isSelectList(tokens.slice(open + 1, i))) {
        best = { start: tokens[open].end, end: close.start }
      }
    }
  }
  for (let i = stack.length - 1; i >= 0; i--) {
    const open = stack[i]
    if (tokens[open].start < local && isSelectList(tokens.slice(open + 1))) {
      return { start: tokens[open].end, end: text.length }
    }
  }
  return best
}

function isSelectList(tokens: Tok[]): boolean {
  const first = tokens.find((t) => t.kind === 'ident')
  return !!first && (first.text.toLowerCase() === 'select' || first.text.toLowerCase() === 'with')
}

function tokenize(sql: string, origin: number): Tok[] {
  const out: Tok[] = []
  let i = 0
  while (i < sql.length) {
    const c = sql[i]
    const n = sql[i + 1]
    if (c === ' ' || c === '\n' || c === '\t' || c === '\r') { i++; continue }
    if (c === '-' && n === '-') {
      while (i < sql.length && sql[i] !== '\n') i++
      continue
    }
    if (c === '/' && n === '*') {
      i += 2
      while (i < sql.length && !(sql[i] === '*' && sql[i + 1] === '/')) i++
      i += 2
      continue
    }
    if (c === "'" || c === '"' || c === '`') {
      const q = c
      i++
      while (i < sql.length) {
        if (sql[i] === q) {
          if (sql[i + 1] === q) { i += 2; continue }
          i++
          break
        }
        i++
      }
      continue
    }
    if (/[A-Za-z_\u00A0-\uFFFF]/.test(c)) {
      const s = i
      i++
      while (i < sql.length && /[A-Za-z0-9_\u00A0-\uFFFF]/.test(sql[i])) i++
      out.push({ text: sql.slice(s, i), start: origin + s, end: origin + i, kind: 'ident' })
      continue
    }
    if ('.,();*'.includes(c)) {
      out.push({ text: c, start: origin + i, end: origin + i + 1, kind: 'sym' })
      i++
      continue
    }
    i++
  }
  return out
}

function editToken(tokens: Tok[], cursor: number): Tok | null {
  for (const tok of tokens) {
    if (tok.kind === 'ident' && tok.start < cursor && cursor <= tok.end) return tok
  }
  return null
}

function previousToken(tokens: Tok[], pos: number): Tok | null {
  let prev: Tok | null = null
  for (const tok of tokens) {
    if (tok.end <= pos) prev = tok
    else break
  }
  return prev
}

function modeAt(tokens: Tok[], pos: number): { kind: SqlContext['kind']; clause: SqlContext['clause'] } {
  let kind: SqlContext['kind'] = 'keyword'
  let clause: SqlContext['clause'] = 'other'
  let expectAlias = false
  for (const tok of tokens) {
    if (tok.start >= pos) break
    if (tok.kind === 'sym') {
      if (tok.text === ',') {
        if (clause === 'from') kind = 'table'
        else kind = clause === 'on' || clause === 'where' || clause === 'select' ? 'column' : 'keyword'
      } else if (tok.text === '(') {
        kind = 'column'
      }
      expectAlias = false
      continue
    }
    const w = tok.text.toLowerCase()
    if (expectAlias && !CLAUSE.has(w)) {
      expectAlias = false
      kind = 'keyword'
      continue
    }
    expectAlias = false
    if (w === 'select' || w === 'with') {
      kind = 'column'
      clause = 'select'
      continue
    }
    if (w === 'from' || w === 'join') {
      kind = 'table'
      clause = 'from'
      continue
    }
    if (w === 'where' || w === 'having' || w === 'and' || w === 'or') {
      kind = 'column'
      if (w === 'where' || w === 'having') clause = 'where'
      else if (clause !== 'on') clause = 'where'
      continue
    }
    if (w === 'on' || w === 'using') {
      kind = 'column'
      clause = 'on'
      continue
    }
    if (w === 'order' || w === 'group') {
      kind = 'keyword'
      clause = 'other'
      continue
    }
    if (w === 'by') {
      kind = 'column'
      continue
    }
    if (w === 'left' || w === 'right' || w === 'inner' || w === 'full' || w === 'cross' || w === 'outer') {
      kind = 'keyword'
      continue
    }
    if (w === 'as') {
      expectAlias = true
      kind = 'none'
      continue
    }
    if (CLAUSE.has(w)) {
      kind = 'keyword'
      continue
    }
    if (kind === 'table') {
      kind = 'keyword'
      clause = 'from'
      continue
    }
    if (kind === 'keyword' && clause === 'from') {
      expectAlias = false
      kind = 'keyword'
      continue
    }
    kind = clause === 'select' || clause === 'where' || clause === 'on' ? 'column' : kind
  }
  if (kind === 'none') kind = 'keyword'
  return { kind, clause }
}

function collectTables(tokens: Tok[]): SqlTableRef[] {
  const tables: SqlTableRef[] = []
  for (let i = 0; i < tokens.length; i++) {
    const w = tokens[i].text.toLowerCase()
    if (w !== 'from' && w !== 'join') continue
    i++
    while (i < tokens.length && tokens[i].text === ',') i++
    const read = readTable(tokens, i)
    if (!read) continue
    let alias = ''
    i = read.next
    if (i < tokens.length && tokens[i].text.toLowerCase() === 'as') i++
    if (i < tokens.length && tokens[i].kind === 'ident' && !CLAUSE.has(tokens[i].text.toLowerCase())) {
      alias = tokens[i].text
    } else {
      i--
    }
    tables.push({ name: read.name, alias })
    while (i + 1 < tokens.length && tokens[i + 1].text === ',') {
      i += 2
      const more = readTable(tokens, i)
      if (!more) break
      alias = ''
      i = more.next
      if (i < tokens.length && tokens[i].text.toLowerCase() === 'as') i++
      if (i < tokens.length && tokens[i].kind === 'ident' && !CLAUSE.has(tokens[i].text.toLowerCase())) {
        alias = tokens[i].text
      } else {
        i--
      }
      tables.push({ name: more.name, alias })
    }
  }
  return tables
}

function readTable(tokens: Tok[], i: number): { name: string; next: number } | null {
  if (i >= tokens.length || tokens[i].kind !== 'ident') return null
  if (CLAUSE.has(tokens[i].text.toLowerCase())) return null
  let name = tokens[i].text
  let next = i + 1
  if (next + 1 < tokens.length && tokens[next].text === '.' && tokens[next + 1].kind === 'ident') {
    name = tokens[i].text + '.' + tokens[next + 1].text
    next += 2
  }
  return { name, next }
}

export function buildSuggestions(opts: {
  ctx: SqlContext
  tableNames: string[]
  columns: Record<string, HintCol[]>
  dialect: SqlDialect
  loading?: boolean
}): SuggestItem[] {
  const { ctx, dialect } = opts
  if (ctx.kind === 'none') return []
  const prefix = ctx.prefix
  const items: SuggestItem[] = []
  if (ctx.kind === 'keyword') {
    for (const word of keywordsOf(dialect)) items.push({ group: 'keyword', label: word, insert: word })
  }
  if (ctx.kind === 'column' && ctx.clause === 'select') {
    for (const word of ['DISTINCT', 'ALL']) items.push({ group: 'keyword', label: word, insert: word })
  }
  if (ctx.kind === 'column' || (ctx.kind === 'keyword' && ctx.clause === 'select')) {
    for (const fn of functionsOf(dialect)) items.push({ group: 'keyword', label: fn, detail: '函数', insert: fn + '(' })
  }
  if (ctx.kind === 'table') {
    for (const name of opts.tableNames) {
      items.push({ group: 'table', label: name, insert: quoteIdent(name, dialect) })
    }
  }
  if (ctx.kind === 'column' || ctx.kind === 'qual') {
    const refs = ctx.kind === 'qual' ? matchQualifier(ctx.tables, ctx.qualifier) : ctx.tables
    const seen = new Map<string, number>()
    const cols: { ref: SqlTableRef; col: HintCol }[] = []
    for (const ref of refs) {
      for (const col of columnsOf(opts.columns, ref)) {
        cols.push({ ref, col })
        const key = col.name.toLowerCase()
        seen.set(key, (seen.get(key) || 0) + 1)
      }
    }
    for (const { ref, col } of cols) {
      const dup = (seen.get(col.name.toLowerCase()) || 0) > 1 && ctx.kind !== 'qual'
      const qual = ref.alias || ref.name
      const insert = dup ? quoteIdent(qual, dialect) + '.' + quoteIdent(col.name, dialect) : quoteIdent(col.name, dialect)
      items.push({
        group: 'column',
        label: columnSuggestLabel(col.name, col.comment, dup ? qual : ''),
        insert,
      })
    }
    if (ctx.clause === 'on' && ctx.kind === 'column') {
      items.push(...equalitySnippets(ctx.tables, opts.columns, dialect))
    }
  }
  if (!prefix && opts.loading && (ctx.kind === 'column' || ctx.kind === 'qual') && !items.some((item) => item.group === 'column')) {
    return []
  }
  return rankItems(items, prefix).slice(0, 50)
}

function columnSuggestLabel(name: string, comment: string | undefined, qual: string): string {
  const note = String(comment || '').trim()
  const base = note ? `${name}-${note}` : name
  return qual ? `${base}  ${qual}` : base
}

function matchQualifier(tables: SqlTableRef[], qualifier: string): SqlTableRef[] {
  const q = qualifier.toLowerCase()
  const hit = tables.filter((t) => t.alias.toLowerCase() === q || t.name.toLowerCase() === q || t.name.split('.').pop()!.toLowerCase() === q)
  return hit.length ? hit : []
}

function columnsOf(all: Record<string, HintCol[]>, ref: SqlTableRef): HintCol[] {
  const keys = [ref.name, ref.name.split('.').pop() || ref.name]
  for (const key of keys) {
    const found = all[key.toLowerCase()]
    if (found) return found
  }
  return []
}

function equalitySnippets(tables: SqlTableRef[], columns: Record<string, HintCol[]>, dialect: SqlDialect): SuggestItem[] {
  if (tables.length < 2) return []
  const a = tables[0]
  const b = tables[1]
  const ac = columnsOf(columns, a)
  const bc = new Set(columnsOf(columns, b).map((c) => c.name.toLowerCase()))
  const out: SuggestItem[] = []
  for (const col of ac) {
    if (!bc.has(col.name.toLowerCase())) continue
    const left = quoteIdent(a.alias || a.name, dialect) + '.' + quoteIdent(col.name, dialect)
    const right = quoteIdent(b.alias || b.name, dialect) + '.' + quoteIdent(col.name, dialect)
    const text = `${left} = ${right}`
    out.push({ group: 'snippet', label: text, detail: '同名列', insert: text })
  }
  return out
}

function rankItems(items: SuggestItem[], prefix: string): SuggestItem[] {
  const p = prefix.toLowerCase()
  const order: Record<SuggestGroup, number> = { keyword: 0, table: 1, column: 2, snippet: 3 }
  const emptyOrder: Record<SuggestGroup, number> = { column: 0, table: 1, snippet: 2, keyword: 3 }
  const scored = items.map((item) => {
    const label = item.label.toLowerCase()
    const insert = item.insert.toLowerCase()
    const name = item.group === 'column' ? (insert.split('.').pop() || insert) : label
    const head = name.startsWith(p) || insert.startsWith(p)
    const has = !p || head || (item.group !== 'column' && label.includes(p)) || insert.includes(p)
    return { item, head, has }
  }).filter((row) => row.has)
  scored.sort((a, b) => {
    if (a.head !== b.head) return a.head ? -1 : 1
    const g = (p ? order : emptyOrder)[a.item.group] - (p ? order : emptyOrder)[b.item.group]
    if (g) return g
    return a.item.label.localeCompare(b.item.label)
  })
  return scored.map((row) => row.item)
}

export function applySuggestion(sql: string, ctx: SqlContext, insert: string): { sql: string; cursor: number } {
  const next = sql.slice(0, ctx.from) + insert + sql.slice(ctx.to)
  return { sql: next, cursor: ctx.from + insert.length }
}
