<template>
  <div class="query-page">
    <div class="query-body">
      <div class="query-wm" aria-hidden="true">
        <span v-for="n in 30" :key="n">{{ username }}</span>
      </div>
      <div class="pane-left" :style="{ width: leftWidth + 'px' }">
        <div class="tree-search">
          <el-input v-model="keyword" clearable placeholder="搜索数据库" :prefix-icon="Search">
            <template #append>
              <el-select v-model="envFilter" placeholder="筛选环境" style="width: 118px" :teleported="false">
                <el-option label="全部环境" value="" />
                <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
              </el-select>
            </template>
          </el-input>
        </div>
        <div v-loading="treeLoading" class="tree-wrap">
          <el-tree
            v-if="hasTreeData"
            ref="treeRef"
            :key="treeRenderKey"
            lazy
            :load="loadNode"
            node-key="id"
            :props="{ label: 'label', isLeaf: 'isLeaf' }"
            highlight-current
            expand-on-click-node
            @node-click="onNodeClick"
          >
            <template #default="{ data }">
              <span class="tree-node" :title="nodeTitle(data)" @dblclick.stop="onNodeDblClick(data)">
                <DbTypeIcon v-if="data.type === 'db'" :type="data.dbType" :size="16" />
                <span v-else-if="data.type === 'table'" class="table-ico" v-html="tableSvg" />
                <span class="tree-label" :class="{ muted: data.type === 'empty' }" v-html="highlight(data.label)" />
                <span v-if="data.tag" class="tree-tag">[{{ data.tag }}]</span>
              </span>
            </template>
          </el-tree>
          <div v-else-if="!treeLoading" class="tree-empty">暂无数据</div>
        </div>
      </div>
      <div class="gutter-x" @mousedown="startDrag('left', $event)" />
      <div class="pane-right">
        <div v-if="!tabs.length" class="empty-main">
          <svg class="bolt" viewBox="0 0 24 24" fill="none">
            <path d="M13 2 4 14h7l-1 8 10-14h-7l0-6Z" fill="#2563eb" />
          </svg>
          <span>先从左侧选择一个数据库进行操作</span>
        </div>
        <template v-else>
          <div class="work-tabs">
            <el-tabs v-model="activeKey" type="card" closable @tab-remove="closeTab" @tab-change="onTabChange">
              <el-tab-pane v-for="t in tabs" :key="t.key" :name="t.key">
                <template #label>
                  <span class="tab-lab">
                    <DbTypeIcon :type="t.dbType" :size="14" />
                    <span>{{ t.dbName }}</span>
                    <span v-if="t.tag" class="tab-tag">[{{ t.tag }}]</span>
                    <span class="tab-time">{{ fmtTime(t.ts) }}</span>
                  </span>
                </template>
              </el-tab-pane>
            </el-tabs>
            <el-button class="tab-add" text @click="addTab">+</el-button>
          </div>
          <div class="editor-split" :style="{ height: editorHeight + 'px' }">
            <div class="editor-col">
              <div class="tool-bar">
                <div class="tool-left">
                  <el-button type="primary" :disabled="!current?.sql?.trim()" :loading="running" @click="runSQL()">
                    <el-icon class="btn-ico"><CaretRight /></el-icon>
                    运行
                  </el-button>
                  <el-button @click="formatSQL">格式化</el-button>
                </div>
                <el-button @click="openFavorite">
                  <el-icon class="btn-ico"><Star /></el-icon>
                  收藏
                </el-button>
              </div>
              <div class="db-meta">
                <el-tag :type="envTagType(current?.env)" size="small">{{ current?.env || '—' }}</el-tag>
                <span v-if="envPeers.length > 1" class="env-swap" @click="cycleEnv" title="切换同名库环境">⇄</span>
                <DbTypeIcon :type="current?.dbType" :size="16" />
                <span class="db-name">
                  {{ current?.dbName }}
                  <span v-if="current?.tag">({{ current.tag }})</span>
                </span>
              </div>
              <textarea
                v-model="current.sql"
                class="sql-box"
                spellcheck="false"
                placeholder="输入 SQL，Ctrl / ⌘ + Enter 运行"
                @keydown="onSqlKey"
              />
            </div>
            <div class="gutter-x inner" @mousedown="startDrag('hist', $event)" />
            <div class="hist-col" :style="{ width: histWidth + 'px' }">
              <el-tabs v-model="storeTab" class="store-tabs">
                <el-tab-pane label="我的收藏" name="fav">
                  <el-input v-model="favKeyword" size="small" clearable placeholder="搜索收藏（收藏名称、SQL内容）" />
                  <div class="store-list">
                    <div
                      v-for="item in filteredFavs"
                      :key="item.id"
                      class="store-item"
                      @click="applyStore(item)"
                    >
                      <div class="store-title">
                        <span>{{ item.name }}</span>
                        <el-button text type="danger" size="small" @click.stop="removeFav(item.id)">删除</el-button>
                      </div>
                      <div class="store-sub">{{ item.dbName }} · {{ fmtTime(item.ts) }}</div>
                      <div class="store-sql">{{ item.sql }}</div>
                    </div>
                    <div v-if="!filteredFavs.length" class="store-empty">暂无收藏</div>
                  </div>
                </el-tab-pane>
                <el-tab-pane label="查询历史" name="hist">
                  <div class="store-list">
                    <div
                      v-for="item in history"
                      :key="item.id"
                      class="store-item"
                      @click="applyStore(item)"
                    >
                      <div class="store-title">
                        <span>{{ item.dbName }}</span>
                        <span class="store-sub">{{ fmtTime(item.ts) }}</span>
                      </div>
                      <div class="store-sql">{{ item.sql }}</div>
                    </div>
                    <div v-if="!history.length" class="store-empty">暂无历史</div>
                  </div>
                </el-tab-pane>
              </el-tabs>
            </div>
          </div>
          <div class="gutter-y" @mousedown="startDrag('editor', $event)" />
          <div v-loading="running" class="result-col">
            <div v-if="current?.error" class="result-err">{{ current.error }}</div>
            <template v-else-if="current?.result">
              <div class="result-meta">
                <span>
                  {{ current.result.rows?.length || 0 }} 行
                  <span v-if="current.result.truncated">（已截断）</span>
                  · {{ current.result.elapsed_ms || 0 }} ms
                </span>
                <el-button type="primary" size="small" @click="exportDQL">导出</el-button>
              </div>
              <el-table :data="resultRows" border stripe height="100%" size="small">
                <el-table-column
                  v-for="col in current.result.columns || []"
                  :key="col"
                  :prop="col"
                  :label="col"
                  min-width="120"
                  show-overflow-tooltip
                />
              </el-table>
            </template>
            <div v-else class="result-placeholder">运行查询后在此展示结果</div>
          </div>
        </template>
      </div>
    </div>

    <el-dialog v-model="favVisible" title="收藏 SQL" width="420px" append-to-body destroy-on-close>
      <el-input v-model="favName" placeholder="收藏名称" />
      <template #footer>
        <el-button @click="favVisible = false">取消</el-button>
        <el-button type="primary" @click="saveFavorite">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onActivated, onDeactivated, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { CaretRight, Search, Star } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import DbTypeIcon from '../../components/DbTypeIcon.vue'

