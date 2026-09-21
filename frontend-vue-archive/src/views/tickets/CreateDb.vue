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

          <el-form-item label="数据源(别名)" prop="instance_ids">
            <el-select
              v-model="form.instance_ids"
              multiple
              filterable
              clearable
              class="ds-select"
              :disabled="!form.env || !form.instance_type"
              :loading="dsLoading"
              :placeholder="dsPlaceholder"
              style="width:100%"
              @change="onDataSourcesChange"
            >
              <el-option v-for="i in dataSourceOptions" :key="i.id" :label="instLabel(i)" :value="i.id">
                <div class="ds-opt">
                  <span>{{ i.name || instAddress(i) }}</span>
                  <span class="muted">{{ instAddress(i) }}</span>
                </div>
              </el-option>
            </el-select>
          </el-form-item>

          <el-form-item label="数据库账号" prop="username">
            <el-input
              v-model="form.username"
              clearable
              :disabled="!form.instance_ids.length"
              :placeholder="form.instance_ids.length ? '请输入数据库账号' : '请先选择数据源(别名)'"
            />
          </el-form-item>

          <el-form-item label="账号权限" prop="permissions">
            <el-select
              v-model="form.permissions"
              multiple
              filterable
              clearable
              class="ds-select"
              placeholder="请选择操作"
              style="width:100%"
            >
              <el-option v-for="p in permissionOps" :key="p.value" :label="p.label" :value="p.value" />
            </el-select>
          </el-form-item>

          <el-form-item label="审批人" prop="assigned_to">
            <el-select
              v-model="form.assigned_to"
              filterable
              clearable
              :loading="reviewerLoading"
              :placeholder="reviewerPlaceholder"
              style="width:100%"
            >
              <el-option v-for="a in approvers" :key="a" :label="a" :value="a" />
            </el-select>
            <div v-if="approvers.length" class="extra">候选来自 create-db/approvers，默认预填 sync_default_approver</div>
          </el-form-item>

          <el-form-item label="描述" prop="description">
            <el-input v-model="form.description" clearable placeholder="如：业务上线" />
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
            <el-button :disabled="submitting" @click="reset">重置</el-button>
            <el-tooltip :disabled="submitting || submitAllowed" :content="submitDisabledReason" placement="top">
              <span class="submit-wrap">
                <el-button type="primary" :loading="submitting" :disabled="!submitAllowed" @click="submit">
                  提交
                </el-button>
              </span>
            </el-tooltip>
            <el-button :disabled="submitting" @click="saveDraft">保存草稿</el-button>
          </el-form-item>
        </el-form>
      </aside>

      <main class="order-main">
        <div class="sql-title">填写SQL</div>
        <el-input
          v-model="form.sql_content"
          type="textarea"
          class="sql-editor"
          :autosize="{ minRows: 16 }"
          placeholder="CREATE DATABASE newdb;"
        />
        <div class="sql-actions">
          <el-button type="primary" :loading="checkLoading" :disabled="checkLoading" @click="check">
            SQL检测
          </el-button>
          <el-button @click="beautify">SQL美化</el-button>
        </div>
        <p class="hint">编辑框内可检测、美化 SQL。仅允许 CREATE DATABASE。</p>
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
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { buildEnvOptions } from '../../utils/env'
import DbTypeIcon from '../../components/DbTypeIcon.vue'
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'

const permissionOps = [
  { label: '读操作', value: 'r' },
  { label: '写操作', value: 'c' },
  { label: '更新操作', value: 'u' },
  { label: '删除操作', value: 'd' },
]

const router = useRouter()
const formRef = ref()
const instances = ref([])
const envOptions = ref([])
const dbTypes = ref([
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'starrocks', label: 'StarRocks' },
])
const approvers = ref([])
const checkRows = ref([])
const lastCheckFp = ref(null)
const dsLoading = ref(false)
const reviewerLoading = ref(false)
const checkLoading = ref(false)
const submitting = ref(false)
const defaultReviewer = ref('')

const form = reactive(emptyForm())

function emptyForm() {
  return {
    env: '',
    instance_type: '',
    instance_ids: [],
    username: '',
    permissions: [],
    assigned_to: '',
    description: '',
    scheduled_at: '',
    sql_content: '',
  }
}

const rules = {
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  instance_type: [{ required: true, message: '请选择实例类型', trigger: 'change' }],
  instance_ids: [{ type: 'array', required: true, min: 1, message: '请选择数据源', trigger: 'change' }],
  username: [{ required: true, message: '请输入数据库账号', trigger: 'blur' }],
  permissions: [{ type: 'array', required: true, min: 1, message: '请选择账号权限', trigger: 'change' }],
  assigned_to: [{ required: true, message: '请选择审批人', trigger: 'change' }],
  description: [{ required: true, message: '请输入描述', trigger: 'blur' }],
}

const dataSourceOptions = computed(() => {
  return instances.value.filter((i) => {
    if (form.env && (i.idc || '') !== form.env) return false
    if (form.instance_type && String(i.db_type || '').toLowerCase() !== form.instance_type) return false
    return true
  })
})

