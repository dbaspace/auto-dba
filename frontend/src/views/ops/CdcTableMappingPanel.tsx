import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Checkbox, Input, Table, Tag, message } from 'antd'
import {
  ArrowDownOutlined,
  ArrowRightOutlined,
  ArrowUpOutlined,
  KeyOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import api from '../../api/request'
import './CdcTableMappingPanel.css'

export interface MappingColumn {
  name: string
  data_type: string
  is_nullable: boolean | null
  is_primary_key: boolean
  comment: string
}
export interface Mapping {
  key: string
  target_table?: string
  source_tables: string[]
  key_columns: string[]
  shard_keys?: string[]
  is_shard_group?: boolean
  shard_pattern?: string
  [k: string]: any
}
interface CacheEntry {
  loading: boolean
  columns: MappingColumn[]
  error: string
}
interface Props {
  mappings: Mapping[]
  onChange: (next: Mapping[]) => void
  instanceId?: string
  dbName?: string
  schema?: string
}

function normalizeCols(raw: any[]): MappingColumn[] {
  return (raw || [])
    .map((c) => ({
      name: c.name || c.column_name,
      data_type: c.data_type || c.udt_name || '',
      is_nullable: typeof c.is_nullable === 'boolean' ? c.is_nullable : c.is_nullable === 'YES',
      is_primary_key: !!c.is_primary_key,
      comment: c.comment || '',
    }))
    .filter((c) => c.name)
}
function intersectCols(groups: MappingColumn[][]): MappingColumn[] {
  if (!groups.length) return []
  const maps = groups.map((g) => new Map(g.map((c) => [c.name, c])))
  return groups[0].filter((c) => maps.every((m) => m.has(c.name)))
}

export default function CdcTableMappingPanel({
  mappings,
  onChange,
  instanceId = '',
  dbName = '',
  schema = 'public',
}: Props) {
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [cache, setCache] = useState<Record<string, CacheEntry>>({})
  const [batchLoading, setBatchLoading] = useState(false)
  const pkApplied = useRef<Set<string>>(new Set())
  const cacheRef = useRef(cache)
  cacheRef.current = cache

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return mappings
    return mappings.filter(
      (m) =>
        String(m.target_table || '').toLowerCase().includes(q) ||
        (m.source_tables || []).some((t) => String(t).toLowerCase().includes(q)) ||
        String(m.shard_pattern || '').toLowerCase().includes(q),
    )
  }, [mappings, search])

  const loadedCount = mappings.filter((m) => (cache[m.key]?.columns || []).length > 0).length
  const pendingSingleCount = mappings.filter(
    (m) => !m.is_shard_group && m.source_tables?.length === 1 && !(cache[m.key]?.columns || []).length,
  ).length
  const hasMissingKey = mappings.some((m) => !(m.key_columns || []).length)

  function emitChange() {
    onChange(mappings.slice())
  }
  function patch(key: string, next: Partial<Mapping>) {
    onChange(mappings.map((m) => (m.key === key ? { ...m, ...next } : m)))
  }
  function remove(key: string) {
    setCache((prev) => {
      const n = { ...prev }
      delete n[key]
      return n
    })
    setExpanded((prev) => {
      const n = new Set(prev)
      n.delete(key)
      return n
    })
    pkApplied.current.delete(key)
    onChange(mappings.filter((m) => m.key !== key))
  }
  function isOpen(key: string) {
    return expanded.has(key)
  }
  function toggleExpand(key: string) {
    setExpanded((prev) => {
      const n = new Set(prev)
      if (n.has(key)) n.delete(key)
      else n.add(key)
      return n
    })
  }

  async function fetchOne(table: string): Promise<MappingColumn[]> {
    const { data } = await api.get(
      `/cdc-sync/instances/${instanceId}/databases/${encodeURIComponent(dbName)}/tables/${encodeURIComponent(table)}/columns`,
      { params: { schema: schema || 'public' } },
    )
    return normalizeCols(data.data?.columns || data.columns)
  }
  async function fetchBatch(tables: string[]): Promise<Map<string, MappingColumn[]>> {
    const { data } = await api.post(`/cdc-sync/instances/${instanceId}/columns/batch`, {
      schema: schema || 'public',
      db_name: dbName,
      tables,
    })
    const raw = data.data?.tables || {}
    const out = new Map<string, MappingColumn[]>()
    for (const [k, v] of Object.entries(raw)) out.set(k, normalizeCols(v as any[]))
    return out
  }

  async function loadColumns(m: Mapping, force = false) {
    if (!instanceId || !dbName) return
    const existing = cacheRef.current[m.key]
    if (!force && existing && !existing.error && existing.columns?.length) return
    setCache((prev) => ({ ...prev, [m.key]: { loading: true, columns: existing?.columns || [], error: '' } }))
    try {
      let cols: MappingColumn[] = []
      if (m.is_shard_group && m.source_tables.length > 1) {
        const map = await fetchBatch(m.source_tables)
        cols = intersectCols(m.source_tables.map((t) => map.get(t) || []).filter((g) => g.length))
        if (!cols.length && m.source_tables[0]) cols = await fetchOne(m.source_tables[0])
      } else {
        cols = await fetchOne(m.source_tables[0])
      }
      setCache((prev) => ({ ...prev, [m.key]: { loading: false, columns: cols, error: '' } }))
      if (!pkApplied.current.has(m.key) && !(m.key_columns || []).length) {
        const pks = cols.filter((c) => c.is_primary_key).map((c) => c.name)
        if (pks.length) {
          pkApplied.current.add(m.key)
          patch(m.key, { key_columns: pks })
        }
      }
    } catch (e: any) {
      setCache((prev) => ({
        ...prev,
        [m.key]: { loading: false, columns: [], error: e?.response?.data?.message || e.message || '字段加载失败' },
      }))
    }
  }

  async function loadAllColumns() {
    if (!instanceId || !dbName) return
    const targets = mappings.filter(
      (m) => !m.is_shard_group && m.source_tables?.length === 1 && !(cache[m.key]?.columns || []).length,
    )
    if (!targets.length) return
    setBatchLoading(true)
    try {
      const map = await fetchBatch(targets.map((m) => m.source_tables[0]))
      const resolved = new Map<string, MappingColumn[]>()
      const cacheUpdate: Record<string, CacheEntry> = {}
      for (const m of targets) {
        let cols = map.get(m.source_tables[0]) || []
        if (!cols.length) {
          try {
            cols = await fetchOne(m.source_tables[0])
          } catch {
            cols = []
          }
        }
        resolved.set(m.key, cols)
        cacheUpdate[m.key] = { loading: false, columns: cols, error: '' }
      }
      setCache((prev) => ({ ...prev, ...cacheUpdate }))
      onChange(
        mappings.map((m) => {
          if (m.is_shard_group || m.key_columns?.length) return m
          const cols = resolved.get(m.key) || []
          const pks = cols.filter((c) => c.is_primary_key).map((c) => c.name)
          if (!pks.length) return m
          pkApplied.current.add(m.key)
          return { ...m, key_columns: pks }
        }),
      )
      const missing = targets.filter(
        (m) => !(m.key_columns || []).length && !(resolved.get(m.key) || []).some((c) => c.is_primary_key),
      )
      if (missing.length) {
        setExpanded((prev) => {
          const n = new Set(prev)
          missing.forEach((m) => n.add(m.key))
          return n
        })
        message.info(
          `${targets.length - missing.length} 张已自动预选主键；${missing.length} 张未检测到主键，已展开请手动勾选 KEY 列`,
        )
      } else {
        message.success(`已加载 ${targets.length} 张表字段并预选主键`)
      }
    } finally {
      setBatchLoading(false)
    }
  }

  function displayColumns(m: Mapping): MappingColumn[] {
    const cols = cache[m.key]?.columns || []
    const keys = m.key_columns || []
    if (!keys.length) return cols
    const set = new Set(keys)
    return [
      ...cols.filter((c) => set.has(c.name)).sort((a, b) => keys.indexOf(a.name) - keys.indexOf(b.name)),
      ...cols.filter((c) => !set.has(c.name)),
    ]
  }
  function allColsChecked(m: Mapping) {
    const cols = cache[m.key]?.columns || []
    return cols.length > 0 && cols.every((c) => m.key_columns.includes(c.name))
  }
  function someColsChecked(m: Mapping) {
    const cols = cache[m.key]?.columns || []
    const n = cols.filter((c) => m.key_columns.includes(c.name)).length
    return n > 0 && n < cols.length
  }
  function applyKeys(m: Mapping, keys: string[]) {
    const keySet = new Set(keys)
    const shard = (m.shard_keys || []).filter((k) => keySet.has(k))
    patch(m.key, { key_columns: keys, shard_keys: shard })
  }
  function toggleCol(m: Mapping, name: string, checked: boolean) {
    const keys = checked
      ? m.key_columns.includes(name)
        ? m.key_columns
        : [...m.key_columns, name]
      : m.key_columns.filter((c) => c !== name)
    applyKeys(m, keys)
  }
  function selectPKs(m: Mapping) {
    const pks = (cache[m.key]?.columns || []).filter((c) => c.is_primary_key).map((c) => c.name)
    if (pks.length) applyKeys(m, pks)
  }
  function moveKey(m: Mapping, index: number, delta: number) {
    const next = [...m.key_columns]
    const j = index + delta
    if (j < 0 || j >= next.length) return
    ;[next[index], next[j]] = [next[j], next[index]]
    applyKeys(m, next)
  }
  function toggleShard(m: Mapping, col: string, checked: boolean) {
    const set = new Set(m.shard_keys || [])
    if (checked) set.add(col)
    else set.delete(col)
    patch(m.key, { shard_keys: [...set] })
  }

  // 展开时自动加载字段
  useEffect(() => {
    mappings.forEach((m) => {
      if (expanded.has(m.key) && !cacheRef.current[m.key]) void loadColumns(m)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [expanded, mappings.map((m) => m.key).join(',')])

  if (!mappings.length) {
    return <div className="tree-empty">左侧勾选或「分表匹配」后，已选映射会显示在这里</div>
  }

  return (
    <div className="map-panel">
      <div className="map-toolbar">
        <Input
          allowClear
          size="small"
          placeholder="搜索源表 / 目标表名"
          prefix={<SearchOutlined />}
          style={{ flex: 1, minWidth: 160 }}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <span className="muted">
          已加载字段 {loadedCount}/{mappings.length}
        </span>
        <Button
          size="small"
          loading={batchLoading}
          disabled={!pendingSingleCount || !instanceId || !dbName}
          onClick={loadAllColumns}
        >
          加载全部字段{pendingSingleCount ? `（${pendingSingleCount}）` : ''}
        </Button>
        {expanded.size > 0 && (
          <Button size="small" type="link" onClick={() => setExpanded(new Set())}>
            全部收起
          </Button>
        )}
      </div>
      {hasMissingKey && (
        <Alert
          type="error"
          showIcon
          className="mb"
          message="请点击下方映射卡片展开后勾选 KEY 列（可多选并调整顺序）；KEY 及其顺序会影响自动建表的数据分布与 SQL 查询性能。存在「KEY 未配置」时无法进入下一步，也可使用「加载全部字段」自动预选主键。"
        />
      )}
      {!filtered.length ? (
        <div className="tree-empty">无匹配映射</div>
      ) : (
        <div className="map-list">
          {filtered.map((m, index) => {
            const entry = cache[m.key]
            const showManual = !!entry?.error || (!!entry && !entry.loading && !(entry.columns || []).length)
            const colDefs = [
              {
                title: (
                  <Checkbox
                    checked={allColsChecked(m)}
                    indeterminate={someColsChecked(m)}
                    onChange={(e) => applyKeys(m, e.target.checked ? (entry?.columns || []).map((c) => c.name) : [])}
                  />
                ),
                key: 'ck',
                width: 46,
                render: (_: any, row: MappingColumn) => (
                  <Checkbox
                    checked={m.key_columns.includes(row.name)}
                    onChange={(e) => toggleCol(m, row.name, e.target.checked)}
                  />
                ),
              },
              {
                title: '列名',
                key: 'name',
                render: (_: any, row: MappingColumn) => (
                  <span>
                    <span className="col-name">{row.name}</span>
                    {row.is_primary_key && <KeyOutlined className="pk" title="源端主键" />}
                  </span>
                ),
              },
              {
                title: '类型',
                key: 'type',
                render: (_: any, row: MappingColumn) => <span className="muted">{row.data_type || '—'}</span>,
              },
              {
                title: 'NULL',
                key: 'null',
                width: 70,
                render: (_: any, row: MappingColumn) =>
                  row.is_nullable === true ? (
                    <Tag>是</Tag>
                  ) : row.is_nullable === false ? (
                    <Tag color="warning">否</Tag>
                  ) : (
                    <span className="muted">—</span>
                  ),
              },
              {
                title: '注释',
                key: 'comment',
                ellipsis: true,
                render: (_: any, row: MappingColumn) => <span className="muted">{row.comment || '—'}</span>,
              },
              ...(m.key_columns.length >= 2
                ? [
                    {
                      title: '顺序',
                      key: 'order',
                      width: 88,
                      align: 'center' as const,
                      render: (_: any, row: MappingColumn) =>
                        m.key_columns.includes(row.name) ? (
                          <span>
                            <Button
                              type="text"
                              size="small"
                              disabled={m.key_columns.indexOf(row.name) === 0}
                              icon={<ArrowUpOutlined />}
                              onClick={() => moveKey(m, m.key_columns.indexOf(row.name), -1)}
                            />
                            <Button
                              type="text"
                              size="small"
                              disabled={m.key_columns.indexOf(row.name) === m.key_columns.length - 1}
                              icon={<ArrowDownOutlined />}
                              onClick={() => moveKey(m, m.key_columns.indexOf(row.name), 1)}
                            />
                          </span>
                        ) : (
                          <span className="muted">—</span>
                        ),
                    },
                  ]
                : []),
            ]
            return (
              <div key={m.key} className="map-item">
                <div className="map-head" onClick={() => toggleExpand(m.key)}>
                  <div className="map-title">
                    <span className="caret">{isOpen(m.key) ? <ArrowDownOutlined /> : <ArrowRightOutlined />}</span>
                    <b>映射 {index + 1}</b>
                    {m.is_shard_group ? (
                      <Tag color="orange">分表组 {m.source_tables.length} 张</Tag>
                    ) : (
                      <Tag>单表</Tag>
                    )}
                    <span className="muted ellip">
                      {m.is_shard_group
                        ? `${m.shard_pattern || ''} = ${m.source_tables[0] || ''}`
                        : m.source_tables[0]}
                    </span>
                    {!isOpen(m.key) && !m.key_columns.length && <Tag color="warning">KEY 未配置</Tag>}
                  </div>
                  <div className="map-head-right">
                    <span className="muted">
                      KEY {m.key_columns.length}
                      {entry?.columns?.length ? ` / ${entry.columns.length}` : ''}
                    </span>
                    <Button
                      type="text"
                      danger
                      size="small"
                      onClick={(e) => {
                        e.stopPropagation()
                        remove(m.key)
                      }}
                    >
                      移除
                    </Button>
                  </div>
                </div>
                <div className="target-row" onClick={(e) => e.stopPropagation()}>
                  <span className="muted target-lab">目标表</span>
                  <Input
                    size="small"
                    placeholder="StarRocks 目标表名"
                    value={m.target_table}
                    onChange={(e) => patch(m.key, { target_table: e.target.value })}
                    onBlur={emitChange}
                  />
                </div>
                {isOpen(m.key) && (
                  <div className="map-body" onClick={(e) => e.stopPropagation()}>
                    <div className="key-bar">
                      <div>
                        <b>KEY 列（多选）</b>{' '}
                        <span className="muted">
                          已选 {m.key_columns.length} / {(entry?.columns || []).length}
                        </span>{' '}
                        {m.is_shard_group && !!entry?.columns?.length && <Tag color="blue">共有列</Tag>}
                      </div>
                      <div>
                        {(entry?.columns || []).some((c) => c.is_primary_key) && (
                          <Button size="small" type="link" onClick={() => selectPKs(m)}>
                            选源端主键
                          </Button>
                        )}
                        <Button
                          size="small"
                          type="link"
                          disabled={!m.key_columns.length}
                          onClick={() => patch(m.key, { key_columns: [], shard_keys: [] })}
                        >
                          清空
                        </Button>
                        <Button size="small" type="link" loading={!!entry?.loading} onClick={() => loadColumns(m, true)}>
                          刷新
                        </Button>
                      </div>
                    </div>
                    <p className="key-hint">
                      可多选 KEY 列并调整顺序；KEY 及顺序会影响自动建表的数据分布与 SQL 查询性能。
                    </p>
                    {m.key_columns.length >= 2 ? (
                      <div className="order-row">
                        <span className="muted">KEY 列顺序</span>
                        <div className="order-chips">
                          {m.key_columns.map((col, i) => (
                            <span key={col} className="chip">
                              {col}
                              <Button
                                type="text"
                                size="small"
                                disabled={i === 0}
                                icon={<ArrowUpOutlined />}
                                onClick={() => moveKey(m, i, -1)}
                              />
                              <Button
                                type="text"
                                size="small"
                                disabled={i === m.key_columns.length - 1}
                                icon={<ArrowDownOutlined />}
                                onClick={() => moveKey(m, i, 1)}
                              />
                            </span>
                          ))}
                        </div>
                      </div>
                    ) : m.key_columns.length === 1 ? (
                      <p className="key-hint">再选 1 列 KEY 后可调整主键/排序键列顺序</p>
                    ) : null}
                    {entry?.error && (
                      <Alert
                        type="warning"
                        showIcon
                        className="mb"
                        message={`字段加载失败：${entry.error}。可手动输入 KEY 列（逗号分隔）。`}
                      />
                    )}
                    {showManual ? (
                      <Input
                        size="small"
                        placeholder="手动输入 KEY 列名，如：id 或 id,tenant_id"
                        defaultValue={m.key_columns.join(',')}
                        onBlur={(e) =>
                          patch(m.key, {
                            key_columns: String(e.target.value || '')
                              .split(/[,，]/)
                              .map((s) => s.trim())
                              .filter(Boolean),
                          })
                        }
                      />
                    ) : (
                      <Table
                        rowKey="name"
                        size="small"
                        loading={!!entry?.loading}
                        dataSource={displayColumns(m)}
                        columns={colDefs as any}
                        pagination={false}
                        scroll={{ y: 220 }}
                        locale={{ emptyText: '加载中或暂无字段' }}
                      />
                    )}
                    {m.is_shard_group && (
                      <div className="shard-box">
                        <div className="muted mb-sm">分桶键 DISTRIBUTED BY HASH（可选）</div>
                        <Checkbox
                          checked={!!m.shard_keys?.length}
                          onChange={(e) =>
                            patch(m.key, {
                              shard_keys: e.target.checked
                                ? [...(m.shard_keys?.length ? m.shard_keys : m.key_columns)]
                                : [],
                            })
                          }
                        >
                          自定义分桶键（默认与 KEY 列相同，无需配置）
                        </Checkbox>
                        {!!m.shard_keys?.length && (
                          <div className="shard-keys">
                            {!m.key_columns.length && <span className="muted">请先选择 KEY 列</span>}
                            {m.key_columns.map((col) => (
                              <Checkbox
                                key={col}
                                checked={m.shard_keys!.includes(col)}
                                onChange={(e) => toggleShard(m, col, e.target.checked)}
                              >
                                {col}
                              </Checkbox>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