defineOptions({ name: 'Query' })

const router = useRouter()
const DQL_DRAFT_KEY = 'auto-dba-dql-draft'
const WORKSPACE_KEY = 'auto-dba-query-workspace'

const FAV_KEY = 'auto-dba-query-favorites'
const HIST_KEY = 'auto-dba-query-history'
const tableSvg = `<svg viewBox="0 0 16 16" width="16" height="16"><rect x="1.5" y="2.5" width="13" height="11" rx="1.2" fill="#60a5fa"/><path d="M1.5 5.5h13M6.5 5.5v8" stroke="#fff" stroke-width="1.2"/></svg>`

const username = (() => {
  try {
    return JSON.parse(localStorage.getItem('user') || '{}').username || ''
  } catch {
    return ''
  }
})()

const treeLoading = ref(false)
const keyword = ref('')
const envFilter = ref('')
const envOptions = ref([])
const databases = ref([])
const instances = ref([])
const tablesMap = ref({})
const treeRef = ref()
const tableLoads = {}

const savedWorkspace = loadWorkspace()
const tabs = ref(savedWorkspace.tabs)
const activeKey = ref(savedWorkspace.activeKey)
const running = ref(false)
const leftWidth = ref(savedWorkspace.leftWidth)
const editorHeight = ref(savedWorkspace.editorHeight)
const histWidth = ref(savedWorkspace.histWidth)
const storeTab = ref('fav')
const favorites = ref(loadJSON(FAV_KEY, []))
const history = ref(loadJSON(HIST_KEY, []))
const favVisible = ref(false)
const favName = ref('')
const favKeyword = ref('')

