import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  EditOutlined,
  InfoCircleFilled,
} from '@ant-design/icons'
import { Button, Card, Collapse, Divider, Form, Input, Modal, Select, Tooltip, Tree, message } from 'antd'
import type { DataNode } from 'antd/es/tree'
import type { Key } from 'react'
import api from '../../api/request'
import './MigrationObjectSelect.css'

export interface MigrationObject {
  source_db_name: string
  source_table_name: string
  target_db_name: string
  target_table_name: string
  total_bytes: number
}

interface Props {
  instanceId: string
  objects: MigrationObject[]
  wholeDbs: string[]
  excludePattern?: string
  onObjectsChange: (objects: MigrationObject[]) => void
  onWholeDbsChange: (wholeDbs: string[]) => void
  onExcludeChange?: (pattern: string) => void
}

interface TableMeta {
  table_name: string
  total_bytes: number
}

function wildcardToRegex(pattern: string): RegExp | null {
  const t = String(pattern || '').trim()
  if (!t) return null
  const escaped = t.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.')
  try {
    return new RegExp(`^${escaped}$`, 'i')
  } catch {
    return null
  }
}

function formatBytes(n: any) {
  const bytes = Number(n) || 0
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(2)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}

function tableKey(db: string, table: string) {
  return `db:${db}:t:${table}`
}
function parseTableKey(key: any): { db: string; table: string } | null {
  const s = String(key)
  if (!s.includes(':t:')) return null
  const rest = s.slice(3)
  const idx = rest.indexOf(':t:')
  if (idx < 0) return null
  return { db: rest.slice(0, idx), table: rest.slice(idx + 3) }
}
function isDbKey(key: any) {
  const s = String(key)
  return s.startsWith('db:') && !s.includes(':t:')
}
function dbFromKey(key: any) {
  return String(key).slice(3)
}

function mapKey(db: string, table: string) {
  return `${db}\0${table}`
}

async function mapPool<T, R>(items: T[], concurrency: number, fn: (item: T) => Promise<R>): Promise<R[]> {
  const out: R[] = new Array(items.length)
  let i = 0
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (i < items.length) {
      const idx = i++
      out[idx] = await fn(items[idx])
    }
  })
  await Promise.all(workers)
  return out
}

