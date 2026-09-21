<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">新增实例</el-button>
    </div>
    <div class="ops-filters">
      <div class="filter-item">
        <span class="label">关键词：</span>
        <el-input v-model="q" placeholder="请输入名称/主机" @keyup.enter="load" />
      </div>
      <div class="toolbar" style="margin:0; flex-shrink:0">
        <el-button type="primary" @click="load">查询</el-button>
      </div>
    </div>
    <el-tabs v-model="activeType" class="inst-tabs" @tab-change="onTabChange">
      <el-tab-pane v-for="t in tabs" :key="t.value" :name="t.value">
        <template #label>
          <span class="tab-label">
            <DbTypeIcon :type="t.value" :size="16" />
            <span>{{ t.label }} ({{ typeCount(t.value) }})</span>
          </span>
        </template>
      </el-tab-pane>
    </el-tabs>
    <div class="ops-table">
    <el-table :data="filteredRows" border height="100%">
      <el-table-column prop="name" label="名称" />
      <el-table-column label="区域" width="120">
        <template #default="{ row }">{{ regionName(row.region_id) }}</template>
      </el-table-column>
      <el-table-column label="环境" width="120">
        <template #default="{ row }">{{ envName(row.env_id) }}</template>
      </el-table-column>
      <el-table-column label="连接主机" min-width="160">
        <template #default="{ row }">{{ row.host }}:{{ row.port }}</template>
      </el-table-column>
      <el-table-column label="域名" min-width="200">
        <template #default="{ row }">
          <template v-if="domainList(row).length">
            <el-tag v-for="d in domainList(row)" :key="d" size="small" class="domain-tag">{{ d }}</el-tag>
          </template>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="标签" min-width="180">
        <template #default="{ row }">
          <template v-if="(row.tags || []).length">
            <el-tag v-for="t in row.tags" :key="t" size="small" type="info" class="domain-tag">{{ t }}</el-tag>
          </template>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="云厂商" width="100">
        <template #default="{ row }">{{ cloudVendorText(row.cloud_vendor) }}</template>
      </el-table-column>
      <el-table-column prop="cloud_instance_id" label="云实例 ID" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ row.cloud_instance_id || '—' }}</template>
      </el-table-column>
      <el-table-column prop="cloud_region" label="云 Region" width="130">
        <template #default="{ row }">{{ row.cloud_region || '—' }}</template>
      </el-table-column>
      <el-table-column prop="approvers" label="审批人">
        <template #default="{ row }">{{ (row.approvers || []).join(',') }}</template>
      </el-table-column>
      <el-table-column label="操作" width="160">
        <template #default="{ row }">
          <el-button text type="primary" @click="open(row)">编辑</el-button>
          <el-button text type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    </div>
    <el-dialog v-model="visible" :title="form.id ? '编辑实例' : '新增实例'" width="620px" append-to-body destroy-on-close>
      <el-form :model="form" label-width="90px">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.db_type" :disabled="!form.id" placeholder="当前 Tab 类型" style="width:100%">
            <el-option v-for="t in typeOptions" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
          <div v-if="!typeOptions.length" class="type-hint">请先在「全局参数」配置 ticket_instance_types</div>
        </el-form-item>
        <el-form-item label="区域">
          <el-select v-model="form.region_id" filterable clearable placeholder="请选择区域" style="width:100%" @change="onRegionChange">
            <el-option v-for="r in regions" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境">
          <el-select
            v-model="form.env_id"
            filterable
            clearable
            :disabled="!form.region_id"
            :placeholder="form.region_id ? '请选择环境' : '请先选择区域'"
            style="width:100%"
          >
            <el-option v-for="e in regionEnvs" :key="e.id" :label="e.name" :value="e.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="连接主机">
          <el-input v-model="form.host" placeholder="实际连接 IP / 主机名" />
        </el-form-item>
        <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" /></el-form-item>
        <el-form-item label="域名">
          <el-select
            v-model="form.domains"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="回车添加，一个主机可绑多个域名"
            style="width:100%"
          />
          <div class="type-hint">同一连接主机可绑定多个域名，输入后回车生成标签</div>
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="form.tags"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="输入后回车，可自定义任意标签"
            style="width:100%"
          >
            <el-option v-for="t in usedTags" :key="t" :label="t" :value="t" />
          </el-select>
          <div class="type-hint">可选。可沿用已有标签，也可输入新标签后回车</div>
        </el-form-item>
        <el-form-item label="账号"><el-input v-model="form.username" /></el-form-item>
        <el-form-item v-if="showPassword" :label="passwordLabel">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            autocomplete="new-password"
            :placeholder="passwordPlaceholder"
          />
          <div class="type-hint">{{ passwordHint }}</div>
        </el-form-item>
        <el-form-item v-if="showCloud" label="云厂商">
          <el-select v-model="form.cloud_vendor" clearable placeholder="空=自建" style="width:100%">
            <el-option label="自建" value="" />
            <el-option label="腾讯云" value="tencent" />
            <el-option label="阿里云" value="aliyun" />
            <el-option label="华为云" value="huawei" />
            <el-option label="AWS" value="aws" />
          </el-select>
          <div class="type-hint">闪回下载 WAL 时按厂商匹配「数据闪回 → 多云密钥」中的 AK/SK。连库仍用上方主机/账密。</div>
        </el-form-item>
        <el-form-item v-if="showCloud" label="云实例 ID">
          <el-input v-model="form.cloud_instance_id" :placeholder="cloudIdPlaceholder" />
        </el-form-item>
        <el-form-item v-if="showCloud" label="云 Region">
          <el-input v-model="form.cloud_region" placeholder="如 ap-guangzhou / cn-hangzhou" />
        </el-form-item>
        <el-form-item label="审批人"><el-input v-model="approverText" placeholder="逗号分隔，如 admin" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="form.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import DbTypeIcon from '../../components/DbTypeIcon.vue'

