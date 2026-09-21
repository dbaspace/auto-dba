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

          <el-form-item label="数据源(别名)" prop="data_source_ids">
            <el-select
              v-model="form.data_source_ids"
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

          <el-form-item label="数据库" prop="db_name">
            <el-select
              v-model="form.db_name"
              filterable
              clearable
              :disabled="!form.data_source_ids.length"
              :loading="dbLoading"
              :placeholder="dbPlaceholder"
              style="width:100%"
              @change="onDatabaseChange"
            >
              <el-option v-for="d in dbNameOptions" :key="d.value" :label="d.label" :value="d.value" />
            </el-select>
          </el-form-item>

          <template v-if="!isStarRocks">
            <el-form-item label="是否批量">
              <div class="batch-row">
                <el-checkbox v-model="form.shard_db" @change="onShardDbChange">分库</el-checkbox>
                <el-checkbox v-model="form.shard_table" @change="onShardTableChange">分表</el-checkbox>
              </div>
            </el-form-item>

            <el-alert
              v-if="form.shard_db && shardDbPrefixHint"
              type="warning"
              show-icon
              :closable="false"
              class="hint-alert"
              :title="shardDbPrefixHint"
            />

            <el-form-item v-if="form.shard_db" label="分库前缀" prop="shard_db_prefix">
              <el-input v-model="form.shard_db_prefix" clearable placeholder="库名前缀匹配如 db_pay_0" />
            </el-form-item>
            <el-form-item v-if="form.shard_table" label="分表前缀" prop="shard_table_prefix">
              <el-input v-model="form.shard_table_prefix" clearable placeholder="表名前缀，如 tbl_order_" />
            </el-form-item>
          </template>

          <el-form-item>
            <template #label>
              <span>审核人 <span class="optional">(可选)</span></span>
            </template>
            <el-select
              v-model="form.assigned_to"
              filterable
              clearable
              :disabled="!form.db_name || !form.data_source_ids.length"
              :loading="reviewerLoading"
              :placeholder="reviewerPlaceholder"
              style="width:100%"
            >
              <el-option v-for="a in approvers" :key="a" :label="a" :value="a" />
            </el-select>
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

          <el-form-item label="是否调测" prop="is_debug">
            <el-radio-group v-model="form.is_debug">
              <el-radio value="yes">是</el-radio>
              <el-radio value="no">否</el-radio>
            </el-radio-group>
          </el-form-item>

          <p class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
          <el-form-item class="aside-actions">
            <el-button @click="fetchStructure">获取表结构</el-button>
            <el-tooltip :disabled="submitting || submitAllowed" :content="submitDisabledReason" placement="top">
              <span class="submit-wrap">
                <el-button type="primary" :loading="submitting" :disabled="!submitAllowed" @click="submit">
                  提交
                </el-button>
              </span>
            </el-tooltip>
            <el-button @click="saveDraft">保存草稿</el-button>
          </el-form-item>
        </el-form>
      </aside>

      <main class="order-main">
        <el-tabs v-model="mainTab">
          <el-tab-pane label="填写SQL" name="sql">
            <el-input
              v-model="form.sql_content"
              type="textarea"
              class="sql-editor"
              :autosize="{ minRows: 16 }"
              placeholder="ALTER TABLE t ADD COLUMN c varchar(32);"
            />
            <div class="sql-actions">
              <el-button type="primary" :loading="checkLoading" :disabled="checkLoading" @click="check">
                SQL检测
              </el-button>
              <el-button @click="beautify">SQL美化</el-button>
            </div>
            <p class="hint">
              编辑框内可检测、美化 SQL
              <template v-if="isStarRocks">；StarRocks 使用 MySQL 兼容语法</template>
            </p>
            <el-alert
              v-if="!isStarRocks && (form.shard_db || form.shard_table)"
              type="info"
              show-icon
              :closable="false"
              class="hint-alert"
              title="分库/分表模式下仅做语法与规范校验，不校验表是否存在于单个库"
            />
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
          </el-tab-pane>
          <el-tab-pane label="表结构详情" name="structure">
            <div v-loading="structureLoading">
              <el-table v-if="structureRows.length" :data="structureRows" size="small" border>
                <el-table-column prop="name" label="表名" min-width="240" />
              </el-table>
              <el-empty v-else :description="structureHint || '暂无表结构数据'" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="索引详情" name="index">
            <el-empty description="暂无索引数据" />
          </el-tab-pane>
        </el-tabs>
      </main>
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
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'

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
const mainTab = ref('sql')
const dsLoading = ref(false)
const dbLoading = ref(false)
const reviewerLoading = ref(false)
const checkLoading = ref(false)
const submitting = ref(false)
const structureLoading = ref(false)
const structureRows = ref([])
const structureHint = ref('')

