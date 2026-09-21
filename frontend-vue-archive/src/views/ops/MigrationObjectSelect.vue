<template>
  <div class="obj-wrap">
    <el-card shadow="never" class="obj-card left">
      <template #header><span class="card-title">源库对象</span></template>
      <template v-if="!instanceId">
        <span class="muted">请先在第二步选择源端实例</span>
      </template>
      <template v-else>
        <el-input v-model="exclude" clearable placeholder="排除规则，如 tmp_*" class="mb" />
        <el-input v-model="leftFilter" clearable placeholder="搜索库名、表名，支持模糊；展开后可搜子级表名" class="mb" />
        <div class="hint-bar">共 {{ treeData.length }} 个库，当前展示 {{ filteredTree.length }} 个{{ leftFilter.trim() ? '（已按搜索过滤）' : '' }}</div>
        <el-tree
          ref="srcTree"
          class="src-tree"
          :data="filteredTree"
          node-key="key"
          show-checkbox
          :props="{ label: 'label', children: 'children', disabled: 'disabled' }"
          :default-expanded-keys="expandedKeys"
          :default-checked-keys="checkedKeys"
          @check="onCheck"
          @node-expand="onExpand"
        />
        <div class="foot-links">
          <el-button link type="primary" @click="refreshRoots">刷新</el-button>
          <el-button link type="primary" @click="selectAllDbs">全选库</el-button>
          <el-button link type="primary" @click="clearAll">清空</el-button>
        </div>
      </template>
    </el-card>

    <div class="obj-mid">
      <el-button type="primary" circle disabled title="左侧勾选的对象会自动出现在右侧已选列表">
        <el-icon><ArrowRight /></el-icon>
      </el-button>
      <el-button circle :disabled="!objects.length" title="清空已选（取消左侧全部勾选）" @click="clearAll">
        <el-icon><ArrowLeft /></el-icon>
      </el-button>
    </div>

    <el-card shadow="never" class="obj-card right">
      <template #header>
        <span class="card-title">已选对象</span>
        <el-tooltip content="目标库、目标表请点击行内编辑图标在弹窗中修改；批量规则请展开「批量操作」。" placement="top">
          <el-icon class="info-ico"><InfoFilled /></el-icon>
        </el-tooltip>
      </template>
      <template v-if="!instanceId">
        <span class="muted">请先选择源端实例</span>
      </template>
      <template v-else>
        <div class="sum">
          已选 <b>{{ dbCount }}</b> 库、<b>{{ objects.length }}</b> 表，预估 <b>{{ formatBytes(totalBytes) }}</b>
        </div>
        <el-input v-model="rightFilter" clearable placeholder="全局搜索原对象名、目标名，支持模糊匹配" class="mb" />
        <div v-if="!objects.length" class="muted">
          在左侧勾选库表后，将在此以树状展示映射；请点击「编辑」在弹窗中修改目标库、目标表。
        </div>
        <template v-else>
          <el-collapse class="batch-box">
            <el-collapse-item name="batch">
              <template #title>
                <span>批量操作</span>
                <span class="batch-sub">按源库改目标库 · 前缀/后缀生成目标表名</span>
              </template>
              <p class="muted tiny">以下操作作用于当前已选全部表级记录（含整库展开后的每一行）。</p>
              <div class="batch-title">按源库批量改目标库</div>
              <el-select v-model="bulkSourceDb" clearable placeholder="选择源库" style="width:100%" class="mb">
                <el-option v-for="d in sourceDbOptions" :key="d" :label="d" :value="d" />
              </el-select>
              <el-input v-model="bulkTargetDb" placeholder="目标库名" class="mb" />
              <el-button type="primary" style="width:100%" @click="applyBulkTargetDb">应用到该源库下全部表</el-button>
              <el-divider />
              <div class="batch-title">按规则批量生成目标表名</div>
              <p class="muted tiny">公式：目标表名 = 前缀 + 源表名 + 后缀</p>
              <el-input v-model="tablePrefix" placeholder="前缀（可选）" class="mb" />
              <el-input v-model="tableSuffix" placeholder="后缀（可选）" class="mb" />
              <el-button style="width:100%" @click="applyPrefixSuffix">应用到全部已选表</el-button>
            </el-collapse-item>
          </el-collapse>
          <el-tree
            class="right-tree"
            :data="rightTree"
            node-key="key"
            default-expand-all
            :expand-on-click-node="false"
          >
            <template #default="{ data }">
              <div class="right-node" @click.stop>
                <div v-if="data.kind === 'whole_db'">
                  <div class="sub">源：{{ data.source_db_name }}（整库 {{ data.tableCount }} 张表）</div>
                  <div class="map-row">
                    <span class="sub">目标库</span>
                    <b class="ellip" :title="data.target_db_name">{{ data.target_db_name }}</b>
                    <el-button link type="primary" @click="openEditDb(data)">
                      <el-icon><EditPen /></el-icon>
                    </el-button>
                  </div>
                  <div class="sub">整库迁移 · 目标表 —</div>
                </div>
                <div v-else-if="data.kind === 'db'">
                  <div class="sub">源库：{{ data.source_db_name }}</div>
                  <div class="map-row">
                    <span class="sub">目标库</span>
                    <template v-if="data.commonTd">
                      <b class="ellip" :title="data.commonTd">{{ data.commonTd }}</b>
                      <el-button link type="primary" @click="openEditDbGroup(data)">
                        <el-icon><EditPen /></el-icon>
                      </el-button>
                    </template>
                    <span v-else class="warn">各表目标库不一致，请在子行分别编辑</span>
                  </div>
                </div>
                <div v-else class="map-row table-row">
                  <span class="sub">源表 {{ data.source_table_name }}</span>
                  <span class="grow" />
                  <template v-if="!data.commonTd">
                    <span class="sub">目标库</span>
                    <b class="ellip sm" :title="data.target_db_name">{{ data.target_db_name }}</b>
                  </template>
                  <span class="sub">目标表</span>
                  <b class="ellip sm" :title="data.target_table_name">{{ data.target_table_name }}</b>
                  <el-button link type="primary" @click="openEditTable(data)">
                    <el-icon><EditPen /></el-icon>
                  </el-button>
                </div>
              </div>
            </template>
          </el-tree>
        </template>
        <div class="foot-links">
          <el-button link type="primary" @click="expandAllRight">展开全部</el-button>
          <el-button link type="primary" @click="collapseAllRight">收起全部</el-button>
          <el-button link type="primary" @click="selectAllDbs">全选库</el-button>
          <el-button link type="primary" @click="clearAll">清空</el-button>
          <el-button link type="primary" :disabled="!objects.length" @click="restoreNames">恢复原名称</el-button>
        </div>
        <p class="muted tiny foot-note">
          目标库表名将随创建任务时的 objects 提交；同一任务内目标「库.表」组合不可重复。按源库成树展示；修改请点击「编辑」图标。
        </p>
      </template>
    </el-card>

    <el-dialog v-model="editOpen" :title="editMode === 'db' ? '编辑库映射' : '编辑表映射'" width="480px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="原库名"><el-input :model-value="editSourceDb" disabled /></el-form-item>
        <el-form-item v-if="editMode === 'table'" label="原表名"><el-input :model-value="editSourceTable" disabled /></el-form-item>
        <el-form-item v-if="editMode === 'db'" label="库名" required>
          <el-input v-model="editTargetDb" placeholder="请输入目标库名" />
        </el-form-item>
        <template v-else>
          <el-form-item label="目标库名">
            <el-input :model-value="editTargetDb" disabled />
            <div class="muted tiny">改库名请在已选对象中点击库节点的「编辑」。</div>
          </el-form-item>
          <el-form-item label="目标表名" required>
            <el-input v-model="editTargetTable" placeholder="请输入目标表名" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="editOpen = false">取消</el-button>
        <el-button type="primary" @click="confirmEdit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeft, ArrowRight, EditPen, InfoFilled } from '@element-plus/icons-vue'
