<template>
  <el-dialog
    :model-value="modelValue"
    :title="form.id ? '编辑任务' : '新建任务'"
    width="800px"
    append-to-body
    destroy-on-close
    class="archive-create-dialog"
    @close="emit('update:modelValue', false)"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
      <el-form-item label="任务名" prop="name">
        <el-input v-model="form.name" :disabled="!!form.id" placeholder="请输入任务名" maxlength="128" show-word-limit />
      </el-form-item>

      <el-form-item v-if="!lockStrategy" label="任务策略" prop="strategy">
        <el-radio-group v-model="form.strategy" :disabled="!!form.id">
          <el-radio-button v-for="s in STRATEGY_OPTIONS" :key="s.value" :value="s.value">{{ s.label }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item label="执行方式" prop="run_mode">
        <el-radio-group v-model="form.run_mode" :disabled="!!form.id">
          <el-radio-button v-for="m in RUN_MODE_OPTIONS" :key="m.value" :value="m.value">{{ m.label }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="form.run_mode === 'scheduled'" prop="schedule_expr">
        <template #label>
          <span>调度配置</span>
          <el-tooltip content="支持 cron 表达式，或如：每日 02:00、每小时" placement="top">
            <span class="help-dot">?</span>
          </el-tooltip>
        </template>
        <el-input v-model="form.schedule_expr" :disabled="!!form.id" placeholder="如: 0 2 * * * 或 每日 02:00" />
      </el-form-item>

      <el-form-item v-if="!form.id" label="审核人" prop="approver">
        <el-select v-model="form.approver" filterable allow-create :placeholder="approverPlaceholder" style="width:100%">
          <el-option v-for="u in approvers" :key="u" :label="u" :value="u" />
        </el-select>
      </el-form-item>

      <el-form-item label="环境" prop="env">
        <el-select v-model="form.env" :disabled="!!form.id" clearable placeholder="请选择环境" style="width:100%" @change="onEnvChange">
          <el-option v-for="e in envOptions" :key="e.value" :label="e.label" :value="e.value" />
        </el-select>
      </el-form-item>

      <el-form-item label="数据库类型" prop="db_type">
        <el-select v-model="form.db_type" :disabled="!!form.id" clearable placeholder="选择类型" style="width:100%" @change="onTypeChange">
          <el-option v-for="t in dbTypes" :key="t.value" :label="t.label" :value="t.value" />
        </el-select>
      </el-form-item>

      <!-- 自定义脚本：仅源端域名 + 脚本 -->
      <template v-if="isCustom">
        <el-form-item label="源端域名" prop="source_instance_id">
          <el-select
            v-model="form.source_instance_id"
            filterable
            :disabled="!!form.id || !form.db_type"
            :placeholder="form.db_type ? '请选择源端数据库域名' : '请先选择数据库类型'"
            style="width:100%"
          >
            <el-option v-for="i in sourceInstances" :key="i.id" :label="domainLabel(i)" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="脚本类型" prop="script_type">
          <el-select v-model="form.script_type" :disabled="!!form.id" placeholder="请选择" style="width:100%">
            <el-option v-for="s in SCRIPT_TYPE_OPTIONS" :key="s.value" :label="s.label" :value="s.value" />
          </el-select>
        </el-form-item>
        <div v-if="form.script_type === 'python'" class="script-guide">
          <div class="guide-title">业务脚本只需写：</div>
          <pre class="guide-code">{{ SCRIPT_TEMPLATES.python }}</pre>
        </div>
        <el-alert
          v-if="form.script_type === 'python'"
          type="info"
          show-icon
          :closable="false"
          class="mb"
          title="脚本仅支持 Python 3 及以上，不支持 Python 2。"
        />
        <el-alert
          v-if="form.script_type === 'go'"
          type="info"
          show-icon
          :closable="false"
          class="mb"
          title="请按模版填写区修改后提交。host / dbname / tables 为空时用源端；删除条件为 create_time < today - retainDays。"
        />
        <el-form-item label="脚本内容" prop="script_content">
          <el-input
            v-model="form.script_content"
            type="textarea"
            :rows="form.script_type === 'go' ? 16 : 10"
            :disabled="!!form.id"
            class="mono"
            :placeholder="SCRIPT_TEMPLATES[form.script_type] || '请输入脚本内容'"
          />
        </el-form-item>
      </template>

      <!-- 直接删除 / 备份到目标实例 / 归档到 OSS -->
      <template v-else>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="源端域名" prop="source_instance_id">
              <el-select
                v-model="form.source_instance_id"
                filterable
                :disabled="!!form.id || !form.db_type"
                :placeholder="form.db_type ? '请选择源端数据库域名' : '请先选择数据库类型'"
                style="width:100%"
                @change="onSourceChange"
              >
                <el-option v-for="i in sourceInstances" :key="i.id" :label="domainLabel(i)" :value="i.id" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="源端库名" prop="source_db">
              <el-select
                v-model="form.source_db"
                filterable
                allow-create
                default-first-option
                :disabled="!!form.id || !form.source_instance_id"
                :loading="loadingDbs"
                :placeholder="form.source_instance_id ? '请选择源端库名' : '请先选择源端域名'"
                style="width:100%"
                @change="onDbChange"
              >
                <el-option v-for="d in sourceDbs" :key="d" :label="d" :value="d" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="源端表名" prop="table_name">
          <el-select
            v-if="liveTableList"
            v-model="form.table_name"
            filterable
            allow-create
            default-first-option
            :disabled="!!form.id || !form.source_db"
            :loading="loadingTables"
            :placeholder="form.source_db ? (loadingTables ? '加载源端表名…' : '请选择源端表名') : '请先选择源端库名'"
            style="width:100%"
          >
            <el-option v-for="t in tables" :key="t" :label="t" :value="t" />
          </el-select>
          <el-input v-else v-model="form.table_name" :disabled="!!form.id" placeholder="源端表名" />
        </el-form-item>

        <template v-if="isBackup">
          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="目标域名" prop="target_host">
                <el-input v-model="form.target_host" :disabled="!!form.id" placeholder="目标数据库域名" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="目标库名">
                <el-input v-model="form.target_db" :disabled="!!form.id" placeholder="目标库名" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-form-item label="目标表名">
            <el-input v-model="form.target_table" :disabled="!!form.id" placeholder="目标表名" />
          </el-form-item>
        </template>

        <el-form-item v-if="isOss" label="目标桶" prop="oss_bucket">
          <el-input v-model="form.oss_bucket" :disabled="!!form.id" placeholder="如: archive-bucket 或 bucket/prefix" />
        </el-form-item>

        <el-form-item v-if="isDelete" label="PG 删除模式">
          <template #label>
            <span>PG 删除模式</span>
            <el-tooltip placement="top-start">
              <template #content>
                <div class="tip-block">
                  <div><b>自动：</b>不选手动时由系统按预估数据量选择；不传 pg_delete_mode，由后端自动兜底。</div>
                  <div><b>CTE：</b>适合中小表。按批次循环删除直到删完。</div>
                  <div><b>Shard：</b>适合超大表。按创建时间切分并导出主键后再删。</div>
                </div>
              </template>
              <span class="help-dot">?</span>
            </el-tooltip>
          </template>
          <el-radio-group v-model="form.pg_delete_mode" :disabled="!!form.id">
            <el-radio-button v-for="m in PG_DELETE_MODE_OPTIONS" :key="m.value" :value="m.value">{{ m.label }}</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item prop="sql">
          <template #label>
            <span>{{ conditionLabel }}</span>
            <el-tooltip :content="conditionTip" placement="top">
              <span class="help-dot">?</span>
            </el-tooltip>
          </template>
          <el-input
            v-model="form.sql"
            type="textarea"
            :rows="4"
            :disabled="!!form.id"
            placeholder="如: create_time < '2024-01-01'"
          />
        </el-form-item>
      </template>

      <el-form-item>
        <div class="actions">
          <el-button v-if="!form.id" type="primary" :loading="saving" @click="submit">提交工单</el-button>
          <el-button :disabled="saving" @click="form.id ? emit('update:modelValue', false) : reset()">{{ form.id ? '关闭' : '重置' }}</el-button>
        </div>
      </el-form-item>
    </el-form>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import {
  STRATEGY_OPTIONS,
  RUN_MODE_OPTIONS,
  SCRIPT_TYPE_OPTIONS,
  PG_DELETE_MODE_OPTIONS,
  SCRIPT_TEMPLATES,
  encodeCondition,
  decodeMeta,
  isStockScript,
} from './archive'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  instances: { type: Array, default: () => [] },
  task: { type: Object, default: null },
  initialStrategy: { type: String, default: '' },
  lockStrategy: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref()
const saving = ref(false)
const loadingDbs = ref(false)
const loadingTables = ref(false)
const approvers = ref([])
const envOptions = ref([
  { value: 'test', label: '测试' },
  { value: 'prod', label: '生产' },
])
const dbTypes = ref([
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgresql', label: 'PostgreSQL' },
  { value: 'oracle', label: 'Oracle' },
  { value: 'sqlserver', label: 'SQL Server' },
])
const localInstances = ref([])
const sourceDbs = ref([])
const tables = ref([])
const form = reactive(emptyForm())

const isCustom = computed(() => form.strategy === 'custom_script')
const isDelete = computed(() => form.strategy === 'delete_only')
const isBackup = computed(() => form.strategy === 'backup_target')
const isOss = computed(() => form.strategy === 'archive_oss')
const conditionLabel = computed(() => (isDelete.value ? '删除条件' : '归档条件'))
const conditionTip = computed(() => (
  isDelete.value
    ? '填写 SQL 条件表达式，用于筛选需删除的数据行'
    : '填写 SQL 条件表达式，用于筛选需归档的数据行'
))
const liveTableList = computed(() => {
  const t = typeKey(form.db_type)
  return t === 'mysql' || t === 'postgres'
})
const allInstances = computed(() => (props.instances && props.instances.length ? props.instances : localInstances.value))
const sourceInstances = computed(() => {
  let list = allInstances.value
  if (form.db_type) {
    const want = typeKey(form.db_type)
    list = list.filter((i) => typeKey(i.db_type) === want)
  }
  if (form.env) {
    list = list.filter((i) => !instEnv(i) || instEnv(i) === form.env)
  }
  return list
})
const approverPlaceholder = computed(() => (
  approvers.value.length
    ? '请选择审核人'
    : '暂无审核人，请在全局参数设置中配置 migration_approver_user_ids'
))

const rules = {
  name: [{ required: true, message: '请输入任务名', trigger: 'blur' }],
  strategy: [{ required: true, message: '请选择任务策略', trigger: 'change' }],
  run_mode: [{ required: true, message: '请选择执行方式', trigger: 'change' }],
  schedule_expr: [{
    validator: (_, v, cb) => {
      if (form.run_mode === 'scheduled' && !String(v || '').trim()) cb(new Error('请填写调度配置'))
      else cb()
    },
    trigger: 'blur',
  }],
  approver: [{
    validator: (_, v, cb) => {
      if (form.id) return cb()
      if (!String(v || '').trim()) cb(new Error('请选择审核人'))
      else cb()
    },
    trigger: 'change',
  }],
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  db_type: [{ required: true, message: '请选择数据库类型', trigger: 'change' }],
  source_instance_id: [{ required: true, message: '请选择源端域名', trigger: 'change' }],
  source_db: [{
    validator: (_, v, cb) => {
      if (form.strategy === 'custom_script') return cb()
      if (!String(v || '').trim()) cb(new Error('请选择源端库名'))
      else cb()
    },
    trigger: 'change',
  }],
  table_name: [{
    validator: (_, v, cb) => {
      if (form.strategy === 'custom_script') return cb()
      if (!String(v || '').trim()) cb(new Error(liveTableList.value ? '请选择源端表名' : '请输入源端表名'))
      else cb()
    },
    trigger: 'change',
  }],
  target_host: [{
    validator: (_, v, cb) => {
      if (form.strategy !== 'backup_target') return cb()
      if (!String(v || '').trim()) cb(new Error('请输入目标域名'))
      else cb()
    },
    trigger: 'blur',
  }],
  oss_bucket: [{
    validator: (_, v, cb) => {
      if (form.strategy !== 'archive_oss') return cb()
      if (!String(v || '').trim()) cb(new Error('请输入目标桶'))
      else cb()
    },
    trigger: 'blur',
  }],
  script_type: [{
    validator: (_, v, cb) => {
      if (form.strategy !== 'custom_script') return cb()
      if (!String(v || '').trim()) cb(new Error('请选择脚本类型'))
      else cb()
    },
    trigger: 'change',
  }],
  script_content: [{
    validator: (_, v, cb) => {
      if (form.strategy !== 'custom_script') return cb()
      if (!String(v || '').trim()) cb(new Error('请输入脚本内容'))
      else cb()
    },
    trigger: 'blur',
  }],
  sql: [{
    validator: (_, v, cb) => {
      if (form.strategy === 'custom_script') return cb()
      if (!String(v || '').trim()) cb(new Error(form.strategy === 'delete_only' ? '请输入删除条件' : '请输入归档条件'))
      else cb()
    },
    trigger: 'blur',
  }],
}

function emptyForm() {
  return {
    id: '',
    name: '',
    strategy: 'backup_target',
    run_mode: 'immediate',
    schedule_expr: '',
    approver: '',
    env: '',
    db_type: '',
    source_instance_id: '',
    source_db: '',
    table_name: '',
    target_host: '',
    target_db: '',
    target_table: '',
    oss_bucket: '',
    pg_delete_mode: 'auto',
    script_type: 'python',
    script_content: SCRIPT_TEMPLATES.python,
    sql: '',
  }
}

function typeKey(t) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'mssql') return 'sqlserver'
  return n
}

