<template>
  <div class="review-page" v-loading="loading">
    <div v-if="item" class="review-split">
      <aside class="review-aside">
        <div class="meta-head">
          <div>
            <div class="meta-label">工作流名称：</div>
            <div class="meta-value">{{ workflowName }}</div>
          </div>
          <el-button
            type="primary"
            size="small"
            :disabled="!canCancel"
            @click="act('cancel')"
          >取消流程</el-button>
        </div>
        <div class="meta-block">
          <div class="meta-label">发起时间：</div>
          <div class="meta-value">{{ fmtTime(item.created_at) }}</div>
        </div>
        <div class="meta-block">
          <div class="meta-label">发起人</div>
          <div class="user-card">
            <el-avatar :size="36" class="user-avatar">{{ userInitial(item.submitter_name) }}</el-avatar>
            <div>
              <div class="user-name">{{ item.submitter_name || '-' }}</div>
              <div class="user-sub">提交人</div>
            </div>
          </div>
        </div>

        <div class="meta-label">执行流程</div>
        <div class="flow-card" :class="approveCardClass">
          <div class="flow-card-head">指定人员审批</div>
          <div class="flow-card-body">
            <template v-if="item.status === 'pending'">
              <div class="approver-line">审批人员：{{ item.assigned_to || '-' }}</div>
              <div class="timeout-hint">提交后超过 8 小时未审核完成，系统将自动取消</div>
              <div class="remark-box">
                <div class="remark-label">附言：</div>
                <el-input v-model="comment" type="textarea" :rows="3" placeholder="请输入附言" />
                <div class="remark-actions">
                  <el-button type="danger" size="small" :disabled="!canApprove" :loading="acting" @click="act('reject')">驳回</el-button>
                  <el-button type="primary" size="small" :disabled="!canApprove" :loading="acting" @click="act('approve')">通过</el-button>
                </div>
              </div>
            </template>
            <template v-else>
              <div class="approve-result">
                <div class="user-card compact">
                  <el-avatar :size="28">{{ userInitial(item.assigned_to) }}</el-avatar>
                  <span>{{ item.assigned_to || '-' }}</span>
                </div>
                <span v-if="item.status === 'rejected'" class="status-text danger">已驳回</span>
                <span v-else-if="item.status === 'canceled'" class="status-text muted">已取消</span>
                <span v-else class="status-text success">已通过</span>
              </div>
              <div v-if="item.reject_reason" class="remark-text">附言：{{ item.reject_reason }}</div>
            </template>
          </div>
        </div>

        <div v-if="showExecCard" class="flow-card" :class="execCardClass">
          <div class="flow-card-head">执行操作</div>
          <div v-if="isFlashback" class="flow-card-body exec-body">
            <div class="exec-wait">{{ flashbackExecText }}</div>
            <el-button v-if="flashbackTaskID" type="primary" size="small" @click="$router.push('/ops/flashback/history/' + flashbackTaskID)">
              查看闪回任务
            </el-button>
          </div>
          <div v-else-if="isCDC" class="flow-card-body exec-body">
            <div class="exec-wait">{{ cdcExecText }}</div>
            <el-button v-if="cdcTaskID" type="primary" size="small" @click="$router.push('/ops/cdc/' + cdcTaskID)">
              查看 CDC 任务
            </el-button>
          </div>
          <div v-else-if="isMigration" class="flow-card-body exec-body">
            <div class="exec-wait">{{ migrationExecText }}</div>
            <el-button v-if="migrationTaskID" type="primary" size="small" @click="$router.push('/ops/migration/' + migrationTaskID)">
              查看迁移任务
            </el-button>
          </div>
          <div v-else-if="isDQL" class="flow-card-body download-body">
            <div class="download-row">
              <span class="exec-wait">点击下载</span>
              <el-button type="primary" :disabled="!canDownload" :loading="downloading" @click="openExport">
                下载
              </el-button>
            </div>
            <div class="download-hint">仅限工单发起人可下载</div>
          </div>
          <div v-else class="flow-card-body exec-body">
            <template v-if="item.status === 'approved'">
              <div class="exec-wait">{{ scheduledWaitText }}</div>
              <el-button type="primary" size="small" :loading="acting" @click="act('execute')">立即执行</el-button>
            </template>
            <template v-else-if="item.status === 'failed'">
              <div class="status-text danger">执行失败</div>
              <div class="user-sub">{{ execSummary || fmtTime(item.updated_at) }}</div>
              <el-button type="primary" size="small" :loading="acting" @click="act('execute')">重试执行</el-button>
            </template>
            <template v-else-if="item.status === 'executed'">
              <div class="status-text success">{{ execSkipped ? '已跳过连库执行' : '执行完成' }}</div>
              <div class="user-sub">{{ execSummary || fmtTime(item.updated_at) }}</div>
            </template>
          </div>
        </div>

        <div class="wf-title">工作流</div>
        <div class="wf-graph">
          <div class="wf-node" :class="wfApproveClass">
            <div class="wf-dot" />
            <div>
              <div class="wf-name">指定人员审批</div>
              <div class="wf-sub">{{ wfApproveSub }}</div>
            </div>
          </div>
          <div class="wf-line" />
          <div class="wf-node" :class="wfExecClass">
            <div class="wf-dot" />
            <div>
              <div class="wf-name">执行操作</div>
              <div class="wf-sub">{{ wfExecSub }}</div>
            </div>
          </div>
        </div>
      </aside>

      <section class="review-main">
        <div class="sec-title">申请信息</div>
        <el-descriptions :column="1" border class="apply-desc" label-width="140px">
          <el-descriptions-item v-for="f in applyFields" :key="f.key" :label="f.label">
            <template v-if="f.kind === 'type-tag'">
              <el-tag size="small" type="primary">{{ f.value }}</el-tag>
            </template>
            <template v-else-if="f.kind === 'db-tag'">
              <el-tag size="small" type="primary" effect="plain">{{ f.value }}</el-tag>
            </template>
            <template v-else-if="f.kind === 'sql'">
              <pre class="sql-pre">{{ f.value }}</pre>
            </template>
            <template v-else-if="f.kind === 'schedule'">
              <el-tag size="small" type="warning" effect="plain">{{ f.value }}</el-tag>
            </template>
            <span v-else>{{ f.value }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <template v-if="showSqlImpact">
          <div class="sec-title mt">预检测SQL影响</div>
          <div class="impact-card">
            <div class="impact-head">
              <span class="db-pill">数据库</span>
              <span class="db-path">{{ dbPath }}</span>
            </div>
            <el-table :data="precheckRows" border size="small" empty-text="暂无数据">
              <el-table-column prop="sql" label="SQL语句" min-width="280" show-overflow-tooltip />
              <el-table-column prop="affected" label="影响行数" width="120" />
            </el-table>
          </div>

          <div class="sec-title mt">执行SQL影响</div>
          <div class="impact-card">
            <div class="impact-head">
              <span class="db-pill">数据库</span>
              <span class="db-path">{{ dbPath }}</span>
              <el-tag size="small" :type="execTagType">{{ execTagText }}</el-tag>
            </div>
            <el-table :data="execRows" border size="small" :empty-text="execEmptyText">
              <el-table-column prop="sql" label="SQL语句" min-width="220" show-overflow-tooltip />
              <el-table-column prop="status" label="结果" width="90" />
              <el-table-column prop="start" label="开始执行时间" width="170" />
              <el-table-column prop="end" label="结束执行时间" width="170" />
              <el-table-column prop="cost" label="执行时间 (ms)" width="130" />
              <el-table-column prop="affected" label="影响行数" width="110" />
              <el-table-column prop="error" label="错误" min-width="160" show-overflow-tooltip />
            </el-table>
          </div>
        </template>
      </section>
    </div>

    <el-dialog v-model="fmtVisible" title="选择下载格式" width="420px" append-to-body destroy-on-close>
      <div class="fmt-label">请选择导出格式：</div>
      <el-select v-model="fileFormat" style="width:100%">
        <el-option label="CSV" value="csv" />
        <el-option label="JSON" value="json" />
        <el-option label="SQL" value="sql" />
      </el-select>
      <template #footer>
        <el-button @click="fmtVisible = false">取消</el-button>
        <el-button type="primary" :loading="downloading" @click="doExport">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

const SQL_TYPES = new Set(['ddl', 'dml'])

const route = useRoute()
const item = ref(null)
const inst = ref(null)
const loading = ref(false)
const acting = ref(false)
const downloading = ref(false)
const comment = ref('')
const fmtVisible = ref(false)
const fileFormat = ref('csv')

const me = (() => {
  try { return JSON.parse(localStorage.getItem('user') || '{}') } catch { return {} }
})()

function fmtTime(v) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function userInitial(name) {
  const s = String(name || '').trim()
  return s ? s.slice(0, 1).toUpperCase() : '?'
}

function isPrivileged() {
  return ['admin', 'platform_admin', 'super_admin', 'operator', 'dba'].includes(String(me.role || '').toLowerCase())
}

const workflowName = computed(() => {
  const it = item.value
  if (!it) return '-'
  const prefix = String(it.issue_type || 'TK').toUpperCase()
  return `${prefix}-${it.ticket_no || it.id}`
})

const dbType = computed(() => String(inst.value?.db_type || '').toLowerCase())
const dbEnv = computed(() => inst.value?.idc || '')
const dbPath = computed(() => {
  const env = dbEnv.value || '-'
  const name = item.value?.db_name || '-'
  return `${env} / ${name}`
})

const showSqlImpact = computed(() => SQL_TYPES.has(item.value?.issue_type))

const dqlExtra = computed(() => {
  const raw = item.value?.need_rollback
  if (!raw || item.value?.issue_type !== 'dql') return null
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
})

const createdbExtra = computed(() => {
  const raw = item.value?.need_rollback
  if (!raw || item.value?.issue_type !== 'createdb') return null
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
})

const applyFields = computed(() => {
  const it = item.value
  if (!it) return []
  const fields = [
    { key: 'reason', label: '原因', value: it.description || '-' },
    { key: 'db_type', label: '数据库类型', kind: 'type-tag', value: dbType.value || '-' },
    { key: 'db', label: '数据库', kind: 'db-tag', value: dbPath.value },
  ]
  if (it.sql_content) {
    fields.push({
      key: 'sql',
      label: (it.issue_type === 'flashback' || it.issue_type === 'cdc_sync' || it.issue_type === 'migration') ? '任务摘要' : '执行SQL',
      kind: 'sql',
      value: it.sql_content,
    })
    fields.push({
      key: 'schedule',
      label: (it.issue_type === 'flashback' || it.issue_type === 'cdc_sync' || it.issue_type === 'migration') ? '执行方式' : 'SQL执行时间',
      kind: 'schedule',
      value: it.issue_type === 'flashback'
        ? '审批通过后自动执行闪回解析'
        : (it.issue_type === 'cdc_sync'
          ? '审批通过后自动执行 CDC 同步'
          : (it.issue_type === 'migration'
            ? '审批通过后由后端按启动方式自动执行'
            : (it.scheduled_at ? `定时 ${fmtTime(it.scheduled_at)}` : '审批完立即执行'))),
    })
  }
  if (it.issue_type === 'createdb' && createdbExtra.value) {
    if (createdbExtra.value.username) fields.push({ key: 'acct', label: '数据库账号', value: createdbExtra.value.username })
    if (createdbExtra.value.permissions?.length) {
      const map = { r: '读操作', c: '写操作', u: '更新操作', d: '删除操作' }
      fields.push({ key: 'perms', label: '账号权限', value: createdbExtra.value.permissions.map((p) => map[p] || p).join('， ') })
    }
  }
  if (it.issue_type === 'dql' && dqlExtra.value) {
    fields.push({ key: 'sensitive', label: '原文导出敏感字段', value: dqlExtra.value.need_sensitive_data ? '开启' : '关闭' })
    fields.push({ key: 'rows', label: '预估行数', value: String(dqlExtra.value.row_count ?? '-') })
    if (dqlExtra.value.dba_assigned_to) fields.push({ key: 'dba', label: 'DBA审批人', value: dqlExtra.value.dba_assigned_to })
    if (dqlExtra.value.security_assigned_to) fields.push({ key: 'sec', label: '安全审批人', value: dqlExtra.value.security_assigned_to })
  }
  fields.push({ key: 'approver', label: '指定审批人', value: it.assigned_to || '-' })
  fields.push({ key: 'created', label: '申请时间', value: fmtTime(it.created_at) })
  return fields
})

const canApprove = computed(() => {
  if (item.value?.status !== 'pending') return false
  if (isPrivileged()) return true
  return String(item.value?.assigned_to || '').toLowerCase() === String(me.username || '').toLowerCase()
})

const canCancel = computed(() => {
  if (item.value?.status !== 'pending') return false
  if (isPrivileged()) return true
  return String(item.value?.submitter_name || '').toLowerCase() === String(me.username || '').toLowerCase()
})

const isDQL = computed(() => item.value?.issue_type === 'dql')
const isFlashback = computed(() => item.value?.issue_type === 'flashback')
const flashbackTaskID = computed(() => {
  const m = String(item.value?.sql_content || '').match(/^flashback\s+([0-9a-fA-F-]{36})/i)
  return m?.[1] || ''
})
const flashbackExecText = computed(() => {
  const s = item.value?.status
  if (s === 'rejected') return '审批已驳回，任务不会执行'
  if (s === 'canceled') return '审批已撤销，任务不会执行'
  return '审批通过后自动执行闪回解析，可在任务页查看进度'
})
const isCDC = computed(() => item.value?.issue_type === 'cdc_sync')
const cdcTaskID = computed(() => {
  const m = String(item.value?.sql_content || '').match(/^cdc_sync\s+\S+\s+([0-9a-fA-F-]{36})/i)
  return m?.[1] || ''
})
const cdcExecText = computed(() => {
  const s = item.value?.status
  if (s === 'rejected') return '审批已驳回，任务不会执行'
  if (s === 'canceled') return '审批已撤销，任务不会执行'
  return '审批通过后自动执行 CDC 同步，可在任务页查看进度'
})
const isMigration = computed(() => item.value?.issue_type === 'migration')
const migrationTaskID = computed(() => {
  const m = String(item.value?.sql_content || '').match(/^migration\s+([0-9a-fA-F-]{36})/i)
  return m?.[1] || ''
})
const migrationExecText = computed(() => {
  const s = item.value?.status
  if (s === 'rejected') return '审批已驳回，任务不会执行'
  if (s === 'canceled') return '审批已撤销，任务不会执行'
  return '审批通过后由后端按启动方式自动执行，可在任务页查看进度'
})
const isSubmitter = computed(() => (
  String(item.value?.submitter_name || '').toLowerCase() === String(me.username || '').toLowerCase()
))
const isCreateDB = computed(() => item.value?.issue_type === 'createdb')
const showExecCard = computed(() => {
  const s = item.value?.status
  if (s === 'pending' || s === 'rejected' || s === 'canceled') return false
  if (isCreateDB.value) {
    return s === 'failed'
  }
  return true
})
const canDownload = computed(() => (
  isDQL.value && isSubmitter.value && (item.value?.status === 'approved' || item.value?.status === 'executed')
))

const approveCardClass = computed(() => {
  const s = item.value?.status
  if (s === 'pending') return 'is-active'
  if (s === 'rejected') return 'is-danger'
  return 'is-done'
})

const execCardClass = computed(() => {
  const s = item.value?.status
  if (s === 'executed') return 'is-done'
  if (s === 'failed') return 'is-danger'
  return 'is-wait'
})

const wfApproveClass = computed(() => {
  const s = item.value?.status
  if (s === 'pending') return 'is-current'
  if (s === 'rejected' || s === 'canceled') return 'is-fail'
  return 'is-ok'
})

const wfExecClass = computed(() => {
  const s = item.value?.status
  if (s === 'approved') return 'is-current'
  if (s === 'executed') return 'is-ok'
  if (s === 'failed') return 'is-fail'
  return 'is-idle'
})

const wfApproveSub = computed(() => {
  const s = item.value?.status
  if (s === 'pending') return '待审批'
  if (s === 'rejected') return '已驳回'
  if (s === 'canceled') return '已取消'
  return '已通过'
})

const wfExecSub = computed(() => {
  const s = item.value?.status
  if (s === 'executed') return isDQL.value ? '已导出' : (execSkipped.value ? '已跳过连库' : '已执行')
  if (s === 'failed') return '执行失败'
  if (s === 'approved') return isDQL.value ? '点击下载' : ((isFlashback.value || isCDC.value || isMigration.value || isCreateDB.value) ? (item.value?.scheduled_at ? '等待定时执行' : '已自动执行') : '等待执行')
  return '未开始'
})

const execObj = computed(() => item.value?.exec || {})
const execSkipped = computed(() => Boolean(execObj.value?.skipped))
const execSummary = computed(() => execObj.value?.summary || execObj.value?.message || '')
const scheduledWaitText = computed(() => (
  item.value?.scheduled_at ? '已预约，等待定时或手动执行' : '审批通过后将自动执行；若未触发可手动执行'
))

const execTagText = computed(() => {
  const s = item.value?.status
  if (s === 'executed') return execSkipped.value ? '已跳过' : '已执行'
  if (s === 'failed') return '失败'
  if (s === 'approved') return '待执行'
  return '未执行'
})

const execTagType = computed(() => {
  const s = item.value?.status
  if (s === 'executed') return 'success'
  if (s === 'failed') return 'danger'
  if (s === 'approved') return 'warning'
  return 'info'
})

const execEmptyText = computed(() => (
  item.value?.status === 'executed' || item.value?.status === 'failed' ? '暂无数据' : '暂无数据，执行后获取'
))

const precheckRows = computed(() => [])
const execRows = computed(() => {
  const recs = execObj.value?.records
  if (!Array.isArray(recs)) return []
  return recs.map((r) => {
    const start = r.started_at || ''
    const end = r.ended_at || ''
    let cost = ''
    if (start && end) {
      const a = Date.parse(start)
      const b = Date.parse(end)
      if (!Number.isNaN(a) && !Number.isNaN(b) && b >= a) cost = String(b - a)
    }
    const statusMap = { success: '成功', failed: '失败', skipped: '跳过' }
    return {
      sql: r.sql || '',
      status: statusMap[r.status] || r.status || '',
      start: fmtTime(start),
      end: fmtTime(end),
      cost,
      affected: r.affected_row ?? '',
      error: r.error_message || '',
    }
  })
})

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/tickets/' + route.params.id)
    item.value = data.data
    inst.value = null
    if (item.value?.instance_id) {
      try {
        const instRes = await api.get('/instances/' + item.value.instance_id)
        inst.value = instRes.data.data || null
      } catch { /* keep empty */ }
    }
  } finally {
    loading.value = false
  }
}