const typeOptions = ref([])
const regions = ref([])
const regionEnvs = ref([])
const allEnvs = ref([])
const rows = ref([])
const q = ref('')
const visible = ref(false)
const form = ref({})
const approverText = ref('admin')
const activeType = ref('')
const OTHER = '__other__'
const REQUIRED_PASSWORD_TYPES = new Set([
  'mysql', 'postgres', 'postgresql', 'tidb', 'starrocks', 'clickhouse',
])
const OPTIONAL_PASSWORD_TYPES = new Set([
  'kafka', 'redis', 'elasticsearch', 'mongodb', 'cockroachdb', 'cockroach',
  'oceanbase', 'cassandra', 'oracle', 'sqlserver', 'mssql',
])

const dbTypeKey = computed(() => String(form.value.db_type || '').toLowerCase())
const passwordRequired = computed(() => REQUIRED_PASSWORD_TYPES.has(dbTypeKey.value))
const showPassword = computed(() => passwordRequired.value || OPTIONAL_PASSWORD_TYPES.has(dbTypeKey.value))
const showCloud = computed(() => {
  const t = String(form.value.db_type || '').toLowerCase()
  return t === 'mysql' || t === 'postgres' || t === 'postgresql'
})
const cloudIdPlaceholder = computed(() => {
  const v = String(form.value.cloud_vendor || '')
  if (v === 'tencent') return '如 postgres-xxxx'
  if (v === 'aliyun') return '如 pgm-xxxx / rr-xxxx'
  if (v === 'huawei') return '云数据库实例 ID'
  if (v === 'aws') return '如 db-xxxx'
  return '云平台实例 ID'
})
function cloudVendorText(v) {
  return { tencent: '腾讯云', aliyun: '阿里云', huawei: '华为云', aws: 'AWS' }[String(v || '')] || (v ? v : '自建')
}
const passwordLabel = computed(() => {
  const t = dbTypeKey.value
  if (t === 'mysql') return 'MySQL 密码'
  if (t === 'postgres' || t === 'postgresql') return 'PostgreSQL 密码'
  if (t === 'tidb') return 'TiDB 密码'
  if (t === 'starrocks') return 'StarRocks 密码'
  if (t === 'clickhouse') return 'ClickHouse 密码'
  if (t === 'kafka') return '密码（可选）'
  if (t === 'redis') return '密码（可选）'
  return passwordRequired.value ? '密码' : '密码（可选）'
})
const passwordPlaceholder = computed(() => {
  if (form.value.id && form.value.password_set) return '已加密保存，不填则不修改'
  return passwordRequired.value ? '请输入密码' : '无认证则留空'
})
const passwordHint = computed(() => (
  passwordRequired.value
    ? '密码入库加密存储，接口不回传明文'
    : '可选。Kafka / Redis 等无 SASL 或无密码时可留空'
))

