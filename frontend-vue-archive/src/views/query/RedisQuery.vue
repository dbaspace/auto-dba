<template>
  <div class="ops-page redis-query">
    <div class="layout">
      <aside class="side">
        <div class="side-head">Redis 实例</div>
        <el-input v-model="keyword" clearable placeholder="搜索实例" class="mb" />
        <el-select v-model="envFilter" clearable placeholder="全部环境" class="mb" style="width:100%">
          <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
        </el-select>
        <div v-loading="instLoading" class="inst-list">
          <div v-for="i in filteredInstances" :key="i.id" class="inst-block">
            <div
              class="inst-item"
              :class="{ active: selectedId === i.id }"
              @click="selectInstance(i)"
            >
              <span class="caret" :class="{ open: selectedId === i.id }">▸</span>
              <span class="name">{{ i.name || instAddr(i) }}</span>
              <span class="env">{{ instEnv(i) }}</span>
            </div>
            <div v-if="selectedId === i.id" v-loading="dbLoading" class="db-list">
              <button
                v-for="d in databases"
                :key="d.name"
                type="button"
                class="db-item"
                :class="{ active: dbName === d.name }"
                @click.stop="selectDB(d.name)"
              >
                <span class="db-name">{{ d.name }}</span>
                <span class="keys">{{ formatKeys(d.keys) }}</span>
              </button>
              <p v-if="!dbLoading && !databases.length" class="empty inner">未拉取到逻辑库，请检查实例账号或点右侧「刷新库」</p>
            </div>
          </div>
          <p v-if="!instLoading && !filteredInstances.length" class="empty">暂无 Redis 实例，请先在「配置 → 实例」登记 db_type=redis</p>
        </div>
      </aside>
      <section class="main">
        <div v-if="!selectedId" class="placeholder">请从左侧选择 Redis 实例和逻辑库，再执行只读命令</div>
        <template v-else>
          <div class="toolbar">
            <strong>{{ selected?.name || instAddr(selected) }}</strong>
            <span class="muted">{{ instAddr(selected) }}</span>
            <el-tag size="small" type="info">{{ dbName || '未选库' }}</el-tag>
            <el-button :loading="dbLoading" @click="loadDBs">刷新库</el-button>
          </div>
          <el-input
            v-model="command"
            type="textarea"
            :rows="4"
            placeholder="只读命令，例如：GET mykey&#10;SCAN 0 MATCH * COUNT 100&#10;INFO server"
            @keydown.ctrl.enter.prevent="run"
            @keydown.meta.enter.prevent="run"
          />
          <div class="actions">
            <el-button type="primary" :loading="running" :disabled="!command.trim() || !dbName" @click="run">运行</el-button>
            <el-button @click="command = 'SCAN 0 MATCH * COUNT 100'">SCAN 示例</el-button>
            <el-button @click="command = 'INFO server'">INFO 示例</el-button>
            <span class="muted">仅允许 GET / INFO / SCAN · Ctrl/⌘ + Enter 运行 · 当前 {{ dbName || '未选库' }}</span>
          </div>
          <div v-if="result" class="result">
            <div class="result-meta">{{ result.row_count ?? 0 }} 行 · {{ result.execution_time_ms ?? 0 }} ms</div>
            <pre v-if="textBlob" class="result-pre">{{ textBlob }}</pre>
            <el-table v-else :data="resultRows" size="small" height="360" empty-text="无数据">
              <el-table-column
                v-for="col in result.columns || []"
                :key="col"
                :prop="col"
                :label="col"
                :min-width="colWidth(col)"
              >
                <template #default="{ row }">
                  <span class="cell-text">{{ row[col] }}</span>
                </template>
              </el-table-column>
            </el-table>
          </div>
          <div v-if="history.length" class="history">
            <div class="side-head">最近命令</div>
            <div v-for="(h, i) in history" :key="i" class="hist-item" @click="command = h">{{ h }}</div>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import api from '../../api/client'

const HIST_KEY = 'auto-dba-redis-history'
const instLoading = ref(false)
const dbLoading = ref(false)
const running = ref(false)
const instances = ref([])
const envs = ref([])
const keyword = ref('')
const envFilter = ref('')
const selectedId = ref('')
const databases = ref([])
const dbName = ref('')
const command = ref('SCAN 0 MATCH * COUNT 100')
const result = ref(null)
const history = ref([])

const selected = computed(() => instances.value.find((i) => i.id === selectedId.value))
const envOptions = computed(() => {
  const set = new Set()
  for (const e of envs.value) {
    if (e.code) set.add(e.code)
    if (e.name) set.add(e.name)
  }
  for (const i of instances.value) {
    if (i.idc) set.add(i.idc)
  }
  return [...set]
})

const resultRows = computed(() => {
  const cols = result.value?.columns || []
  return (result.value?.rows || []).map((row) => {
    const obj = {}
    cols.forEach((c, i) => { obj[c] = row[i] == null ? 'null' : String(row[i]) })
    return obj
  })
})

const textBlob = computed(() => {
  const cols = result.value?.columns || []
  const rows = result.value?.rows || []
  if (cols.length === 1 && rows.length === 1 && typeof rows[0]?.[0] === 'string' && /[\r\n]/.test(rows[0][0])) {
    return rows[0][0]
  }
  return ''
})

function colWidth(col) {
  if (col === 'section') return 120
  if (col === 'key') return 220
  if (col === 'value') return 280
  return 160
}