const checkFp = computed(() => JSON.stringify({
  env: form.env,
  instance_type: form.instance_type,
  instance_ids: [...form.instance_ids].sort(),
  username: String(form.username || '').trim(),
  permissions: [...form.permissions].sort(),
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
  if (dsLoading.value) return '加载数据源…'
  return '选择数据源(别名) 或搜索...'
})

const reviewerPlaceholder = computed(() => {
  if (reviewerLoading.value) return '加载审批人…'
  return approvers.value.length ? '请选择审批人' : '暂无审批人'
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
  form.instance_ids = []
  resetCheck()
}

function onDataSourcesChange() {
  resetCheck()
}

function reset() {
  Object.assign(form, emptyForm())
  checkRows.value = []
  lastCheckFp.value = null
  if (defaultReviewer.value) form.assigned_to = defaultReviewer.value
  formRef.value?.clearValidate()
}

async function check() {
  const sql = String(form.sql_content || '').trim()
  if (!sql) {
    ElMessage.warning('请填写 SQL')
    return
  }
  if (!form.env || !form.instance_type || !form.instance_ids.length || !String(form.username || '').trim() || !form.permissions.length) {
    ElMessage.warning('检测前请先选择环境、实例类型、数据源、数据库账号与账号权限')
    return
  }
  const fp = checkFp.value
  checkLoading.value = true
  try {
    const { data } = await api.post('/create-db/check-sql', {
      db_type: form.instance_type,
      idc_scope: form.env,
      instance_id: form.instance_ids[0],
      instance_ids: form.instance_ids,
      sql_content: sql,
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
    .replace(/\b(CREATE DATABASE|IF NOT EXISTS)\b/gi, '\n$1')
    .replace(/\n+/g, '\n')
    .trim()
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'createdb',
  label: '新增数据库',
  path: '/ops/createdb',
  form,
  applyValues: (d) => {
    Object.assign(form, {
      ...emptyForm(),
      ...d,
      instance_ids: Array.isArray(d.instance_ids) ? d.instance_ids : [],
      permissions: Array.isArray(d.permissions) ? d.permissions : [],
    })
  },
})

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
  submitting.value = true
  try {
    const { data } = await api.post('/create-db', {
      db_type: form.instance_type,
      idc_scope: form.env,
      instance_id: form.instance_ids[0],
      instance_ids: form.instance_ids,
      username: String(form.username || '').trim(),
      permissions: form.permissions,
      assigned_to: form.assigned_to,
      description: form.description,
      scheduled_at: form.scheduled_at || '',
      sql_content: sql,
    })
    const no = data.ticket_no || data.data?.ticket_no || ''
    const id = data.issue_id || data.id || data.data?.id || ''
    ElMessage.success(no
      ? `提交成功，工单号 ${no}。工单审批通过后，账号将写入「配置 → 账号管理」，密码加密存储。`
      : '提交成功。工单审批通过后，账号将写入「配置 → 账号管理」。')
    clearOnSubmitSuccess()
    router.push(id ? '/ops/tickets/' + id : '/ops/tickets')
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '提交失败')
  } finally {
    submitting.value = false
  }
}

async function loadMeta() {
  dsLoading.value = true
  reviewerLoading.value = true
  try {
    const [instRes, envRes, apprRes] = await Promise.all([
      api.get('/create-db/instances'),
      api.get('/environments').catch(() => ({ data: { data: [] } })),
      api.get('/create-db/approvers'),
    ])
    instances.value = instRes.data.data || []
    envOptions.value = buildEnvOptions(envRes.data.data || [], instances.value)
    const names = (apprRes.data.data || []).map((a) => a.name || a).filter(Boolean)
    approvers.value = [...new Set(names)]
    defaultReviewer.value = approvers.value[0] || ''
    if (!form.assigned_to && defaultReviewer.value) form.assigned_to = defaultReviewer.value
  } finally {
    dsLoading.value = false
    reviewerLoading.value = false
  }
}

onMounted(async () => {
  restoreDraft()
  await loadMeta()
  const valid = new Set(dataSourceOptions.value.map((i) => i.id))
  form.instance_ids = (form.instance_ids || []).filter((id) => valid.has(id))
})
</script>

<style scoped>
.type-opt { display: inline-flex; align-items: center; gap: 8px; }
.ds-opt { display: flex; justify-content: space-between; gap: 12px; width: 100%; }
.ds-select :deep(.el-select__selection) { flex-wrap: wrap; }
.muted { color: #9ca3af; font-size: 12px; }
.hint { color: #9ca3af; font-size: 12px; margin: 0 0 12px; }
.extra { color: #9ca3af; font-size: 12px; line-height: 1.4; margin-top: 4px; }
.sql-title { font-size: 14px; font-weight: 500; color: #111827; margin-bottom: 12px; flex-shrink: 0; }
.aside-actions { margin-bottom: 0; margin-top: 8px; }
.aside-actions :deep(.el-form-item__content) { display: flex; flex-wrap: wrap; gap: 8px; }
.submit-wrap { display: inline-block; }
.lv-error { color: #dc2626; }
.lv-warn { color: #f59e0b; }
.lv-ok { color: #16a34a; }
</style>
