<template>
  <div class="dcl-page">
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="140px"
      label-position="right"
      class="dcl-form"
    >
      <el-form-item label="环境" prop="env">
        <el-select v-model="form.env" clearable placeholder="请选择环境" style="width:100%" @change="onEnvOrTypeChange">
          <el-option label="测试" value="test" />
          <el-option label="生产" value="prod" />
          <el-option v-for="e in extraEnvs" :key="e" :label="e" :value="e" />
        </el-select>
      </el-form-item>
      <el-form-item label="数据库类型" prop="db_type">
        <el-select v-model="form.db_type" clearable placeholder="请选择数据库类型" style="width:100%" @change="onEnvOrTypeChange">
          <el-option label="MySQL" value="mysql" />
          <el-option label="PostgreSQL" value="postgres" />
          <el-option label="StarRocks" value="starrocks" />
        </el-select>
      </el-form-item>
      <el-form-item label="数据源(别名)" prop="instance_id">
        <el-select
          v-model="form.instance_id"
          filterable
          clearable
          :disabled="!form.env || !form.db_type"
          :placeholder="sourcePlaceholder"
          style="width:100%"
          @change="onSourceChange"
        >
          <el-option v-for="i in sourceOptions" :key="i.id" :label="instLabel(i)" :value="i.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="数据库" prop="db_ids" class="db-item">
        <DatabasePicker
          v-model="form.db_ids"
          :options="dbOptions"
          :loading="dbLoading"
          :disabled="!form.env || !form.db_type || !form.instance_id"
        />
      </el-form-item>
      <el-form-item label="数据库账号" prop="username">
        <el-select
          v-model="form.username"
          filterable
          allow-create
          clearable
          default-first-option
          :disabled="!form.instance_id || !form.db_type"
          :placeholder="accountPlaceholder"
          style="width:100%"
        >
          <el-option v-for="a in accountOptions" :key="a" :label="a" :value="a" />
        </el-select>
        <div class="extra">从列表选择已有账号；自定义输入时账号名须以 _user 结尾</div>
      </el-form-item>
      <el-form-item label="账号权限" prop="permissions">
        <el-checkbox-group v-model="form.permissions">
          <el-checkbox label="r">读操作</el-checkbox>
          <el-checkbox label="c">写操作</el-checkbox>
          <el-checkbox label="u">更新操作</el-checkbox>
          <el-checkbox label="d">删除操作</el-checkbox>
        </el-checkbox-group>
      </el-form-item>
      <el-form-item label="选择审批人" prop="assigned_to">
        <el-select
          v-model="form.assigned_to"
          filterable
          clearable
          :loading="approverLoading"
          :placeholder="approverPlaceholder"
          style="width:100%"
        >
          <el-option v-for="a in approverOptions" :key="a" :label="a" :value="a" />
        </el-select>
      </el-form-item>
      <el-form-item label="执行原因" prop="description">
        <el-input v-model="form.description" type="textarea" :rows="3" placeholder="请输入执行原因" />
      </el-form-item>
      <p class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
      <div class="ticket-form-actions">
        <div class="actions">
          <el-button @click="reset">取消</el-button>
          <el-button @click="saveDraft">保存草稿</el-button>
          <el-button type="primary" :loading="submitting" @click="submit">提交申请</el-button>
        </div>
      </div>
    </el-form>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import DatabasePicker from '../../components/DatabasePicker.vue'
import { dbLabel } from '../../utils/db'
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'
const PERM_SQL = { r: 'SELECT', c: 'INSERT', u: 'UPDATE', d: 'DELETE' }

const router = useRouter()
const formRef = ref()
const instances = ref([])
const databases = ref([])
const dbLoading = ref(false)
const approverLoading = ref(false)
const submitting = ref(false)
const defaultApprover = ref('')
const form = reactive(emptyForm())

function emptyForm() {
  return {
    env: '',
    db_type: '',
    instance_id: '',
    db_ids: [],
    username: '',
    permissions: [],
    assigned_to: '',
    description: '',
  }
}

const extraEnvs = computed(() => {
  const set = new Set()
  for (const i of instances.value) {
    if (i.idc && i.idc !== 'test' && i.idc !== 'prod') set.add(i.idc)
  }
  return [...set]
})

const sourceOptions = computed(() => {
  if (!form.env || !form.db_type) return []
  return instances.value.filter((i) => i.db_type === form.db_type && (!i.idc || i.idc === form.env || i.idc === 'default'))
})

const currentInst = computed(() => instances.value.find((i) => i.id === form.instance_id))

const dbOptions = computed(() =>
  databases.value
    .filter((d) => d.instance_id === form.instance_id)
    .map((d) => ({ value: d.id, label: dbLabel(d) })),
)

const accountOptions = computed(() => {
  const names = []
  if (currentInst.value?.username) names.push(currentInst.value.username)
  return names
})

const approverOptions = computed(() => {
  const set = new Set()
  if (defaultApprover.value) set.add(defaultApprover.value)
  for (const a of currentInst.value?.approvers || []) if (a) set.add(a)
  for (const id of form.db_ids || []) {
    const db = databases.value.find((d) => d.id === id)
    for (const a of db?.approvers || []) if (a) set.add(a)
  }
  return [...set]
})