function instEnv(i) {
  if (!i) return ''
  if (i.env_id) {
    const hit = envOptions.value.find((e) => e.id === i.env_id || e.value === i.env_id)
    if (hit) return hit.value
  }
  return i.idc || i.env || i.env_code || ''
}

function domainLabel(i) {
  const domain = (i.domains && i.domains[0]) || i.domain_name || ''
  const addr = i.host ? `${i.host}:${i.port}` : ''
  const main = domain || addr || i.name
  return i.name && main !== i.name ? `${i.name} — ${main}` : main
}

function parseTypes(raw) {
  try {
    const arr = JSON.parse(String(raw || '').trim() || '[]')
    if (!Array.isArray(arr)) return []
    return arr
      .map((it) => ({ value: String(it?.value || '').trim(), label: String(it?.label || it?.value || '').trim() }))
      .filter((it) => it.value)
  } catch {
    return []
  }
}

function applyTask(task) {
  const meta = task ? decodeMeta(task.condition) : { strategy: props.initialStrategy || 'backup_target', run_mode: 'immediate', sql: '', schedule_expr: '', extra: {} }
  const extra = meta.extra || {}
  if (task) {
    Object.assign(form, {
      id: task.id,
      name: task.name,
      strategy: meta.strategy,
      run_mode: meta.run_mode,
      schedule_expr: task.schedule_expr || meta.schedule_expr || '',
      approver: task.assigned_to || extra.approver || '',
      env: extra.env || '',
      db_type: extra.db_type || '',
      source_instance_id: task.source_instance_id,
      source_db: task.source_db,
      table_name: task.table_name === 'script' ? '' : task.table_name,
      target_host: extra.target_host || '',
      target_db: extra.target_db || '',
      target_table: extra.target_table || '',
      oss_bucket: extra.oss_bucket || '',
      pg_delete_mode: extra.pg_delete_mode || 'auto',
      script_type: extra.script_type || 'python',
      script_content: meta.strategy === 'custom_script' ? meta.sql : SCRIPT_TEMPLATES.python,
      sql: meta.strategy === 'custom_script' ? '' : meta.sql,
    })
    return
  }
  Object.assign(form, emptyForm(), {
    strategy: props.initialStrategy || 'backup_target',
    approver: approvers.value[0] || '',
    script_content: SCRIPT_TEMPLATES.python,
  })
}