export default function MigrationObjectSelect({
  instanceId,
  objects,
  wholeDbs,
  excludePattern = '',
  onObjectsChange,
  onWholeDbsChange,
  onExcludeChange,
}: Props) {
  const [treeData, setTreeData] = useState<DataNode[]>([])
  const [expandedKeys, setExpandedKeys] = useState<Key[]>([])
  const [checkedKeys, setCheckedKeys] = useState<Key[]>([])
  const [exclude, setExclude] = useState(excludePattern)
  const [leftFilter, setLeftFilter] = useState('')
  const [rightFilter, setRightFilter] = useState('')
  const [rightExpandedKeys, setRightExpandedKeys] = useState<Key[]>([])
  const [loadingDbs, setLoadingDbs] = useState<Set<string>>(new Set())

  const [bulkSourceDb, setBulkSourceDb] = useState<string | undefined>(undefined)
  const [bulkTargetDb, setBulkTargetDb] = useState('')
  const [tablePrefix, setTablePrefix] = useState('')
  const [tableSuffix, setTableSuffix] = useState('')

  const [editOpen, setEditOpen] = useState(false)
  const [editMode, setEditMode] = useState<'db' | 'table'>('db')
  const [editSourceDb, setEditSourceDb] = useState('')
  const [editSourceTable, setEditSourceTable] = useState('')
  const [editTargetDb, setEditTargetDb] = useState('')
  const [editTargetTable, setEditTargetTable] = useState('')

  const loadedRef = useRef<Map<string, TableMeta[]>>(new Map())
  const objectsRef = useRef<MigrationObject[]>(objects)
  objectsRef.current = objects
  const wholeDbsRef = useRef<string[]>(wholeDbs)
  wholeDbsRef.current = wholeDbs
  const treeDataRef = useRef<DataNode[]>(treeData)
  treeDataRef.current = treeData
  const checkedRef = useRef<Key[]>(checkedKeys)
  checkedRef.current = checkedKeys
  const targetMapRef = useRef<Map<string, { target_db_name: string; target_table_name: string }>>(new Map())

  const excludeRe = useMemo(() => wildcardToRegex(exclude), [exclude])
  const excludeReRef = useRef<RegExp | null>(excludeRe)
  excludeReRef.current = excludeRe

  // 同步目标映射，避免整库重算丢映射
  useEffect(() => {
    const m = targetMapRef.current
    for (const o of objects) {
      m.set(mapKey(o.source_db_name, o.source_table_name), {
        target_db_name: o.target_db_name,
        target_table_name: o.target_table_name,
      })
    }
  }, [objects])

  const totalBytes = useMemo(
    () => objects.reduce((s, o) => s + (Number(o.total_bytes) || 0), 0),
    [objects],
  )
  const dbCount = useMemo(() => {
    const s = new Set(objects.map((o) => o.source_db_name))
    for (const d of wholeDbs) s.add(d)
    return s.size
  }, [objects, wholeDbs])
  const sourceDbOptions = useMemo(() => {
    const s = new Set(objects.map((o) => o.source_db_name))
    for (const d of wholeDbs) s.add(d)
    return [...s].sort()
  }, [objects, wholeDbs])

  const filteredTree = useMemo(() => {
    const q = leftFilter.trim().toLowerCase()
    const base = treeData.map((n) => {
      if (!n.children?.length) return n
      const ex = excludeRe
      return {
        ...n,
        children: n.children.map((c) => {
          const p = parseTableKey(c.key)
          const disabled = Boolean(ex && p && (ex.test(`${p.db}.${p.table}`) || ex.test(p.table)))
          return { ...c, disabled }
        }),
      }
    })
    if (!q) return base
    return base
      .map((n) => {
        const self = String(n.title as any).toLowerCase().includes(q)
        const ch = (n.children || []).filter((c) => String((c as any).title).toLowerCase().includes(q))
        if (self || ch.length) return { ...n, children: ch.length ? ch : n.children }
        return null
      })
      .filter(Boolean) as DataNode[]
  }, [treeData, leftFilter, excludeRe])

  async function fetchDbs(): Promise<string[]> {
    if (!instanceId) return []
    const { data } = await api.get(`/migration/instances/${instanceId}/databases`)
    return (data.data || [])
      .filter((d: any) => !d.system)
      .map((d: any) => d.name || d.db_name)
      .filter(Boolean)
  }

  async function fetchTables(db: string, withMeta = false): Promise<TableMeta[]> {
    if (!instanceId || !db) return []
    if (loadedRef.current.has(db)) return loadedRef.current.get(db)!
    const { data } = await api.get(
      `/migration/instances/${instanceId}/databases/${encodeURIComponent(db)}/tables`,
      { params: withMeta ? {} : { names_only: 1 } },
    )
    const raw = data.data || []
    const tables: TableMeta[] = raw
      .map((t: any) => {
        if (typeof t === 'string') return { table_name: t, total_bytes: 0 }
        return { table_name: t.name || t.table_name, total_bytes: Number(t.total_bytes || t.bytes || 0) }
      })
      .filter((t: TableMeta) => t.table_name)
    loadedRef.current.set(db, tables)
    return tables
  }

  function patchDbChildren(db: string, tables: TableMeta[]) {
    const key = `db:${db}`
    setTreeData((prev) =>
      prev.map((n) => {
        if (n.key !== key) return n
        return {
          ...n,
          children: tables.map((t) => ({
            key: tableKey(db, t.table_name),
            title: t.table_name,
            isLeaf: true,
          })),
        }
      }),
    )
  }

  function rebuildObjects(nextWhole: string[], nextTableKeys: Key[]) {
    const ex = excludeReRef.current
    const prevMap = targetMapRef.current
    const byKey = new Map<string, MigrationObject>()

    for (const db of nextWhole) {
      const tables = loadedRef.current.get(db) || []
      for (const t of tables) {
        if (ex && (ex.test(`${db}.${t.table_name}`) || ex.test(t.table_name))) continue
        const k = mapKey(db, t.table_name)
        const prev = prevMap.get(k)
        byKey.set(k, {
          source_db_name: db,
          source_table_name: t.table_name,
          target_db_name: prev?.target_db_name || db,
          target_table_name: prev?.target_table_name || t.table_name,
          total_bytes: t.total_bytes || 0,
        })
      }
    }

    for (const raw of nextTableKeys) {
      const p = parseTableKey(raw)
      if (!p) continue
      if (nextWhole.includes(p.db)) continue
      if (ex && (ex.test(`${p.db}.${p.table}`) || ex.test(p.table))) continue
      const tables = loadedRef.current.get(p.db) || []
      const meta = tables.find((t) => t.table_name === p.table)
      const k = mapKey(p.db, p.table)
      const prev = prevMap.get(k)
      byKey.set(k, {
        source_db_name: p.db,
        source_table_name: p.table,
        target_db_name: prev?.target_db_name || p.db,
        target_table_name: prev?.target_table_name || p.table,
        total_bytes: meta?.total_bytes || 0,
      })
    }

    const rows = [...byKey.values()].sort(
      (a, b) =>
        a.source_db_name.localeCompare(b.source_db_name) ||
        a.source_table_name.localeCompare(b.source_table_name),
    )
    onObjectsChange(rows)
    onWholeDbsChange(nextWhole)
  }

  async function hydrateWholeDb(db: string) {
    setLoadingDbs((s) => new Set(s).add(db))
    try {
      const tables = await fetchTables(db, false)
      if (expandedKeys.includes(`db:${db}`) || treeDataRef.current.some((n) => n.key === `db:${db}` && (n as any).children?.length)) {
        patchDbChildren(db, tables)
      }
      if (wholeDbsRef.current.includes(db)) {
        const tableKeys = checkedRef.current.filter((k) => parseTableKey(k))
        rebuildObjects(wholeDbsRef.current, tableKeys)
      }
    } finally {
      setLoadingDbs((s) => {
        const n = new Set(s)
        n.delete(db)
        return n
      })
    }
  }

  async function refreshRoots() {
    loadedRef.current.clear()
    setTreeData([])
    setExpandedKeys([])
    setCheckedKeys([])
    onObjectsChange([])
    onWholeDbsChange([])
    if (!instanceId) return
    const dbs = await fetchDbs()
    setTreeData(dbs.map((d) => ({ key: `db:${d}`, title: d, isLeaf: false })))
  }

  async function onLoadData(node: any) {
    if (!isDbKey(node.key)) return
    const db = dbFromKey(node.key)
    if (loadedRef.current.has(db) && (node.children?.length || 0) > 0) return
    const tables = await fetchTables(db, false)
    patchDbChildren(db, tables)
  }

  function onCheck(checked: any, info: any) {
    const raw: Key[] = Array.isArray(checked)
      ? checked
      : Array.isArray(checked?.checked)
        ? checked.checked
        : []
    const nodeKey = info?.node?.key
    const isCheck = Boolean(info?.checked)

    let nextWhole = [...wholeDbsRef.current]
    let nextChecked = raw.map(String)

    if (isDbKey(nodeKey)) {
      const db = dbFromKey(nodeKey)
      if (isCheck) {
        if (!nextWhole.includes(db)) nextWhole.push(db)
        // 整库：checkedKeys 只保留库节点，去掉该库下表叶子，避免 901 叶子进 state
        nextChecked = nextChecked.filter((k) => {
          const p = parseTableKey(k)
          return !p || p.db !== db
        })
        if (!nextChecked.includes(`db:${db}`)) nextChecked.push(`db:${db}`)
        setCheckedKeys(nextChecked)
        onWholeDbsChange(nextWhole)
        // 先占位：若表未加载，右侧显示整库加载中；后台补表
        void hydrateWholeDb(db)
        return
      }
      nextWhole = nextWhole.filter((d) => d !== db)
      nextChecked = nextChecked.filter((k) => {
        if (k === `db:${db}`) return false
        const p = parseTableKey(k)
        return !p || p.db !== db
      })
      setCheckedKeys(nextChecked)
      rebuildObjects(nextWhole, nextChecked)
      return
    }

    // 勾选/取消单表：若原属整库，降级为表级勾选
    const p = parseTableKey(nodeKey)
    if (p && nextWhole.includes(p.db)) {
      nextWhole = nextWhole.filter((d) => d !== p.db)
      nextChecked = nextChecked.filter((k) => k !== `db:${p.db}`)
      const tables = loadedRef.current.get(p.db) || []
      for (const t of tables) {
        if (!isCheck && t.table_name === p.table) continue
        const tk = tableKey(p.db, t.table_name)
        if (!nextChecked.includes(tk)) nextChecked.push(tk)
      }
    }

    nextChecked = nextChecked.filter((k) => !isDbKey(k) || nextWhole.includes(dbFromKey(k)))
    setCheckedKeys(nextChecked)
    rebuildObjects(nextWhole, nextChecked)
  }

  async function selectAllDbs() {
    const dbs = treeDataRef.current.map((n) => dbFromKey(n.key))
    const keys = dbs.map((d) => `db:${d}`)
    setCheckedKeys(keys)
    onWholeDbsChange(dbs)
    message.info(`已勾选 ${dbs.length} 个库，正在后台加载表清单…`)
    // 优先批量接口；失败再回退单库并发
    try {
      if (instanceId && dbs.length) {
        const { data } = await api.post(
          `/migration/instances/${instanceId}/databases/tables`,
          { databases: dbs, names_only: true },
          { timeout: 120000 },
        )
        for (const row of data.data || []) {
          if (row.error || !row.db) continue
          const tables: TableMeta[] = (row.tables || []).map((t: any) =>
            typeof t === 'string'
              ? { table_name: t, total_bytes: 0 }
              : { table_name: t.name || t.table_name, total_bytes: Number(t.total_bytes || 0) },
          )
          loadedRef.current.set(row.db, tables.filter((t: TableMeta) => t.table_name))
        }
        rebuildObjects(dbs, [])
        return
      }
    } catch {
      /* fallback */
    }
    await mapPool(dbs, 6, async (db) => {
      await hydrateWholeDb(db)
    })
  }

  function clearAll() {
    setCheckedKeys([])
    onObjectsChange([])
    onWholeDbsChange([])
  }

  function setObjects(next: MigrationObject[]) {
    onObjectsChange(next)
  }

  function applyBulkTargetDb() {
    const src = String(bulkSourceDb || '').trim()
    const tgt = bulkTargetDb.trim()
    if (!src) {
      message.warning('请选择源库')
      return
    }
    if (!tgt) {
      message.warning('请输入要批量应用的目标库名')
      return
    }
    const next = objects.map((o) => (o.source_db_name === src ? { ...o, target_db_name: tgt } : o))
    setObjects(next)
    message.success(`已将源库「${src}」下表的目标库设为「${tgt}」`)
  }

  function applyPrefixSuffix() {
    if (!objects.length) {
      message.warning('暂无已选对象')
      return
    }
    const next = objects.map((o) => ({
      ...o,
      target_table_name: `${tablePrefix}${o.source_table_name}${tableSuffix}`,
    }))
    setObjects(next)
    // 表名被批量改写后不能再按整库摘要提交
    if (wholeDbs.length) onWholeDbsChange([])
    message.success('已按「前缀 + 源表名 + 后缀」更新目标表名')
  }

  function restoreNames() {
    if (!objects.length) {
      message.warning('暂无已选对象')
      return
    }
    setObjects(
      objects.map((o) => ({
        ...o,
        target_db_name: o.source_db_name,
        target_table_name: o.source_table_name,
      })),
    )
    message.success('已将目标库表名恢复为与源端一致')
  }

  function openEditDb(data: any) {
    setEditMode('db')
    setEditSourceDb(data.source_db_name)
    setEditTargetDb(data.target_db_name)
    setEditOpen(true)
  }
  function openEditDbGroup(data: any) {
    setEditMode('db')
    setEditSourceDb(data.source_db_name)
    setEditTargetDb(data.commonTd || '')
    setEditOpen(true)
  }
  function openEditTable(data: any) {
    setEditMode('table')
    setEditSourceDb(data.source_db_name)
    setEditSourceTable(data.source_table_name)
    setEditTargetDb(data.target_db_name)
    setEditTargetTable(data.target_table_name)
    setEditOpen(true)
  }
  function confirmEdit() {
    if (editMode === 'db') {
      const v = editTargetDb.trim()
      if (!v) {
        message.warning('请输入库名')
        return
      }
      setObjects(
        objects.map((o) => (o.source_db_name === editSourceDb ? { ...o, target_db_name: v } : o)),
      )
    } else {
      const t = editTargetTable.trim()
      if (!t) {
        message.warning('请填写目标表名')
        return
      }
      setObjects(
        objects.map((o) =>
          o.source_db_name === editSourceDb && o.source_table_name === editSourceTable
            ? { ...o, target_table_name: t }
            : o,
        ),
      )
    }
    setEditOpen(false)
    message.success('已更新映射')
  }

  const objectsForRight = useMemo(() => {
    const q = rightFilter.trim().toLowerCase()
    if (!q) return objects
    return objects.filter((o) => {
      const blob = `${o.source_db_name}.${o.source_table_name} ${o.target_db_name} ${o.target_table_name}`.toLowerCase()
      return blob.includes(q)
    })
  }, [objects, rightFilter])

  const rightTree: DataNode[] = useMemo(() => {
    const byDb = new Map<string, MigrationObject[]>()
    for (const o of objectsForRight) {
      if (!byDb.has(o.source_db_name)) byDb.set(o.source_db_name, [])
      byDb.get(o.source_db_name)!.push(o)
    }
    for (const db of wholeDbs) {
      if (!byDb.has(db)) byDb.set(db, [])
    }
    const whole = new Set(wholeDbs)
    const nodes: DataNode[] = []
    for (const db of [...byDb.keys()].sort()) {
      const list = byDb
        .get(db)!
        .slice()
        .sort((a, b) => a.source_table_name.localeCompare(b.source_table_name))
      if (whole.has(db)) {
        const loading = loadingDbs.has(db) && list.length === 0
        const data: any = {
          kind: 'whole_db',
          source_db_name: db,
          target_db_name: list[0]?.target_db_name || db,
          tableCount: list.length,
        }
        nodes.push({
          key: `right-whole:${db}`,
          isLeaf: true,
          title: (
            <div className="right-node" onClick={(e) => e.stopPropagation()}>
              <div className="sub">
                源：{data.source_db_name}
                {loading ? '（整库表清单加载中…）' : `（整库 ${data.tableCount} 张表）`}
              </div>
              <div className="map-row">
                <span className="sub">目标库</span>
                <b className="ellip" title={data.target_db_name}>
                  {data.target_db_name}
                </b>
                <Button type="link" size="small" onClick={() => openEditDb(data)}>
                  <EditOutlined />
                </Button>
              </div>
              <div className="sub">整库迁移 · 目标表 —</div>
            </div>
          ),
        })
        continue
      }
      const tds = new Set(list.map((o) => o.target_db_name))
      const commonTd = tds.size === 1 ? list[0].target_db_name : null
      const dbData: any = { kind: 'db', source_db_name: db, commonTd }
      nodes.push({
        key: `right-db:${db}`,
        title: (
          <div className="right-node" onClick={(e) => e.stopPropagation()}>
            <div className="sub">源库：{dbData.source_db_name}</div>
            <div className="map-row">
              <span className="sub">目标库</span>
              {commonTd ? (
                <>
                  <b className="ellip" title={commonTd}>
                    {commonTd}
                  </b>
                  <Button type="link" size="small" onClick={() => openEditDbGroup(dbData)}>
                    <EditOutlined />
                  </Button>
                </>
              ) : (
                <span className="warn">各表目标库不一致，请在子行分别编辑</span>
              )}
            </div>
          </div>
        ),
        children: list.map((o) => {
          const data: any = { kind: 'table', commonTd, ...o }
          return {
            key: `right-t:${o.source_db_name}\0${o.source_table_name}`,
            isLeaf: true,
            title: (
              <div className="right-node" onClick={(e) => e.stopPropagation()}>
                <div className="map-row table-row">
                  <span className="sub">源表 {data.source_table_name}</span>
                  <span className="grow" />
                  {!commonTd && (
                    <>
                      <span className="sub">目标库</span>
                      <b className="ellip sm" title={data.target_db_name}>
                        {data.target_db_name}
                      </b>
                    </>
                  )}
                  <span className="sub">目标表</span>
                  <b className="ellip sm" title={data.target_table_name}>
                    {data.target_table_name}
                  </b>
                  <Button type="link" size="small" onClick={() => openEditTable(data)}>
                    <EditOutlined />
                  </Button>
                </div>
              </div>
            ),
          }
        }),
      })
    }
    return nodes
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [objectsForRight, wholeDbs, loadingDbs])

  const rightAllKeys = useMemo(() => rightTree.map((n) => n.key), [rightTree])

  useEffect(() => {
    setRightExpandedKeys(rightAllKeys)
  }, [rightAllKeys])

  function expandAllRight() {
    setRightExpandedKeys(rightAllKeys)
  }
  function collapseAllRight() {
    setRightExpandedKeys([])
  }

  useEffect(() => {
    refreshRoots()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId])

  useEffect(() => {
    onExcludeChange?.(exclude)
    if (wholeDbsRef.current.length || checkedRef.current.some((k) => parseTableKey(k))) {
      rebuildObjects(wholeDbsRef.current, checkedRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [excludeRe])

  const hasSelection = objects.length > 0 || wholeDbs.length > 0

  return (
    <div className="obj-wrap">
      <Card className="obj-card left" bordered title={<span className="card-title">源库对象</span>}>
        {!instanceId ? (
          <span className="muted">请先在第二步选择源端实例</span>
        ) : (
          <>
            <Input
              allowClear
              className="mb"
              placeholder="排除规则，如 tmp_*"
              value={exclude}
              onChange={(e) => setExclude(e.target.value)}
            />
            <Input
              allowClear
              className="mb"
              placeholder="搜索库名、表名，支持模糊；展开后可搜子级表名"
              value={leftFilter}
              onChange={(e) => setLeftFilter(e.target.value)}
            />
            <div className="hint-bar">
              共 {treeData.length} 个库，当前展示 {filteredTree.length} 个
              {leftFilter.trim() ? '（已按搜索过滤）' : ''}
              · 勾选整库不会展开全部表节点
            </div>
            <Tree
              className="src-tree"
              checkable
              checkStrictly
              virtual
              height={360}
              blockNode
              selectable={false}
              treeData={filteredTree}
              loadData={onLoadData}
              expandedKeys={expandedKeys}
              onExpand={(keys) => setExpandedKeys(keys)}
              checkedKeys={checkedKeys}
              onCheck={onCheck}
            />
            <div className="foot-links">
              <Button type="link" size="small" onClick={refreshRoots}>
                刷新
              </Button>
              <Button type="link" size="small" onClick={selectAllDbs}>
                全选库
              </Button>
              <Button type="link" size="small" onClick={clearAll}>
                清空
              </Button>
            </div>
          </>
        )}
      </Card>

      <div className="obj-mid">
        <Button
          type="primary"
          shape="circle"
          disabled
          title="左侧勾选的对象会自动出现在右侧已选列表"
          icon={<ArrowRightOutlined />}
        />
        <Button
          shape="circle"
          disabled={!hasSelection}
          title="清空已选（取消左侧全部勾选）"
          onClick={clearAll}
          icon={<ArrowLeftOutlined />}
        />
      </div>

      <Card
        className="obj-card right"
        bordered
        title={
          <>
            <span className="card-title">已选对象</span>
            <Tooltip
              title="目标库、目标表请点击行内编辑图标在弹窗中修改；批量规则请展开「批量操作」。"
              placement="top"
            >
              <InfoCircleFilled className="info-ico" />
            </Tooltip>
          </>
        }
      >
        {!instanceId ? (
          <span className="muted">请先选择源端实例</span>
        ) : (
          <>
            <div className="sum">
              已选 <b>{dbCount}</b> 库、<b>{objects.length}</b> 表，预估 <b>{formatBytes(totalBytes)}</b>
              {loadingDbs.size ? ` · ${loadingDbs.size} 个库表清单加载中` : ''}
            </div>
            <Input
              allowClear
              className="mb"
              placeholder="全局搜索原对象名、目标名，支持模糊匹配"
              value={rightFilter}
              onChange={(e) => setRightFilter(e.target.value)}
            />
            {!hasSelection ? (
              <div className="muted">
                在左侧勾选库表后，将在此以树状展示映射；整库勾选不会在左侧展开全部表。
              </div>
            ) : (
              <>
                <Collapse
                  className="batch-box"
                  ghost
                  items={[
                    {
                      key: 'batch',
                      label: (
                        <span>
                          <span>批量操作</span>
                          <span className="batch-sub">按源库改目标库 · 前缀/后缀生成目标表名</span>
                        </span>
                      ),
                      children: (
                        <>
                          <p className="muted tiny">
                            以下操作作用于当前已选全部表级记录（含整库展开后的每一行）。
                          </p>
                          <div className="batch-title">按源库批量改目标库</div>
                          <Select
                            allowClear
                            placeholder="选择源库"
                            style={{ width: '100%' }}
                            className="mb"
                            value={bulkSourceDb}
                            onChange={(v) => setBulkSourceDb(v)}
                            options={sourceDbOptions.map((d) => ({ value: d, label: d }))}
                          />
                          <Input
                            className="mb"
                            placeholder="目标库名"
                            value={bulkTargetDb}
                            onChange={(e) => setBulkTargetDb(e.target.value)}
                          />
                          <Button type="primary" style={{ width: '100%' }} onClick={applyBulkTargetDb}>
                            应用到该源库下全部表
                          </Button>
                          <Divider />
                          <div className="batch-title">按规则批量生成目标表名</div>
                          <p className="muted tiny">公式：目标表名 = 前缀 + 源表名 + 后缀</p>
                          <Input
                            className="mb"
                            placeholder="前缀（可选）"
                            value={tablePrefix}
                            onChange={(e) => setTablePrefix(e.target.value)}
                          />
                          <Input
                            className="mb"
                            placeholder="后缀（可选）"
                            value={tableSuffix}
                            onChange={(e) => setTableSuffix(e.target.value)}
                          />
                          <Button style={{ width: '100%' }} onClick={applyPrefixSuffix}>
                            应用到全部已选表
                          </Button>
                        </>
                      ),
                    },
                  ]}
                />
                <Tree
                  className="right-tree"
                  blockNode
                  virtual
                  height={360}
                  selectable={false}
                  treeData={rightTree}
                  expandedKeys={rightExpandedKeys}
                  onExpand={(keys) => setRightExpandedKeys(keys)}
                />
              </>
            )}
            <div className="foot-links">
              <Button type="link" size="small" onClick={expandAllRight}>
                展开全部
              </Button>
              <Button type="link" size="small" onClick={collapseAllRight}>
                收起全部
              </Button>
              <Button type="link" size="small" onClick={selectAllDbs}>
                全选库
              </Button>
              <Button type="link" size="small" onClick={clearAll}>
                清空
              </Button>
              <Button type="link" size="small" disabled={!objects.length} onClick={restoreNames}>
                恢复原名称
              </Button>
            </div>
            <p className="muted tiny foot-note">
              整库勾选时提交会携带 whole_dbs；目标「库.表」组合不可重复。
            </p>
          </>
        )}
      </Card>

      <Modal
        open={editOpen}
        title={editMode === 'db' ? '编辑库映射' : '编辑表映射'}
        width={480}
        destroyOnClose
        onCancel={() => setEditOpen(false)}
        onOk={confirmEdit}
        okText="确定"
        cancelText="取消"
      >
        <Form layout="vertical">
          <Form.Item label="原库名">
            <Input value={editSourceDb} disabled />
          </Form.Item>
          {editMode === 'table' && (
            <Form.Item label="原表名">
              <Input value={editSourceTable} disabled />
            </Form.Item>
          )}
          {editMode === 'db' ? (
            <Form.Item label="库名" required>
              <Input
                placeholder="请输入目标库名"
                value={editTargetDb}
                onChange={(e) => setEditTargetDb(e.target.value)}
              />
            </Form.Item>
          ) : (
            <>
              <Form.Item label="目标库名">
                <Input value={editTargetDb} disabled />
                <div className="muted tiny">改库名请在已选对象中点击库节点的「编辑」。</div>
              </Form.Item>
              <Form.Item label="目标表名" required>
                <Input
                  placeholder="请输入目标表名"
                  value={editTargetTable}
                  onChange={(e) => setEditTargetTable(e.target.value)}
                />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>
    </div>
  )
}
