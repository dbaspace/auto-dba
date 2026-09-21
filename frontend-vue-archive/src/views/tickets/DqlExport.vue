<template>
  <div class="gray-page">
    <div class="white-card dql-card">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="160px" label-position="right" class="dql-form">
        <el-form-item label="数据库类型" prop="db_type">
          <el-select v-model="form.db_type" filterable placeholder="请选择数据库类型" style="width:100%" @change="onFilterChange">
            <el-option v-for="t in dbTypes" :key="t.value" :label="t.label" :value="t.value">
              <span class="type-opt">
                <DbTypeIcon :type="t.value" :size="16" />
                <span>{{ t.label }}</span>
              </span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="环境" prop="env">
          <el-select v-model="form.env" filterable placeholder="请选择环境" style="width:100%" @change="onFilterChange">
            <el-option v-for="e in envs" :key="e" :label="e" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="数据库" prop="database_id">
          <el-select v-model="form.database_id" filterable placeholder="请选择数据库" style="width:100%" @change="onDbChange">
            <el-option v-for="d in dbOptions" :key="d.id" :label="dbLabel(d)" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="SQL语句" prop="sql_content" required class="sql-item">
          <el-input
            v-model="form.sql_content"
            type="textarea"
            class="sql-editor"
            :rows="4"
            placeholder="SELECT ... "
          />
        </el-form-item>
        <el-form-item label="选择导出审批人" prop="assigned_to">
          <el-select v-model="form.assigned_to" filterable placeholder="请选择审批人" style="width:100%">
            <el-option v-for="a in approvers" :key="a" :label="a" :value="a" />
          </el-select>
          <div class="form-extra">如果没有审批人，请联系运维配置。</div>
        </el-form-item>

        <div class="warn-box">
          <div class="warn-rules">
            <p>1. 原文导出 <b>敏感字段</b> 需开启原文导出敏感字段，并配置安全和DBA审批，<b>否则此字段加密导出。</b></p>
            <div class="mask-line">
              <span>检测到查询敏感信息：</span>
              <span v-if="!matchMask.length" class="muted">无</span>
              <span v-for="m in matchMask" :key="m.key" class="mask-chip">
                表:{{ m.table || '—' }}，预设敏感字段：{{ m.columns.join(', ') }}
              </span>
            </div>
            <p>2. 导出 <b>数据超过500条</b> 需配置安全和DBA审批，<b>否则禁止导出。</b></p>
            <p class="warn-strong">非以上场景无需设置安全和DBA审批。</p>
          </div>
          <el-form-item label="原文导出敏感字段">
            <el-switch v-model="form.need_sensitive_data" active-text="开启" inactive-text="关闭" />
          </el-form-item>
          <el-form-item label="DBA审批人">
            <el-select v-model="form.dba_assigned_to" filterable clearable placeholder="请选择审批人" style="width:100%">
              <el-option v-for="a in extraApprovers" :key="'dba-' + a" :label="a" :value="a" />
            </el-select>
          </el-form-item>
          <el-form-item label="安全审批人">
            <el-select v-model="form.security_assigned_to" filterable clearable placeholder="请选择审批人" style="width:100%">
              <el-option v-for="a in extraApprovers" :key="'sec-' + a" :label="a" :value="a" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item label="导出原因" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="请输入导出原因" />
        </el-form-item>
        <div class="ticket-form-actions">
          <el-button type="primary" :loading="saving" @click="submit">提交申请</el-button>
          <el-button @click="$router.push('/ops/tickets')">取消</el-button>
        </div>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { dbLabel } from '../../utils/db'
import { buildEnvOptions } from '../../utils/env'
import DbTypeIcon from '../../components/DbTypeIcon.vue'

const DQL_DRAFT_KEY = 'auto-dba-dql-draft'
const SENSITIVE = /phone|mobile|email|mail|id_card|idcard|身份证|手机|邮箱|password|passwd|secret|token|card_no|bank|身份证号/i

const router = useRouter()
const formRef = ref()
const saving = ref(false)
const instances = ref([])
const databases = ref([])
const envs = ref([])
const dbTypes = ref([
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
])
const approvers = ref([])
const extraApprovers = ref([])
const rowCount = ref(0)
const resultCols = ref([])