function reset() {
  applyTask(null)
  sourceDbs.value = []
  tables.value = []
  formRef.value?.clearValidate()
}

function onEnvChange() {
  if (form.id) return
  const still = sourceInstances.value.some((i) => i.id === form.source_instance_id)
  if (!still) {
    form.source_instance_id = ''
    form.source_db = ''
    form.table_name = ''
    sourceDbs.value = []
    tables.value = []
  }
}

function onTypeChange() {
  if (form.id) return
  const still = sourceInstances.value.some((i) => i.id === form.source_instance_id)
  if (!still) {
    form.source_instance_id = ''
    form.source_db = ''
    form.table_name = ''
    sourceDbs.value = []
    tables.value = []
  }
}

async function onSourceChange() {
  if (form.id) return
  form.source_db = ''
  form.table_name = ''
  tables.value = []
  await loadDbs()
  await loadApprovers()
}

async function onDbChange() {
  if (form.id) return
  form.table_name = ''
  await loadTables()
  await loadApprovers()
}

async function loadDbs() {
  if (!form.source_instance_id || isCustom.value) {
    sourceDbs.value = []
    return
  }
  loadingDbs.value = true
  try {
    const [live, reg] = await Promise.all([
      api.get(`/instances/${form.source_instance_id}/databases`).catch(() => ({ data: { data: [] } })),
      api.get('/databases', { params: { instance_id: form.source_instance_id } }).catch(() => ({ data: { data: [] } })),
    ])
    const names = (live.data.data || []).filter((d) => !d.system).map((d) => d.name || d.db_name).filter(Boolean)
    const registered = (reg.data.data || []).map((d) => d.db_name).filter(Boolean)
    const set = [...new Set([...names, ...registered])]
    if (form.source_db && !set.includes(form.source_db)) set.unshift(form.source_db)
    sourceDbs.value = set
  } catch {
    sourceDbs.value = form.source_db ? [form.source_db] : []
  } finally {
    loadingDbs.value = false
  }
}