import api from '../../api/client'

const props = defineProps({
  instanceId: { type: String, default: '' },
  objects: { type: Array, default: () => [] },
  wholeDbs: { type: Array, default: () => [] },
})
const emit = defineEmits(['update:objects', 'update:wholeDbs'])

const srcTree = ref()
const treeData = ref([])
const expandedKeys = ref([])
const checkedKeys = ref([])
const exclude = ref('')
const leftFilter = ref('')
const rightFilter = ref('')
const loaded = new Map()
const bulkSourceDb = ref()
const bulkTargetDb = ref('')
const tablePrefix = ref('')
const tableSuffix = ref('')
const rightExpand = ref([])

const editOpen = ref(false)
const editMode = ref('db')
const editSourceDb = ref('')
const editSourceTable = ref('')
const editTargetDb = ref('')
const editTargetTable = ref('')
const editTableCount = ref(0)

const objects = computed(() => props.objects || [])
const wholeDbs = computed(() => props.wholeDbs || [])

function wildcardToRegex(pattern) {
  const t = String(pattern || '').trim()
  if (!t) return null
  const escaped = t.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.')
  try {
    return new RegExp(`^${escaped}$`, 'i')
  } catch {
    return null
  }
}

const excludeRe = computed(() => wildcardToRegex(exclude.value))

