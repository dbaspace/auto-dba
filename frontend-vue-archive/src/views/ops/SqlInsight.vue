<template>
  <div class="ops-page">
    <div class="ops-filters">
      <div class="filter-item" style="flex:1.2">
        <span class="label">实例</span>
        <el-select v-model="draftInstance" filterable clearable placeholder="选择 PostgreSQL 实例">
          <el-option
            v-for="i in instances"
            :key="i.id"
            :label="instLabel(i)"
            :value="i.id"
          />
        </el-select>
      </div>
      <div class="filter-item" style="flex:1.6">
        <span class="label">时间范围</span>
        <el-date-picker
          v-model="draftRange"
          type="datetimerange"
          format="YYYY-MM-DD HH:mm:ss"
          start-placeholder="开始"
          end-placeholder="结束"
          style="width:100%"
        />
      </div>
      <el-button type="primary" @click="applyQuery">查询</el-button>
      <el-button @click="resetQuery">重置</el-button>
    </div>

    <el-tabs v-model="tab">
      <el-tab-pane label="SQL 洞察" name="sql" />
      <el-tab-pane label="洞察分析" name="analysis" />
      <el-tab-pane label="AWR" name="awr" />
      <el-tab-pane label="空间分析" name="space" />
    </el-tabs>

    <div v-if="tab === 'sql'" class="toolbar extra">
      <span class="muted">慢事务</span>
      <el-radio-group v-model="slow" size="small">
        <el-radio-button label="all">全部</el-radio-button>
        <el-radio-button label="1">&gt; 1s</el-radio-button>
        <el-radio-button label="10">&gt; 10s</el-radio-button>
      </el-radio-group>
      <span class="muted">显示 SQL</span>
      <el-switch v-model="showSql" size="small" />
      <span class="muted">自动刷新</span>
      <el-switch v-model="autoRefresh" size="small" />
      <el-select v-model="intervalSec" size="small" :disabled="!autoRefresh" style="width:80px">
        <el-option v-for="s in [15, 30, 45, 60]" :key="s" :label="`${s}s`" :value="s" />
      </el-select>
      <el-button size="small" :loading="loading" @click="loadSnapshots">刷新</el-button>
      <el-button size="small" type="primary" :loading="collecting" @click="collect">手动采集</el-button>
    </div>

    <div v-if="tab === 'analysis'" class="toolbar extra">
      <el-input v-model="draftDb" clearable placeholder="数据库" style="width:160px" />
      <el-input v-model="draftUser" clearable placeholder="用户" style="width:140px" />
      <el-radio-group v-model="orderBy" size="small">
        <el-radio-button value="count">按次数</el-radio-button>
        <el-radio-button value="avg_duration">按平均时长</el-radio-button>
        <el-radio-button value="max_duration">按最大时长</el-radio-button>
        <el-radio-button value="last_seen">按最近出现</el-radio-button>
      </el-radio-group>
      <el-button size="small" type="primary" @click="applyStatsFilter">查询</el-button>
    </div>

    <div v-if="tab === 'sql'" class="ops-table">
      <el-table v-loading="loading" :data="displayRows" border height="100%" empty-text="请选择实例并查询">
        <el-table-column prop="collected_at" label="采集时间" width="170">
          <template #default="{ row }">{{ formatTime(row.collected_at) }}</template>
        </el-table-column>
        <el-table-column label="实例" min-width="180">
          <template #default="{ row }">
            <div>{{ row.instance_name || '-' }}</div>
            <div class="sub">{{ row.host }}:{{ row.port }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="pid" label="PID" width="80" />
        <el-table-column prop="p_user" label="用户" width="110" />
        <el-table-column prop="p_host" label="客户端" width="130" />
        <el-table-column prop="p_db" label="数据库" width="120" />
        <el-table-column label="事务时长" width="110" sortable :sort-method="sortDuration">
          <template #default="{ row }">
            <span :class="durationClass(row.xact_duration)">{{ formatDuration(row.xact_duration) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="state" label="状态" width="150">
          <template #default="{ row }">
            <el-tag size="small" :type="stateType(row.state)">{{ row.state || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="showSql" label="SQL" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">
            <el-button v-if="row.info" link type="primary" @click="sqlDrawer = row">{{ row.info }}</el-button>
            <span v-else>-</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div v-if="tab === 'sql'" class="pager">
      <el-pagination
        background
        layout="total, sizes, prev, pager, next"
        :total="total"
        v-model:current-page="current"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        @current-change="loadSnapshots"
        @size-change="onSizeChange"
      />
    </div>

    <div v-else-if="tab === 'analysis'" class="ops-table">
      <el-table v-loading="statsLoading" :data="statsRows" border height="100%" empty-text="请选择实例并查询">
        <el-table-column label="归一化 SQL" min-width="260">
          <template #default="{ row }">
            <el-button link type="primary" @click="statDrawer = row">{{ truncate(row.query_normalized, 80) }}</el-button>
          </template>
        </el-table-column>
        <el-table-column prop="count" label="次数" width="80" />
        <el-table-column label="平均时长" width="110">
          <template #default="{ row }">{{ formatDuration(row.avg_xact_duration) }}</template>
        </el-table-column>
        <el-table-column label="最大时长" width="110">
          <template #default="{ row }">{{ formatDuration(row.max_xact_duration) }}</template>
        </el-table-column>
        <el-table-column prop="distinct_dbs" label="库数" width="70" />
        <el-table-column label="首次出现" width="170">
          <template #default="{ row }">{{ formatTime(row.first_seen) }}</template>
        </el-table-column>
        <el-table-column label="最近出现" width="170">
          <template #default="{ row }">{{ formatTime(row.last_seen) }}</template>
        </el-table-column>
      </el-table>
    </div>
    <div v-if="tab === 'analysis'" class="pager">
      <el-pagination
        background
        layout="total, sizes, prev, pager, next"
        :total="statsTotal"
        v-model:current-page="statsCurrent"
        v-model:page-size="statsPageSize"
        :page-sizes="[10, 20, 50, 100]"
        @current-change="loadStats"
        @size-change="onStatsSizeChange"
      />
    </div>

    <el-empty v-else description="该能力仍在开发中，当前已对齐老项目「SQL 洞察」快照采集与洞察分析" />

    <el-drawer v-model="sqlOpen" title="SQL 详情" size="520px" append-to-body>
      <div v-if="sqlDrawer" class="drawer-meta">
        <div>采集时间：{{ formatTime(sqlDrawer.collected_at) }}</div>
        <div>PID：{{ sqlDrawer.pid }} · {{ sqlDrawer.p_user }} @ {{ sqlDrawer.p_db }}</div>
        <div>事务时长：{{ formatDuration(sqlDrawer.xact_duration) }}</div>
      </div>
      <el-input :model-value="sqlDrawer?.info || ''" type="textarea" :rows="16" readonly />
      <div class="drawer-actions">
        <el-button type="primary" @click="copyText(sqlDrawer?.info)">复制 SQL</el-button>
      </div>
    </el-drawer>

    <el-drawer v-model="statOpen" title="归一化 SQL" size="520px" append-to-body>
      <el-input :model-value="statDrawer?.query_normalized || ''" type="textarea" :rows="10" readonly />
      <div class="drawer-meta">样例 SQL</div>
      <el-input :model-value="statDrawer?.sample_info || ''" type="textarea" :rows="10" readonly />
      <div class="drawer-actions">
        <el-button type="primary" @click="copyText(statDrawer?.query_normalized)">复制模板</el-button>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

function pad(n) {
  return String(n).padStart(2, '0')
}
function defaultRange() {
  const end = new Date()
  const start = new Date(end.getTime() - 3600 * 1000)
  return [start, end]
}
function rangeISO(r) {
  const pair = r?.length === 2 ? r : defaultRange()
  const a = pair[0] instanceof Date ? pair[0] : new Date(pair[0])
  const b = pair[1] instanceof Date ? pair[1] : new Date(pair[1])
  return [a.toISOString(), b.toISOString()]
}

const instances = ref([])
const draftInstance = ref('')
const draftRange = ref(defaultRange())
const instanceId = ref('')
const range = ref(defaultRange())
const tab = ref('sql')
const slow = ref('all')
const showSql = ref(true)
const autoRefresh = ref(false)
const intervalSec = ref(15)
const loading = ref(false)
const collecting = ref(false)
const rows = ref([])
const total = ref(0)
const current = ref(1)
const pageSize = ref(20)
const sqlDrawer = ref(null)

const draftDb = ref('')
const draftUser = ref('')
const pDb = ref('')
const pUser = ref('')
const orderBy = ref('count')
const statsLoading = ref(false)
const statsRows = ref([])
const statsTotal = ref(0)
const statsCurrent = ref(1)
const statsPageSize = ref(20)
const statDrawer = ref(null)

const sqlOpen = computed({
  get: () => !!sqlDrawer.value,
  set: (v) => { if (!v) sqlDrawer.value = null },
})
const statOpen = computed({
  get: () => !!statDrawer.value,
  set: (v) => { if (!v) statDrawer.value = null },
})

const displayRows = computed(() => {
  if (slow.value === 'all') return rows.value
  const th = Number(slow.value)
  return rows.value.filter((r) => (r.xact_duration ?? 0) > th)
})

function instLabel(i) {
  const host = i.host || i.domain_name || '-'
  return `${i.name || host} (${host}:${i.port})`
}
function isPG(i) {
  const t = String(i.db_type || '').toLowerCase()
  return t === 'postgres' || t === 'postgresql' || t === 'pg'
}
function formatTime(v) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function formatDuration(seconds) {
  if (seconds == null || !Number.isFinite(Number(seconds))) return '-'
  const s = Math.max(0, Number(seconds))
  if (s < 1) return `${s.toFixed(2)}s`
  if (s < 60) return `${s.toFixed(1)}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}
function durationClass(v) {
  const s = v ?? 0
  if (s >= 10) return 'dur-danger'
  if (s >= 1) return 'dur-warn'
  return ''
}
function sortDuration(a, b) {
  return (a.xact_duration ?? 0) - (b.xact_duration ?? 0)
}
function stateType(state) {
  const s = String(state || '').toLowerCase()
  if (s.includes('active')) return 'success'
  if (s.includes('idle in transaction')) return 'warning'
  if (s.includes('abort')) return 'danger'
  return 'info'
}
function truncate(text, n) {
  if (!text) return '-'
  return text.length > n ? `${text.slice(0, n)}…` : text
}
async function copyText(text) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}

async function loadInstances() {
  const { data } = await api.get('/instances')
  instances.value = (data.data || []).filter(isPG)
}

function snapshotParams() {
  const [start, end] = rangeISO(range.value)
  return {
    instance_id: instanceId.value,
    start_time: start,
    end_time: end,
    current: current.value,
    page_size: pageSize.value,
  }
}

async function loadSnapshots() {
  if (!instanceId.value) {
    rows.value = []
    total.value = 0
    return
  }
  loading.value = true
  try {
    const { data } = await api.get('/pg-activity-snapshots', { params: snapshotParams() })
    rows.value = data.data || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

async function loadStats() {
  if (!instanceId.value) {
    statsRows.value = []
    statsTotal.value = 0
    return
  }
  statsLoading.value = true
  try {
    const [start, end] = rangeISO(range.value)
    const { data } = await api.get('/pg-activity-snapshots/stats', {
      params: {
        instance_id: instanceId.value,
        start_time: start,
        end_time: end,
        p_db: pDb.value,
        p_user: pUser.value,
        order_by: orderBy.value,
        current: statsCurrent.value,
        page_size: statsPageSize.value,
      },
    })
    statsRows.value = data.data || []
    statsTotal.value = data.total || 0
  } finally {
    statsLoading.value = false
  }
}

function applyQuery() {
  instanceId.value = draftInstance.value
  range.value = draftRange.value?.length === 2 ? draftRange.value : defaultRange()
  current.value = 1
  statsCurrent.value = 1
  if (tab.value === 'analysis') loadStats()
  else loadSnapshots()
}
function resetQuery() {
  const next = defaultRange()
  draftInstance.value = ''
  draftRange.value = next
  instanceId.value = ''
  range.value = next
  rows.value = []
  total.value = 0
  statsRows.value = []
  statsTotal.value = 0
  loadInstances()
}
function applyStatsFilter() {
  pDb.value = draftDb.value
  pUser.value = draftUser.value
  statsCurrent.value = 1
  loadStats()
}
function onSizeChange() {
  current.value = 1
  loadSnapshots()
}
function onStatsSizeChange() {
  statsCurrent.value = 1
  loadStats()
}

async function collect() {
  try {
    await ElMessageBox.confirm(
      instanceId.value
        ? '将对当前实例立即采集一次活动会话，确定？'
        : '将对所有 PostgreSQL 实例立即采集一次活动会话，可能耗时较久，确定？',
      '手动采集',
      { type: 'warning', confirmButtonText: '确定采集' },
    )
  } catch {
    return
  }
  collecting.value = true
  try {
    const { data } = await api.post('/pg-activity-snapshots/collect', {
      instance_id: instanceId.value || undefined,
    }, { timeout: 120000 })
    ElMessage.success(`采集完成：处理 ${data.instances_processed ?? 0} 个实例，写入 ${data.rows_written ?? 0} 条`)
    if ((data.warnings || []).length) {
      ElMessage.warning(data.warnings.slice(0, 3).join('；'))
    }
    await loadSnapshots()
    if (tab.value === 'analysis') await loadStats()
  } finally {
    collecting.value = false
  }
}

watch(tab, (name) => {
  if (name === 'analysis') loadStats()
  if (name === 'sql') loadSnapshots()
})
watch(orderBy, () => {
  if (tab.value === 'analysis') {
    statsCurrent.value = 1
    loadStats()
  }
})

let timer = null
watch([autoRefresh, intervalSec, tab], () => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  if (autoRefresh.value && tab.value === 'sql') {
    timer = setInterval(loadSnapshots, intervalSec.value * 1000)
  }
})
onMounted(async () => {
  await loadInstances()
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.extra { margin-bottom: 12px; gap: 10px; }
.muted { color: #6b7280; font-size: 13px; }
.sub { color: #9ca3af; font-size: 12px; }
.dur-warn { color: #e6a23c; }
.dur-danger { color: #f56c6c; font-weight: 600; }
.pager { flex-shrink: 0; display: flex; justify-content: flex-end; padding: 8px 0 0; }
.drawer-meta { color: #6b7280; font-size: 13px; margin-bottom: 12px; line-height: 1.7; }
.drawer-actions { margin-top: 12px; }
</style>