async function loadTables() {
  if (!form.source_instance_id || !form.source_db || isCustom.value || !liveTableList.value) {
    tables.value = []
    return
  }
  loadingTables.value = true
  try {
    const { data } = await api.get(`/migration/instances/${form.source_instance_id}/databases/${encodeURIComponent(form.source_db)}/tables`)
    const list = (data.data || []).map((t) => (typeof t === 'string' ? t : (t.table_name || t.name))).filter(Boolean)
    if (form.table_name && !list.includes(form.table_name)) list.unshift(form.table_name)
    tables.value = list
  } catch {
    tables.value = form.table_name ? [form.table_name] : []
  } finally {
    loadingTables.value = false
  }
}

async function loadApprovers() {
  const set = new Set()
  try {
    const argRes = await api.get('/global-args')
    const args = argRes.data.data || []
    String(args.find((a) => a.key === 'migration_approver_user_ids')?.value || '')
      .split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean).forEach((u) => set.add(u))
    const def = String(args.find((a) => a.key === 'sync_default_approver')?.value || '').trim()
    if (def) set.add(def)
    const raw = args.find((a) => a.key === 'ticket_instance_types')?.value || ''
    const parsed = parseTypes(raw)
    if (parsed.length) dbTypes.value = parsed
  } catch { /* ignore */ }
  if (form.source_instance_id) {
    try {
      const { data } = await api.get('/tickets/target-approvers', { params: { instance_id: form.source_instance_id, db_name: form.source_db } })
      for (const u of data.data || []) if (u) set.add(u)
    } catch { /* ignore */ }
  }
  approvers.value = [...set]
  if (!form.id && !form.approver && approvers.value.length) form.approver = approvers.value[0]
}