const form = reactive(emptyForm())

function emptyForm() {
  return {
    env: '',
    instance_type: '',
    data_source_ids: [],
    db_name: '',
    shard_db: false,
    shard_db_prefix: '',
    shard_table: false,
    shard_table_prefix: '',
    assigned_to: '',
    description: '',
    scheduled_at: '',
    is_debug: 'yes',
    sql_content: '',
  }
}

const rules = {
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  instance_type: [{ required: true, message: '请选择实例类型', trigger: 'change' }],
  data_source_ids: [{ type: 'array', required: true, min: 1, message: '请选择数据源', trigger: 'change' }],
  db_name: [{ required: true, message: '请选择数据库', trigger: 'change' }],
  description: [{ required: true, message: '请输入描述', trigger: 'blur' }],
  is_debug: [{ required: true, message: '请选择', trigger: 'change' }],
  shard_db_prefix: [{
    validator: (_, v, cb) => {
      if (form.shard_db && !String(v || '').trim()) cb(new Error('勾选分库时请填写分库前缀'))
      else cb()
    },
    trigger: 'blur',
  }],
  shard_table_prefix: [{
    validator: (_, v, cb) => {
      if (form.shard_table && !String(v || '').trim()) cb(new Error('勾选分表时请填写分表前缀'))
      else cb()
    },
    trigger: 'blur',
  }],
}

const isStarRocks = computed(() => form.instance_type === 'starrocks')

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

const shardDbPrefixHint = computed(() => getShardDbPrefixHint(form.db_name, form.shard_db_prefix))

