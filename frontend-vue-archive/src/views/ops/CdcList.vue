<template>
  <div class="cdc-shell cdc-list">
    <div class="cdc-head">
      <h2>CDC 同步任务</h2>
      <div class="cdc-actions">
        <el-button @click="goCreate('remove_table')">移除同步表</el-button>
        <el-button @click="goCreate('alter_schema')">源表加列</el-button>
        <el-button @click="goCreate('add_table')">新增同步表</el-button>
        <el-button type="primary" @click="goCreate('init')">初始化同步</el-button>
      </div>
    </div>

    <el-alert v-if="loadError" type="error" show-icon class="mb" title="CDC 接口不可用" :description="loadError" />

    <el-tabs v-model="tab" class="cdc-tabs" @tab-change="onTabChange">
      <el-tab-pane name="running">
        <template #label>
          <span :class="{ 'tab-abnormal': abnormalCount }">{{ runningLabel }}</span>
          <el-badge v-if="abnormalCount" :value="abnormalCount" class="tab-badge" type="danger" />
        </template>
      </el-tab-pane>
      <el-tab-pane name="pending">
        <template #label>
          <span>待审核列表</span>
          <el-badge v-if="pendingCount" :value="pendingCount" class="tab-badge" />
        </template>
      </el-tab-pane>
      <el-tab-pane name="approval_result" label="审批结果列表" />
    </el-tabs>

    <div class="cdc-filters">
      <el-form :inline="true" @submit.prevent="onQuery">
        <el-form-item label="环境">
          <el-select v-model="filters.env" clearable placeholder="全部" style="width:140px">
            <el-option v-for="e in envOptions" :key="e.value" :label="e.label" :value="e.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="任务类型">
          <el-select
            v-model="filters.task_type"
            :clearable="tab !== 'running'"
            :disabled="tab === 'running'"
            :placeholder="tab === 'running' ? '初始化' : '全部'"
            style="width:130px"
          >
            <el-option v-for="t in taskTypeOptions" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="任务名称">
          <el-input v-model="filters.q" clearable placeholder="模糊搜索" style="width:160px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :disabled="loading" native-type="submit">查询</el-button>
          <el-button :disabled="loading" @click="load">刷新</el-button>
        </el-form-item>
      </el-form>
      <el-popover placement="bottom-end" :width="220" trigger="click">
        <div class="col-pick">
          <div v-for="c in columnDefs" :key="c.key" class="col-pick-item">
            <el-checkbox :model-value="visibleCols.includes(c.key)" :disabled="c.locked" @change="(v) => toggleCol(c.key, v)">{{ c.label }}</el-checkbox>
          </div>
        </div>
        <template #reference>
          <el-button>设置列</el-button>
        </template>
      </el-popover>
    </div>

    <el-alert
      v-if="tab === 'running' && pageDriftCount"
      type="warning"
      show-icon
      :closable="false"
      class="mb"
      :title="`本页有 ${pageDriftCount} 个运行中的初始化任务存在表结构差异`"
      description="PostgreSQL 已有、StarRocks 目标表尚未同步的字段需通过「源表加列」处理。点击任务名称进入详情查看明细。"
    />
    <p v-if="tab !== 'running'" class="hint">任务类型选「全部」可查看 init / 新增表 / 移除表 / 源表加列 全部审批申请记录</p>

    <div class="table-scroll">
    <el-table :data="pageRows" v-loading="loading" size="small" empty-text="暂无数据">
      <template #empty>
        <div class="cdc-empty">
          <span class="ico">▣</span>
          <div>{{ emptyText }}</div>
        </div>
      </template>
      <el-table-column v-if="colOn('name')" label="任务名称" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">
          <button type="button" class="name-link" @click="$router.push('/ops/cdc/' + row.id)">{{ row.name }}</button>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('type')" label="任务类型" width="110">
        <template #default="{ row }">
          <el-tag size="small" :type="typeTag(row.task_type)">{{ typeText(row.task_type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('env')" label="环境" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="envTag(rowEnv(row))">{{ rowEnv(row) || '—' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('publication')" label="Publication" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ extraOf(row).publication_name || '—' }}</template>
      </el-table-column>
      <el-table-column v-if="colOn('source')" label="源端" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <div class="cell-2">
            <div>{{ instAddr(instOf(row.source_instance_id)) }}</div>
            <div class="sub">{{ sourceScopeOf(row) }}</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('target')" label="目标端" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <div class="cell-2">
            <div>{{ row.target_instance_id ? instAddr(instOf(row.target_instance_id)) : '未配置' }}</div>
            <div class="sub">{{ row.target_db || (row.target_instance_id ? '—' : '未配置') }}</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('tables')" label="同步表数" width="150">
        <template #default="{ row }">
          <el-tooltip content="源端：任务配置表数，与 Publication 一致；目标：映射组/目标表数；分表：分表归并映射组数" placement="top">
            <div class="count-cell">
              <span>源端 <span class="c-src">{{ countOf(row).source }}</span></span>
              <span class="muted"> · </span>
              <span>目标 <span class="c-tgt">{{ countOf(row).target }}</span></span>
              <template v-if="countOf(row).shard">
                <span class="muted"> · </span>
                <span>分表 <span class="c-shard">{{ countOf(row).shard }}</span></span>
              </template>
            </div>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('debezium')" label="Debezium 模式" width="130">
        <template #default="{ row }">{{ extraOf(row).debezium_mode || 'after_only' }}</template>
      </el-table-column>
      <el-table-column v-if="colOn('status')" label="状态" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row._pipeline?.healthy === false ? 'danger' : statusTag(row.status)">{{ row._pipeline?.healthy === false ? '异常' : statusText(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column v-if="tab === 'running' && colOn('schema')" label="结构差异" width="120">
        <template #default="{ row }">
          <el-tag v-if="driftOf(row)?.state === 'WARN' && driftOf(row).drift_table_count" size="small" type="warning">{{ driftOf(row).drift_table_count }} 张表有差异</el-tag>
          <el-tag v-else-if="driftOf(row)?.state === 'ERROR'" size="small" type="danger">检查失败</el-tag>
          <el-tag v-else-if="driftOf(row)?.state === 'OK' || (driftOf(row)?.state === 'WARN' && !driftOf(row).drift_table_count)" size="small" type="success">一致</el-tag>
          <span v-else class="muted">未检查</span>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('approval')" label="审批" width="100">
        <template #default="{ row }">
          <el-button v-if="row.issue_id && approvalText(row.approval_status) !== '—'" link type="primary" @click="$router.push('/ops/tickets/' + row.issue_id)">
            {{ approvalText(row.approval_status) }}
          </el-button>
          <span v-else class="muted">{{ approvalText(row.approval_status) }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="colOn('created_by')" label="创建人" width="110" show-overflow-tooltip>
        <template #default="{ row }">{{ extraOf(row).created_by || '—' }}</template>
      </el-table-column>
      <el-table-column v-if="colOn('created_at')" prop="created_at" label="创建时间" width="180" />
      <el-table-column v-if="colOn('op')" label="操作" width="240">
        <template #default="{ row }">
          <div class="ops">
            <el-button link type="primary" @click="$router.push('/ops/cdc/' + row.id)">详情</el-button>
            <el-button v-if="row.status === 'paused' && !isPending(row)" link type="primary" @click="act(row, 'resume')">恢复</el-button>
            <el-button v-if="row.status === 'running' && !isPending(row)" link @click="act(row, 'pause')">暂停</el-button>
            <el-button v-if="row.status === 'running' && !isPending(row)" link @click="act(row, 'restart')">重启</el-button>
            <el-button v-if="(row.status === 'error' || row.status === 'failed') && !isPending(row)" link type="primary" @click="act(row, 'retry')">重试</el-button>
            <el-button v-if="row.task_type === 'init' && !isPending(row)" link type="danger" @click="remove(row)">删除</el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>
    </div>
    <div class="cdc-pager">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="filtered.length"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next"
        :pager-count="7"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { navigateToWorkOrderCreate } from '../../utils/workOrderDraft'
import {
  approvalText,
  decodeCdcExtra,
  envTag,
  formatSourceScopeLabel,
  instAddr,
  instEnv,
  isValidCdcTaskID,
  statusTag,
  statusText,
  syncTableCountView,
  typeTag,
  typeText,
} from './cdc'
import './cdc.css'

const RUNNING_STATUSES = new Set(['init', 'running', 'paused', 'error', 'failed', 'creating', 'provisioning', 'configuring'])
const TYPE_ALL = [
  { label: '初始化', value: 'init' },
  { label: '新增表', value: 'add_table' },
  { label: '移除表', value: 'remove_table' },
  { label: '源表加列', value: 'alter_schema' },
  { label: '删除任务', value: 'delete_task' },
]

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const scanLoading = ref(false)
const driftById = ref({})
const loadError = ref('')
const allRows = ref([])
const instances = ref([])
const envOptions = ref([])
const tab = ref('running')
const page = ref(1)
const pageSize = ref(10)
const filters = reactive({ env: '', task_type: 'init', q: '' })
const COL_STORE = 'auto-dba.cdc.listCols.v1'
const columnDefs = [
  { key: 'name', label: '任务名称', locked: true },
  { key: 'type', label: '任务类型' },
  { key: 'env', label: '环境' },
  { key: 'publication', label: 'Publication' },
  { key: 'source', label: '源端' },
  { key: 'target', label: '目标端' },
  { key: 'tables', label: '同步表数' },
  { key: 'debezium', label: 'Debezium 模式' },
  { key: 'status', label: '状态' },
  { key: 'schema', label: '结构差异' },
  { key: 'approval', label: '审批' },
  { key: 'created_by', label: '创建人' },
  { key: 'created_at', label: '创建时间' },
  { key: 'op', label: '操作', locked: true },
]
const DEFAULT_COLS = ['name', 'type', 'source', 'target', 'tables', 'debezium', 'status', 'schema', 'op']
const visibleCols = ref(loadCols())

function loadCols() {
  try {
    const raw = JSON.parse(localStorage.getItem(COL_STORE) || '[]')
    if (Array.isArray(raw) && raw.includes('name')) return raw
  } catch { /* ignore */ }
  return [...DEFAULT_COLS]
}
function colOn(key) {
  return visibleCols.value.includes(key)
}
function toggleCol(key, on) {
  const next = new Set(visibleCols.value)
  if (on) next.add(key)
  else next.delete(key)
  next.add('name')
  next.add('op')
  visibleCols.value = [...next]
  localStorage.setItem(COL_STORE, JSON.stringify(visibleCols.value))
}

const instMap = computed(() => {
  const m = new Map()
  for (const i of instances.value) m.set(i.id, i)
  return m
})

const taskTypeOptions = computed(() => (tab.value === 'running' ? [{ label: '初始化', value: 'init' }] : TYPE_ALL))

function instOf(id) {
  return instMap.value.get(id)
}
function extraOf(row) {
  return decodeCdcExtra(row.description).extra
}
function driftOf(row) {
  return driftById.value[row.id]
}
function sourceScopeOf(row) {
  const extra = extraOf(row)
  return formatSourceScopeLabel(row.source_db, extra.source_db_prefix, extra.source_db_names)
}
function countOf(row) {
  return syncTableCountView(extraOf(row))
}
function rowEnv(row) {
  return extraOf(row).env || instEnv(instOf(row.source_instance_id))
}
function isPending(row) {
  return row.approval_status === 'pending' || row.status === 'pending'
}
function isApprovalResult(row) {
  return row.approval_status === 'approved' || row.approval_status === 'rejected' || row.approval_status === 'canceled'
}
function isRunning(row) {
  return RUNNING_STATUSES.has(row.status) && !isPending(row)
}

const pendingCount = computed(() => allRows.value.filter(isPending).length)
const abnormalCount = computed(() => allRows.value.filter((r) => isRunning(r) && (r.status === 'error' || r.status === 'failed' || r._pipeline?.healthy === false)).length)
const runningCount = computed(() => allRows.value.filter(isRunning).length)
const runningLabel = computed(() => (tab.value === 'running' ? `运行列表 (${filtered.value.length || runningCount.value})` : '运行列表'))

const filtered = computed(() => {
  return allRows.value.filter((r) => {
    if (tab.value === 'pending' && !isPending(r)) return false
    if (tab.value === 'approval_result' && !isApprovalResult(r)) return false
    if (tab.value === 'running' && !isRunning(r)) return false
    if (filters.task_type && r.task_type !== filters.task_type) return false
    if (filters.env && rowEnv(r) !== filters.env) return false
    if (filters.q && !String(r.name || '').toLowerCase().includes(filters.q.trim().toLowerCase())) return false
    return true
  })
})

const pageRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filtered.value.slice(start, start + pageSize.value)
})

