/** Publication Tab 展示行：单表或分表组（一组一个移除） */
export type PublicationDisplayRow = {
  key: string
  kind: 'single' | 'shard'
  tables: string[]
  shard_pattern?: string
  target_table?: string
  mapping_id?: string
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

function isShardLikeMapping(m: PubMappingLike, matchedCount: number): boolean {
  return (
    m.is_shard_group === true ||
    matchedCount > 1 ||
    Boolean(m.shard_pattern?.trim())
  )
}

function mappingShardPriority(m: PubMappingLike): number {
  if (m.is_shard_group) return 2
  if ((m.source_tables?.length ?? 0) > 1) return 1
  if (m.shard_pattern?.trim()) return 1
  return 0
}

/**
 * 将 PG 扁平 published 表名按 mappings 聚合为展示行。
 * 分表组一行、一个移除；单表一行；未匹配映射的作 orphan。
 */
export function groupPublishedTablesByMappings(
  published: string[],
  mappings: PubMappingLike[],
): PublicationDisplayRow[] {
  const publishedSet = new Set(published)
  const used = new Set<string>()
  const rows: PublicationDisplayRow[] = []

  const active = dedupeMappings(mappings).filter((m) => !isDeleted(m))
  const sorted = [...active].sort(
    (a, b) => mappingShardPriority(b) - mappingShardPriority(a),
  )

  for (const m of sorted) {
    const matched = (m.source_tables || []).filter(
      (t) => publishedSet.has(t) && !used.has(t),
    )
    if (!matched.length) continue
    if (!isShardLikeMapping(m, matched.length)) continue

    matched.forEach((t) => used.add(t))
    rows.push({
      key: mappingDedupeKey(m) || matched.join('\0'),
      kind: 'shard',
      tables: [...matched].sort(),
      shard_pattern: m.shard_pattern?.trim() || undefined,
      target_table: targetTableOf(m),
      mapping_id: m.id,
    })
  }

  for (const m of sorted) {
    const matched = (m.source_tables || []).filter(
      (t) => publishedSet.has(t) && !used.has(t),
    )
    if (matched.length !== 1) continue

    const t = matched[0]
    used.add(t)
    rows.push({
      key: m.id || t,
      kind: 'single',
      tables: [t],
      target_table: targetTableOf(m),
      mapping_id: m.id,
    })
  }

  for (const t of published) {
    if (!used.has(t)) {
      rows.push({
        key: `orphan:${t}`,
        kind: 'single',
        tables: [t],
      })
    }
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