function formatBytes(n) {
  const bytes = Number(n) || 0
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(2)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}

function tableKey(db, table) {
  return `db:${db}:t:${table}`
}
function parseTableKey(key) {
  const s = String(key)
  if (!s.includes(':t:')) return null
  const rest = s.slice(3)
  const idx = rest.indexOf(':t:')
  if (idx < 0) return null
  return { db: rest.slice(0, idx), table: rest.slice(idx + 3) }
}
function isDbKey(key) {
  const s = String(key)
  return s.startsWith('db:') && !s.includes(':t:')
}

function applyExclude(nodes) {
  const ex = excludeRe.value
  return nodes.map((n) => {
    if (!n.children) return n
    return {
      ...n,
      children: n.children.map((c) => {
        const p = parseTableKey(c.key)
        const disabled = Boolean(ex && p && (ex.test(`${p.db}.${p.table}`) || ex.test(p.table)))
        return { ...c, disabled }
      }),
    }
  })
}

const filteredTree = computed(() => {
  const base = applyExclude(treeData.value)
  const q = leftFilter.value.trim().toLowerCase()
  if (!q) return base
  return base
    .map((n) => {
      const self = String(n.label).toLowerCase().includes(q)
      const ch = (n.children || []).filter((c) => String(c.label).toLowerCase().includes(q))
      if (self || ch.length) return { ...n, children: ch.length ? ch : n.children }
      return null
    })
    .filter(Boolean)
})

const totalBytes = computed(() => objects.value.reduce((s, o) => s + (Number(o.total_bytes) || 0), 0))
const dbCount = computed(() => new Set(objects.value.map((o) => o.source_db_name)).size)
const sourceDbOptions = computed(() => [...new Set(objects.value.map((o) => o.source_db_name))].sort())

const objectsForRight = computed(() => {
  const q = rightFilter.value.trim().toLowerCase()
  if (!q) return objects.value
  return objects.value.filter((o) => {
    const blob = `${o.source_db_name}.${o.source_table_name} ${o.target_db_name} ${o.target_table_name}`.toLowerCase()
    return blob.includes(q)
  })
})

const rightTree = computed(() => {
  const byDb = new Map()
  for (const o of objectsForRight.value) {
    if (!byDb.has(o.source_db_name)) byDb.set(o.source_db_name, [])
    byDb.get(o.source_db_name).push(o)
  }
  const whole = new Set(wholeDbs.value)
  const nodes = []
  for (const db of [...byDb.keys()].sort()) {
    const list = byDb.get(db).slice().sort((a, b) => a.source_table_name.localeCompare(b.source_table_name))
    if (whole.has(db)) {
      nodes.push({
        key: `right-whole:${db}`,
        label: db,
        kind: 'whole_db',
        source_db_name: db,
        target_db_name: list[0]?.target_db_name || db,
        tableCount: list.length,
      })
      continue
    }
    const tds = new Set(list.map((o) => o.target_db_name))
    const commonTd = tds.size === 1 ? list[0].target_db_name : null
    nodes.push({
      key: `right-db:${db}`,
      label: db,
      kind: 'db',
      source_db_name: db,
      commonTd,
      children: list.map((o) => ({
        key: `right-t:${o.source_db_name}\0${o.source_table_name}`,
        label: o.source_table_name,
        kind: 'table',
        commonTd,
        ...o,
      })),
    })
  }
  return nodes
})

async function fetchDbs() {
  if (!props.instanceId) return []
  const { data } = await api.get(`/migration/instances/${props.instanceId}/databases`)
  return (data.data || []).filter((d) => !d.system).map((d) => d.name || d.db_name).filter(Boolean)
}

async function fetchTables(db) {
  if (!props.instanceId || !db) return []
  if (loaded.has(db)) return loaded.get(db)
  const { data } = await api.get(`/migration/instances/${props.instanceId}/databases/${encodeURIComponent(db)}/tables`)
  const raw = data.data || []
  const tables = raw.map((t) => {
    if (typeof t === 'string') return { table_name: t, total_bytes: 0 }
    return { table_name: t.name || t.table_name, total_bytes: Number(t.total_bytes || t.bytes || 0) }
  }).filter((t) => t.table_name)
  loaded.set(db, tables)
  return tables
}

function patchDbChildren(db, tables) {
  const key = `db:${db}`
  treeData.value = treeData.value.map((n) => {
    if (n.key !== key) return n
    return {
      ...n,
      children: tables.map((t) => ({
        key: tableKey(db, t.table_name),
        label: t.table_name,
        isLeaf: true,
        bytes: t.total_bytes || 0,
      })),
    }
  })
}