const current = computed(() => tabs.value.find((t) => t.key === activeKey.value) || null)
const instMap = computed(() => {
  const m = {}
  for (const i of instances.value) m[i.id] = i
  return m
})

const envPeers = computed(() => {
  const cur = current.value
  if (!cur) return []
  return databases.value.filter((d) => d.db_name === cur.dbName)
})

const treeRenderKey = computed(() => `${envFilter.value}|${keyword.value}|${envOptions.value.join(',')}`)

const hasTreeData = computed(() => groupedDbs().size > 0)

const TYPE_ORDER = [
  'mysql', 'postgres', 'tidb', 'cockroachdb', 'oceanbase', 'cassandra',
  'oracle', 'elasticsearch', 'mongodb', 'sqlserver', 'starrocks', 'clickhouse',
]

function dbEnv(db) {
  return instMap.value[db.instance_id]?.idc || '未分组'
}

function normalizeType(t) {
  const n = String(t || '').toLowerCase().trim()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'cockroach' || n === 'cockroadchdb') return 'cockroachdb'
  if (n === 'mssql' || n === 'sql-server' || n === 'sql_server') return 'sqlserver'
  if (n === 'es' || n === 'elastic') return 'elasticsearch'
  if (n === 'mongo') return 'mongodb'
  if (n === 'ob') return 'oceanbase'
  return n || 'other'
}

function dbTypeOf(db) {
  return normalizeType(instMap.value[db.instance_id]?.db_type)
}

function typeRank(t) {
  const i = TYPE_ORDER.indexOf(normalizeType(t))
  return i < 0 ? TYPE_ORDER.length : i
}

function cmpName(a, b) {
  return String(a || '').localeCompare(String(b || ''), 'zh-CN', { numeric: true, sensitivity: 'base' })
}

function sortDbs(dbs) {
  return [...dbs].sort((a, b) => {
    const ra = typeRank(dbTypeOf(a))
    const rb = typeRank(dbTypeOf(b))
    if (ra !== rb) return ra - rb
    const ta = dbTypeOf(a)
    const tb = dbTypeOf(b)
    if (ta !== tb) return ta.localeCompare(tb)
    const byName = cmpName(a.db_name, b.db_name)
    if (byName) return byName
    return cmpName(dbTags(a).join(','), dbTags(b).join(','))
  })
}

function matchKeyword(db) {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return true
  if (String(db.db_name || '').toLowerCase().includes(kw)) return true
  if (dbTags(db).join(' ').toLowerCase().includes(kw)) return true
  if (instanceHint(db).toLowerCase().includes(kw)) return true
  return (tablesMap.value[db.id] || []).some((t) => String(t).toLowerCase().includes(kw))
}

function groupedDbs() {
  const groups = new Map()
  for (const db of databases.value) {
    const envName = dbEnv(db)
    if (envFilter.value && envName !== envFilter.value) continue
    if (!matchKeyword(db)) continue
    if (!groups.has(envName)) groups.set(envName, [])
    groups.get(envName).push(db)
  }
  for (const [name, list] of groups) {
    groups.set(name, sortDbs(list))
  }
  return groups
}

