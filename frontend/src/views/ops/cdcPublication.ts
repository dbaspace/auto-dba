/** Publication Tab 展示行：单表或分表组（一组一个移除） */
export type PublicationDisplayRow = {
  key: string
  kind: 'single' | 'shard'
  tables: string[]
  shard_pattern?: string
  target_table?: string
  mapping_id?: string
  /** 无映射覆盖的已发布表 */
  orphan?: boolean
  /** debezium_signal 等 */
  system?: boolean
}

/** 表映射行（与 /tables、tableRows 字段对齐，宽松 any 兼容） */
export type PubMappingLike = {
  id?: string
  name?: string
  target_table?: string
  source_tables?: string[]
  is_shard_group?: boolean
  shard_pattern?: string
  status?: string
}

const SOURCE_TABLE_SHARD_SUFFIX_RE = /^(.+?)(_[0-9]+|[0-9]+)$/
const DEBEZIUM_ROUTER_TYPE = 'io.debezium.transforms.ByLogicalTableRouter'

/** 去掉 public. 前缀并 trim；比对用小写 key */
export function normalizeTableName(name: string): string {
  let t = String(name || '').trim()
  if (!t) return ''
  const lower = t.toLowerCase()
  if (lower.startsWith('public.')) t = t.slice('public.'.length)
  return t.trim()
}

export function tableNameKey(name: string): string {
  return normalizeTableName(name).toLowerCase()
}

export function isProtectedPublicationTable(name: string): boolean {
  const t = tableNameKey(name)
  const bare = t.includes('.') ? t.slice(t.lastIndexOf('.') + 1) : t
  return bare === 'debezium_signal'
}

function targetTableOf(m: PubMappingLike): string | undefined {
  const t = String(m.target_table || '').trim()
  return t || undefined
}

function isDeleted(m: PubMappingLike): boolean {
  return String(m.status || '').toLowerCase() === 'deleted'
}

function mappingDedupeKey(m: PubMappingLike): string {
  if (m.id) return String(m.id)
  const tgt = targetTableOf(m) || ''
  const srcs = (m.source_tables || []).slice().sort().join(',')
  return `${tgt}|${srcs}`
}

function dedupeMappings(mappings: PubMappingLike[]): PubMappingLike[] {
  const seen = new Set<string>()
  const out: PubMappingLike[] = []
  for (const m of mappings) {
    const k = mappingDedupeKey(m)
    if (seen.has(k)) continue
    seen.add(k)
    out.push(m)
  }
  return out
}

export function isShardLikeMapping(m: PubMappingLike, matchedCount = 0): boolean {
  const src = (m.source_tables || []).map((t) => String(t).trim()).filter(Boolean)
  if (m.is_shard_group === true || matchedCount > 1 || Boolean(m.shard_pattern?.trim()) || src.length > 1) {
    return true
  }
  const tgt = tableNameKey(m.target_table || '')
  if (!tgt) return false
  return src.some((s) => tableNameKey(s) !== tgt)
}

function mappingShardPriority(m: PubMappingLike): number {
  if (m.is_shard_group) return 2
  if ((m.source_tables?.length ?? 0) > 1) return 1
  if (m.shard_pattern?.trim()) return 1
  return 0
}

export function sanitizeIdent(s: string): string {
  const raw = String(s || '').trim()
  if (!raw) return ''
  let out = ''
  for (let i = 0; i < raw.length; i++) {
    const c = raw[i]
    if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c === '_') out += c
    else out += '_'
  }
  return out.replace(/^_+|_+$/g, '').toLowerCase()
}

function shardTableRegexPart(logicalTable: string): string {
  const base = String(logicalTable || '').trim()
  if (!base) return ''
  return `${base}(_[0-9]+|[0-9]+)`
}

function inferSourceTableStem(tableName: string): string {
  const t = normalizeTableName(tableName)
  if (!t) return ''
  const m = SOURCE_TABLE_SHARD_SUFFIX_RE.exec(t)
  return m?.[1]?.trim() || t
}

function longestCommonPrefix(values: string[]): string {
  if (!values.length) return ''
  let prefix = values[0]
  for (let i = 1; i < values.length; i++) {
    const v = values[i]
    while (prefix && !v.startsWith(prefix)) prefix = prefix.slice(0, -1)
    if (!prefix) break
  }
  return prefix
}