async function refreshRoots() {
  loaded.clear()
  treeData.value = []
  expandedKeys.value = []
  checkedKeys.value = []
  emit('update:objects', [])
  emit('update:wholeDbs', [])
  if (!props.instanceId) return
  const dbs = await fetchDbs()
  treeData.value = dbs.map((d) => ({ key: `db:${d}`, label: d, isLeaf: false }))
}

async function onExpand(node) {
  if (!isDbKey(node.key)) return
  const db = String(node.key).slice(3)
  if (loaded.has(db)) return
  const tables = await fetchTables(db)
  patchDbChildren(db, tables)
}

async function ensureDbTables(db) {
  const tables = await fetchTables(db)
  patchDbChildren(db, tables)
  return tables
}

function emitFromKeys(keys, wholeAdds) {
  const ex = excludeRe.value
  const rows = []
  for (const k of keys) {
    const p = parseTableKey(k)
    if (!p) continue
    if (ex && (ex.test(`${p.db}.${p.table}`) || ex.test(p.table))) continue
    const tables = loaded.get(p.db) || []
    const meta = tables.find((t) => t.table_name === p.table)
    const prev = objects.value.find((o) => o.source_db_name === p.db && o.source_table_name === p.table)
    rows.push({
      source_db_name: p.db,
      source_table_name: p.table,
      target_db_name: prev?.target_db_name || p.db,
      target_table_name: prev?.target_table_name || p.table,
      total_bytes: meta?.total_bytes || prev?.total_bytes || 0,
    })
  }
  rows.sort((a, b) => a.source_db_name.localeCompare(b.source_db_name) || a.source_table_name.localeCompare(b.source_table_name))
  const keySet = new Set(keys.map(String))
  let whole = [...wholeDbs.value]
  if (wholeAdds?.length) whole = [...new Set([...whole, ...wholeAdds])]
  whole = whole.filter((db) => {
    const forDb = rows.filter((r) => r.source_db_name === db)
    return forDb.length && forDb.every((r) => keySet.has(tableKey(r.source_db_name, r.source_table_name)))
  })
  emit('update:objects', rows)
  emit('update:wholeDbs', whole)
}

async function resolveToTableKeys(rawKeys, wholeDbsToLoad) {
  const leaf = []
  for (const k of rawKeys) {
    if (parseTableKey(k)) leaf.push(String(k))
  }
  const dbs = wholeDbsToLoad?.length
    ? wholeDbsToLoad
    : rawKeys.filter(isDbKey).map((k) => String(k).slice(3))
  for (const db of dbs) {
    const tables = await ensureDbTables(db)
    for (const t of tables) {
      if (excludeRe.value && (excludeRe.value.test(`${db}.${t.table_name}`) || excludeRe.value.test(t.table_name))) continue
      leaf.push(tableKey(db, t.table_name))
    }
  }
  return [...new Set(leaf)]
}

async function onCheck(_data, info) {
  const raw = info.checkedKeys || []
  const nodeKey = info.node?.key
  if (info.checked && isDbKey(nodeKey)) {
    const keys = await resolveToTableKeys(raw, [String(nodeKey).slice(3)])
    checkedKeys.value = keys
    srcTree.value?.setCheckedKeys(keys)
    emitFromKeys(keys, [String(nodeKey).slice(3)])
    return
  }
  const keys = raw.filter((k) => !isDbKey(k))
  checkedKeys.value = keys
  emitFromKeys(keys, null)
}

async function selectAllDbs() {
  const dbs = treeData.value.map((n) => String(n.key).slice(3))
  const keys = await resolveToTableKeys(dbs.map((d) => `db:${d}`), dbs)
  checkedKeys.value = keys
  srcTree.value?.setCheckedKeys(keys)
  emitFromKeys(keys, dbs)
}

function clearAll() {
  checkedKeys.value = []
  srcTree.value?.setCheckedKeys([])
  emit('update:objects', [])
  emit('update:wholeDbs', [])
}

function setObjects(next) {
  emit('update:objects', next)
}

function applyBulkTargetDb() {
  const src = String(bulkSourceDb.value || '').trim()
  const tgt = bulkTargetDb.value.trim()
  if (!src) return ElMessage.warning('请选择源库')
  if (!tgt) return ElMessage.warning('请输入要批量应用的目标库名')
  const next = objects.value.map((o) => (o.source_db_name === src ? { ...o, target_db_name: tgt } : o))
  setObjects(next)
  ElMessage.success(`已将源库「${src}」下表的目标库设为「${tgt}」`)
}