const sourcePlaceholder = computed(() => {
  if (!form.env) return '请先选择环境'
  if (!form.db_type) return '请先选择数据库类型'
  return '选择数据源(别名) 或搜索...'
})
const accountPlaceholder = computed(() => {
  if (!form.instance_id) return '请先选择数据源(别名)'
  if (!form.db_type) return '请先选择数据库类型'
  return '输入账号筛选，或直接选择/自定义输入'
})
const approverPlaceholder = computed(() => {
  if (approverLoading.value) return '加载审批人…'
  if (approverOptions.value.length) return '请选择审批人'
  return '暂无审批人，请配置 sync_default_approver'
})

const rules = {
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  db_type: [{ required: true, message: '请选择数据库类型', trigger: 'change' }],
  instance_id: [{ required: true, message: '请选择数据源', trigger: 'change' }],
  db_ids: [{ type: 'array', required: true, min: 1, message: '请选择数据库', trigger: 'change' }],
  username: [
    { required: true, message: '请选择或输入数据库账号', trigger: 'blur' },
    {
      validator: (_, val, cb) => {
        const v = String(val || '').trim()
        if (!v) return cb()
        if (accountOptions.value.includes(v) || v.toLowerCase().endsWith('_user')) return cb()
        cb(new Error('自定义账号须以 _user 结尾'))
      },
      trigger: 'blur',
    },
  ],
  permissions: [{ type: 'array', required: true, min: 1, message: '请至少选择一项账号权限', trigger: 'change' }],
  assigned_to: [{ required: true, message: '请选择审批人', trigger: 'change' }],
  description: [{ required: true, message: '请输入执行原因', trigger: 'blur' }],
}

function instLabel(i) {
  const addr = i.host ? `${i.host}:${i.port}` : (i.domain_name || '')
  return i.name ? `${i.name} — ${addr}` : addr
}

function onEnvOrTypeChange() {
  form.instance_id = ''
  form.db_ids = []
  form.username = ''
}

async function onSourceChange() {
  form.db_ids = []
  form.username = currentInst.value?.username || ''
  await loadDbs()
  if (!form.assigned_to && approverOptions.value[0]) form.assigned_to = approverOptions.value[0]
}

async function loadMeta() {
  instances.value = (await api.get('/instances')).data.data || []
  approverLoading.value = true
  try {
    const args = (await api.get('/global-args')).data.data || []
    defaultApprover.value = args.find((a) => a.key === 'sync_default_approver')?.value || ''
    if (!form.assigned_to && defaultApprover.value) form.assigned_to = defaultApprover.value
  } finally {
    approverLoading.value = false
  }
}

async function loadDbs() {
  if (!form.instance_id) {
    databases.value = []
    return
  }
  dbLoading.value = true
  try {
    databases.value = (await api.get('/databases', { params: { instance_id: form.instance_id } })).data.data || []
  } finally {
    dbLoading.value = false
  }
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'dcl',
  label: '权限申请',
  path: '/ops/dcl',
  form,
  applyValues: (d) => {
    Object.assign(form, {
      ...emptyForm(),
      ...d,
      db_ids: Array.isArray(d.db_ids) ? d.db_ids : [],
      permissions: Array.isArray(d.permissions) ? d.permissions : [],
    })
  },
})

function reset() {
  Object.assign(form, emptyForm())
  if (defaultApprover.value) form.assigned_to = defaultApprover.value
  formRef.value?.clearValidate()
}

function buildGrantSql() {
  const privs = (form.permissions || []).map((p) => PERM_SQL[p]).filter(Boolean).join(', ')
  const dbs = (form.db_ids || [])
    .map((id) => databases.value.find((d) => d.id === id)?.db_name)
    .filter(Boolean)
  return `GRANT ${privs} ON ${dbs.join(', ')} TO ${form.username};`
}

async function submit() {
  await formRef.value?.validate()
  const dbName = (form.db_ids || [])
    .map((id) => databases.value.find((d) => d.id === id)?.db_name)
    .filter(Boolean)
    .join(',')
  submitting.value = true
  try {
    await api.post('/dcl-grants', {
      instance_id: form.instance_id,
      db_name: dbName,
      assigned_to: form.assigned_to,
      description: form.description,
      sql_content: buildGrantSql(),
    })
    ElMessage.success('工单创建成功')
    clearOnSubmitSuccess()
    router.push('/ops/tickets')
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  await loadMeta()
  restoreDraft()
  if (form.instance_id) await loadDbs()
})
</script>

<style scoped>
.dcl-page {
  width: 100%;
  height: 100%;
  max-height: 100%;
  min-height: 0;
  padding: 16px;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #fff;
}
.dcl-form {
  max-width: 1152px;
  margin: 0 auto;
  width: 100%;
  flex: 1 1 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.dcl-form :deep(.el-form-item) {
  margin-bottom: 12px;
  flex-shrink: 0;
}
.dcl-form :deep(.el-form-item.db-item) {
  flex: 1 1 0;
  min-height: 0;
  overflow: hidden;
}
.dcl-form :deep(.db-item .el-form-item__label) {
  align-items: flex-start;
  padding-top: 8px;
}
.dcl-form :deep(.db-item .el-form-item__content) {
  min-width: 0;
  min-height: 0;
  align-self: stretch;
  line-height: normal;
}
.dcl-form :deep(.db-item .db-picker) {
  height: 100%;
}
.dcl-form :deep(.el-select),
.dcl-form :deep(.el-textarea) { width: 100%; }
.extra { margin-top: 4px; color: #909399; font-size: 12px; line-height: 1.4; }
.actions { display: flex; justify-content: center; gap: 12px; width: 100%; }
</style>