function defaultPort(dbType) {
  const map = {
    mysql: 3306, postgres: 5432, postgresql: 5432, tidb: 4000,
    starrocks: 9030, clickhouse: 8123, kafka: 9092, redis: 6379,
    cockroachdb: 26257, cockroach: 26257, oceanbase: 2881, cassandra: 9042,
    oracle: 1521, elasticsearch: 9200, mongodb: 27017, sqlserver: 1433, mssql: 1433,
  }
  return map[String(dbType || '').toLowerCase()] || 5432
}

function parseInstanceTypes(raw) {
  try {
    const arr = JSON.parse(String(raw || '').trim() || '[]')
    if (!Array.isArray(arr)) return []
    const seen = new Set()
    const out = []
    for (const it of arr) {
      const value = String(it?.value ?? '').trim()
      if (!value || seen.has(value)) continue
      seen.add(value)
      out.push({ value, label: String(it?.label || value).trim() || value })
    }
    return out
  } catch {
    return []
  }
}

const knownTypes = computed(() => new Set(typeOptions.value.map((t) => t.value)))
const tabs = computed(() => {
  const list = typeOptions.value.map((t) => ({ value: t.value, label: t.label }))
  if (rows.value.some((r) => r.db_type && !knownTypes.value.has(r.db_type))) {
    list.push({ value: OTHER, label: '其他' })
  }
  return list
})
const filteredRows = computed(() => {
  if (activeType.value === OTHER) {
    return rows.value.filter((r) => r.db_type && !knownTypes.value.has(r.db_type) && !knownTypes.value.has(typeKey(r.db_type)))
  }
  if (!activeType.value) return rows.value
  return rows.value.filter((r) => typeKey(r.db_type) === typeKey(activeType.value) || r.db_type === activeType.value)
})

const usedTags = computed(() => {
  const set = new Set()
  for (const r of rows.value) {
    for (const t of r.tags || []) {
      const s = String(t).trim()
      if (s) set.add(s)
    }
  }
  return [...set].sort()
})

function typeKey(t) {
  const n = String(t || '').toLowerCase()
  if (n === 'cockroach' || n === 'cockroadchdb') return 'cockroachdb'
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  return n
}
function typeCount(value) {
  if (value === OTHER) return rows.value.filter((r) => r.db_type && !knownTypes.value.has(r.db_type) && !knownTypes.value.has(typeKey(r.db_type))).length
  return rows.value.filter((r) => typeKey(r.db_type) === typeKey(value) || r.db_type === value).length
}

function ensureActiveTab() {
  const names = tabs.value.map((t) => t.value)
  if (!names.includes(activeType.value)) activeType.value = names[0] || ''
}

function onTabChange() {
  q.value = ''
}

async function loadTypes() {
  const args = (await api.get('/global-args')).data.data || []
  const raw = args.find((a) => a.key === 'ticket_instance_types')?.value || ''
  typeOptions.value = parseInstanceTypes(raw)
  ensureActiveTab()
}