function envTreeNodes() {
  return [...groupedDbs().entries()]
    .sort((a, b) => {
      if (a[0] === '未分组') return 1
      if (b[0] === '未分组') return -1
      return cmpName(a[0], b[0])
    })
    .map(([name, dbs]) => ({
      id: `env:${name}`,
      label: name,
      type: 'env',
      env: name,
      isLeaf: dbs.length === 0,
    }))
}

function dbTreeNode(db, siblings) {
  const inst = instMap.value[db.instance_id] || {}
  return {
    id: db.id,
    label: db.db_name,
    type: 'db',
    dbId: db.id,
    env: inst.idc || '未分组',
    dbType: inst.db_type,
    tag: dbTagText(db, siblings),
    isLeaf: false,
  }
}

function tableTreeNodes(dbNode, names) {
  if (!names.length) {
    return [{ id: `${dbNode.dbId}__empty`, label: '暂无表', type: 'empty', isLeaf: true }]
  }
  return [...names].sort(cmpName).map((name) => ({
    id: `${dbNode.dbId}__${name}`,
    label: name,
    type: 'table',
    dbId: dbNode.dbId,
    table: name,
    env: dbNode.env,
    dbType: dbNode.dbType,
    isLeaf: true,
  }))
}

function fetchTables(dbId) {
  if (Object.prototype.hasOwnProperty.call(tablesMap.value, dbId)) {
    return Promise.resolve(tablesMap.value[dbId])
  }
  if (tableLoads[dbId]) return tableLoads[dbId]
  tableLoads[dbId] = api.get(`/databases/${dbId}/tables`)
    .then((res) => {
      const names = res.data.data || []
      tablesMap.value = { ...tablesMap.value, [dbId]: names }
      return names
    })
    .catch(() => {
      tablesMap.value = { ...tablesMap.value, [dbId]: [] }
      return []
    })
    .finally(() => {
      delete tableLoads[dbId]
    })
  return tableLoads[dbId]
}

function loadNode(node, resolve) {
  if (node.level === 0) {
    const envs = envTreeNodes()
    resolve(envs)
    nextTick(() => {
      for (const e of envs) treeRef.value?.getNode(e.id)?.expand()
    })
    return
  }
  const data = node.data || {}
  if (data.type === 'env') {
    const list = groupedDbs().get(data.env) || []
    resolve(list.map((db) => dbTreeNode(db, list)))
    return
  }
  if (data.type === 'db') {
    fetchTables(data.dbId).then((names) => resolve(tableTreeNodes(data, names)))
    return
  }
  resolve([])
}

const filteredFavs = computed(() => {
  const q = favKeyword.value.trim().toLowerCase()
  if (!q) return favorites.value
  return favorites.value.filter((f) => `${f.name} ${f.sql}`.toLowerCase().includes(q))
})

const resultRows = computed(() => {
  const res = current.value?.result
  if (!res) return []
  return (res.rows || []).map((row) => {
    const obj = {}
    ;(res.columns || []).forEach((c, i) => {
      obj[c] = cellText(row[i])
    })
    return obj
  })
})

