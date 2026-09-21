<template>
  <div class="order-page">
    <div class="order-split">
      <aside class="order-aside">
        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-position="top"
          @submit.prevent
        >
          <el-form-item label="环境" prop="env">
            <el-select v-model="form.env" filterable clearable placeholder="请选择" style="width:100%" @change="onEnvOrTypeChange">
              <el-option v-for="e in envOptions" :key="e.value" :label="e.label" :value="e.value" />
            </el-select>
          </el-form-item>

          <el-form-item label="实例类型" prop="instance_type">
            <el-select
              v-model="form.instance_type"
              filterable
              clearable
              :disabled="!form.env"
              :placeholder="form.env ? '请选择' : '请先选择环境'"
              style="width:100%"
              @change="onEnvOrTypeChange"
            >
              <el-option v-for="t in dbTypes" :key="t.value" :label="t.label" :value="t.value">
                <span class="type-opt">
                  <DbTypeIcon :type="t.value" :size="16" />
                  <span>{{ t.label }}</span>
                </span>
              </el-option>
            </el-select>
          </el-form-item>

          <el-form-item label="数据源" prop="instance_id">
            <el-select
              v-model="form.instance_id"
              filterable
              clearable
              :disabled="!form.env || !form.instance_type"
              :loading="dsLoading"
              :placeholder="dsPlaceholder"
              style="width:100%"
              @change="onDataSourceChange"
            >
              <el-option v-for="i in dataSourceOptions" :key="i.id" :label="instLabel(i)" :value="i.id">
                <div class="ds-opt">
                  <span>{{ i.name || instAddress(i) }}</span>
                  <span class="muted">{{ instAddress(i) }}</span>
                </div>
              </el-option>
            </el-select>
          </el-form-item>

          <el-form-item label="数据库" prop="db_name">
            <el-select
              v-model="form.db_name"
              filterable
              clearable
              :disabled="!form.instance_id"
              :loading="dbLoading"
              :placeholder="dbPlaceholder"
              style="width:100%"
              @change="onDatabaseChange"
            >
              <el-option v-for="d in dbNameOptions" :key="d.value" :label="d.label" :value="d.value" />
            </el-select>
          </el-form-item>

          <el-form-item>
            <template #label>
              <span>审核人 <span class="optional">(可选)</span></span>
            </template>
            <el-select
              v-model="form.assigned_to"
              filterable
              clearable
              :disabled="!form.db_name || !form.instance_id"
              :loading="reviewerLoading"
              :placeholder="reviewerPlaceholder"
              style="width:100%"
            >
              <el-option v-for="a in approvers" :key="a" :label="a" :value="a" />
            </el-select>
          </el-form-item>

          <el-form-item label="描述" prop="description">
            <el-input v-model="form.description" clearable placeholder="如：业务数据修复" />
          </el-form-item>

          <el-form-item label="定时执行">
            <el-date-picker
              v-model="form.scheduled_at"
              type="datetime"
              format="YYYY-MM-DD HH:mm"
              value-format="YYYY-MM-DD HH:mm:ss"
              placeholder="年/月/日 --:--"
              style="width:100%"
            />
          </el-form-item>

          <p class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
          <el-form-item class="aside-actions">
            <el-tooltip :disabled="submitting || submitAllowed" :content="submitDisabledReason" placement="top">
              <span class="submit-wrap">
                <el-button type="primary" style="width:100%" :loading="submitting" :disabled="!submitAllowed" @click="submit">
                  提交
                </el-button>
              </span>
            </el-tooltip>
          </el-form-item>
          <el-form-item class="aside-actions draft-item">
            <el-button style="width:100%" @click="saveDraft">保存草稿</el-button>
          </el-form-item>
        </el-form>
      </aside>

      <main class="order-main">
        <div class="sql-title">填写 SQL</div>
        <el-input
          v-model="form.sql_content"
          type="textarea"
          class="sql-editor"
          :autosize="{ minRows: 16 }"
          placeholder="UPDATE t SET c=1 WHERE id=1;"
        />
        <div class="sql-actions">
          <el-button type="primary" :loading="checkLoading" :disabled="checkLoading" @click="check">
            SQL检测
          </el-button>
          <el-button @click="beautify">SQL美化</el-button>
        </div>
        <p class="hint">编辑框内可检测、美化 SQL</p>
        <el-table :data="checkRows" size="small" border empty-text="暂无检测结果">
          <el-table-column prop="stage" label="阶段" width="100" />
          <el-table-column label="错误等级" width="100">
            <template #default="{ row }">
              <span :class="levelClass(row.error_level)">{{ levelLabel(row.error_level) }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="message" label="错误信息" min-width="180" show-overflow-tooltip />
          <el-table-column prop="sql" label="SQL语句" min-width="240" show-overflow-tooltip />
          <el-table-column prop="affected_rows" label="影响行数" width="90" />
          <el-table-column label="操作" width="70">
            <template #default>—</template>
          </el-table-column>
        </el-table>
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { dbLabel } from '../../utils/db'
import { buildEnvOptions } from '../../utils/env'
import DbTypeIcon from '../../components/DbTypeIcon.vue'
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'

const PREFILL_KEY = 'auto-dba-dml-prefill'

const route = useRoute()
const router = useRouter()
const formRef = ref()
const instances = ref([])
const envOptions = ref([])
const dbTypes = ref([
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
])
const dbRecords = ref([])
const approvers = ref([])
const checkRows = ref([])
const lastCheckFp = ref(null)
const dsLoading = ref(false)
const dbLoading = ref(false)
const reviewerLoading = ref(false)
const checkLoading = ref(false)
const submitting = ref(false)

const form = reactive(emptyForm())

function emptyForm() {
  return {
    env: '',
    instance_type: '',
    instance_id: '',
    db_name: '',
    assigned_to: '',
    description: '',
    scheduled_at: '',
    sql_content: '',
  }
}

const rules = {
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  instance_type: [{ required: true, message: '请选择实例类型', trigger: 'change' }],
  instance_id: [{ required: true, message: '请选择数据源', trigger: 'change' }],
  db_name: [{ required: true, message: '请选择数据库', trigger: 'change' }],
  description: [{ required: true, message: '请输入描述', trigger: 'blur' }],
}

const dataSourceOptions = computed(() => {
  return instances.value.filter((i) => {
    if (form.env && (i.idc || '') !== form.env) return false
    if (form.instance_type && String(i.db_type || '').toLowerCase() !== form.instance_type) return false
    return true
  })
})

const dbNameOptions = computed(() => {
  const seen = new Set()
  const out = []
  for (const d of dbRecords.value) {
    const name = d.db_name || ''
    if (!name || seen.has(name)) continue
    seen.add(name)
    out.push({ value: name, label: dbLabel(d) })
  }
  return out
})

/** 仅环境 / 类型 / 数据源 / 库 / SQL 参与指纹，改审核人/描述/定时不失效 */
const checkFp = computed(() => JSON.stringify({
  env: form.env,
  instance_type: form.instance_type,
  instance_id: form.instance_id,
  db_name: form.db_name,
  sql: String(form.sql_content || '').trim(),
}))

const checkHasError = computed(() => checkRows.value.some((r) => String(r.error_level || '').toUpperCase() === 'ERROR'))
const fingerprintMatches = computed(() => lastCheckFp.value != null && lastCheckFp.value === checkFp.value)
const submitAllowed = computed(() => fingerprintMatches.value && !checkHasError.value)

const submitDisabledReason = computed(() => {
  if (lastCheckFp.value == null) return '请先点击「SQL检测」'
  if (!fingerprintMatches.value) return 'SQL 或表单与上次检测不一致，请重新检测后再提交'
  if (checkHasError.value) return 'SQL 检测存在错误，请修复后再提交'
  return ''
})

const dsPlaceholder = computed(() => {
  if (!form.env) return '请先选择环境'
  if (!form.instance_type) return '请先选择实例类型'
  return '请选择数据源或搜索...'
})

const dbPlaceholder = computed(() => {
  if (!form.instance_id) return '请先选择数据源'
  if (dbLoading.value) return '加载逻辑库…'
  return '请选择'
})

const reviewerPlaceholder = computed(() => {
  if (!form.db_name || !form.instance_id) return '请先选择数据源与数据库'
  if (reviewerLoading.value) return '加载审核人…'
  return '请选择（可选）'
})

function instAddress(i) {
  if (i?.host) return `${i.host}:${i.port || ''}`
  const d = (i?.domains || [])[0] || i?.domain_name || ''
  return d
}

function instLabel(i) {
  const addr = instAddress(i)
  return i?.name ? (addr ? `${i.name} (${addr})` : i.name) : addr
}

function levelLabel(lv) {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return '错误'
  if (x === 'WARN' || x === 'WARNING') return '警告'
  if (x === 'INFO' || x === 'OK') return '通过'
  return lv || '—'
}

function levelClass(lv) {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return 'lv-error'
  if (x === 'WARN' || x === 'WARNING') return 'lv-warn'
  if (x === 'INFO' || x === 'OK') return 'lv-ok'
  return ''
}

function resetCheck() {
  lastCheckFp.value = null
  checkRows.value = []
}

function onEnvOrTypeChange() {
  form.instance_id = ''
  form.db_name = ''
  form.assigned_to = ''
  dbRecords.value = []
  approvers.value = []
  resetCheck()
}

async function onDataSourceChange() {
  form.db_name = ''
  form.assigned_to = ''
  approvers.value = []
  resetCheck()
  await loadDatabases()
}

async function onDatabaseChange() {
  form.assigned_to = ''
  resetCheck()
  await loadApprovers()
}

async function loadDatabases() {
  const id = form.instance_id
  if (!id) {
    dbRecords.value = []
    return
  }
  dbLoading.value = true
  try {
    const { data } = await api.get(`/tickets/instances/${id}/databases`)
    dbRecords.value = (data.data || []).map((d) => ({ ...d, instance_id: d.instance_id || id }))
    if (form.db_name && !dbNameOptions.value.some((o) => o.value === form.db_name)) {
      form.db_name = ''
      form.assigned_to = ''
      approvers.value = []
    }
  } catch (e) {
    dbRecords.value = []
    ElMessage.error(e.response?.data?.message || '加载逻辑库失败')
  } finally {
    dbLoading.value = false
  }
}

async function loadApprovers() {
  const id = form.instance_id
  const dbName = String(form.db_name || '').trim()
  if (!id || !dbName) {
    approvers.value = []
    return
  }
  reviewerLoading.value = true
  try {
    const { data } = await api.get('/tickets/target-approvers', { params: { instance_id: id, db_name: dbName } })
    const out = (data.data || []).map((a) => a.name || a).filter(Boolean)
    approvers.value = [...new Set(out)]
    if (form.assigned_to && !approvers.value.includes(form.assigned_to)) form.assigned_to = ''
  } catch {
    approvers.value = []
    ElMessage.error('加载审核人失败')
  } finally {
    reviewerLoading.value = false
  }
}

function pickPrimary() {
  const dbName = String(form.db_name || '').trim()
  const hit = dbRecords.value.find((d) => d.db_name === dbName)
  return {
    instance_id: form.instance_id || hit?.instance_id || '',
    database_id: hit?.id || '',
    db_name: dbName,
  }
}

async function check() {
  const sql = String(form.sql_content || '').trim()
  if (!sql) {
    ElMessage.error('请填写 SQL')
    return
  }
  if (!form.env || !form.instance_type || !form.instance_id || !form.db_name) {
    ElMessage.warning('检测前请先选择环境、实例类型、数据源与数据库')
    return
  }
  const fp = checkFp.value
  checkLoading.value = true
  try {
    const { data } = await api.post('/tickets/check-sql', {
      sql_content: sql,
      type: 'dml',
      db_type: form.instance_type,
      instance_id: form.instance_id,
      db_name: form.db_name,
    })
    checkRows.value = (data.statements || []).map((r) => ({
      stage: r.stage || 'parse',
      error_level: r.error_level || '',
      message: r.message || '',
      sql: r.sql || sql,
      affected_rows: r.affected_rows ?? '—',
    }))
    ElMessage[data.ok ? 'success' : 'warning'](data.ok ? '检测通过' : '检测未通过')
  } catch (e) {
    const msg = e.response?.data?.message || e.message || 'SQL 检测请求失败'
    ElMessage.error(msg)
    checkRows.value = [{ stage: '—', error_level: 'ERROR', message: msg, sql, affected_rows: '—' }]
  } finally {
    checkLoading.value = false
    lastCheckFp.value = fp
  }
}

function beautify() {
  const sql = String(form.sql_content || '').trim()
  if (!sql) {
    ElMessage.warning('请填写 SQL')
    return
  }
  form.sql_content = sql
    .replace(/\s+/g, ' ')
    .replace(/\s*;\s*/g, ';\n')
    .replace(/\b(SELECT|FROM|WHERE|AND|OR|JOIN|LEFT|RIGHT|INNER|GROUP BY|ORDER BY|LIMIT|UPDATE|SET|INSERT INTO|VALUES|ALTER TABLE|ADD|DROP|CREATE|GRANT|TO|DELETE|REPLACE)\b/gi, '\n$1')
    .replace(/\n+/g, '\n')
    .trim()
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'dml',
  label: 'DML工单',
  path: '/ops/dml',
  form,
  applyValues: (d) => {
    Object.assign(form, {
      ...emptyForm(),
      ...d,
      instance_id: d.instance_id || '',
      sql_content: d.sql_content || d.sql || '',
    })
  },
})