async function loadMeta() {
  const [r, e] = await Promise.all([api.get('/regions'), api.get('/environments')])
  regions.value = r.data.data || []
  allEnvs.value = e.data.data || []
}

function regionName(id) {
  return regions.value.find((r) => r.id === id)?.name || '—'
}
function envName(id) {
  return allEnvs.value.find((e) => e.id === id)?.name || '—'
}
function domainList(row) {
  const list = Array.isArray(row?.domains) ? row.domains.filter(Boolean) : []
  if (list.length) return list
  return row?.domain_name ? [row.domain_name] : []
}

async function loadRegionEnvs(regionId) {
  if (!regionId) {
    regionEnvs.value = []
    return
  }
  regionEnvs.value = (await api.get(`/regions/${regionId}/envs`)).data.data || []
}

function onRegionChange() {
  form.value.env_id = ''
  loadRegionEnvs(form.value.region_id)
}

async function load() {
  const { data } = await api.get('/instances', { params: { q: q.value } })
  rows.value = data.data || []
  ensureActiveTab()
}
function open(row) {
  const tabType = activeType.value !== OTHER ? activeType.value : ''
  form.value = row
    ? { ...row, domains: domainList(row), tags: [...(row.tags || [])], password: '' }
    : { db_type: tabType, port: defaultPort(tabType), region_id: '', env_id: '', host: '', domains: [], tags: [], password: '', approvers: ['admin'], cloud_vendor: '', cloud_instance_id: '', cloud_region: '' }
  const cur = String(form.value.db_type || '').trim()
  if (cur && !typeOptions.value.some((t) => t.value === cur)) {
    typeOptions.value = [...typeOptions.value, { value: cur, label: cur }]
  }
  approverText.value = (form.value.approvers || []).join(',')
  visible.value = true
  loadRegionEnvs(form.value.region_id)
}
async function save() {
  const domains = (form.value.domains || []).map((s) => String(s).trim()).filter(Boolean)
  const payload = {
    ...form.value,
    domains,
    domain_name: domains[0] || '',
    tags: (form.value.tags || []).map((s) => String(s).trim()).filter(Boolean),
    approvers: approverText.value.split(',').map((s) => s.trim()).filter(Boolean),
  }
  if (!payload.host) {
    ElMessage.warning('请填写连接主机')
    return
  }
  if (!domains.length) {
    ElMessage.warning('请至少添加一个域名')
    return
  }
  if (passwordRequired.value && !payload.id && !String(payload.password || '').trim()) {
    ElMessage.warning('请填写密码')
    return
  }
  const t = String(payload.db_type || '').toLowerCase()
  if (payload.cloud_vendor && (t === 'postgres' || t === 'postgresql')) {
    if (!String(payload.cloud_instance_id || '').trim()) {
      ElMessage.warning('云实例须填写云实例 ID，用于从云平台下载 WAL')
      return
    }
    if (!String(payload.cloud_region || '').trim()) {
      ElMessage.warning('云实例须填写云 Region')
      return
    }
  }
  if (!String(payload.password || '').trim()) delete payload.password
  if (payload.id) await api.put('/instances/' + payload.id, payload)
  else await api.post('/instances', payload)
  ElMessage.success('已保存')
  visible.value = false
  if (payload.db_type) activeType.value = payload.db_type
  await load()
}
async function remove(row) {
  await ElMessageBox.confirm('删除实例及其已登记库？')
  await api.delete('/instances/' + row.id)
  await load()
}
onMounted(async () => {
  await Promise.all([load(), loadTypes(), loadMeta()])
})
</script>

<style scoped>
.type-hint { margin-top: 6px; color: #909399; font-size: 12px; }
.inst-tabs { flex-shrink: 0; }
.inst-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
.inst-tabs :deep(.el-tabs__content) { display: none; }
.tab-label { display: inline-flex; align-items: center; gap: 6px; }
.domain-tag { margin: 0 4px 4px 0; }
.muted { color: #9ca3af; }
</style>
