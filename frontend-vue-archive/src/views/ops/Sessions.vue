<template>
  <div class="ops-page">
    <div class="ops-filters">
      <div class="filter-item">
        <span class="label">实例</span>
        <el-select v-model="instanceId" filterable clearable placeholder="选择 PostgreSQL 实例" @change="onInstanceChange">
          <el-option
            v-for="i in instances"
            :key="i.id"
            :label="instLabel(i)"
            :value="i.id"
          />
        </el-select>
      </div>
    </div>

    <el-tabs v-model="tab">
      <el-tab-pane label="会话列表" name="list" />
      <el-tab-pane label="锁信息" name="locks" />
    </el-tabs>

    <div class="toolbar extra">
      <span class="muted">自动刷新</span>
      <el-switch v-model="autoRefresh" size="small" />
      <el-select v-model="intervalSec" size="small" :disabled="!autoRefresh" style="width:80px">
        <el-option v-for="s in [15, 30, 45, 60]" :key="s" :label="`${s}s`" :value="s" />
      </el-select>
      <el-button size="small" :loading="loading" @click="reload">刷新</el-button>
      <el-input
        v-model="keyword"
        clearable
        size="small"
        :placeholder="tab === 'list' ? '模糊搜索 SQL 语句' : '搜索 PID / 库 / relation'"
        style="width:260px;margin-left:auto"
      />
    </div>

    <div v-if="tab === 'list'" class="ops-table">
      <el-table v-loading="loading" :data="sessionPageRows" border height="100%" empty-text="请选择实例后查看会话">
        <el-table-column prop="pid" label="会话 ID" width="90" />
        <el-table-column prop="datname" label="数据库" width="130" />
        <el-table-column prop="usename" label="用户名" width="120" />
        <el-table-column prop="state" label="会话状态" width="140">
          <template #default="{ row }">
            <el-tag size="small" :type="stateType(row.state)">{{ row.state || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="query" label="执行中语句" min-width="220" show-overflow-tooltip />
        <el-table-column prop="application_name" label="连接程序名" width="140" show-overflow-tooltip />
        <el-table-column prop="client_addr" label="客户端地址" width="130" />
        <el-table-column prop="backend_start" label="连接启动时间" width="170" />
        <el-table-column prop="duration" label="连接持续时间" width="120" />
        <el-table-column prop="wait_event_type" label="等待事件类型" width="130" />
        <el-table-column prop="wait_event" label="等待事件" width="130" />
        <el-table-column label="操作" width="170" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="cancelPid(row)">退出执行</el-button>
            <el-button link type="danger" @click="terminatePid(row)">关闭会话</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else class="ops-table">
      <el-table v-loading="loading" :data="lockPageRows" border height="100%" empty-text="请选择实例后查看锁信息">
        <el-table-column prop="locktype" label="锁类型" width="130" />
        <el-table-column prop="database" label="数据库" width="120" />
        <el-table-column prop="pid" label="PID" width="90" />
        <el-table-column prop="mode" label="模式" width="150" />
        <el-table-column prop="relation" label="Relation" min-width="160" />
      </el-table>
    </div>

    <div class="pager">
      <el-pagination
        background
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="pageTotal"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next, jumper"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

const instances = ref([])
const instanceId = ref('')
const tab = ref('list')
const autoRefresh = ref(true)
const intervalSec = ref(15)
const loading = ref(false)
const keyword = ref('')
const sessions = ref([])
const locks = ref([])
const page = ref(1)
const pageSize = ref(10)

const sessionView = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return sessions.value
  return sessions.value.filter((r) => String(r.query || '').toLowerCase().includes(q))
})
const lockView = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return locks.value
  return locks.value.filter((r) =>
    String(r.relation || '').toLowerCase().includes(q) ||
    String(r.database || '').toLowerCase().includes(q) ||
    String(r.pid).includes(q),
  )
})
const pageTotal = computed(() => (tab.value === 'locks' ? lockView.value.length : sessionView.value.length))
const sessionPageRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return sessionView.value.slice(start, start + pageSize.value)
})
const lockPageRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return lockView.value.slice(start, start + pageSize.value)
})

function instLabel(i) {
  const host = i.host || i.domain_name || '-'
  return `${i.name || host} (${host}:${i.port})`
}
function isPG(i) {
  const t = String(i.db_type || '').toLowerCase()
  return t === 'postgres' || t === 'postgresql' || t === 'pg'
}
function stateType(state) {
  const s = String(state || '').toLowerCase()
  if (s.includes('active')) return 'success'
  if (s.includes('idle in transaction')) return 'warning'
  if (s.includes('idle')) return 'info'
  if (s.includes('abort')) return 'danger'
  return ''
}

async function loadInstances() {
  const { data } = await api.get('/instances')
  instances.value = (data.data || []).filter(isPG)
}

async function loadSessions() {
  if (!instanceId.value) {
    sessions.value = []
    return
  }
  loading.value = true
  try {
    const { data } = await api.get('/pg-sessions/activities', { params: { instance_id: instanceId.value } })
    sessions.value = data.data || []
  } finally {
    loading.value = false
  }
}

async function loadLocks() {
  if (!instanceId.value) {
    locks.value = []
    return
  }
  loading.value = true
  try {
    const { data } = await api.get('/pg-sessions/locks', { params: { instance_id: instanceId.value } })
    locks.value = data.data || []
  } finally {
    loading.value = false
  }
}

function reload() {
  if (tab.value === 'locks') return loadLocks()
  return loadSessions()
}
function resetPage() {
  page.value = 1
}
function onInstanceChange() {
  keyword.value = ''
  resetPage()
  reload()
}

async function cancelPid(row) {
  try {
    await ElMessageBox.confirm(`确认对会话 ${row.pid} 执行 pg_cancel_backend（退出当前查询）？`, '退出执行', {
      type: 'warning',
    })
  } catch {
    return
  }
  await api.post(`/pg-sessions/${row.pid}/cancel`, { instance_id: instanceId.value }, { params: { instance_id: instanceId.value } })
  ElMessage.success(`已请求退出执行：会话 ${row.pid}`)
  await loadSessions()
}

async function terminatePid(row) {
  try {
    await ElMessageBox.confirm(`确认对会话 ${row.pid} 执行 pg_terminate_backend（关闭连接）？`, '关闭会话', {
      type: 'warning',
      confirmButtonText: '关闭会话',
    })
  } catch {
    return
  }
  await api.post(`/pg-sessions/${row.pid}/terminate`, { instance_id: instanceId.value }, { params: { instance_id: instanceId.value } })
  ElMessage.success(`已请求关闭会话：${row.pid}`)
  await loadSessions()
}

watch(tab, () => {
  resetPage()
  reload()
})
watch(keyword, resetPage)
watch(pageTotal, (total) => {
  const maxPage = Math.max(1, Math.ceil(total / pageSize.value) || 1)
  if (page.value > maxPage) page.value = maxPage
})

let timer = null
watch([autoRefresh, intervalSec, instanceId], () => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  if (autoRefresh.value && instanceId.value) {
    timer = setInterval(reload, intervalSec.value * 1000)
  }
})

onMounted(loadInstances)
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.extra { margin-bottom: 12px; gap: 10px; }
.muted { color: #6b7280; font-size: 13px; }
.pager { flex-shrink: 0; display: flex; justify-content: flex-end; padding: 8px 0 0; }
</style>