function readJson(key) {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const d = JSON.parse(raw)
    return d && typeof d === 'object' ? d : null
  } catch {
    sessionStorage.removeItem(key)
    return null
  }
}

function applyIncoming() {
  const q = route.query || {}
  const fromQuery = {
    env: String(q.env || '').trim(),
    instance_type: String(q.instance_type || q.db_type || '').trim(),
    instance_id: String(q.instance_id || '').trim(),
    db_name: String(q.db_name || q.database || '').trim(),
    description: String(q.description || '').trim(),
    sql_content: String(q.sql || q.sql_content || '').trim(),
  }
  const hasQuery = Object.values(fromQuery).some(Boolean)
  const prefill = readJson(PREFILL_KEY)
  const src = hasQuery ? fromQuery : prefill
  if (!src) return false
  Object.assign(form, {
    ...emptyForm(),
    ...src,
    instance_id: src.instance_id || '',
    sql_content: src.sql_content || src.sql || '',
  })
  if (prefill) sessionStorage.removeItem(PREFILL_KEY)
  return true
}

async function submit() {
  try {
    await formRef.value?.validate()
  } catch {
    ElMessage.warning('请完善表单必填项后再提交')
    return
  }
  const sql = String(form.sql_content || '').trim()
  if (!sql) {
    ElMessage.error('请填写 SQL')
    return
  }
  if (!submitAllowed.value) {
    ElMessage.warning(submitDisabledReason.value)
    return
  }
  const rec = pickPrimary()
  if (!rec.instance_id) {
    ElMessage.warning('未解析到有效数据源，请重新选择数据源后再提交')
    return
  }
  submitting.value = true
  try {
    const { data } = await api.post('/tickets', {
      type: 'dml',
      db_type: form.instance_type,
      instance_id: rec.instance_id,
      database_id: rec.database_id,
      db_name: rec.db_name,
      sql_content: sql,
      description: form.description,
      assigned_to: form.assigned_to,
      scheduled_at: form.scheduled_at || '',
      need_rollback: JSON.stringify({
        env: form.env,
        instance_type: form.instance_type,
      }),
    })
    ElMessage.success(data.data?.ticket_no ? `提交成功，工单号 ${data.data.ticket_no}` : '提交成功')
    clearOnSubmitSuccess()
    router.push('/ops/tickets/' + (data.data?.id || ''))
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '提交失败')
  } finally {
    submitting.value = false
  }
}