const checkFp = computed(() => JSON.stringify({
  env: form.env,
  instance_type: form.instance_type,
  data_source_ids: [...form.data_source_ids].sort(),
  db_name: form.db_name,
  shard_db: form.shard_db,
  shard_db_prefix: String(form.shard_db_prefix || '').trim(),
  shard_table: form.shard_table,
  shard_table_prefix: String(form.shard_table_prefix || '').trim(),
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
  return '选择数据源(别名) 或搜索...'
})

const dbPlaceholder = computed(() => {
  if (!form.data_source_ids.length) return '请先选择数据源(别名)'
  if (dbLoading.value) return '加载逻辑库…'
  return '请选择'
})

const reviewerPlaceholder = computed(() => {
  if (!form.db_name || !form.data_source_ids.length) return '请先选择数据源与数据库'
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

function longestCommonPrefixLen(a, b) {
  const n = Math.min(a.length, b.length)
  let i = 0
  for (; i < n; i++) {
    if (a[i] !== b[i]) break
  }
  return i
}

function getShardDbPrefixHint(database, prefix) {
  const db = String(database || '').trim()
  const pre = String(prefix || '').trim()
  if (!db || !pre) return ''
  const dbL = db.toLowerCase()
  const preL = pre.toLowerCase()
  if (dbL.startsWith(preL)) return ''
  const lcp = longestCommonPrefixLen(dbL, preL)
  const maxLen = Math.max(dbL.length, preL.length)
  const ratio = maxLen ? lcp / maxLen : 0
  if (ratio >= 0.72) return '逻辑库名未以分库前缀开头，但两者前缀较相似，请确认分库前缀是否正确'
  return '逻辑库名与分库前缀不一致，建议检查是否选错源库或前缀填写错误'
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

function onShardDbChange(on) {
  if (!on) form.shard_db_prefix = ''
}

function onShardTableChange(on) {
  if (!on) form.shard_table_prefix = ''
}

function resetCheck() {
  lastCheckFp.value = null
  checkRows.value = []
}

function onEnvOrTypeChange() {
  form.data_source_ids = []
  form.db_name = ''
  form.assigned_to = ''
  form.shard_db = false
  form.shard_table = false
  form.shard_db_prefix = ''
  form.shard_table_prefix = ''
  dbRecords.value = []
  approvers.value = []
  structureRows.value = []
  structureHint.value = ''
  resetCheck()
}

async function onDataSourcesChange() {
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
  const ids = form.data_source_ids || []
  if (!ids.length) {
    dbRecords.value = []
    return
  }
  dbLoading.value = true
  try {
    const lists = await Promise.all(ids.map(async (id) => {
      const { data } = await api.get(`/tickets/instances/${id}/databases`)
      return (data.data || []).map((d) => ({ ...d, instance_id: d.instance_id || id }))
    }))
    dbRecords.value = lists.flat()
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
  const ids = form.data_source_ids || []
  const dbName = String(form.db_name || '').trim()
  if (!ids.length || !dbName) {
    approvers.value = []
    return
  }
  reviewerLoading.value = true
  try {
    const lists = await Promise.all(ids.map(async (id) => {
      const { data } = await api.get('/tickets/target-approvers', { params: { instance_id: id, db_name: dbName } })
      return (data.data || []).map((a) => a.name || a).filter(Boolean)
    }))
    const seen = new Set()
    const out = []
    for (const name of lists.flat()) {
      if (seen.has(name)) continue
      seen.add(name)
      out.push(name)
    }
    approvers.value = out
    if (form.assigned_to && !out.includes(form.assigned_to)) form.assigned_to = ''
  } catch {
    approvers.value = []
    ElMessage.error('加载审核人失败')
  } finally {
    reviewerLoading.value = false
  }
}

function pickPrimary() {
  const dbName = String(form.db_name || '').trim()
  const hit = dbRecords.value.find((d) => d.db_name === dbName && form.data_source_ids.includes(d.instance_id))
    || dbRecords.value.find((d) => d.db_name === dbName)
  const instanceId = hit?.instance_id || form.data_source_ids[0] || ''
  return {
    instance_id: instanceId,
    database_id: hit?.id || '',
    db_name: dbName,
  }
}

async function check() {
  mainTab.value = 'sql'
  const sql = String(form.sql_content || '').trim()
  if (!sql) {
    ElMessage.error('请填写 SQL')
    return
  }
  if (!form.env || !form.instance_type || !form.data_source_ids.length || !form.db_name) {
    ElMessage.warning('检测前请先选择环境、实例类型、数据源与数据库')
    return
  }
  if (form.shard_db && !String(form.shard_db_prefix || '').trim()) {
    ElMessage.warning('已勾选分库时请填写分库前缀后再检测')
    return
  }
  if (form.shard_table && !String(form.shard_table_prefix || '').trim()) {
    ElMessage.warning('已勾选分表时请填写分表前缀后再检测')
    return
  }
  const fp = checkFp.value
  checkLoading.value = true
  try {
    const { data } = await api.post('/tickets/check-sql', {
      sql_content: sql,
      type: 'ddl',
      db_type: form.instance_type,
      instance_id: form.data_source_ids[0] || '',
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
  mainTab.value = 'sql'
  const sql = String(form.sql_content || '').trim()
  if (!sql) {
    ElMessage.warning('请填写 SQL')
    return
  }
  form.sql_content = sql
    .replace(/\s+/g, ' ')
    .replace(/\s*;\s*/g, ';\n')
    .replace(/\b(SELECT|FROM|WHERE|AND|OR|JOIN|LEFT|RIGHT|INNER|GROUP BY|ORDER BY|LIMIT|UPDATE|SET|INSERT INTO|VALUES|ALTER TABLE|ADD|DROP|CREATE|GRANT|TO|COMMENT|RENAME|TRUNCATE)\b/gi, '\n$1')
    .replace(/\n+/g, '\n')
    .trim()
}

async function fetchStructure() {
  mainTab.value = 'structure'
  structureRows.value = []
  structureHint.value = ''
  if (!form.data_source_ids.length || !form.db_name) {
    structureHint.value = '请先选择数据源与数据库'
    return
  }
  const rec = pickPrimary()
  if (!rec.database_id) {
    structureHint.value = '未找到已登记库，无法拉取表结构'
    return
  }
  structureLoading.value = true
  try {
    const { data } = await api.get(`/databases/${rec.database_id}/tables`)
    const tables = data.data || []
    structureRows.value = tables.map((name) => ({ name }))
    if (!structureRows.value.length) structureHint.value = data.message || '暂无表结构数据'
  } catch (e) {
    structureHint.value = e.response?.data?.message || '拉取表结构失败'
  } finally {
    structureLoading.value = false
  }
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'ddl',
  label: 'DDL工单',
  path: '/ops/ddl',
  form,
  applyValues: (d) => {
    Object.assign(form, {
      ...emptyForm(),
      ...d,
      data_source_ids: Array.isArray(d.data_source_ids) ? d.data_source_ids : [],
    })
  },
})

async function submit() {
  try {
    await formRef.value?.validate()
  } catch {
    ElMessage.warning('请完善表单必填项（含分库/分表前缀）后再提交')
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
  const extra = {
    env: form.env,
    instance_type: form.instance_type,
    data_source_ids: form.data_source_ids,
    shard_db: form.shard_db,
    shard_db_prefix: String(form.shard_db_prefix || '').trim(),
    shard_table: form.shard_table,
    shard_table_prefix: String(form.shard_table_prefix || '').trim(),
    is_debug: form.is_debug,
  }
  submitting.value = true
  try {
    const { data } = await api.post('/tickets', {
      type: 'ddl',
      db_type: form.instance_type,
      instance_id: rec.instance_id,
      database_id: rec.database_id,
      db_name: rec.db_name,
      sql_content: sql,
      description: form.description,
      assigned_to: form.assigned_to,
      scheduled_at: form.scheduled_at || '',
      need_rollback: JSON.stringify(extra),
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
  restoreDraft()
  await loadMeta()
  const validIds = new Set(dataSourceOptions.value.map((i) => i.id))
  form.data_source_ids = (form.data_source_ids || []).filter((id) => validIds.has(id))
  await loadDatabases()
  await loadApprovers()
})
</script>

<style scoped>
.type-opt { display: inline-flex; align-items: center; gap: 8px; }
.ds-opt { display: flex; justify-content: space-between; gap: 12px; width: 100%; }
.ds-select :deep(.el-select__selection) { flex-wrap: wrap; }
.muted { color: #9ca3af; font-size: 12px; }
.optional { color: #9ca3af; font-weight: 400; }
.batch-row { display: flex; gap: 24px; align-items: center; }
.hint { color: #9ca3af; font-size: 12px; margin: 0 0 12px; }
.hint-alert { margin-bottom: 12px; }
.aside-actions { margin-bottom: 0; margin-top: 8px; }
.aside-actions :deep(.el-form-item__content) { display: flex; flex-wrap: wrap; gap: 8px; }
.submit-wrap { display: inline-block; }
.lv-error { color: #dc2626; }
.lv-warn { color: #f59e0b; }
.lv-ok { color: #16a34a; }
</style>
