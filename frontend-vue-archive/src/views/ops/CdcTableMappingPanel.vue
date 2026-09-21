<template>
  <div class="map-panel">
    <div v-if="!mappings.length" class="tree-empty">左侧勾选或「分表匹配」后，已选映射会显示在这里</div>
    <template v-else>
      <div class="map-toolbar">
        <el-input v-model="search" clearable size="small" placeholder="搜索源表 / 目标表名">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
        <span class="muted">已加载字段 {{ loadedCount }}/{{ mappings.length }}</span>
        <el-button size="small" :loading="batchLoading" :disabled="!pendingSingleCount || !instanceId || !dbName" @click="loadAllColumns">
          加载全部字段{{ pendingSingleCount ? `（${pendingSingleCount}）` : '' }}
        </el-button>
        <el-button v-if="expanded.size" size="small" text type="primary" @click="expanded = new Set()">全部收起</el-button>
      </div>
      <el-alert
        v-if="hasMissingKey"
        type="error"
        show-icon
        :closable="false"
        class="mb"
        title="请点击下方映射卡片展开后勾选 KEY 列（可多选并调整顺序）；KEY 及其顺序会影响自动建表的数据分布与 SQL 查询性能。存在「KEY 未配置」时无法进入下一步，也可使用「加载全部字段」自动预选主键。"
      />
      <div v-if="!filtered.length" class="tree-empty">无匹配映射</div>
      <div v-else class="map-list">
        <div v-for="(m, index) in filtered" :key="m.key" class="map-item">
          <div class="map-head" @click="toggleExpand(m.key)">
            <div class="map-title">
              <el-icon class="caret"><ArrowDown v-if="isOpen(m.key)" /><ArrowRight v-else /></el-icon>
              <b>映射 {{ index + 1 }}</b>
              <el-tag v-if="m.is_shard_group" size="small" type="warning">分表组 {{ m.source_tables.length }} 张</el-tag>
              <el-tag v-else size="small">单表</el-tag>
              <span class="muted ellip">{{ m.is_shard_group ? `${m.shard_pattern || ''} = ${m.source_tables[0] || ''}` : m.source_tables[0] }}</span>
              <el-tag v-if="!isOpen(m.key) && !m.key_columns.length" size="small" type="warning">KEY 未配置</el-tag>
            </div>
            <div class="map-head-right">
              <span class="muted">KEY {{ m.key_columns.length }}{{ cache[m.key]?.columns?.length ? ` / ${cache[m.key].columns.length}` : '' }}</span>
              <el-button text type="danger" @click.stop="remove(m.key)">移除</el-button>
            </div>
          </div>
          <div class="target-row" @click.stop>
            <span class="muted target-lab">目标表</span>
            <el-input v-model="m.target_table" size="small" placeholder="StarRocks 目标表名" @change="emitChange" />
          </div>
          <div v-if="isOpen(m.key)" class="map-body" @click.stop>
            <div class="key-bar">
              <div>
                <b>KEY 列（多选）</b>
                <span class="muted">已选 {{ m.key_columns.length }} / {{ (cache[m.key]?.columns || []).length }}</span>
                <el-tag v-if="m.is_shard_group && cache[m.key]?.columns?.length" size="small" type="info">共有列</el-tag>
              </div>
              <div>
                <el-button v-if="(cache[m.key]?.columns || []).some((c) => c.is_primary_key)" size="small" text type="primary" @click="selectPKs(m)">选源端主键</el-button>
                <el-button size="small" text type="primary" :disabled="!m.key_columns.length" @click="patch(m.key, { key_columns: [], shard_keys: [] })">清空</el-button>
                <el-button size="small" text type="primary" :loading="!!cache[m.key]?.loading" @click="loadColumns(m, true)">刷新</el-button>
              </div>
            </div>
            <p class="key-hint">可多选 KEY 列并调整顺序；KEY 及顺序会影响自动建表的数据分布与 SQL 查询性能。</p>
            <div v-if="m.key_columns.length >= 2" class="order-row">
              <span class="muted">KEY 列顺序</span>
              <div class="order-chips">
                <span v-for="(col, i) in m.key_columns" :key="col" class="chip">
                  {{ col }}
                  <el-button text :disabled="i === 0" @click="moveKey(m, i, -1)"><el-icon><ArrowUp /></el-icon></el-button>
                  <el-button text :disabled="i === m.key_columns.length - 1" @click="moveKey(m, i, 1)"><el-icon><ArrowDown /></el-icon></el-button>
                </span>
              </div>
            </div>
            <p v-else-if="m.key_columns.length === 1" class="key-hint">再选 1 列 KEY 后可调整主键/排序键列顺序</p>
            <el-alert v-if="cache[m.key]?.error" type="warning" show-icon :closable="false" class="mb" :title="`字段加载失败：${cache[m.key].error}。可手动输入 KEY 列（逗号分隔）。`" />
            <el-input
              v-if="cache[m.key]?.error || (cache[m.key] && !cache[m.key].loading && !(cache[m.key].columns || []).length)"
              :model-value="m.key_columns.join(',')"
              size="small"
              placeholder="手动输入 KEY 列名，如：id 或 id,tenant_id"
              @change="(v) => patch(m.key, { key_columns: String(v || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean) })"
            />
            <el-table v-else v-loading="!!cache[m.key]?.loading" :data="displayColumns(m)" size="small" max-height="220" empty-text="加载中或暂无字段">
              <el-table-column width="46">
                <template #header>
                  <el-checkbox
                    :model-value="allColsChecked(m)"
                    :indeterminate="someColsChecked(m)"
                    @change="(v) => applyKeys(m, v ? (cache[m.key]?.columns || []).map((c) => c.name) : [])"
                  />
                </template>
                <template #default="{ row }">
                  <el-checkbox :model-value="m.key_columns.includes(row.name)" @change="(v) => toggleCol(m, row.name, v)" />
                </template>
              </el-table-column>
              <el-table-column label="列名" min-width="120">
                <template #default="{ row }">
                  <span class="col-name">{{ row.name }}</span>
                  <el-icon v-if="row.is_primary_key" class="pk" title="源端主键"><Key /></el-icon>
                </template>
              </el-table-column>
              <el-table-column label="类型" min-width="110">
                <template #default="{ row }"><span class="muted">{{ row.data_type || '—' }}</span></template>
              </el-table-column>
              <el-table-column label="NULL" width="70">
                <template #default="{ row }">
                  <el-tag v-if="row.is_nullable === true" size="small">是</el-tag>
                  <el-tag v-else-if="row.is_nullable === false" size="small" type="warning">否</el-tag>
                  <span v-else class="muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="注释" min-width="100" show-overflow-tooltip>
                <template #default="{ row }"><span class="muted">{{ row.comment || '—' }}</span></template>
              </el-table-column>
              <el-table-column v-if="m.key_columns.length >= 2" label="顺序" width="88" align="center">
                <template #default="{ row }">
                  <template v-if="m.key_columns.includes(row.name)">
                    <el-button text :disabled="m.key_columns.indexOf(row.name) === 0" @click="moveKey(m, m.key_columns.indexOf(row.name), -1)"><el-icon><ArrowUp /></el-icon></el-button>
                    <el-button text :disabled="m.key_columns.indexOf(row.name) === m.key_columns.length - 1" @click="moveKey(m, m.key_columns.indexOf(row.name), 1)"><el-icon><ArrowDown /></el-icon></el-button>
                  </template>
                  <span v-else class="muted">—</span>
                </template>
              </el-table-column>
            </el-table>
            <div v-if="m.is_shard_group" class="shard-box">
              <div class="muted mb-sm">分桶键 DISTRIBUTED BY HASH（可选）</div>
              <el-checkbox :model-value="!!m.shard_keys?.length" @change="(v) => patch(m.key, { shard_keys: v ? [...(m.shard_keys?.length ? m.shard_keys : m.key_columns)] : [] })">
                自定义分桶键（默认与 KEY 列相同，无需配置）
              </el-checkbox>
              <div v-if="m.shard_keys?.length" class="shard-keys">
                <span v-if="!m.key_columns.length" class="muted">请先选择 KEY 列</span>
                <el-checkbox
                  v-for="col in m.key_columns"
                  :key="col"
                  :model-value="m.shard_keys.includes(col)"
                  @change="(v) => toggleShard(m, col, v)"
                >{{ col }}</el-checkbox>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowDown, ArrowRight, ArrowUp, Key, Search } from '@element-plus/icons-vue'