function instEnv(i) {
  return i?.idc || i?.env_id || ''
}
function instAddr(i) {
  if (!i) return ''
  const host = i.host || i.domain_name || ''
  return host ? `${host}:${i.port || 6379}` : ''
}
function formatKeys(n) {
  if (n == null || Number(n) < 0) return '—'
  const v = Number(n)
  if (v >= 10000) return `${(v / 10000).toFixed(v >= 100000 ? 0 : 1)}w keys`
  return `${v} keys`
}

const filteredInstances = computed(() => {
  let list = instances.value
  if (envFilter.value) list = list.filter((i) => instEnv(i) === envFilter.value)
  const q = keyword.value.trim().toLowerCase()
  if (q) {
    list = list.filter((i) => (i.name || '').toLowerCase().includes(q) || instAddr(i).toLowerCase().includes(q))
  }
  return list
})

function selectInstance(inst) {
  if (selectedId.value === inst.id) return
  selectedId.value = inst.id
  dbName.value = ''
  databases.value = []
  result.value = null
  loadDBs()
}

function selectDB(name) {
  dbName.value = name
  result.value = null
}

async function loadDBs() {
  if (!selectedId.value) return
  dbLoading.value = true
  try {
    const { data } = await api.get(`/instances/${selectedId.value}/redis/databases`)
    databases.value = data.data || []
    if (!databases.value.some((d) => d.name === dbName.value)) {
      dbName.value = databases.value[0]?.name || ''
    }
  } catch {
    databases.value = []
    dbName.value = ''
  } finally {
    dbLoading.value = false
  }
}

async function run() {
  const sql = command.value.trim()
  if (!selectedId.value || !sql || !dbName.value) return
  running.value = true
  try {
    const { data } = await api.post(`/instances/${selectedId.value}/redis/execute`, {
      command: sql,
      db_name: dbName.value,
    })
    result.value = data
    pushHistory(sql)
  } catch {
    result.value = null
  } finally {
    running.value = false
  }
}

function pushHistory(sql) {
  const next = [sql, ...history.value.filter((x) => x !== sql)].slice(0, 12)
  history.value = next
  try { localStorage.setItem(HIST_KEY, JSON.stringify(next)) } catch { /* ignore */ }
}

async function loadMeta() {
  instLoading.value = true
  try {
    const [instRes, envRes] = await Promise.all([
      api.get('/instances', { params: { db_type: 'redis' } }),
      api.get('/environments'),
    ])
    instances.value = instRes.data.data || []
    envs.value = envRes.data.data || []
    try { history.value = JSON.parse(localStorage.getItem(HIST_KEY) || '[]') } catch { history.value = [] }
  } finally {
    instLoading.value = false
  }
}

onMounted(loadMeta)
</script>

<style scoped>
.redis-query { height: 100%; }
.layout { display: flex; flex: 1; min-height: 0; background: #fff; border: 1px solid #e5e7eb; border-radius: 8px; overflow: hidden; }
.side { width: 280px; flex-shrink: 0; border-right: 1px solid #e5e7eb; display: flex; flex-direction: column; padding: 12px; }
.side-head { font-weight: 600; margin-bottom: 10px; }
.mb { margin-bottom: 8px; }
.inst-list { flex: 1; overflow: auto; min-height: 0; }
.inst-block { margin-bottom: 2px; }
.inst-item { display: flex; align-items: center; gap: 6px; padding: 8px 8px; border-radius: 6px; cursor: pointer; font-size: 13px; }
.inst-item:hover { background: #f0f7ff; }
.inst-item.active { background: #e6f4ff; color: #1677ff; }
.inst-item .caret { width: 12px; flex-shrink: 0; color: #9ca3af; font-size: 12px; line-height: 1; transition: transform .15s; }
.inst-item.active .caret { color: #1677ff; transform: rotate(90deg); }
.inst-item .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.inst-item .env { color: #9ca3af; flex-shrink: 0; font-size: 12px; }
.db-list { padding: 2px 0 8px 18px; }
.db-item {
  width: 100%;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  border: 0;
  background: transparent;
  border-radius: 6px;
  padding: 5px 8px;
  cursor: pointer;
  font-size: 12px;
  color: #374151;
  text-align: left;
}
.db-item:hover { background: #f3f4f6; }
.db-item.active { background: #e6f4ff; color: #1677ff; }
.db-item .db-name { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.db-item .keys { color: #9ca3af; flex-shrink: 0; }
.db-item.active .keys { color: #69b1ff; }
.main { flex: 1; min-width: 0; display: flex; flex-direction: column; padding: 12px; overflow: auto; }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: 10px; }
.muted { color: #9ca3af; font-size: 13px; }
.placeholder, .empty { color: #9ca3af; font-size: 13px; padding: 24px 8px; }
.empty.inner { padding: 8px; }
.placeholder { flex: 1; display: flex; align-items: center; justify-content: center; }
.actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 10px 0; }
.result-meta { font-size: 12px; color: #6b7280; margin-bottom: 6px; }
.result-pre {
  margin: 0;
  padding: 12px 14px;
  max-height: 420px;
  overflow: auto;
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 6px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-all;
}
.cell-text {
  white-space: pre-wrap;
  word-break: break-all;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.history { margin-top: 16px; }
.hist-item { font-size: 12px; color: #4b5563; padding: 4px 0; cursor: pointer; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.hist-item:hover { color: #1677ff; }
</style>