const form = reactive({
  db_type: '',
  env: '',
  database_id: '',
  instance_id: '',
  db_name: '',
  sql_content: '',
  assigned_to: '',
  need_sensitive_data: false,
  dba_assigned_to: '',
  security_assigned_to: '',
  description: '',
})

const rules = {
  db_type: [{ required: true, message: '请选择数据库类型', trigger: 'change' }],
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  database_id: [{ required: true, message: '请选择数据库', trigger: 'change' }],
  sql_content: [{ required: true, message: 'SQL语句不能为空', trigger: 'blur' }],
  assigned_to: [{ required: true, message: '请选择审批人', trigger: 'change' }],
  description: [{ required: true, message: '请输入导出原因', trigger: 'blur' }],
}

const instMap = computed(() => {
  const m = {}
  for (const i of instances.value) m[i.id] = i
  return m
})

const dbOptions = computed(() => {
  return databases.value.filter((d) => {
    const inst = instMap.value[d.instance_id] || {}
    if (form.db_type && String(inst.db_type || '').toLowerCase() !== form.db_type) return false
    if (form.env && (inst.idc || '') !== form.env) return false
    return true
  })
})

const currentDB = computed(() => databases.value.find((d) => d.id === form.database_id) || null)

const parsedTables = computed(() => parseTables(form.sql_content))