const emptyText = computed(() => ({
  running: '暂无运行中的任务',
  pending: '暂无待审核任务',
  approval_result: '暂无审批结果',
}[tab.value]))

const pageDriftCount = computed(() => pageRows.value.filter((r) => {
  const d = driftOf(r)
  return r.task_type === 'init' && d?.state === 'WARN' && Number(d.drift_table_count || 0) > 0
}).length)

function goCreate(type) {
  if (type === 'init') {
    navigateToWorkOrderCreate(router, 'cdc', { type })
    return
  }
  router.push({ path: '/ops/cdc/create', query: { type } })
}

function onTabChange() {
  page.value = 1
  filters.task_type = tab.value === 'running' ? 'init' : ''
}

function onQuery() {
  page.value = 1
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [{ data }, inst, envRes] = await Promise.all([
      api.get('/cdc-sync/tasks'),
      api.get('/instances'),
      api.get('/environments').catch(() => ({ data: { data: [] } })),
    ])
    allRows.value = data.data || []
    instances.value = inst.data.data || []
    await loadDriftCache()
    void probeRunningPipelines()
    const envs = envRes.data.data || []
    envOptions.value = envs.map((e) => ({ value: e.code || e.name, label: e.name || e.code })).filter((e) => e.value)
    if (!envOptions.value.length) envOptions.value = [{ value: 'test', label: 'test' }, { value: 'prod', label: 'prod' }]
  } catch (e) {
    loadError.value = e?.response?.data?.message || e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function probeRunningPipelines() {
  const targets = allRows.value.filter((r) => isRunning(r) && r.task_type === 'init').slice(0, 20)
  const chunk = 4
  for (let i = 0; i < targets.length; i += chunk) {
    const part = targets.slice(i, i + chunk)
    await Promise.all(part.map(async (row) => {
      if (!isValidCdcTaskID(row.id)) return
      const { data } = await api.get(`/cdc-sync/tasks/${row.id}/status`, { silent: true }).catch(() => ({ data: {} }))
      const pipe = data.data || {}
      row._pipeline = { healthy: pipe.healthy, unhealthy_reason: pipe.unhealthy_reason }
    }))
  }
}

async function loadDriftCache() {
  const { data } = await api.get('/cdc-sync/schema-alerts', { silent: true }).catch(() => ({ data: {} }))
  const list = data.data || []
  const m = {}
  for (const it of list) {
    if (it?.init_task_id) m[it.init_task_id] = it
  }
  driftById.value = m
}

async function scanAll() {
  scanLoading.value = true
  try {
    const { data } = await api.post('/cdc-sync/schema-alerts/scan-all')
    const list = data.data?.items || []
    const m = {}
    for (const it of list) {
      if (it?.init_task_id) m[it.init_task_id] = it
    }
    driftById.value = m
    ElMessage.success(`已巡检 ${data.data?.scanned || 0} 个任务，${data.data?.drifted || 0} 个有差异`)
  } finally {
    scanLoading.value = false
  }
}

async function act(row, kind) {
  if (isPending(row) && (kind === 'resume' || kind === 'retry' || kind === 'restart')) {
    ElMessage.warning('CDC 任务须审批通过后才能执行')
    return
  }
  const { data } = await api.post(`/cdc-sync/tasks/${row.id}/${kind}`)
  ElMessage.info(data.message || '已更新')
  await load()
}

async function remove(row) {
  const { value } = await ElMessageBox.prompt(
    `删除任务「${row.name}」将提交审批，通过后清理 Publication / Slot / Connector（保留 StarRocks 表）。请填写审核人：`,
    '删除同步任务',
    { confirmButtonText: '提交审批', inputPlaceholder: '审核人', inputPattern: /\S+/, inputErrorMessage: '审核人必填' },
  )
  const { data } = await api.post(`/cdc-sync/tasks/${row.id}/delete`, { reviewer: value, approver: value })
  ElMessage.success('已提交删除审批')
  if (data.data?.issue_id) router.push('/ops/tickets/' + data.data.issue_id)
  else await load()
}

watch([filtered, pageSize], () => {
  const maxPage = Math.max(1, Math.ceil(filtered.value.length / pageSize.value) || 1)
  if (page.value > maxPage) page.value = maxPage
})

watch(() => route.query.tab, (t) => {
  if (t === 'pending' || t === 'approval_result' || t === 'running') {
    tab.value = t
    onTabChange()
  }
}, { immediate: true })

onMounted(load)
</script>

<style scoped>
.cdc-list :deep(.el-table),
.cdc-list :deep(.el-table__inner-wrapper),
.cdc-list :deep(.el-table__body-wrapper) {
  height: auto !important;
}
.cdc-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.cdc-filters :deep(.el-form-item) { margin-bottom: 8px; }
.ops { display: inline-flex; flex-wrap: nowrap; gap: 4px; white-space: nowrap; }
.ops :deep(.el-button) { margin: 0; }
.cell-2 { line-height: 1.4; }
.cell-2 .sub { color: #8c8c8c; font-size: 12px; }
.hint { color: #8c8c8c; font-size: 13px; margin: 0 0 12px; }
.col-pick { display: flex; flex-direction: column; gap: 6px; }
.col-pick-item { line-height: 1.6; }
</style>
