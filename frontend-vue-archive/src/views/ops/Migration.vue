<template>
  <div class="mig-page">
    <div class="mig-head">
      <div class="mig-actions">
        <el-button @click="$router.push('/ops/migration/settings')">迁移设置</el-button>
        <el-button type="primary" @click="goCreate">新建迁移任务</el-button>
      </div>
    </div>

    <el-form :inline="true" class="mig-filters" @submit.prevent="onQuery">
      <el-form-item label="任务状态">
        <el-select v-model="filters.status" multiple clearable collapse-tags placeholder="多选" style="min-width:200px">
          <el-option v-for="s in STATUS_OPTS" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="审批状态">
        <el-select v-model="filters.approval_status" clearable placeholder="全部" style="width:120px">
          <el-option label="草稿" value="draft" />
          <el-option label="待审批" value="pending" />
          <el-option label="已通过" value="approved" />
          <el-option label="已驳回" value="rejected" />
        </el-select>
      </el-form-item>
      <el-form-item label="数据库类型">
        <el-select v-model="filters.db_type" clearable placeholder="全部" style="width:140px">
          <el-option label="MySQL" value="mysql" />
          <el-option label="PostgreSQL" value="postgres" />
        </el-select>
      </el-form-item>
      <el-form-item label="环境">
        <el-select v-model="filters.env" clearable placeholder="全部" style="width:100px">
          <el-option label="test" value="test" />
          <el-option label="prod" value="prod" />
        </el-select>
      </el-form-item>
      <el-form-item label="创建时间">
        <el-date-picker v-model="filters.range" type="datetimerange" start-placeholder="开始" end-placeholder="结束" />
      </el-form-item>
      <el-form-item label="关键字">
        <el-input v-model="filters.q" clearable placeholder="任务名称" style="width:140px" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" native-type="submit">查询</el-button>
        <el-button @click="load">刷新</el-button>
      </el-form-item>
    </el-form>

    <el-table :data="pageRows" v-loading="loading" border empty-text="暂无迁移任务">
      <el-table-column label="任务名称" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <a class="name-link" @click.prevent="goDetail(row)">{{ row.name }}</a>
        </template>
      </el-table-column>
      <el-table-column label="数据库类型" width="110">
        <template #default="{ row }">{{ dbTypeLabel(row.db_type) }}</template>
      </el-table-column>
      <el-table-column label="源端 → 目标端" min-width="240" show-overflow-tooltip>
        <template #default="{ row }">{{ endpointText(row) }}</template>
      </el-table-column>
      <el-table-column label="迁移模式" width="110">
        <template #default="{ row }">{{ modeText(row.migration_mode) }}</template>
      </el-table-column>
      <el-table-column label="数据中转方式" width="120">
        <template #default="{ row }">{{ transferText(row.transfer_mode) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="120">
        <template #default="{ row }">
          <el-tag size="small" :type="statusTag(row.status, row)">{{ statusText(row.status, row) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="审批" width="100">
        <template #default="{ row }">
          <el-button v-if="row.issue_id" link type="primary" @click="$router.push('/ops/tickets/' + row.issue_id)">
            {{ approvalText(row.approval_status) }}
          </el-button>
          <el-tag v-else size="small" :type="approvalTag(row.approval_status)">{{ approvalText(row.approval_status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="进度" width="170">
        <template #default="{ row }">
          <el-progress :percentage="progress(row)" :stroke-width="10" :status="progressStatus(row)" />
        </template>
      </el-table-column>
      <el-table-column prop="created_by" label="创建人" width="100" />
      <el-table-column prop="created_at" label="创建时间" width="180" />
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <div class="ops">
            <el-button link type="primary" @click="goDetail(row)">详情</el-button>
            <el-button v-if="canStart(row)" link type="primary" @click="act(row, 'start')">启动</el-button>
            <el-button v-if="canPause(row)" link type="warning" @click="act(row, 'pause')">暂停</el-button>
            <el-button v-if="canResume(row)" link type="primary" @click="act(row, 'resume')">恢复</el-button>
            <el-button v-if="canCancel(row)" link type="warning" @click="act(row, 'cancel')">取消</el-button>
            <el-button v-if="canRetry(row)" link type="primary" @click="act(row, 'retry')">重试</el-button>
            <el-button v-if="canDelete(row)" link type="danger" @click="remove(row)">删除</el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>
    <div class="mig-pager">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="filtered.length"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { navigateToWorkOrderCreate } from '../../utils/workOrderDraft'

const STATUS_OPTS = [
  { label: '待执行', value: 'pending' },
  { label: '待定时触发', value: 'scheduled' },
  { label: '预检中', value: 'pre_checking' },
  { label: '预检失败', value: 'pre_check_failed' },
  { label: '迁移中', value: 'running' },
  { label: '已完成', value: 'completed' },
  { label: '失败', value: 'failed' },
  { label: '已暂停', value: 'paused' },
  { label: '已取消', value: 'canceled' },
]

const router = useRouter()
const loading = ref(false)
const allRows = ref([])
const page = ref(1)
const pageSize = ref(10)
const settings = reactive({ approval_enabled: false })
const filters = reactive({ status: [], approval_status: '', db_type: '', env: '', range: null, q: '' })
let timer = null

function dbTypeLabel(t) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgres' || n === 'postgresql') return 'PostgreSQL'
  if (n === 'mysql' || n === 'tidb' || n === 'oceanbase') return 'MySQL'
  return t || '—'
}
function endpointText(row) {
  const src = [row.source_host, row.source_port].filter(Boolean).join(':') || row.source_db || '—'
  const tgt = [row.target_host, row.target_port].filter(Boolean).join(':') || row.target_db || '—'
  return `${src} / ${row.source_db || '—'} → ${tgt} / ${row.target_db || '—'}`
}
function modeText(m) {
  return { full_data: '全量迁移', schema_only: '仅结构迁移' }[m] || m || '—'
}
function transferText(t) {
  return { direct: '直连', oss: 'OSS 中转' }[t] || t || '—'
}
function statusText(s, row) {
  if (row?.approval_status === 'pending' && (s === 'pending' || s === 'init')) return '待审批'
  return {
    pending: '待执行', init: '待执行', scheduled: '待定时触发',
    pre_checking: '预检中', pre_check_failed: '预检失败',
    running: '迁移中', executed: '已完成', completed: '已完成',
    failed: '失败', paused: '已暂停', canceled: '已取消',
  }[s] || s || '—'
}
function statusTag(s, row) {
  if (row?.approval_status === 'pending' && (s === 'pending' || s === 'init')) return 'warning'
  if (s === 'executed' || s === 'completed') return 'success'
  if (s === 'running' || s === 'pre_checking' || s === 'scheduled') return 'warning'
  if (s === 'failed' || s === 'pre_check_failed') return 'danger'
  return 'info'
}
function approvalText(s) {
  return { draft: '草稿', pending: '待审批', approved: '已通过', rejected: '已驳回' }[s] || s || '—'
}
function approvalTag(s) {
  if (s === 'approved') return 'success'
  if (s === 'pending') return 'warning'
  if (s === 'rejected') return 'danger'
  return 'info'
}
function progress(row) {
  if (row.status === 'executed' || row.status === 'completed') return 100
  const total = Number(row.total_objects || 0)
  const done = Number(row.completed_objects || 0)
  if (total > 0) return Math.min(100, Math.round((done * 100) / total))
  if (row.status === 'running' || row.status === 'pre_checking') return 5
  return 0
}
function progressStatus(row) {
  if (row.status === 'failed' || row.status === 'pre_check_failed') return 'exception'
  if (row.status === 'completed' || row.status === 'executed') return 'success'
  return undefined
}
function canStart(row) {
  return ['pending', 'scheduled'].includes(row.status) && row.approval_status === 'approved'
}
function canPause(row) {
  return ['running', 'pre_checking'].includes(row.status)
}
function canResume(row) {
  return row.status === 'paused'
}
function canCancel(row) {
  return ['pending', 'scheduled', 'running', 'pre_checking', 'paused'].includes(row.status)
}
function canRetry(row) {
  return ['failed', 'pre_check_failed', 'canceled'].includes(row.status)
}
function canDelete(row) {
  return !['running', 'pre_checking'].includes(row.status)
}

const filtered = computed(() => {
  return allRows.value.filter((r) => {
    if (filters.status.length && !filters.status.includes(r.status) && !(filters.status.includes('pending') && r.status === 'init')) return false
    if (filters.approval_status && r.approval_status !== filters.approval_status) return false
    if (filters.db_type) {
      const n = String(r.db_type || '').toLowerCase()
      const want = filters.db_type
      if (want === 'mysql' && !['mysql', 'tidb', 'oceanbase'].includes(n)) return false
      if (want === 'postgres' && !['postgres', 'postgresql', 'pg'].includes(n)) return false
    }
    if (filters.env && r.env !== filters.env) return false
    if (filters.q && !String(r.name || '').toLowerCase().includes(filters.q.trim().toLowerCase())) return false
    if (filters.range?.length === 2) {
      const t = new Date(r.created_at).getTime()
      if (t < new Date(filters.range[0]).getTime() || t > new Date(filters.range[1]).getTime()) return false
    }
    return true
  })
})

const pageRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filtered.value.slice(start, start + pageSize.value)
})

function onQuery() {
  page.value = 1
}
function goDetail(row) {
  router.push(`/ops/migration/${row.id}`)
}
function goCreate() {
  navigateToWorkOrderCreate(router, 'migration')
}

async function load() {
  loading.value = true
  try {
    allRows.value = (await api.get('/migration/tasks')).data.data || []
  } finally {
    loading.value = false
  }
}

async function act(row, kind) {
  await api.post(`/migration/tasks/${row.id}/${kind}`)
  ElMessage.success('已提交')
  await load()
}

async function remove(row) {
  await ElMessageBox.confirm(`确认删除任务「${row.name}」？`, '删除', { type: 'warning' })
  await api.delete(`/migration/tasks/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

function setupPoll() {
  if (timer) clearInterval(timer)
  timer = setInterval(() => {
    const busy = allRows.value.some((r) => ['running', 'scheduled', 'pre_checking'].includes(r.status))
    if (busy) load()
  }, 5000)
}

onMounted(async () => {
  try {
    const { data } = await api.get('/migration/settings')
    Object.assign(settings, data.data || {})
  } catch { /* ignore */ }
  await load()
  setupPoll()
})
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.mig-page { display: flex; flex-direction: column; height: 100%; min-height: 0; }
.mig-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-bottom: 16px; }
.mig-head h2 { margin: 0; font-size: 18px; font-weight: 500; color: #111827; }
.mig-actions { display: flex; gap: 8px; }
.mig-filters { margin-bottom: 12px; }
.mig-filters :deep(.el-form-item) { margin-bottom: 8px; }
.name-link { color: #1677ff; cursor: pointer; }
.mig-pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.ops { display: inline-flex; gap: 4px; white-space: nowrap; }
.ops :deep(.el-button) { margin: 0; }
</style>