function inferSourceRegexStem(sourceTables: string[]): string {
  const tables = sourceTables.map(normalizeTableName).filter(Boolean)
  if (!tables.length) return ''
  const stems = tables.map(inferSourceTableStem)
  const first = stems[0]
  if (stems.every((s) => s === first)) return first
  return longestCommonPrefix(stems)
}

/** 对齐后端 debeziumShardRegexFromPattern */
export function debeziumShardRegexFromPattern(pattern: string): string {
  const p = String(pattern || '').trim()
  if (!p) return ''
  if (/[()[\]{}|\\+]/.test(p)) return p
  if (p.endsWith('*')) {
    const t = p.replace(/\*+$/, '')
    if (t && !/[ *?]/.test(t)) return shardTableRegexPart(t.replace(/_+$/, ''))
  }
  if (/[*?]/.test(p)) {
    let b = ''
    for (let i = 0; i < p.length; i++) {
      const c = p[i]
      if (c === '*') b += '.*'
      else if (c === '?') b += '.'
      else b += c.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    }
    return b
  }
  return shardTableRegexPart(p)
}

/** 对齐后端 deriveShardRegexPart */
export function deriveShardRegexPart(
  targetTable: string,
  fePattern: string,
  sourceTables: string[],
): string {
  const p = String(fePattern || '').trim()
  if (p) {
    const regex = debeziumShardRegexFromPattern(p)
    if (regex) {
      try {
        // eslint-disable-next-line no-new
        new RegExp(regex)
        return regex
      } catch {
        /* fall through */
      }
    }
  }
  let stem = inferSourceRegexStem(sourceTables)
  if (!stem) stem = normalizeTableName(targetTable)
  return shardTableRegexPart(stem)
}

export function splitTableIncludeList(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of String(raw || '').split(',')) {
    const p = part.trim()
    if (!p || seen.has(p)) continue
    seen.add(p)
    out.push(p)
  }
  return out
}

/** include 条目是否为正则（分表）而非普通表名 */
export function isIncludeListPattern(entry: string): boolean {
  const bare = normalizeTableName(entry)
  return /[()[\]{}|*+?]/.test(bare)
}

export function includeListTablePart(entry: string): string {
  return normalizeTableName(entry)
}

export type ParsedConnectorTables = {
  exactTables: string[]
  patterns: string[]
  routerLogicalNames: string[]
  signalCollection: string
}