function loadWorkspace() {
  const empty = { tabs: [], activeKey: '', leftWidth: 280, editorHeight: 320, histWidth: 300 }
  try {
    const raw = sessionStorage.getItem(WORKSPACE_KEY)
    if (!raw) return empty
    const d = JSON.parse(raw)
    const tabs = Array.isArray(d.tabs) ? d.tabs.map(normalizeSavedTab).filter(Boolean) : []
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

function normalizeSavedTab(t) {
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

function persistWorkspace() {
  try {
    writeWorkspace()
  } catch { /* ignore persist errors so they never freeze the UI */ }
}

function writeWorkspace() {
  const payload = {
    activeKey: activeKey.value,
    leftWidth: leftWidth.value,
    editorHeight: editorHeight.value,
    histWidth: histWidth.value,
    tabs: tabs.value.map((t) => ({
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
    try { sessionStorage.setItem(WORKSPACE_KEY, JSON.stringify(payload)) } catch { /* ignore quota */ }
  }
}

function loadJSON(key, fallback) {
  try {
    const v = JSON.parse(localStorage.getItem(key) || 'null')
    return Array.isArray(v) ? v : fallback
  } catch {
    return fallback
  }
}

function saveJSON(key, val) {
  localStorage.setItem(key, JSON.stringify(val))
}

function cellText(v) {
  if (v === null || v === undefined) return 'NULL'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

function fmtTime(ts) {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function envTagType(env) {
  const e = String(env || '').toLowerCase()
  if (['prod', 'production', 'prd', '线上'].includes(e)) return 'danger'
  return 'primary'
}

function dbTags(db) {
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

function instanceHint(db) {
  const inst = instMap.value[db?.instance_id] || {}
  return inst.name || inst.domain_name || (inst.domains && inst.domains[0]) || inst.host || ''
}

function dbTagText(db, siblings) {
  const tags = dbTags(db)
  if (tags.length) return tags.join(',')
  const name = db?.db_name
  const same = (siblings || databases.value || []).filter((d) => d.db_name === name)
  if (same.length > 1) return instanceHint(db)
  return ''
}

function dbTag(db) {
  return dbTagText(db, databases.value)
}

function nodeTitle(data) {
  return data.tag ? `${data.label} [${data.tag}]` : data.label
}

function highlight(text) {
  const kw = keyword.value.trim()
  const raw = String(text || '')
  if (!kw) return escapeHtml(raw)
  const idx = raw.toLowerCase().indexOf(kw.toLowerCase())
  if (idx < 0) return escapeHtml(raw)
  return `${escapeHtml(raw.slice(0, idx))}<span class="hit">${escapeHtml(raw.slice(idx, idx + kw.length))}</span>${escapeHtml(raw.slice(idx + kw.length))}`
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}

function quoteTable(name, dbType) {
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

function newTab(db, { sql = '', table = '' } = {}) {
  const inst = instMap.value[db.instance_id] || {}
  return {
    key: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    dbId: db.id,
    dbName: db.db_name,
    dbType: inst.db_type || '',
    env: inst.idc || '',
    instanceId: db.instance_id,
    tag: dbTag(db),
    table,
    sql,
    ts: Date.now(),
    result: null,
    error: '',
  }
}

function openDB(db, { sql, table, run } = {}) {
  const exist = tabs.value.find((t) => t.dbId === db.id && !t.table)
  if (exist && !table && sql === undefined) {
    activeKey.value = exist.key
    return exist
  }
  const tab = newTab(db, { sql: sql ?? (table ? `SELECT * FROM ${quoteTable(table, instMap.value[db.instance_id]?.db_type)} LIMIT 50;` : ''), table })
  tabs.value.push(tab)
  activeKey.value = tab.key
  if (run) runSQL(tab)
  return tab
}

function onNodeClick(data) {
  if (!data || data.type === 'env' || data.type === 'empty') return
  const db = databases.value.find((d) => d.id === data.dbId || d.id === data.id)
  if (!db) return
  if (data.type === 'table') {
    const tab = tabs.value.find((t) => t.key === activeKey.value && t.dbId === db.id) || openDB(db)
    tab.sql = `SELECT * FROM ${quoteTable(data.table, data.dbType)} LIMIT 50;`
    tab.table = data.table
    activeKey.value = tab.key
    return
  }
  openDB(db)
}

function onNodeDblClick(data) {
  if (data.type !== 'table') {
    onNodeClick(data)
    return
  }
  onNodeClick(data)
  runSQL()
}

function closeTab(key) {
  const idx = tabs.value.findIndex((t) => t.key === key)
  if (idx < 0) return
  tabs.value.splice(idx, 1)
  if (activeKey.value === key) {
    const next = tabs.value[idx] || tabs.value[idx - 1]
    activeKey.value = next?.key || ''
  }
}

function addTab() {
  const cur = current.value
  const db = databases.value.find((d) => d.id === cur?.dbId)
  if (!db) return
  const tab = newTab(db, { sql: '' })
  tabs.value.push(tab)
  activeKey.value = tab.key
}

function onTabChange() {}

function cycleEnv() {
  const cur = current.value
  if (!cur || envPeers.value.length < 2) return
  const idx = envPeers.value.findIndex((d) => d.id === cur.dbId)
  const next = envPeers.value[(idx + 1) % envPeers.value.length]
  const tab = tabs.value.find((t) => t.key === activeKey.value)
  if (!tab) return
  const inst = instMap.value[next.instance_id] || {}
  tab.dbId = next.id
  tab.dbName = next.db_name
  tab.instanceId = next.instance_id
  tab.dbType = inst.db_type || tab.dbType
  tab.env = inst.idc || ''
  tab.tag = dbTag(next)
  tab.result = null
  tab.error = ''
}

function formatSQL() {
  const tab = current.value
  if (!tab) return
  tab.sql = String(tab.sql || '')
    .replace(/\s+/g, ' ')
    .replace(/\s*;\s*/g, ';\n')
    .replace(/\b(SELECT|FROM|WHERE|AND|OR|JOIN|LEFT|RIGHT|INNER|GROUP BY|ORDER BY|LIMIT|UPDATE|SET|INSERT INTO|VALUES|ALTER TABLE|ADD|DROP|CREATE|GRANT|TO|SHOW|EXPLAIN)\b/gi, '\n$1')
    .replace(/\n+/g, '\n')
    .trim()
}

function onSqlKey(e) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault()
    runSQL()
  }
}

async function runSQL(tab = current.value) {
  if (!tab?.sql?.trim() || !tab.dbId) return
  running.value = true
  tab.error = ''
  try {
    const { data } = await api.post('/query', { database_id: tab.dbId, sql: tab.sql, limit: 200 })
    tab.result = data.data || { columns: [], rows: [] }
    pushHistory(tab)
  } catch (err) {
    tab.result = null
    tab.error = err.response?.data?.message || err.message || '查询失败'
  } finally {
    running.value = false
  }
}

async function exportDQL() {
  const tab = current.value
  if (!tab?.dbId || !tab.sql?.trim()) {
    ElMessage.warning('请先选择数据库并填写 SQL')
    return
  }
  try {
    await ElMessageBox.confirm('导出表数据需发起导出数据工单，是否继续？', '数据导出', {
      confirmButtonText: '发起导出数据工单',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
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
  router.push('/ops/dql')
}

function pushHistory(tab) {
  history.value = [
    { id: `${Date.now()}`, dbId: tab.dbId, dbName: tab.dbName, sql: tab.sql, ts: Date.now() },
    ...history.value.filter((h) => h.sql !== tab.sql || h.dbId !== tab.dbId),
  ].slice(0, 50)
  saveJSON(HIST_KEY, history.value)
}

function openFavorite() {
  if (!current.value?.sql?.trim()) {
    ElMessage.warning('请先输入 SQL')
    return
  }
  favName.value = current.value.dbName || '未命名'
  favVisible.value = true
}

function saveFavorite() {
  const tab = current.value
  if (!tab) return
  favorites.value = [
    {
      id: `${Date.now()}`,
      name: favName.value.trim() || tab.dbName,
      dbId: tab.dbId,
      dbName: tab.dbName,
      sql: tab.sql,
      ts: Date.now(),
    },
    ...favorites.value,
  ]
  saveJSON(FAV_KEY, favorites.value)
  favVisible.value = false
  ElMessage.success('已收藏')
}

function removeFav(id) {
  favorites.value = favorites.value.filter((f) => f.id !== id)
  saveJSON(FAV_KEY, favorites.value)
}

function applyStore(item) {
  const db = databases.value.find((d) => d.id === item.dbId)
  if (db) {
    const tab = openDB(db, { sql: item.sql })
    tab.sql = item.sql
    return
  }
  if (current.value) current.value.sql = item.sql
}

let dragCleanup = null

function stopDrag() {
  if (dragCleanup) {
    dragCleanup()
    dragCleanup = null
  }
}

function startDrag(kind, e) {
  e.preventDefault()
  stopDrag()
  const sx = e.clientX
  const sy = e.clientY
  const startLeft = leftWidth.value
  const startHist = histWidth.value
  const startEditor = editorHeight.value
  const move = (ev) => {
    if (kind === 'left') leftWidth.value = Math.min(520, Math.max(200, startLeft + ev.clientX - sx))
    if (kind === 'hist') histWidth.value = Math.min(480, Math.max(180, startHist - (ev.clientX - sx)))
    if (kind === 'editor') editorHeight.value = Math.min(640, Math.max(200, startEditor + ev.clientY - sy))
  }
  const up = () => stopDrag()
  window.addEventListener('mousemove', move)
  window.addEventListener('mouseup', up)
  window.addEventListener('blur', up)
  dragCleanup = () => {
    window.removeEventListener('mousemove', move)
    window.removeEventListener('mouseup', up)
    window.removeEventListener('blur', up)
  }
}

async function loadTree() {
  treeLoading.value = true
  try {
    const [dbRes, instRes] = await Promise.all([api.get('/databases'), api.get('/instances')])
    databases.value = dbRes.data.data || []
    instances.value = instRes.data.data || []
    const envs = [...new Set(instances.value.map((i) => i.idc).filter(Boolean))].sort(cmpName)
    envOptions.value = envs
  } finally {
    treeLoading.value = false
  }
}

watch([tabs, activeKey, leftWidth, editorHeight, histWidth], persistWorkspace, { deep: true })

function releasePageLocks() {
  stopDrag()
  favVisible.value = false
  running.value = false
  treeLoading.value = false
}

onMounted(loadTree)
onActivated(() => {
  if (!databases.value.length) loadTree()
})
onDeactivated(releasePageLocks)
onUnmounted(releasePageLocks)
</script>

<style scoped>
.query-page {
  position: relative;
  z-index: 0;
  isolation: isolate;
  margin: -16px;
  width: calc(100% + 32px);
  height: calc(100% + 32px);
  overflow: hidden !important;
  display: flex;
  flex-direction: column;
  background: #fff;
}
.query-body {
  flex: 1;
  min-height: 0;
  min-width: 0;
  display: flex;
  position: relative;
  overflow: hidden;
  contain: paint;
  box-shadow: inset 0 0 10px rgba(0, 0, 0, 0.04);
}
.query-wm {
  position: absolute;
  inset: 0;
  overflow: hidden;
  pointer-events: none !important;
  z-index: 0;
  display: flex;
  flex-wrap: wrap;
  align-content: flex-start;
}
.query-wm span {
  pointer-events: none !important;
  display: inline-block;
  margin: 48px 70px;
  opacity: 0.07;
  font-size: 16px;
  font-weight: 300;
  transform: rotate(-22deg);
  color: #000;
  user-select: none;
}
.pane-left,
.pane-right,
.editor-col,
.hist-col,
.result-col {
  position: relative;
  z-index: 2;
}
.pane-left {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: #fff;
}
.tree-search {
  padding: 16px 8px 8px;
  flex-shrink: 0;
  overflow: visible;
  z-index: 4;
}
.tree-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 0 8px 12px 12px;
}
.tree-node {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  width: 100%;
}
.tree-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tree-tag {
  color: #6b7280;
  flex-shrink: 0;
  font-size: 12px;
}
.muted { color: #9ca3af; }
.tree-empty {
  color: #9ca3af;
  text-align: center;
  padding: 48px 8px 0;
  font-size: 13px;
}
.table-ico { display: inline-flex; line-height: 0; }
.pane-right {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
}
.empty-main {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #2563eb;
  font-size: 16px;
  gap: 8px;
}
.bolt { width: 26px; height: 26px; }
.work-tabs {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  border-bottom: 1px solid #f3f4f6;
  padding-right: 8px;
}
.work-tabs :deep(.el-tabs) { flex: 1; min-width: 0; }
.work-tabs :deep(.el-tabs__header) { margin: 0; }
.work-tabs :deep(.el-tabs__nav-wrap::after) { display: none; }
.work-tabs :deep(.el-tabs__item) { height: 36px; }
.tab-lab {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
}
.tab-time { color: #9ca3af; margin-left: 4px; }
.tab-tag { color: #6b7280; }
.tab-add { font-size: 18px; padding: 0 8px; }
.editor-split {
  display: flex;
  flex-shrink: 0;
  min-height: 0;
  overflow: hidden;
}
.editor-col {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.tool-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px 12px;
  border-bottom: 1px solid #e5e7eb;
  flex-shrink: 0;
}
.tool-left { display: flex; gap: 12px; }
.btn-ico { margin-right: 4px; }
.db-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  flex-shrink: 0;
}
.db-name { font-size: 14px; color: #111827; }
.env-swap { color: #6b7280; cursor: pointer; }
.env-swap:hover { color: #2563eb; }
.sql-box {
  flex: 1;
  min-height: 0;
  width: 100%;
  border: none;
  resize: none;
  padding: 0 20px 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
  line-height: 1.6;
  outline: none;
  color: #111827;
  background: transparent;
}
.hist-col {
  flex-shrink: 0;
  min-width: 0;
  border-left: 1px solid transparent;
  padding: 12px 12px 8px;
  display: flex;
  flex-direction: column;
}
.store-tabs { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.store-tabs :deep(.el-tabs__header) { margin-bottom: 8px; }
.store-tabs :deep(.el-tabs__content) { flex: 1; min-height: 0; overflow: hidden; }
.store-tabs :deep(.el-tab-pane) { height: 100%; display: flex; flex-direction: column; gap: 8px; }
.store-list { flex: 1; min-height: 0; overflow: auto; }
.store-item {
  padding: 8px;
  border-radius: 4px;
  cursor: pointer;
}
.store-item:hover { background: #f5f7fa; }
.store-title { display: flex; justify-content: space-between; align-items: center; font-size: 13px; }
.store-sub { color: #9ca3af; font-size: 12px; margin-top: 2px; }
.store-sql {
  color: #6b7280;
  font-size: 12px;
  margin-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.store-empty { color: #9ca3af; text-align: center; padding: 24px 0; font-size: 13px; }
.result-col {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 8px 12px 12px;
}
.result-meta {
  flex-shrink: 0;
  color: #6b7280;
  font-size: 12px;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.result-err { color: #dc2626; font-size: 13px; padding: 12px; }
.result-placeholder {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #9ca3af;
  font-size: 13px;
}
.gutter-x {
  width: 6px;
  flex-shrink: 0;
  cursor: col-resize;
  background: rgba(0, 0, 0, 0.04);
  border-left: 1px solid #eee;
  z-index: 3;
  position: relative;
}
.gutter-x::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 0;
  width: 6px;
  height: 30px;
  margin-top: -15px;
  background: rgba(0, 0, 0, 0.06);
}
.gutter-y {
  height: 6px;
  flex-shrink: 0;
  cursor: row-resize;
  background: rgba(0, 0, 0, 0.04);
  border-top: 1px solid #eee;
  z-index: 3;
  position: relative;
}
.gutter-y::after {
  content: '';
  position: absolute;
  left: 50%;
  top: 0;
  width: 30px;
  height: 6px;
  margin-left: -15px;
  background: rgba(0, 0, 0, 0.06);
}
:deep(.hit) { color: #ef4444; }
:deep(.el-tree-node__content) { height: 28px; }
</style>