async function loadMeta() {
  const jobs = [
    api.get('/environments').catch(() => ({ data: { data: [] } })),
    api.get('/global-args').catch(() => ({ data: { data: [] } })),
  ]
  if (!props.instances?.length) jobs.push(api.get('/instances').catch(() => ({ data: { data: [] } })))
  const [envRes, argRes, instRes] = await Promise.all(jobs)
  const envs = envRes.data.data || []
  const mapped = envs.map((e) => ({
    id: e.id,
    value: e.code || e.name || e.id,
    label: e.name && e.code && e.name !== e.code ? `${e.name}` : (e.name || e.code),
  })).filter((e) => e.value)
  if (mapped.length) envOptions.value = mapped
  const args = argRes.data.data || []
  const parsed = parseTypes(args.find((a) => a.key === 'ticket_instance_types')?.value || '')
  if (parsed.length) dbTypes.value = parsed
  if (instRes) localInstances.value = instRes.data.data || []
  await loadApprovers()
}

watch(() => form.strategy, (next, prev) => {
  if (!props.modelValue || form.id || next === prev) return
  formRef.value?.clearValidate()
  if (next === 'custom_script' && isStockScript(form.script_content)) {
    form.script_content = SCRIPT_TEMPLATES[form.script_type] || SCRIPT_TEMPLATES.python
  }
})