export function parseConnectorTableConfig(cfg: Record<string, any> | null | undefined): ParsedConnectorTables {
  const include = splitTableIncludeList(String(cfg?.['table.include.list'] || ''))
  const exactTables: string[] = []
  const patterns: string[] = []
  for (const e of include) {
    const part = includeListTablePart(e)
    if (!part) continue
    if (isIncludeListPattern(e) || isIncludeListPattern(part)) patterns.push(part)
    else exactTables.push(part)
  }
  const transforms = String(cfg?.transforms || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  const routerLogicalNames: string[] = []
  for (const name of transforms) {
    const typ = String(cfg?.[`transforms.${name}.type`] || '')
    if (typ.includes('ByLogicalTableRouter')) routerLogicalNames.push(name)
  }
  const signalCollection = String(
    cfg?.['signal.data.collection'] || cfg?.signal_data_collection || '',
  ).trim()
  return { exactTables, patterns, routerLogicalNames, signalCollection }
}

function tryMatchRegex(pattern: string, table: string): boolean {
  const name = normalizeTableName(table)
  if (!pattern || !name) return false
  try {
    return new RegExp(`^${pattern}$`).test(name)
  } catch {
    return false
  }
}

/**
 * 将 PG 扁平 published 表名按 mappings 聚合为展示行。
 * 分表组一行、一个移除；单表一行；未匹配映射的作 orphan（系统表打标）。
 */
export function groupPublishedTablesByMappings(
  published: string[],
  mappings: PubMappingLike[],
): PublicationDisplayRow[] {
  const publishedNorm = published.map(normalizeTableName).filter(Boolean)
  const publishedSet = new Set(publishedNorm.map((t) => t.toLowerCase()))
  const keyToDisplay = new Map<string, string>()
  for (const t of publishedNorm) keyToDisplay.set(t.toLowerCase(), t)

  const used = new Set<string>()
  const rows: PublicationDisplayRow[] = []

  const active = dedupeMappings(mappings).filter((m) => !isDeleted(m))
  const sorted = [...active].sort((a, b) => mappingShardPriority(b) - mappingShardPriority(a))

  for (const m of sorted) {
    if (!isShardLikeMapping(m)) continue
    const srcs = (m.source_tables || []).map(normalizeTableName).filter(Boolean)
    const matched = srcs.filter((t) => publishedSet.has(t.toLowerCase()) && !used.has(t.toLowerCase()))
    const regex = deriveShardRegexPart(targetTableOf(m) || '', m.shard_pattern || '', srcs)
    const fromPattern: string[] = []
    if (regex) {
      for (const t of publishedNorm) {
        const k = t.toLowerCase()
        if (used.has(k) || matched.some((x) => x.toLowerCase() === k)) continue
        if (tryMatchRegex(regex, t)) fromPattern.push(t)
      }
    }
    const all = [...matched, ...fromPattern]
    if (!all.length) continue
    all.forEach((t) => used.add(t.toLowerCase()))
    rows.push({
      key: mappingDedupeKey(m) || all.join('\0'),
      kind: 'shard',
      tables: [...all].sort((a, b) => a.localeCompare(b)),
      shard_pattern: m.shard_pattern?.trim() || regex || undefined,
      target_table: targetTableOf(m),
      mapping_id: m.id,
    })
  }

  for (const m of sorted) {
    if (isShardLikeMapping(m)) continue
    const matched = (m.source_tables || [])
      .map(normalizeTableName)
      .filter((t) => t && publishedSet.has(t.toLowerCase()) && !used.has(t.toLowerCase()))
    if (matched.length !== 1) continue
    const t = matched[0]
    used.add(t.toLowerCase())
    rows.push({
      key: m.id || t,
      kind: 'single',
      tables: [keyToDisplay.get(t.toLowerCase()) || t],
      target_table: targetTableOf(m),
      mapping_id: m.id,
    })
  }

  for (const t of publishedNorm) {
    const k = t.toLowerCase()
    if (used.has(k)) continue
    const system = isProtectedPublicationTable(t)
    rows.push({
      key: `orphan:${t}`,
      kind: 'single',
      tables: [t],
      orphan: !system,
      system,
    })
  }

  return rows
}

/** 客户端搜索：源表 / 分表模式 / 目标表（分表任一张表命中保留整行） */
export function filterPublicationDisplayRows(
  rows: PublicationDisplayRow[],
  keyword: string,
): PublicationDisplayRow[] {
  const k = keyword.trim().toLowerCase()
  if (!k) return rows
  return rows.filter((row) => {
    if (row.shard_pattern?.toLowerCase().includes(k)) return true
    if (row.target_table?.toLowerCase().includes(k)) return true
    return row.tables.some((t) => t.toLowerCase().includes(k))
  })
}

export type MappingPipelineDrift = {
  plainMappingCount: number
  shardGroupCount: number
  publishedBusinessCount: number
  publishedSystemCount: number
  orphanCount: number
  includeExactCount: number
  includePatternCount: number
  /** 仅映射有（单表） */
  onlyInMapping: string[]
  /** 仅 Publication 有（业务孤儿） */
  onlyInPublication: string[]
  /** 单表映射未出现在 include 精确项 */
  missingFromInclude: string[]
  /** include 精确项不在映射中 */
  includeOrphans: string[]
  /** 分表：include 缺正则 */
  shardMissingInclude: string[]
  /** 分表：缺 ByLogicalTableRouter */
  shardMissingRouter: string[]
  hasDrift: boolean
  alertLines: string[]
}

function activeMappings(mappings: PubMappingLike[]): PubMappingLike[] {
  return dedupeMappings(mappings).filter((m) => !isDeleted(m))
}

/**
 * 映射 ↔ Publication ↔ Debezium include/transforms 三方差集（单表精确 + 分表规则）。
 */
export function analyzeMappingPipelineDrift(
  published: string[],
  mappings: PubMappingLike[],
  connectorCfg?: Record<string, any> | null,
): MappingPipelineDrift {
  const active = activeMappings(mappings)
  const plain: PubMappingLike[] = []
  const shards: PubMappingLike[] = []
  for (const m of active) {
    if (isShardLikeMapping(m)) shards.push(m)
    else plain.push(m)
  }

  const publishedNorm = published.map(normalizeTableName).filter(Boolean)
  const publishedSystem = publishedNorm.filter(isProtectedPublicationTable)
  const publishedBusiness = publishedNorm.filter((t) => !isProtectedPublicationTable(t))

  const plainSources = plain
    .flatMap((m) => m.source_tables || [])
    .map(normalizeTableName)
    .filter(Boolean)
  const plainSourceKeys = new Set(plainSources.map((t) => t.toLowerCase()))

  const shardSpecs = shards.map((m) => {
    const srcs = (m.source_tables || []).map(normalizeTableName).filter(Boolean)
    const target = normalizeTableName(targetTableOf(m) || srcs[0] || '')
    const regex = deriveShardRegexPart(target, m.shard_pattern || '', srcs)
    const logical = sanitizeIdent(target)
    return { m, srcs, target, regex, logical }
  })

  const coveredByMapping = new Set<string>()
  for (const t of plainSources) coveredByMapping.add(t.toLowerCase())
  for (const s of shardSpecs) {
    for (const t of s.srcs) coveredByMapping.add(t.toLowerCase())
    for (const t of publishedBusiness) {
      if (s.regex && tryMatchRegex(s.regex, t)) coveredByMapping.add(t.toLowerCase())
    }
  }

  const onlyInPublication = publishedBusiness.filter((t) => !coveredByMapping.has(t.toLowerCase()))
  const pubBizKeys = new Set(publishedBusiness.map((t) => t.toLowerCase()))
  const onlyInMapping = plainSources.filter((t) => !pubBizKeys.has(t.toLowerCase()))

  const parsed = parseConnectorTableConfig(connectorCfg)
  const includeExactKeys = new Set(parsed.exactTables.map((t) => t.toLowerCase()))
  const includePatterns = parsed.patterns
  const routerSet = new Set(parsed.routerLogicalNames.map((n) => n.toLowerCase()))

  const missingFromInclude = plainSources.filter((t) => {
    if (includeExactKeys.has(t.toLowerCase())) return false
    return !includePatterns.some((p) => tryMatchRegex(p, t))
  })

  const mappingCoverKeys = new Set(coveredByMapping)
  const includeOrphans = parsed.exactTables.filter(
    (t) => !isProtectedPublicationTable(t) && !mappingCoverKeys.has(t.toLowerCase()),
  )

  const shardMissingInclude: string[] = []
  const shardMissingRouter: string[] = []
  for (const s of shardSpecs) {
    const label = s.target || s.regex || '分表'
    const hasInclude =
      !!s.regex &&
      includePatterns.some((p) => p === s.regex || p.includes(s.regex) || s.regex.includes(p))
    if (s.regex && !hasInclude) shardMissingInclude.push(`${label}（期望 include: ${s.regex}）`)
    if (s.logical && !routerSet.has(s.logical)) {
      shardMissingRouter.push(`${label}（缺 transforms.${s.logical} Router）`)
    }
  }

  const alertLines: string[] = []
  if (onlyInMapping.length) alertLines.push(`映射有、Publication 无：${onlyInMapping.slice(0, 8).join('、')}${onlyInMapping.length > 8 ? '…' : ''}`)
  if (onlyInPublication.length) {
    alertLines.push(
      `Publication 有、映射未覆盖：${onlyInPublication.slice(0, 8).join('、')}${onlyInPublication.length > 8 ? '…' : ''}`,
    )
  }
  if (missingFromInclude.length) {
    alertLines.push(
      `映射有、Debezium include.list 无：${missingFromInclude.slice(0, 8).join('、')}${missingFromInclude.length > 8 ? '…' : ''}`,
    )
  }
  if (includeOrphans.length) {
    alertLines.push(
      `include.list 有、映射未覆盖：${includeOrphans.slice(0, 8).join('、')}${includeOrphans.length > 8 ? '…' : ''}`,
    )
  }
  if (shardMissingInclude.length) alertLines.push(`分表未写入 include.list：${shardMissingInclude.join('；')}`)
  if (shardMissingRouter.length) alertLines.push(`分表 Router 未配置：${shardMissingRouter.join('；')}`)

  return {
    plainMappingCount: plain.length,
    shardGroupCount: shards.length,
    publishedBusinessCount: publishedBusiness.length,
    publishedSystemCount: publishedSystem.length,
    orphanCount: onlyInPublication.length,
    includeExactCount: parsed.exactTables.length,
    includePatternCount: parsed.patterns.length,
    onlyInMapping,
    onlyInPublication,
    missingFromInclude,
    includeOrphans,
    shardMissingInclude,
    shardMissingRouter,
    hasDrift: alertLines.length > 0,
    alertLines,
  }
}