const matchMask = computed(() => {
  const cols = resultCols.value.filter((c) => SENSITIVE.test(c))
  const sqlHits = [...String(form.sql_content || '').matchAll(/[`"'.\w]+/g)]
    .map((m) => m[0].replace(/[`"']/g, ''))
    .filter((n) => SENSITIVE.test(n))
  const names = [...new Set([...cols, ...sqlHits])]
  if (!names.length) return []
  const table = parsedTables.value[0] || ''
  return [{ key: names.join(','), table, columns: names }]
})

function parseTables(sql) {
  const out = []
  const re = /\b(?:from|join)\s+([`"']?[\w.]+[`"']?)/gi
  let m
  while ((m = re.exec(String(sql || '')))) {
    const name = String(m[1] || '').replace(/[`"']/g, '')
    if (name && !out.includes(name)) out.push(name)
  }
  return out
}

function onFilterChange() {
  if (form.database_id && !dbOptions.value.some((d) => d.id === form.database_id)) {
    form.database_id = ''
    form.instance_id = ''
    form.db_name = ''
    approvers.value = []
  }
}

async function onDbChange() {
  const db = currentDB.value
  form.instance_id = db?.instance_id || ''
  form.db_name = db?.db_name || ''
  const inst = instMap.value[form.instance_id]
  if (inst) {
    if (!form.db_type) form.db_type = String(inst.db_type || '').toLowerCase()
    if (!form.env) form.env = inst.idc || ''
  }
  await loadApprovers()
}

async function loadApprovers() {
  if (!form.instance_id) {
    approvers.value = []
    return
  }
  const { data } = await api.get('/tickets/target-approvers', {
    params: { instance_id: form.instance_id, db_name: form.db_name },
  })
  approvers.value = (data.data || []).map((a) => a.name || a).filter(Boolean)
  if (!form.assigned_to && approvers.value[0]) form.assigned_to = approvers.value[0]
}

function applyDraft() {
  try {
    const raw = sessionStorage.getItem(DQL_DRAFT_KEY)
    if (!raw) return
    const d = JSON.parse(raw)
    form.database_id = d.database_id || ''
    form.instance_id = d.instance_id || ''
    form.db_name = d.db_name || ''
    form.env = d.env || ''
    form.db_type = String(d.db_type || '').toLowerCase()
    form.sql_content = d.sql || ''
    rowCount.value = Number(d.row_count || 0)
    resultCols.value = Array.isArray(d.columns) ? d.columns : []
    sessionStorage.removeItem(DQL_DRAFT_KEY)
  } catch {
    sessionStorage.removeItem(DQL_DRAFT_KEY)
  }
}

async function loadMeta() {
  const [instRes, dbRes, envRes, argRes, apprRes] = await Promise.all([
    api.get('/instances'),
    api.get('/databases'),
    api.get('/environments').catch(() => ({ data: { data: [] } })),
    api.get('/global-args').catch(() => ({ data: { data: [] } })),
    api.get('/create-db/approvers').catch(() => ({ data: { data: [] } })),
  ])
  instances.value = instRes.data.data || []
  databases.value = dbRes.data.data || []
  envs.value = buildEnvOptions(envRes.data.data || [], instances.value).map((e) => e.value)
  const raw = (argRes.data.data || []).find((a) => a.key === 'ticket_instance_types')?.value || ''
  try {
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed) && parsed.length) {
      dbTypes.value = parsed
        .filter((t) => t.value && !['kafka', 'redis', 'mongodb', 'elasticsearch', 'cassandra'].includes(t.value))
        .map((t) => ({ value: t.value, label: t.label || t.value }))
    }
  } catch { /* keep default */ }
  extraApprovers.value = [...new Set((apprRes.data.data || []).map((a) => a.name || a).filter(Boolean))]
}

async function submit() {
  await formRef.value?.validate()
  if (!form.sql_content.trim()) {
    ElMessage.error('SQL语句不能为空')
    return
  }
  const hasDBA = !!form.dba_assigned_to
  const hasSec = !!form.security_assigned_to
  if (hasDBA !== hasSec) {
    ElMessage.error('请同步设置 DBA审批人 和 安全审批人')
    return
  }
  if (form.need_sensitive_data && !hasDBA) {
    ElMessage.error('开启原文导出敏感字段需设置DBA审批人和安全审批人')
    return
  }
  if (rowCount.value > 500 && !hasDBA) {
    ElMessage.error('导出数据超过500条需设置DBA审批人和安全审批人')
    return
  }
  saving.value = true
  try {
    const { data } = await api.post('/tickets', {
      type: 'dql',
      instance_id: form.instance_id,
      database_id: form.database_id,
      db_name: form.db_name,
      sql_content: form.sql_content,
      description: form.description,
      assigned_to: form.assigned_to,
      need_sensitive_data: form.need_sensitive_data,
      dba_assigned_to: form.dba_assigned_to,
      security_assigned_to: form.security_assigned_to,
      row_count: rowCount.value,
    })
    ElMessage.success('工单创建成功 ' + (data.data?.ticket_no || ''))
    router.push('/ops/tickets/' + (data.data?.id || ''))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  applyDraft()
  await loadMeta()
  if (form.database_id) await onDbChange()
})
</script>

<style scoped>
.dql-card {
  max-width: 880px;
  width: 100%;
  margin: 0 auto;
  flex: 1 1 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 16px 24px 12px;
  box-sizing: border-box;
}
.dql-form {
  flex: 1 1 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.dql-form :deep(.el-form-item) { margin-bottom: 10px; flex-shrink: 0; }
.dql-form :deep(.sql-item) {
  flex: 1 1 0;
  min-height: 0;
}
.dql-form :deep(.sql-item .el-form-item__label) { align-items: flex-start; padding-top: 6px; }
.dql-form :deep(.sql-item .el-form-item__content) {
  min-height: 0;
  align-self: stretch;
}
.dql-form :deep(.sql-item .el-textarea),
.dql-form :deep(.sql-item .el-textarea__inner) {
  height: 100% !important;
  min-height: 72px;
}
.type-opt { display: inline-flex; align-items: center; gap: 8px; }
.form-extra { color: #9ca3af; font-size: 12px; line-height: 1.4; margin-top: 4px; }
.warn-box {
  margin: 4px 0 10px;
  padding: 8px 12px 0;
  background: #fffbeb;
  border: 1px solid #fcd34d;
  border-radius: 4px;
  flex-shrink: 0;
}
.warn-rules { border-bottom: 1px solid #fcd34d; margin-bottom: 8px; }
.warn-rules p { margin: 0 0 6px; color: #a16207; font-size: 12px; line-height: 1.5; }
.warn-rules b { color: #854d0e; font-weight: 600; }
.warn-strong { color: #713f12 !important; font-weight: 600; }
.mask-line {
  margin: 0 0 6px;
  padding: 6px 8px;
  background: #f3f0d8;
  color: #a16207;
  font-size: 12px;
}
.mask-chip {
  display: inline-block;
  margin: 4px 0 0 8px;
  padding: 2px 8px;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  color: #374151;
  font-size: 12px;
}
.muted { color: #9ca3af; }
.sql-editor :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
</style>