function applyPrefixSuffix() {
  if (!objects.value.length) return ElMessage.warning('暂无已选对象')
  const next = objects.value.map((o) => ({
    ...o,
    target_table_name: `${tablePrefix.value}${o.source_table_name}${tableSuffix.value}`,
  }))
  setObjects(next)
  ElMessage.success('已按「前缀 + 源表名 + 后缀」更新目标表名')
}

function restoreNames() {
  if (!objects.value.length) return ElMessage.warning('暂无已选对象')
  setObjects(objects.value.map((o) => ({ ...o, target_db_name: o.source_db_name, target_table_name: o.source_table_name })))
  ElMessage.success('已将目标库表名恢复为与源端一致')
}

function openEditDb(data) {
  editMode.value = 'db'
  editSourceDb.value = data.source_db_name
  editTableCount.value = data.tableCount
  editTargetDb.value = data.target_db_name
  editOpen.value = true
}
function openEditDbGroup(data) {
  editMode.value = 'db'
  editSourceDb.value = data.source_db_name
  editTableCount.value = (objects.value.filter((o) => o.source_db_name === data.source_db_name) || []).length
  editTargetDb.value = data.commonTd || ''
  editOpen.value = true
}
function openEditTable(data) {
  editMode.value = 'table'
  editSourceDb.value = data.source_db_name
  editSourceTable.value = data.source_table_name
  editTargetDb.value = data.target_db_name
  editTargetTable.value = data.target_table_name
  editOpen.value = true
}
function confirmEdit() {
  if (editMode.value === 'db') {
    const v = editTargetDb.value.trim()
    if (!v) return ElMessage.warning('请输入库名')
    setObjects(objects.value.map((o) => (o.source_db_name === editSourceDb.value ? { ...o, target_db_name: v } : o)))
  } else {
    const t = editTargetTable.value.trim()
    if (!t) return ElMessage.warning('请填写目标表名')
    setObjects(objects.value.map((o) => (
      o.source_db_name === editSourceDb.value && o.source_table_name === editSourceTable.value
        ? { ...o, target_table_name: t }
        : o
    )))
  }
  editOpen.value = false
  ElMessage.success('已更新映射')
}

function expandAllRight() {
  rightExpand.value = rightTree.value.map((n) => n.key)
}
function collapseAllRight() {
  rightExpand.value = []
}

watch(() => props.instanceId, () => { refreshRoots() }, { immediate: true })
watch(excludeRe, () => {
  if (checkedKeys.value.length) emitFromKeys(checkedKeys.value, null)
})
</script>

<style scoped>
.obj-wrap {
  display: flex;
  align-items: stretch;
  gap: 12px;
  width: 100%;
  min-height: 520px;
}
.obj-card { border-radius: 8px; }
.obj-card.left { flex: 0 1 360px; min-width: 260px; }
.obj-card.right { flex: 1 1 0; min-width: 0; }
.obj-card :deep(.el-card__header) { display: flex; align-items: center; gap: 6px; padding: 10px 16px; }
.card-title { font-weight: 500; }
.info-ico { color: #999; margin-left: 4px; }
.obj-mid {
  width: 56px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
}
.mb { margin-bottom: 8px; }
.hint-bar {
  margin-bottom: 8px;
  padding: 6px 12px;
  font-size: 12px;
  color: rgba(0,0,0,.75);
  background: #e6f4ff;
  border: 1px solid #91caff;
  border-radius: 4px;
}
.src-tree, .right-tree { max-height: 400px; overflow: auto; }
.foot-links { margin-top: 8px; padding-top: 8px; border-top: 1px solid #f3f4f6; display: flex; flex-wrap: wrap; gap: 4px 8px; }
.sum { font-size: 12px; color: #4b5563; margin-bottom: 8px; }
.muted { color: #9ca3af; font-size: 13px; }
.tiny { font-size: 12px; }
.foot-note { margin: 8px 0 0; }
.batch-box { margin-bottom: 8px; border: 1px solid #f3f4f6; background: #fafafa; border-radius: 6px; }
.batch-sub { margin-left: 8px; font-weight: 400; font-size: 12px; color: #9ca3af; }
.batch-title { font-size: 13px; font-weight: 500; margin-bottom: 8px; }
.right-node { width: 100%; padding: 2px 0; }
.sub { font-size: 12px; color: #9ca3af; }
.map-row { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 8px; }
.table-row { width: 100%; }
.grow { flex: 1; }
.ellip { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ellip.sm { max-width: 140px; }
.warn { font-size: 12px; color: #d97706; }
</style>