import api from '../../api/client'

const props = defineProps({
  mappings: { type: Array, default: () => [] },
  instanceId: { type: String, default: '' },
  dbName: { type: String, default: '' },
  schema: { type: String, default: 'public' },
})
const emit = defineEmits(['update:mappings'])

const search = ref('')
const expanded = ref(new Set())
const cache = reactive({})
const batchLoading = ref(false)
const pkApplied = new Set()

const mappings = computed(() => props.mappings || [])
const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return mappings.value
  return mappings.value.filter((m) => (
    String(m.target_table || '').toLowerCase().includes(q)
    || (m.source_tables || []).some((t) => String(t).toLowerCase().includes(q))
    || String(m.shard_pattern || '').toLowerCase().includes(q)
  ))
})
const loadedCount = computed(() => mappings.value.filter((m) => (cache[m.key]?.columns || []).length > 0).length)
const pendingSingleCount = computed(() => mappings.value.filter((m) => !m.is_shard_group && m.source_tables?.length === 1 && !(cache[m.key]?.columns || []).length).length)
const hasMissingKey = computed(() => mappings.value.some((m) => !(m.key_columns || []).length))

function emitChange() {
  emit('update:mappings', mappings.value.slice())
}
function patch(key, next) {
  emit('update:mappings', mappings.value.map((m) => (m.key === key ? { ...m, ...next } : m)))
}
function remove(key) {
  delete cache[key]
  const next = new Set(expanded.value)
  next.delete(key)
  expanded.value = next
  pkApplied.delete(key)
  emit('update:mappings', mappings.value.filter((m) => m.key !== key))
}
function isOpen(key) {
  return expanded.value.has(key)
}
function toggleExpand(key) {
  const next = new Set(expanded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expanded.value = next
}

function normalizeCols(raw) {
  return (raw || []).map((c) => ({
    name: c.name || c.column_name,
    data_type: c.data_type || c.udt_name || '',
    is_nullable: typeof c.is_nullable === 'boolean' ? c.is_nullable : c.is_nullable === 'YES',
    is_primary_key: !!c.is_primary_key,
    comment: c.comment || '',
  })).filter((c) => c.name)
}

function intersectCols(groups) {
  if (!groups.length) return []
  const maps = groups.map((g) => new Map(g.map((c) => [c.name, c])))
  return groups[0].filter((c) => maps.every((m) => m.has(c.name)))
}

async function fetchOne(table) {
  const { data } = await api.get(
    `/cdc-sync/instances/${props.instanceId}/databases/${encodeURIComponent(props.dbName)}/tables/${encodeURIComponent(table)}/columns`,
    { params: { schema: props.schema || 'public' } },
  )
  return normalizeCols(data.data?.columns || data.columns)
}

async function fetchBatch(tables) {
  const { data } = await api.post(`/cdc-sync/instances/${props.instanceId}/columns/batch`, {
    schema: props.schema || 'public',
    db_name: props.dbName,
    tables,
  })
  const raw = data.data?.tables || {}
  const out = new Map()
  for (const [k, v] of Object.entries(raw)) out.set(k, normalizeCols(v))
  return out
}

async function loadColumns(m, force = false) {
  if (!props.instanceId || !props.dbName) return
  const existing = cache[m.key]
  if (!force && existing && !existing.error && existing.columns?.length) return
  cache[m.key] = { loading: true, columns: existing?.columns || [], error: '' }
  try {
    let cols = []
    if (m.is_shard_group && m.source_tables.length > 1) {
      const map = await fetchBatch(m.source_tables)
      cols = intersectCols(m.source_tables.map((t) => map.get(t) || []).filter((g) => g.length))
      if (!cols.length && m.source_tables[0]) cols = await fetchOne(m.source_tables[0])
    } else {
      cols = await fetchOne(m.source_tables[0])
    }
    cache[m.key] = { loading: false, columns: cols, error: '' }
    if (!pkApplied.has(m.key) && !(m.key_columns || []).length) {
      const pks = cols.filter((c) => c.is_primary_key).map((c) => c.name)
      if (pks.length) {
        pkApplied.add(m.key)
        patch(m.key, { key_columns: pks })
      }
    }
  } catch (e) {
    cache[m.key] = { loading: false, columns: [], error: e?.response?.data?.message || e.message || '字段加载失败' }
  }
}

async function loadAllColumns() {
  if (!props.instanceId || !props.dbName) return
  const targets = mappings.value.filter((m) => !m.is_shard_group && m.source_tables?.length === 1 && !(cache[m.key]?.columns || []).length)
  if (!targets.length) return
  batchLoading.value = true
  try {
    const map = await fetchBatch(targets.map((m) => m.source_tables[0]))
    const resolved = new Map()
    for (const m of targets) {
      let cols = map.get(m.source_tables[0]) || []
      if (!cols.length) {
        try { cols = await fetchOne(m.source_tables[0]) } catch { cols = [] }
      }
      resolved.set(m.key, cols)
      cache[m.key] = { loading: false, columns: cols, error: '' }
    }
    emit('update:mappings', mappings.value.map((m) => {
      if (m.is_shard_group || m.key_columns?.length) return m
      const cols = resolved.get(m.key) || []
      const pks = cols.filter((c) => c.is_primary_key).map((c) => c.name)
      if (!pks.length) return m
      pkApplied.add(m.key)
      return { ...m, key_columns: pks }
    }))
    const missing = targets.filter((m) => !(m.key_columns || []).length && !(resolved.get(m.key) || []).some((c) => c.is_primary_key))
    if (missing.length) {
      const next = new Set(expanded.value)
      missing.forEach((m) => next.add(m.key))
      expanded.value = next
      ElMessage.info(`${targets.length - missing.length} 张已自动预选主键；${missing.length} 张未检测到主键，已展开请手动勾选 KEY 列`)
    } else {
      ElMessage.success(`已加载 ${targets.length} 张表字段并预选主键`)
    }
  } finally {
    batchLoading.value = false
  }
}

function displayColumns(m) {
  const cols = cache[m.key]?.columns || []
  const keys = m.key_columns || []
  if (!keys.length) return cols
  const set = new Set(keys)
  return [...cols.filter((c) => set.has(c.name)).sort((a, b) => keys.indexOf(a.name) - keys.indexOf(b.name)), ...cols.filter((c) => !set.has(c.name))]
}
function allColsChecked(m) {
  const cols = cache[m.key]?.columns || []
  return cols.length > 0 && cols.every((c) => m.key_columns.includes(c.name))
}
function someColsChecked(m) {
  const cols = cache[m.key]?.columns || []
  const n = cols.filter((c) => m.key_columns.includes(c.name)).length
  return n > 0 && n < cols.length
}
function applyKeys(m, keys) {
  const keySet = new Set(keys)
  const shard = (m.shard_keys || []).filter((k) => keySet.has(k))
  patch(m.key, { key_columns: keys, shard_keys: shard })
}
function toggleCol(m, name, checked) {
  const keys = checked
    ? (m.key_columns.includes(name) ? m.key_columns : [...m.key_columns, name])
    : m.key_columns.filter((c) => c !== name)
  applyKeys(m, keys)
}
function selectPKs(m) {
  const pks = (cache[m.key]?.columns || []).filter((c) => c.is_primary_key).map((c) => c.name)
  if (pks.length) applyKeys(m, pks)
}
function moveKey(m, index, delta) {
  const next = [...m.key_columns]
  const j = index + delta
  if (j < 0 || j >= next.length) return
  ;[next[index], next[j]] = [next[j], next[index]]
  applyKeys(m, next)
}
function toggleShard(m, col, checked) {
  const set = new Set(m.shard_keys || [])
  if (checked) set.add(col)
  else set.delete(col)
  patch(m.key, { shard_keys: [...set] })
}

watch(expanded, (set) => {
  mappings.value.forEach((m) => {
    if (set.has(m.key) && !cache[m.key]) void loadColumns(m)
  })
}, { deep: true })
watch(() => mappings.value.map((m) => m.key).join(','), () => {
  mappings.value.forEach((m) => {
    if (expanded.value.has(m.key) && !cache[m.key]) void loadColumns(m)
  })
})
</script>

<style scoped>
.map-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 8px; }
.map-toolbar .el-input { flex: 1; min-width: 160px; }
.map-list { max-height: 600px; overflow: auto; display: flex; flex-direction: column; gap: 12px; }
.map-item { border: 1px solid #e5e7eb; border-radius: 8px; padding: 12px; background: #fff; }
.map-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; cursor: pointer; }
.map-title { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; min-width: 0; }
.map-head-right { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.caret { color: #9ca3af; }
.ellip { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.target-row { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
.target-lab { width: 48px; flex-shrink: 0; }
.map-body { margin-top: 10px; padding-top: 10px; border-top: 1px dashed #e5e7eb; }
.key-bar { display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; }
.key-hint { margin: 6px 0; color: #cf1322; font-size: 12px; font-weight: 600; }
.order-row { margin-bottom: 8px; }
.order-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; }
.chip { display: inline-flex; align-items: center; gap: 2px; padding: 2px 8px; background: #f3f4f6; border-radius: 4px; font-size: 12px; }
.col-name { font-weight: 500; }
.pk { color: #faad14; margin-left: 4px; vertical-align: middle; }
.shard-box { margin-top: 10px; padding-top: 10px; border-top: 1px dashed #e5e7eb; }
.shard-keys { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; padding-left: 16px; }
.tree-empty { padding: 28px 12px; text-align: center; color: #9ca3af; font-size: 13px; }
.muted { color: #9ca3af; font-size: 12px; }
.mb { margin-bottom: 8px; }
.mb-sm { margin-bottom: 6px; }
</style>
