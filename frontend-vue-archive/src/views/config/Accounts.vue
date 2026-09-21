<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">新建账号</el-button>
    </div>
    <p class="hint">新增数据库工单审批通过后会自动写入此处；密码以 AES-256-GCM 加密存储，查看时再解密。</p>
    <el-tabs v-model="activeType" class="inst-tabs">
      <el-tab-pane v-for="t in tabs" :key="t.value" :name="t.value">
        <template #label>
          <span class="tab-label">
            <DbTypeIcon :type="t.value" :size="16" />
            <span>{{ t.label }} ({{ typeCount(t.value) }})</span>
          </span>
        </template>
      </el-tab-pane>
    </el-tabs>
    <div class="ops-filters">
      <div class="filter-item">
        <el-select v-model="idc" clearable filterable placeholder="选择环境" style="width:160px">
          <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
        </el-select>
      </div>
      <div class="filter-item">
        <el-select v-model="dbName" clearable filterable placeholder="选择数据库" style="width:220px">
          <el-option v-for="n in dbNameOptions" :key="n" :label="n" :value="n" />
        </el-select>
      </div>
      <div class="filter-item">
        <el-input v-model="q" clearable placeholder="搜索用户名、库名、描述" style="width:240px" />
      </div>
    </div>
    <div class="ops-table">
      <el-table :data="filteredRows" border height="100%" v-loading="loading">
        <el-table-column label="账号类型" width="120">
          <template #default="{ row }">
            <el-tag size="small" type="primary">{{ typeLabel(row.db_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="用户名" min-width="140">
          <template #default="{ row }">
            <el-button text type="primary" @click="view(row)">{{ row.username }}</el-button>
          </template>
        </el-table-column>
        <el-table-column label="数据库" min-width="140">
          <template #default="{ row }">
            <el-tag size="small" type="primary" effect="plain">{{ row.db_name || '—' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="域名" min-width="200">
          <template #default="{ row }">{{ domainText(row) }}</template>
        </el-table-column>
        <el-table-column prop="idc" label="IDC" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.idc" size="small" type="success">{{ row.idc }}</el-tag>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="180" show-overflow-tooltip />
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="view(row)">查看</el-button>
            <el-button text type="primary" @click="open(row)">编辑</el-button>
            <el-button text type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="formVisible" :title="form.id ? '编辑账号' : '新建账号'" width="560px" append-to-body destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="实例" prop="instance_id">
          <el-select v-model="form.instance_id" filterable :disabled="!!form.id" style="width:100%" @change="onInstChange">
            <el-option v-for="i in formInstances" :key="i.id" :label="instLabel(i)" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="数据库" prop="db_name">
          <el-select v-model="form.db_name" filterable allow-create style="width:100%">
            <el-option v-for="d in instDbs" :key="d.id || d.db_name" :label="d.db_name" :value="d.db_name" />
          </el-select>
        </el-form-item>
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" :disabled="!!form.id" />
        </el-form-item>
        <el-form-item :label="form.id ? '新密码' : '密码'">
          <el-input v-model="form.password" type="password" show-password :placeholder="form.id ? '留空则不修改' : '留空则自动生成'" />
        </el-form-item>
        <el-form-item label="账号权限" prop="permissions">
          <el-select v-model="form.permissions" multiple style="width:100%">
            <el-option v-for="p in permissionOps" :key="p.value" :label="p.label" :value="p.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="viewVisible" title="账号详情" size="520px" append-to-body>
      <el-descriptions v-if="viewing" :column="1" border>
        <el-descriptions-item label="账号名称">
          <div class="pwd-row">
            <b>{{ viewing.username }}</b>
            <code v-if="plainPassword" class="pwd">{{ plainPassword }}</code>
            <el-button v-if="!plainPassword" text type="primary" :loading="pwdLoading" @click="revealPwd">查看密码</el-button>
            <template v-else>
              <el-button text @click="plainPassword = ''">隐藏</el-button>
              <el-button text type="primary" @click="copyPwd">复制</el-button>
            </template>
          </div>
        </el-descriptions-item>
        <el-descriptions-item label="数据库">{{ viewing.db_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="账号权限">{{ permText(viewing.permissions) }}</el-descriptions-item>
        <el-descriptions-item label="域名">{{ domainText(viewing) }}</el-descriptions-item>
        <el-descriptions-item label="数据库类型">{{ typeLabel(viewing.db_type) }}</el-descriptions-item>
        <el-descriptions-item label="IDC">{{ viewing.idc || '—' }}</el-descriptions-item>
        <el-descriptions-item label="描述">{{ viewing.description || '—' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ fmtTime(viewing.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="创建人">{{ viewing.created_by || '—' }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{ fmtTime(viewing.updated_at) }}</el-descriptions-item>
        <el-descriptions-item label="更新人">{{ viewing.updated_by || '—' }}</el-descriptions-item>
      </el-descriptions>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'
import DbTypeIcon from '../../components/DbTypeIcon.vue'

const permissionOps = [
  { label: '读操作', value: 'r' },
  { label: '写操作', value: 'c' },
  { label: '更新操作', value: 'u' },
  { label: '删除操作', value: 'd' },
]
const defaultTabs = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
]

const loading = ref(false)
const saving = ref(false)
const rows = ref([])
const instances = ref([])
const databases = ref([])
const envOptions = ref([])
const tabs = ref(defaultTabs)
const activeType = ref('mysql')
const idc = ref('')
const dbName = ref('')
const q = ref('')
const formVisible = ref(false)
const viewVisible = ref(false)
const viewing = ref(null)
const plainPassword = ref('')
const pwdLoading = ref(false)
const formRef = ref()
const instDbs = ref([])

const form = reactive({
  id: '',
  instance_id: '',
  db_name: '',
  username: '',
  password: '',
  permissions: [],
  description: '',
})

const rules = {
  instance_id: [{ required: true, message: '请选择实例', trigger: 'change' }],
  db_name: [{ required: true, message: '请选择数据库', trigger: 'change' }],
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  permissions: [{ type: 'array', required: true, min: 1, message: '请选择账号权限', trigger: 'change' }],
}

const formInstances = computed(() => instances.value.filter((i) => String(i.db_type || '').toLowerCase() === activeType.value))

const filteredRows = computed(() => {
  const kw = String(q.value || '').trim().toLowerCase()
  return rows.value.filter((r) => {
    if (activeType.value && String(r.db_type || '').toLowerCase() !== activeType.value) return false
    if (idc.value && r.idc !== idc.value) return false
    if (dbName.value && r.db_name !== dbName.value) return false
    if (kw && !`${r.username} ${r.db_name} ${r.description || ''}`.toLowerCase().includes(kw)) return false
    return true
  })
})

const dbNameOptions = computed(() => {
  const set = new Set()
  for (const r of rows.value) {
    if (activeType.value && String(r.db_type || '').toLowerCase() !== activeType.value) continue
    if (r.db_name) set.add(r.db_name)
  }
  for (const d of databases.value) {
    const inst = instances.value.find((i) => i.id === d.instance_id)
    if (inst && String(inst.db_type || '').toLowerCase() === activeType.value && d.db_name) set.add(d.db_name)
  }
  return [...set]
})

function typeCount(t) {
  return rows.value.filter((r) => String(r.db_type || '').toLowerCase() === t).length
}

function typeLabel(t) {
  return tabs.value.find((x) => x.value === t)?.label || t || '—'
}

function permText(list) {
  const map = Object.fromEntries(permissionOps.map((p) => [p.value, p.label]))
  return (list || []).map((v) => map[v] || v).join('， ') || '—'
}

function domainText(row) {
  const host = row.domain_name || row.host
  if (!host) return row.instance_name || '—'
  return row.port ? `${host}:${row.port}` : host
}

function instLabel(i) {
  const addr = i.host ? `${i.host}:${i.port}` : (i.domain_name || '')
  return i.name ? (addr ? `${i.name} (${addr})` : i.name) : addr
}

function fmtTime(v) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function onInstChange() {
  form.db_name = ''
  const inst = instances.value.find((i) => i.id === form.instance_id)
  instDbs.value = databases.value.filter((d) => d.instance_id === form.instance_id)
  if (inst) {
    form.db_type = inst.db_type
  }
}

function open(row) {
  if (row) {
    Object.assign(form, {
      id: row.id,
      instance_id: row.instance_id,
      db_name: row.db_name,
      username: row.username,
      password: '',
      permissions: [...(row.permissions || [])],
      description: row.description || '',
    })
    instDbs.value = databases.value.filter((d) => d.instance_id === row.instance_id)
  } else {
    Object.assign(form, { id: '', instance_id: '', db_name: '', username: '', password: '', permissions: [], description: '' })
    instDbs.value = []
  }
  showDialog(formVisible)
}

async function save() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = {
      instance_id: form.instance_id,
      db_name: form.db_name,
      username: form.username,
      password: form.password,
      permissions: form.permissions,
      description: form.description,
    }
    if (form.id) {
      await api.put('/db-accounts/' + form.id, payload)
      ElMessage.success('已保存')
    } else {
      const { data } = await api.post('/db-accounts', payload)
      const pwd = data.real_password || ''
      ElMessage.success(pwd ? `已创建，初始密码：${pwd}` : '已创建')
    }
    formVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

function view(row) {
  viewing.value = row
  plainPassword.value = ''
  viewVisible.value = true
}

async function revealPwd() {
  if (!viewing.value?.id) return
  pwdLoading.value = true
  try {
    const { data } = await api.get('/db-accounts/' + viewing.value.id, { params: { view_credential: 1 } })
    plainPassword.value = data.real_password || ''
    if (!plainPassword.value) ElMessage.warning('未存储密码')
  } finally {
    pwdLoading.value = false
  }
}

async function copyPwd() {
  try {
    await navigator.clipboard.writeText(plainPassword.value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择')
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`确定删除账号 ${row.username}？`, '删除确认', { type: 'warning' })
  await api.delete('/db-accounts/' + row.id)
  ElMessage.success('已删除')
  await load()
}

async function load() {
  loading.value = true
  try {
    const [accRes, instRes, dbRes, envRes, argRes] = await Promise.all([
      api.get('/db-accounts'),
      api.get('/instances'),
      api.get('/databases'),
      api.get('/environments'),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
    ])
    rows.value = accRes.data.data || []
    instances.value = instRes.data.data || instRes.data || []
    databases.value = dbRes.data.data || dbRes.data || []
    const envRows = envRes.data.data || envRes.data || []
    const codes = new Set(envRows.map((e) => e.code).filter(Boolean))
    for (const i of instances.value) if (i.idc) codes.add(i.idc)
    for (const r of rows.value) if (r.idc) codes.add(r.idc)
    envOptions.value = [...codes]
    const args = argRes.data.data || argRes.data || []
    const raw = (Array.isArray(args) ? args : []).find((a) => a.key === 'ticket_instance_types')?.value || ''
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length) {
        tabs.value = parsed.filter((t) => t.value && t.value !== 'kafka' && t.value !== 'redis').map((t) => ({ value: t.value, label: t.label || t.value }))
      }
    } catch { /* keep default */ }
    if (!tabs.value.some((t) => t.value === activeType.value) && tabs.value[0]) {
      activeType.value = tabs.value[0].value
    }
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.hint { color: #6b7280; font-size: 13px; margin: 0 0 12px; }
.tab-label { display: inline-flex; align-items: center; gap: 6px; }
.ops-filters { display: flex; gap: 12px; margin-bottom: 12px; flex-wrap: wrap; }
.pwd-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.pwd { background: #f3f4f6; padding: 2px 8px; border-radius: 4px; }
</style>