async function loadMeta() {
  dsLoading.value = true
  try {
    const [instRes, envRes, argRes] = await Promise.all([
      api.get('/tickets/instances'),
      api.get('/environments').catch(() => ({ data: { data: [] } })),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
    ])
    instances.value = instRes.data.data || []
    envOptions.value = buildEnvOptions(envRes.data.data || [], instances.value)
    const args = argRes.data.data || argRes.data || []
    const raw = (Array.isArray(args) ? args : []).find((a) => a.key === 'ticket_instance_types')?.value || ''
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length) {
        dbTypes.value = parsed
          .filter((t) => t.value && !['kafka', 'redis', 'mongodb', 'elasticsearch', 'cassandra'].includes(t.value))
          .map((t) => ({ value: t.value, label: t.label || t.value }))
      }
    } catch { /* keep default */ }
    if (form.instance_type && !dbTypes.value.some((t) => t.value === form.instance_type)) {
      form.instance_type = ''
    }
  } finally {
    dsLoading.value = false
  }
}

onMounted(async () => {
  const incoming = applyIncoming()
  if (!incoming) restoreDraft()
  await loadMeta()
  if (form.instance_id && !dataSourceOptions.value.some((i) => i.id === form.instance_id)) {
    form.instance_id = ''
    form.db_name = ''
    form.assigned_to = ''
  }
  await loadDatabases()
  await loadApprovers()
})
</script>

<style scoped>
.type-opt { display: inline-flex; align-items: center; gap: 8px; }
.ds-opt { display: flex; justify-content: space-between; gap: 12px; width: 100%; }
.muted { color: #9ca3af; font-size: 12px; }
.optional { color: #9ca3af; font-weight: 400; }
.hint { color: #9ca3af; font-size: 12px; margin: 0 0 12px; }
.sql-title { font-size: 15px; font-weight: 500; color: #111827; margin-bottom: 8px; flex-shrink: 0; }
.aside-actions { margin-bottom: 8px; margin-top: 8px; }
.aside-actions :deep(.el-form-item__content) { display: block; width: 100%; }
.draft-item { margin-top: 0; margin-bottom: 0; }
.submit-wrap { display: block; width: 100%; }
.lv-error { color: #dc2626; }
.lv-warn { color: #f59e0b; }
.lv-ok { color: #16a34a; }
</style>