watch(() => form.script_type, (next, prev) => {
  if (!props.modelValue || form.id || !isCustom.value || next === prev) return
  if (isStockScript(form.script_content)) {
    form.script_content = SCRIPT_TEMPLATES[next] || ''
  }
})

watch(() => props.modelValue, async (open) => {
  if (!open) return
  applyTask(props.task)
  await loadMeta()
  if (!form.id && !form.approver && approvers.value.length) form.approver = approvers.value[0]
  if (form.source_instance_id && !isCustom.value) {
    await loadDbs()
    await loadTables()
  }
})

async function submit() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const extra = {
      env: form.env,
      db_type: form.db_type,
      target_host: isBackup.value ? form.target_host : '',
      target_db: isBackup.value ? form.target_db : '',
      target_table: isBackup.value ? form.target_table : '',
      oss_bucket: isOss.value ? form.oss_bucket : '',
      pg_delete_mode: isDelete.value && (form.pg_delete_mode === 'cte' || form.pg_delete_mode === 'shard') ? form.pg_delete_mode : '',
      script_type: isCustom.value ? form.script_type : '',
    }
    const body = isCustom.value ? form.script_content : form.sql
    await api.post('/archive/tasks', {
      name: form.name,
      source_instance_id: form.source_instance_id,
      source_db: isCustom.value ? '' : form.source_db,
      table_name: isCustom.value ? (form.script_type || 'script') : form.table_name,
      schedule_expr: form.run_mode === 'scheduled' ? form.schedule_expr : '',
      approver: form.approver,
      condition: encodeCondition(body, form.strategy, form.run_mode, form.schedule_expr, extra),
    })
    ElMessage.success('工单已提交，审批通过后按任务执行方式运行')
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.mb { margin-bottom: 16px; }
.actions { display: flex; gap: 12px; }
.help-dot {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  margin-left: 6px;
  border-radius: 50%;
  background: #d1d5db;
  color: #fff;
  font-size: 10px;
  cursor: help;
  vertical-align: middle;
}
.script-guide {
  margin-bottom: 16px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  border-radius: 8px;
  padding: 10px 12px;
  color: #334155;
  font-size: 13px;
}
.guide-title { font-weight: 600; color: #0f172a; }
.guide-code {
  margin: 8px 0 0;
  overflow: auto;
  border-radius: 6px;
  background: #fff;
  padding: 8px 12px;
  font-size: 12px;
  line-height: 1.6;
  color: #1e293b;
}
.mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
.tip-block { max-width: 360px; line-height: 1.6; }
.tip-block > div + div { margin-top: 6px; }
</style>

<style>
.archive-create-dialog .el-dialog__body {
  max-height: 70vh;
  overflow-y: auto;
  padding-top: 8px;
}
</style>
