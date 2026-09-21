<template>
  <div class="mig-detail" v-loading="loading">
    <div class="mig-toolbar">
      <el-button @click="$router.push('/ops/migration')">返回列表</el-button>
    </div>

    <el-alert v-if="task?.error_message" :title="task.error_message" type="error" show-icon :closable="false" class="mb" />
    <el-alert v-if="approvalPending && isCreator" type="info" show-icon :closable="false" class="mb"
      title="当前为「待审批」"
      :description="`请通知指定审核人「${task.assigned_approver || '—'}」在本页完成通过或驳回。超过 8 小时未审核完成，系统将自动取消。`" />
    <el-alert v-else-if="approvalPending && canApprove" type="warning" show-icon :closable="false" class="mb"
      title="该任务待您审批"
      description="请在「审批操作」区域点击「通过」或「驳回」（驳回需填写意见）。" />
    <el-alert v-else-if="approvalPending" type="info" show-icon :closable="false" class="mb"
      title="您当前无法审批该任务"
      :description="`本任务已指定审核人为「${task.assigned_approver || '—'}」，仅该账号可在本页通过/驳回。`" />
    <el-alert v-if="task?.status === 'pre_check_failed'" type="warning" show-icon :closable="false" class="mb"
      title="预检未通过"
      description="请修正配置后重新编辑并预检，或联系任务创建人处理。" />

    <el-card v-if="canApprove" shadow="never" class="mb">
      <div class="sec-title">审批操作</div>
      <el-input v-model="comment" type="textarea" :rows="2" placeholder="通过可填备注；驳回必须填写原因" class="mb" />
      <el-button type="primary" @click="approve">通过</el-button>
      <el-button type="danger" @click="reject">驳回</el-button>
    </el-card>

    <el-card shadow="never" class="mb head-card">
      <div class="head-row">
        <div class="head-main">
          <h2 class="task-name">{{ task?.name || task?.task_name || '任务详情' }}</h2>
          <div class="head-meta">
            <span class="muted">任务 ID</span>
            <span class="mono">{{ task?.id }}</span>
            <el-tag size="small" :type="statusTag(task?.status)">{{ statusText(task?.status) }}</el-tag>
            <el-tag size="small" :type="approvalTag(task?.approval_status)">{{ approvalText(task?.approval_status) }}</el-tag>
          </div>
        </div>
        <div class="head-ops">
          <el-button v-if="canStart" type="primary" @click="act('start')">启动</el-button>
          <el-button v-if="canCancel" type="warning" @click="act('cancel')">取消</el-button>
          <el-button v-if="canRetry" type="primary" @click="act('retry')">重试</el-button>
          <el-button v-if="task?.issue_id" @click="$router.push('/ops/tickets/' + task.issue_id)">查看工单</el-button>
        </div>
      </div>

      <div class="sec-title">任务配置</div>
      <el-descriptions :column="3" border size="small" class="mb">
        <el-descriptions-item label="数据库类型">{{ emptyPlain(task?.db_type) }}</el-descriptions-item>
        <el-descriptions-item label="迁移模式">{{ emptyPlain(task?.migration_mode) }}</el-descriptions-item>
        <el-descriptions-item label="数据中转方式">{{ transferText(task?.transfer_mode) }}</el-descriptions-item>
        <el-descriptions-item label="迁移数据量（估算）">
          <div v-if="estimatedBytes != null">
            <span>{{ formatHumanDataSize(estimatedBytes) }}</span>
            <span class="muted mono"> （{{ Number(estimatedBytes).toLocaleString() }} B）</span>
            <p class="field-note">取值优先 tags.selection.estimated_total_bytes，否则回退任务根字段；与创建时选表汇总一致。</p>
          </div>
          <span v-else class="muted">{{ task?.migration_mode === 'schema_only' ? '暂无 — 仅结构迁移时该项可为 0 或未返回' : '暂无' }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="涉及库数">{{ selectionScale.dbCount ?? '暂无' }}</el-descriptions-item>
        <el-descriptions-item label="选中表数">{{ selectionScale.tableCount ?? '暂无' }}</el-descriptions-item>
        <el-descriptions-item label="启动方式">{{ task?.start_mode === 'scheduled' ? '定时启动' : '立即启动' }}</el-descriptions-item>
        <el-descriptions-item label="定时时间">{{ fmtTime(task?.scheduled_at) }}</el-descriptions-item>
        <el-descriptions-item label="距定时触发">{{ remainText }}</el-descriptions-item>
        <el-descriptions-item label="源 → 目标" :span="3">
          <span class="mono">{{ endpointPair }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <div class="sec-title">审批与流转</div>
      <el-descriptions :column="3" border size="small">
        <el-descriptions-item label="审批状态">
          <el-tag size="small" :type="approvalTag(task?.approval_status)">{{ approvalText(task?.approval_status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="指定审核人">{{ emptyPlain(task?.assigned_approver || task?.assigned_approver_login) }}</el-descriptions-item>
        <el-descriptions-item label="审批人">{{ emptyPlain(task?.approved_by) }}</el-descriptions-item>
        <el-descriptions-item label="审批意见 / 驳回原因" :span="3">{{ emptyPlain(task?.approval_comment) }}</el-descriptions-item>
        <el-descriptions-item label="提交审批时间">{{ fmtTime(task?.submitted_at) }}</el-descriptions-item>
        <el-descriptions-item label="通过时间">{{ fmtTime(task?.approved_at) }}</el-descriptions-item>
        <el-descriptions-item label="驳回时间">{{ fmtTime(task?.rejected_at) }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-card shadow="never">
      <el-tabs v-model="tab">
        <el-tab-pane label="概览" name="overview">
          <div class="overview-block">
            <div class="block-title">总体进度</div>
            <el-progress :percentage="overviewPct" :stroke-width="12" :status="progressStatus" />
            <div class="muted">对象 {{ overviewDone }} / {{ overviewTotal }}</div>
          </div>
          <div class="overview-block">
            <div class="block-title">数据统计与速率</div>
            <div class="stat-row">
              <div class="stat">
                <div class="stat-label">行数</div>
                <div class="stat-value">{{ prog?.completed_rows ?? task?.completed_rows ?? 0 }} / {{ prog?.total_rows ?? task?.total_rows ?? 0 }}</div>
              </div>
              <div class="stat">
                <div class="stat-label">数据量（字节）</div>
                <div class="stat-value">{{ prog?.completed_bytes ?? task?.completed_bytes ?? 0 }} / {{ byteTotal }}</div>
              </div>
              <div class="stat">
                <div class="stat-label">行/秒</div>
                <div class="stat-value">{{ Number(prog?.speed_rows_per_sec || 0).toFixed(1) }}</div>
              </div>
              <div class="stat">
                <div class="stat-label">MB/s</div>
                <div class="stat-value">{{ mbps.toFixed(2) }}</div>
              </div>
            </div>
          </div>
          <div class="overview-block">
            <div class="block-title">耗时与当前对象</div>
            <el-descriptions :column="1" size="small">
              <el-descriptions-item label="已用时间（秒）">{{ emptyNum(prog?.elapsed_seconds) }}</el-descriptions-item>
              <el-descriptions-item label="预估剩余（秒）">{{ emptyNum(prog?.estimated_remaining_seconds) }}</el-descriptions-item>
              <el-descriptions-item label="当前对象">{{ emptyPlain(prog?.current_object) }}</el-descriptions-item>
            </el-descriptions>
          </div>
        </el-tab-pane>

        <el-tab-pane label="对象明细" name="objects">
          <el-select v-model="objStatus" clearable placeholder="按状态筛选" style="width:160px;margin-bottom:12px">
            <el-option label="待迁移" value="pending" />
            <el-option label="迁移中" value="running" />
            <el-option label="已完成" value="completed" />
            <el-option label="失败" value="failed" />
            <el-option label="跳过" value="skipped" />
          </el-select>
          <el-table :data="filteredObjects" border empty-text="暂无对象">
            <el-table-column label="源端库/表" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ objectName(row, 'source') }}</template>
            </el-table-column>
            <el-table-column label="目标端库/表" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ objectName(row, 'target') }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="objStatusTag(row.status)">{{ objStatusText(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="行数进度" width="180">
              <template #default="{ row }">{{ row.completed_rows || 0 }} / {{ row.total_rows || 0 }} ({{ rowPct(row) }}%)</template>
            </el-table-column>
            <el-table-column label="数据量" width="180">
              <template #default="{ row }">{{ row.completed_bytes || 0 }} / {{ row.total_bytes || 0 }}</template>
            </el-table-column>
            <el-table-column prop="error_message" label="错误信息" min-width="200" show-overflow-tooltip />
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="日志" name="logs">
          <div class="log-tools">
            <el-radio-group v-model="logLevel" size="small">
              <el-radio-button label="">全部</el-radio-button>
              <el-radio-button label="INFO">INFO</el-radio-button>
              <el-radio-button label="WARN">WARN</el-radio-button>
              <el-radio-button label="ERROR">ERROR</el-radio-button>
            </el-radio-group>
            <el-input v-model="logKw" clearable placeholder="关键字过滤" style="width:200px" />
            <el-button @click="exportLog">导出日志</el-button>
          </div>
          <div class="log-box">
            <div v-for="l in pagedLogs" :key="l.id" class="log-line">
              <span class="muted">{{ l.created_at }}</span>
              <span :class="logLevelClass(l.log_level)"> [{{ l.log_level }}] </span>
              <span>{{ l.message }}</span>
              <span v-if="l.detail"> — {{ l.detail }}</span>
            </div>
            <div v-if="!pagedLogs.length" class="muted">暂无日志</div>
          </div>
          <div class="log-pager">
            <el-pagination
              v-model:current-page="logPage"
              v-model:page-size="logPageSize"
              :total="filteredLogs.length"
              :page-sizes="[10, 20, 50, 100]"
              layout="total, sizes, prev, pager, next"
              small
            />
          </div>
        </el-tab-pane>

        <el-tab-pane label="预检" name="precheck">
          <el-table :data="prechecks" border empty-text="暂无预检记录">
            <el-table-column prop="check_item" label="检查项" width="180" />
            <el-table-column prop="check_level" label="级别" width="100" />
            <el-table-column label="状态" width="120">
              <template #default="{ row }">
                <el-tag size="small" :type="precheckTag(row.check_status)">{{ row.check_status }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="详情" min-width="280">
              <template #default="{ row }">{{ row.message || row.detail || '—' }}</template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="校验" name="verify">
          <div v-if="!verifyEnabled" class="muted">任务完成后可在此触发数据校验。</div>
          <template v-else>
            <el-button type="primary" :loading="verifying" @click="triggerVerify">触发校验</el-button>
            <el-table :data="verifyRows" border empty-text="暂无校验结果" class="mt">
              <el-table-column prop="table" label="表" min-width="160" />
              <el-table-column prop="check_type" label="校验类型" width="120" />
              <el-table-column prop="status" label="状态" width="100" />
              <el-table-column prop="detail" label="说明" min-width="240" show-overflow-tooltip />
            </el-table>
          </template>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

const route = useRoute()
const loading = ref(false)
const task = ref(null)
const objects = ref([])
const logs = ref([])
const prechecks = ref([])
const prog = ref(null)
const verifyRows = ref([])
const tab = ref('overview')
const comment = ref('')
const objStatus = ref('')
const logLevel = ref('')
const logKw = ref('')
const logPage = ref(1)
const logPageSize = ref(10)
const verifying = ref(false)
const remainSec = ref(null)
let timer = null
let remainTimer = null

const me = (() => {
  try { return JSON.parse(localStorage.getItem('user') || '{}') } catch { return {} }
})()

const isCreator = computed(() => task.value && me.username && me.username === task.value.created_by)
const approvalPending = computed(() => task.value?.approval_status === 'pending')
const canApprove = computed(() => {
  if (!task.value || task.value.approval_status !== 'pending') return false
  if (isCreator.value) return false
  if (task.value.assigned_approver && task.value.assigned_approver !== me.username) return false
  return true
})
const canStart = computed(() => ['pending', 'scheduled'].includes(task.value?.status) && task.value?.approval_status === 'approved')
const canCancel = computed(() => ['pending', 'scheduled', 'running', 'pre_checking'].includes(task.value?.status))
const canRetry = computed(() => ['failed', 'pre_check_failed', 'canceled'].includes(task.value?.status))
const verifyEnabled = computed(() => ['completed', 'executed'].includes(task.value?.status))

function readSelection(taskVal) {
  const tags = taskVal?.tags
  const sel = tags?.selection
  if (sel && typeof sel === 'object') return sel
  return {}
}
function readNonNeg(v) {
  if (v == null || v === '') return undefined
  const n = Number(v)
  if (!Number.isFinite(n) || n < 0) return undefined
  return Math.floor(n)
}
const estimatedBytes = computed(() => {
  const t = task.value
  if (!t) return undefined
  const fromSel = readNonNeg(readSelection(t).estimated_total_bytes)
  if (fromSel != null) return fromSel
  return readNonNeg(t.total_bytes ?? t.estimated_total_bytes ?? t.objects_total_bytes)
})
const selectionScale = computed(() => {
  const sel = readSelection(task.value)
  return {
    dbCount: readNonNeg(sel.db_count),
    tableCount: readNonNeg(sel.table_count),
  }
})
const endpointPair = computed(() => {
  const t = task.value
  if (!t) return '—'
  const src = `${t.source_host || ''}${t.source_port ? ':' + t.source_port : ''}`
  const tgt = `${t.target_host || ''}${t.target_port ? ':' + t.target_port : ''}`
  return `${src || '—'} → ${tgt || '—'}`
})
const overviewTotal = computed(() => prog.value?.total_objects ?? task.value?.total_objects ?? 0)
const overviewDone = computed(() => prog.value?.completed_objects ?? task.value?.completed_objects ?? 0)
const overviewPct = computed(() => {
  if (task.value?.status === 'completed' || task.value?.status === 'executed') return 100
  if (overviewTotal.value > 0) return Math.min(100, Math.round((overviewDone.value * 100) / overviewTotal.value))
  return 0
})
const progressStatus = computed(() => {
  const s = task.value?.status
  if (s === 'failed' || s === 'pre_check_failed') return 'exception'
  if (s === 'completed' || s === 'executed') return 'success'
  return undefined
})
const byteTotal = computed(() => prog.value?.total_bytes ?? estimatedBytes.value ?? task.value?.total_bytes ?? 0)
const mbps = computed(() => Number(prog.value?.speed_bytes_per_sec || 0) / (1024 * 1024))
const remainText = computed(() => {
  if (remainSec.value == null) return '暂无'
  if (remainSec.value <= 0) return '已到点'
  const h = Math.floor(remainSec.value / 3600)
  const m = Math.floor((remainSec.value % 3600) / 60)
  const s = remainSec.value % 60
  if (h > 0) return `${h}小时${m}分${s}秒`
  if (m > 0) return `${m}分${s}秒`
  return `${s}秒`
})
const filteredObjects = computed(() => {
  if (!objStatus.value) return objects.value
  return objects.value.filter((r) => r.status === objStatus.value)
})
const filteredLogs = computed(() => {
  return logs.value.filter((l) => {
    if (logLevel.value && l.log_level !== logLevel.value) return false
    if (logKw.value.trim()) {
      const q = logKw.value.trim().toLowerCase()
      return String(l.message || '').toLowerCase().includes(q) || String(l.detail || '').toLowerCase().includes(q)
    }
    return true
  })
})
const pagedLogs = computed(() => {
  const start = (logPage.value - 1) * logPageSize.value
  return filteredLogs.value.slice(start, start + logPageSize.value)
})

function emptyPlain(v) {
  const s = v != null ? String(v).trim() : ''
  return s || '暂无'
}
function emptyNum(v) {
  if (v == null || v === '' || v === 0) return '—'
  return v
}
function fmtTime(v) {
  if (!v) return '暂无'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function formatHumanDataSize(bytes) {
  if (bytes === 0) return '0 B'
  if (bytes >= 1024 ** 4) return `${(bytes / 1024 ** 4).toFixed(2)} TB`
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(2)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}
function transferText(t) {
  return { direct: '直连', oss: 'OSS 中转' }[t] || t || '暂无'
}
function statusText(s) {
  if (task.value?.approval_status === 'pending' && (s === 'pending' || s === 'init')) return '待审批'
  return {
    pending: '待执行', init: '待执行', scheduled: '待定时触发',
    pre_checking: '预检中', pre_check_failed: '预检失败',
    running: '迁移中', executed: '已完成', completed: '已完成',
    failed: '失败', canceled: '已取消',
  }[s] || s || '—'
}
function statusTag(s) {
  if (s === 'executed' || s === 'completed') return 'success'
  if (s === 'running' || s === 'pre_checking' || s === 'scheduled' || s === 'pending') return 'warning'
  if (s === 'failed' || s === 'pre_check_failed') return 'danger'
  return 'info'
}
function approvalText(s) {
  return { draft: '草稿', pending: '待审批', approved: '已通过', rejected: '已驳回', canceled: '已撤销' }[s] || s || '暂无'
}
function approvalTag(s) {
  if (s === 'approved') return 'success'
  if (s === 'pending') return 'warning'
  if (s === 'rejected') return 'danger'
  return 'info'
}
function objStatusText(s) {
  return { pending: '待迁移', running: '迁移中', completed: '已完成', failed: '失败', skipped: '跳过' }[s] || s || '—'
}
function objStatusTag(s) {
  if (s === 'completed') return 'success'
  if (s === 'running') return 'warning'
  if (s === 'failed') return 'danger'
  return 'info'
}
function objectName(row, side) {
  const db = side === 'source' ? row.source_db_name : row.target_db_name
  const schema = side === 'source' ? row.source_schema : row.target_schema
  const table = side === 'source' ? row.source_table_name : row.target_table_name
  return schema ? `${db}.${schema}.${table}` : `${db}.${table}`
}
function rowPct(row) {
  const t = Number(row.total_rows || 0)
  const c = Number(row.completed_rows || 0)
  return t > 0 ? Math.round((c * 100) / t) : 0
}
function precheckTag(s) {
  if (s === 'passed') return 'success'
  if (s === 'failed') return 'danger'
  if (s === 'warning') return 'warning'
  return 'info'
}
function logLevelClass(lv) {
  if (lv === 'ERROR') return 'lv-error'
  if (lv === 'WARN') return 'lv-warn'
  return ''
}

function syncRemain() {
  const t = task.value
  if (!t) { remainSec.value = null; return }
  const server = t.seconds_until_scheduled
  if (server != null && server !== '') {
    remainSec.value = Math.max(0, Math.floor(Number(server)))
    return
  }
  if (t.start_mode === 'scheduled' && t.scheduled_at) {
    const diff = Math.floor((new Date(t.scheduled_at).getTime() - Date.now()) / 1000)
    remainSec.value = Number.isNaN(diff) ? null : Math.max(0, diff)
    return
  }
  remainSec.value = null
}

function mapVerifyRows(raw) {
  const result = raw?.data ?? raw
  let items = []
  if (Array.isArray(result)) items = result
  else if (Array.isArray(result?.list)) items = result.list
  else if (Array.isArray(result?.details?.objects)) items = result.details.objects
  return items.map((r) => {
    if (typeof r.row_match === 'boolean') {
      return {
        table: r.table,
        check_type: r.check_type || '行数比对',
        status: r.row_match ? '一致' : '不一致',
        detail: r.detail || `源 ${r.source_rows ?? '-'} 行 / 目标 ${r.target_rows ?? '-'} 行`,
      }
    }
    if (typeof r.schema_match === 'boolean') {
      return {
        table: r.table,
        check_type: r.check_type || '结构比对',
        status: r.schema_match ? '一致' : '不一致',
        detail: r.detail || (r.schema_match ? '列、约束、索引一致' : '结构不一致'),
      }
    }
    return { table: r.table, check_type: r.check_type, status: r.status, detail: r.detail }
  })
}

async function load() {
  const id = route.params.id
  loading.value = true
  try {
    const [t, o, l, p, g] = await Promise.all([
      api.get(`/migration/tasks/${id}`),
      api.get(`/migration/tasks/${id}/objects`),
      api.get(`/migration/tasks/${id}/logs`, { params: { since_id: 0, limit: 500 } }),
      api.get(`/migration/tasks/${id}/prechecks`),
      api.get(`/migration/tasks/${id}/progress`).catch(() => ({ data: {} })),
    ])
    task.value = t.data.data
    objects.value = o.data.data || []
    logs.value = l.data.data || []
    prechecks.value = p.data.data || []
    prog.value = g.data.data || null
    syncRemain()
    if (verifyEnabled.value) {
      try {
        const vr = await api.get(`/migration/tasks/${id}/verify`)
        verifyRows.value = mapVerifyRows(vr.data)
      } catch { /* ignore */ }
    }
  } finally {
    loading.value = false
  }
}

async function act(kind) {
  await api.post(`/migration/tasks/${route.params.id}/${kind}`)
  ElMessage.success('已提交')
  await load()
}
async function approve() {
  await api.post(`/migration/tasks/${route.params.id}/approve`, { comment: comment.value })
  ElMessage.success('已通过')
  await load()
}
async function reject() {
  if (!comment.value.trim()) {
    ElMessage.warning('驳回原因必填')
    return
  }
  await ElMessageBox.confirm('确认驳回该任务？', '驳回', { type: 'warning' })
  await api.post(`/migration/tasks/${route.params.id}/reject`, { comment: comment.value })
  ElMessage.success('已驳回')
  await load()
}
async function triggerVerify() {
  verifying.value = true
  try {
    const { data } = await api.post(`/migration/tasks/${route.params.id}/verify`, { row_count: true })
    verifyRows.value = mapVerifyRows(data)
    ElMessage.success('校验完成')
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '校验失败')
  } finally {
    verifying.value = false
  }
}
function exportLog() {
  const text = filteredLogs.value.map((l) => `${l.created_at} [${l.log_level}] ${l.message}${l.detail ? ` ${l.detail}` : ''}`).join('\n')
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `migration-${route.params.id}.log`
  a.click()
  URL.revokeObjectURL(url)
}

watch([logLevel, logKw], () => { logPage.value = 1 })

onMounted(async () => {
  await load()
  timer = setInterval(() => {
    if (['running', 'scheduled', 'pre_checking'].includes(task.value?.status)) load()
  }, 4000)
  remainTimer = setInterval(() => {
    if (remainSec.value != null && remainSec.value > 0) remainSec.value -= 1
  }, 1000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
  if (remainTimer) clearInterval(remainTimer)
})
</script>

<style scoped>
.mig-detail { padding-bottom: 32px; max-width: 1400px; }
.mig-toolbar { margin-bottom: 12px; }
.mb { margin-bottom: 16px; }
.mt { margin-top: 12px; }
.head-card :deep(.el-card__body) { padding-top: 20px; }
.head-row { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; flex-wrap: wrap; margin-bottom: 20px; padding: 16px 20px; background: #fafafa; border: 1px solid #f0f0f0; border-radius: 8px; }
.head-ops { display: flex; gap: 8px; flex-wrap: wrap; }
.task-name { margin: 0 0 8px; font-size: 22px; font-weight: 600; color: #111827; }
.head-meta { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 13px; word-break: break-all; }
.muted { color: #9ca3af; font-size: 13px; }
.field-note { margin: 6px 0 0; color: #9ca3af; font-size: 12px; line-height: 1.5; }
.sec-title { margin: 8px 0 12px; font-size: 15px; font-weight: 500; color: #434343; }
.overview-block { margin-bottom: 20px; padding: 12px 16px; border: 1px solid #f0f0f0; border-radius: 6px; background: #fff; }
.block-title { font-size: 14px; font-weight: 500; margin-bottom: 10px; color: #374151; }
.stat-row { display: flex; gap: 32px; flex-wrap: wrap; }
.stat-label { color: #9ca3af; font-size: 13px; margin-bottom: 4px; }
.stat-value { font-size: 22px; font-weight: 500; color: #111827; }
.log-tools { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 8px; }
.log-box { height: 360px; overflow: auto; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 12px; background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 6px; padding: 8px; }
.log-line { white-space: pre-wrap; word-break: break-all; padding: 2px 0; }
.log-pager { display: flex; justify-content: flex-end; margin-top: 8px; }
.lv-error { color: #dc2626; }
.lv-warn { color: #d97706; }
</style>