function openExport() {
  if (!canDownload.value) {
    ElMessage.warning('仅限工单发起人可下载')
    return
  }
  fileFormat.value = 'csv'
  fmtVisible.value = true
}

function filenameFromHeader(header, fallback) {
  const raw = String(header || '')
  const star = raw.match(/filename\*=UTF-8''([^;]+)/i)
  if (star?.[1]) {
    try { return decodeURIComponent(star[1]) } catch { /* ignore */ }
  }
  const plain = raw.match(/filename="?([^";]+)"?/i)
  return plain?.[1] || fallback
}

async function doExport() {
  downloading.value = true
  try {
    const res = await api.post(
      `/tickets/${item.value.id}/export`,
      { file_format: fileFormat.value },
      { responseType: 'blob', timeout: 120000 },
    )
    const blob = res.data instanceof Blob ? res.data : new Blob([res.data])
    const name = filenameFromHeader(res.headers['content-disposition'], `dql_export.${fileFormat.value}`)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = name
    a.click()
    URL.revokeObjectURL(url)
    fmtVisible.value = false
    ElMessage.success('开始下载')
    await load()
  } finally {
    downloading.value = false
  }
}

async function act(kind) {
  acting.value = true
  try {
    if (kind === 'reject') {
      if (!comment.value.trim()) {
        const { value } = await ElMessageBox.prompt('驳回原因', '驳回', { inputPlaceholder: '原因' })
        comment.value = value || ''
      }
      await api.put(`/tickets/${item.value.id}/reject`, { reason: comment.value })
    } else if (kind === 'approve') {
      await api.put(`/tickets/${item.value.id}/approve`)
    } else if (kind === 'cancel') {
      await ElMessageBox.confirm('确定取消该流程？', '取消流程', { type: 'warning' })
      await api.put(`/tickets/${item.value.id}/cancel`)
    } else if (kind === 'execute') {
      await api.post(`/tickets/${item.value.id}/execute`)
    }
    ElMessage.success('已处理')
    comment.value = ''
    await load()
  } catch (err) {
    if (err !== 'cancel' && err !== 'close') {
      ElMessage.error(err.response?.data?.message || err.message || '操作失败')
    }
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.review-page {
  width: 100%;
  height: 100%;
  max-height: 100%;
  padding: 16px 20px;
  background: #fff;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.back-row { flex-shrink: 0; margin-bottom: 12px; }
.back-link {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  cursor: pointer;
  color: #374151;
  font-size: 13px;
}
.back-link:hover { background: #f9fafb; }
.review-split {
  flex: 1;
  min-height: 0;
  display: flex;
  border-top: 1px solid #f3f4f6;
}
.review-aside {
  width: 360px;
  flex-shrink: 0;
  padding: 16px 16px 24px 4px;
  overflow: auto;
}
.review-main {
  flex: 1;
  min-width: 0;
  padding: 16px 8px 24px 24px;
  border-left: 1px solid #e5e7eb;
  overflow: auto;
}
@media (max-width: 1100px) {
  .review-split { flex-direction: column; }
  .review-aside {
    width: 100%;
    max-height: 36%;
  }
  .review-main {
    border-left: none;
    border-top: 1px solid #e5e7eb;
    padding: 16px 4px 8px;
  }
}
.meta-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 16px;
}
.meta-block { margin-bottom: 16px; }
.meta-label { color: #6b7280; font-size: 13px; }
.meta-value { color: #111827; margin-top: 4px; word-break: break-all; }
.user-card {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 6px;
  padding: 8px;
  background: #f3f4f6;
  border-radius: 4px;
}
.user-card.compact { background: transparent; padding: 0; margin: 0; }
.user-avatar { background: #6366f1; }
.user-name { color: #111827; font-size: 14px; }
.user-sub { color: #9ca3af; font-size: 12px; }
.flow-card {
  margin: 8px 0 16px;
  border: 1px solid #fcd34d;
  border-radius: 4px;
  overflow: hidden;
}
.flow-card.is-done { border-color: #86efac; }
.flow-card.is-danger { border-color: #fca5a5; }
.flow-card.is-wait { border-color: #fcd34d; }
.flow-card-head {
  padding: 8px 12px;
  background: #fffbeb;
  font-size: 13px;
  color: #374151;
}
.flow-card.is-done .flow-card-head { background: #f0fdf4; }
.flow-card.is-danger .flow-card-head { background: #fef2f2; }
.flow-card-body { padding: 12px; }
.approver-line {
  padding: 8px 10px;
  border: 1px solid #e5e7eb;
  border-bottom: none;
  font-size: 13px;
}
.timeout-hint {
  padding: 6px 10px;
  border: 1px solid #e5e7eb;
  border-bottom: none;
  background: #fffbeb;
  color: #b45309;
  font-size: 12px;
}
.remark-box {
  padding: 10px;
  border: 1px solid #e5e7eb;
  background: #fff;
}
.remark-label { font-size: 13px; margin-bottom: 6px; }
.remark-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 10px; }
.approve-result { display: flex; justify-content: space-between; align-items: center; }
.remark-text { margin-top: 8px; color: #4b5563; font-size: 13px; white-space: pre-wrap; }
.exec-body { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.download-body { background: linear-gradient(90deg, #fffbeb, #fff); }
.download-row { display: flex; align-items: center; gap: 12px; }
.download-hint { margin-top: 8px; color: #a16207; font-size: 12px; }
.exec-wait { color: #a16207; font-weight: 600; }
.fmt-label { margin-bottom: 8px; color: #4b5563; }
.status-text { font-size: 13px; }
.status-text.success { color: #16a34a; }
.status-text.danger { color: #dc2626; }
.status-text.muted { color: #6b7280; }
.wf-title { margin: 8px 0 12px; font-weight: 600; color: #111827; }
.wf-graph { display: flex; flex-direction: column; align-items: flex-start; padding-left: 8px; }
.wf-node {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 200px;
  padding: 10px 12px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  background: #fff;
}
.wf-node.is-current { border-color: #f59e0b; background: #fffbeb; }
.wf-node.is-ok { border-color: #86efac; background: #f0fdf4; }
.wf-node.is-fail { border-color: #fca5a5; background: #fef2f2; }
.wf-dot {
  width: 10px;
  height: 10px;
  margin-top: 4px;
  border-radius: 50%;
  background: #d1d5db;
  flex-shrink: 0;
}
.wf-node.is-current .wf-dot { background: #f59e0b; }
.wf-node.is-ok .wf-dot { background: #22c55e; }
.wf-node.is-fail .wf-dot { background: #ef4444; }
.wf-name { font-size: 13px; color: #111827; }
.wf-sub { font-size: 12px; color: #6b7280; margin-top: 2px; }
.wf-line {
  width: 2px;
  height: 20px;
  margin-left: 16px;
  background: #e5e7eb;
}
.sec-title { font-weight: 600; color: #111827; margin-bottom: 12px; }
.sec-title.mt { margin-top: 28px; }
.apply-desc :deep(.el-descriptions__label) {
  width: 140px;
  background: #fafafa;
  color: #4b5563;
}
.sql-pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
  line-height: 1.5;
}
.impact-card {
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  padding: 12px 14px 14px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
}
.impact-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.db-pill {
  background: #3b82f6;
  color: #fff;
  border-radius: 999px;
  padding: 2px 10px;
  font-size: 12px;
}
.db-path { color: #1e40af; font-size: 13px; }
</style>
