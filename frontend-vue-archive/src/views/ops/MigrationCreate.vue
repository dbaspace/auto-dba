<template>
  <div class="mig-create">
    <div class="ops-title">
      <h1>新建迁移任务</h1>
      <el-button @click="$router.push('/ops/migration')">返回列表</el-button>
    </div>
    <el-alert v-if="form.env === 'prod'" type="warning" show-icon :closable="false" class="mb" title="当前为生产环境，请务必确认配置与审批要求。" />
    <el-steps :active="step" finish-status="success" align-center class="mb">
      <el-step title="基本信息" />
      <el-step title="选择端点" />
      <el-step title="迁移策略" />
      <el-step title="选择对象" />
      <el-step title="预检提交" />
    </el-steps>

    <MigrationObjectSelect
      v-show="step === 3"
      :instance-id="form.source_instance_id"
      v-model:objects="form.objects"
      v-model:whole-dbs="form.whole_dbs"
    />

    <el-form v-show="step !== 3" :model="form" label-position="top" class="white-card">
      <template v-if="step === 0">
        <el-form-item label="任务名称" required><el-input v-model="form.name" maxlength="80" show-word-limit /></el-form-item>
        <el-form-item label="说明"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="数据库类型" required>
          <el-select v-model="form.db_type" style="width:100%" @change="onDbTypeChange">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgres" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境" required>
          <el-select v-model="form.env" style="width:100%">
            <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="启动方式">
          <el-radio-group v-model="form.start_mode">
            <el-radio value="immediate">立即启动</el-radio>
            <el-radio value="scheduled">定时启动</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="form.start_mode === 'scheduled'" label="定时时间" required>
          <el-date-picker v-model="form.scheduled_at" type="datetime" value-format="YYYY-MM-DD HH:mm:ss" style="width:100%" />
        </el-form-item>
        <el-form-item label="审核人" required>
          <el-select v-model="form.assigned_approver" filterable allow-create placeholder="审批通过后自动执行" style="width:100%">
            <el-option v-for="u in approvers" :key="u" :label="u" :value="u" />
          </el-select>
        </el-form-item>
      </template>

      <template v-else-if="step === 1">
        <el-form-item label="源实例" required>
          <el-select v-model="form.source_instance_id" filterable style="width:100%" @change="srcConn = null">
            <el-option v-for="i in sourceInst" :key="i.id" :label="instLabel(i)" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button :disabled="!form.source_instance_id" :loading="srcTesting" @click="testConn('source')">测试源端连通</el-button>
          <span v-if="srcConn" :class="srcConn.connected ? 'ok' : 'bad'">{{ srcConn.connected ? `连接成功 ${srcConn.username || ''}` : srcConn.error_message }}</span>
        </el-form-item>
        <el-form-item label="目标实例" required>
          <el-select v-model="form.target_instance_id" filterable style="width:100%" @change="tgtConn = null">
            <el-option v-for="i in targetInst" :key="i.id" :label="instLabel(i)" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button :disabled="!form.target_instance_id" :loading="tgtTesting" @click="testConn('target')">测试目标端连通</el-button>
          <span v-if="tgtConn" :class="tgtConn.connected ? 'ok' : 'bad'">{{ tgtConn.connected ? `连接成功 ${tgtConn.username || ''}` : tgtConn.error_message }}</span>
        </el-form-item>
      </template>

      <template v-else-if="step === 2">
        <el-form-item label="迁移模式">
          <el-radio-group v-model="form.migration_mode">
            <el-radio value="full_data">全量迁移</el-radio>
            <el-radio value="schema_only">仅结构迁移</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="结构同步策略">
          <el-radio-group v-model="form.schema_strategy">
            <el-radio value="init">结构初始化</el-radio>
            <el-radio value="no_init">不结构初始化</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="冲突处理策略">
          <el-select v-model="form.conflict_strategy" style="width:100%">
            <el-option label="报错中止" value="abort" />
            <el-option label="跳过已存在" value="skip" />
            <el-option label="先清空再写入" value="truncate" />
            <el-option label="先删除再创建" value="drop_create" />
          </el-select>
        </el-form-item>
        <el-alert v-if="form.conflict_strategy === 'drop_create'" type="warning" show-icon :closable="false" class="mb" title="将删除目标端已存在对象后再创建，请谨慎使用。" />
        <el-form-item v-if="form.migration_mode === 'full_data'" label="数据中转方式">
          <el-radio-group v-model="form.transfer_mode">
            <el-radio value="direct">直连流式传输</el-radio>
            <el-radio value="oss">OSS 中转</el-radio>
          </el-radio-group>
          <el-alert
            v-if="form.transfer_mode === 'oss' && !settings.oss_configured"
            type="warning"
            show-icon
            :closable="false"
            class="mt"
            title="请先在迁移设置中配置 OSS 连接信息"
          />
        </el-form-item>
        <el-form-item label="批次大小">
            <el-input-number v-model="form.batch_size" :min="100" :max="100000" :step="100" />
        </el-form-item>
      </template>

      <template v-else-if="step === 4">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="任务名称">{{ form.name }}</el-descriptions-item>
          <el-descriptions-item label="环境">{{ form.env }}</el-descriptions-item>
          <el-descriptions-item label="类型">{{ form.db_type }}</el-descriptions-item>
          <el-descriptions-item label="启动">{{ form.start_mode === 'scheduled' ? `定时 ${form.scheduled_at}` : '审批通过后立即执行' }}</el-descriptions-item>
          <el-descriptions-item label="审核人">{{ form.assigned_approver || '—' }}</el-descriptions-item>
          <el-descriptions-item label="源库">{{ selectedSourceDbs || form.source_db || '—' }}</el-descriptions-item>
          <el-descriptions-item label="目标库">{{ selectedTargetDbs || form.target_db || '—' }}</el-descriptions-item>
          <el-descriptions-item label="迁移模式">{{ form.migration_mode === 'schema_only' ? '仅结构迁移' : '全量迁移' }}</el-descriptions-item>
          <el-descriptions-item label="结构策略">{{ form.schema_strategy === 'no_init' ? '不初始化' : '结构初始化' }}</el-descriptions-item>
          <el-descriptions-item label="冲突策略">{{ conflictLabel(form.conflict_strategy) }}</el-descriptions-item>
          <el-descriptions-item label="表数量">{{ form.objects.length || form.tables.length }}</el-descriptions-item>
        </el-descriptions>
        <div class="pre-actions">
          <el-button :loading="prechecking" @click="doPrecheck">预检</el-button>
        </div>
        <el-table v-if="precheckItems.length" :data="precheckItems" border size="small" class="mb">
          <el-table-column prop="check_item" label="检查项" width="180" />
          <el-table-column label="级别" width="90">
            <template #default="{ row }">{{ row.check_level }}</template>
          </el-table-column>
          <el-table-column label="结果" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="row.check_status === 'passed' ? 'success' : 'danger'">{{ row.check_status }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="message" label="说明" />
        </el-table>
      </template>
    </el-form>

    <div class="mig-nav">
      <el-button @click="saveDraft">保存草稿</el-button>
      <el-button v-if="step > 0" @click="step--">上一步</el-button>
      <el-button v-if="step < 4" type="primary" :disabled="step === 1 && !endpointsOk" @click="next">下一步</el-button>
      <el-button v-else type="primary" :loading="submitting" :disabled="!form.assigned_approver" @click="submit">提交审批</el-button>
    </div>
    <p v-if="step === 4" class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'
import MigrationObjectSelect from './MigrationObjectSelect.vue'
const router = useRouter()
const step = ref(0)
const submitting = ref(false)
const prechecking = ref(false)
const instances = ref([])
const envOptions = ref(['test', 'prod'])
const precheckItems = ref([])
const srcConn = ref(null)
const tgtConn = ref(null)
const srcTesting = ref(false)
const tgtTesting = ref(false)
const settings = reactive({ approval_enabled: true, approver_user_ids: '', oss_configured: false })
const extraApprovers = ref([])
const form = reactive({
  name: '', description: '', db_type: 'postgres', env: 'test', start_mode: 'immediate', scheduled_at: '',
  assigned_approver: '',
  source_instance_id: '', target_instance_id: '',
  migration_mode: 'full_data', schema_strategy: 'init', conflict_strategy: 'abort',
  transfer_mode: 'direct', batch_size: 1000, speed_limit: 0,
  source_db: '', target_db: '', tables: [], objects: [], whole_dbs: [],
})

const approvers = computed(() => {
  const set = new Set(String(settings.approver_user_ids || '').split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean))
  for (const u of extraApprovers.value) if (u) set.add(u)
  const src = instances.value.find((i) => i.id === form.source_instance_id)
  for (const u of src?.approvers || []) if (u) set.add(u)
  return [...set]
})
const endpointsOk = computed(() => Boolean(srcConn.value?.connected && tgtConn.value?.connected))
const selectedSourceDbs = computed(() => [...new Set((form.objects || []).map((o) => o.source_db_name))].join('、'))
const selectedTargetDbs = computed(() => [...new Set((form.objects || []).map((o) => o.target_db_name))].join('、'))
const sourceInst = computed(() => instances.value.filter((i) => matchType(i.db_type, form.db_type)))
const targetInst = computed(() => sourceInst.value)

function matchType(a, b) {
  const n = (t) => {
    t = String(t || '').toLowerCase()
    if (['postgresql', 'pg', 'pgsql'].includes(t)) return 'postgres'
    if (['tidb', 'oceanbase'].includes(t)) return 'mysql'
    return t
  }
  return n(a) === n(b)
}
function instLabel(i) {
  const addr = i.host ? `${i.host}:${i.port}` : ''
  return i.name ? `${i.name} — ${addr}` : addr
}
function conflictLabel(v) {
  return { abort: '报错中止', skip: '跳过已存在', truncate: '先清空再写入', drop_create: '先删除再创建' }[v] || v
}
function onDbTypeChange() {
  form.source_instance_id = ''
  form.target_instance_id = ''
  srcConn.value = null
  tgtConn.value = null
}

function next() {
  if (step.value === 0) {
    if (!form.name || !form.db_type || !form.env) {
      ElMessage.warning('请完善基本信息')
      return
    }
    if (form.start_mode === 'scheduled' && !form.scheduled_at) {
      ElMessage.warning('请填写定时时间')
      return
    }
    if (!form.assigned_approver) {
      ElMessage.warning('请选择审核人')
      return
    }
  }
  if (step.value === 1 && (!form.source_instance_id || !form.target_instance_id || !endpointsOk.value)) {
    ElMessage.warning('请选择源/目标实例并完成连通测试')
    return
  }
  if (step.value === 3) {
    if (!form.objects?.length) {
      ElMessage.warning('请至少选择一张表，或勾选整个库（将包含该库下未排除的表）')
      return
    }
    const err = validateObjects(form.objects)
    if (err) {
      ElMessage.warning(err)
      return
    }
  }
  step.value += 1
}

function validateObjects(list) {
  const seen = new Set()
  for (const o of list || []) {
    const td = String(o.target_db_name || '').trim()
    const tt = String(o.target_table_name || '').trim()
    if (!td || !tt) return '每条迁移对象的目标库、目标表均不能为空'
    if (td.length > 128 || tt.length > 128) return '目标库名或目标表名过长（单字段不超过 128 字符）'
    const k = `${td}\0${tt}`
    if (seen.has(k)) return '存在重复的目标库.目标表组合，请调整映射'
    seen.add(k)
  }
  return ''
}

async function testConn(side) {
  const id = side === 'source' ? form.source_instance_id : form.target_instance_id
  if (side === 'source') srcTesting.value = true
  else tgtTesting.value = true
  try {
    const { data } = await api.get(`/migration/instances/${id}/connect`)
    if (side === 'source') srcConn.value = data
    else tgtConn.value = data
  } finally {
    if (side === 'source') srcTesting.value = false
    else tgtTesting.value = false
  }
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'migration',
  label: '数据迁移',
  path: '/ops/migration/create',
  form,
  extraWatch: [step],
  getValues: () => ({ form: { ...form }, step: step.value }),
  applyValues: (v) => {
    Object.assign(form, v.form || v)
    if (Number.isFinite(v.step)) step.value = v.step
  },
})

function payload() {
  return {
    name: form.name,
    description: form.description,
    db_type: form.db_type,
    env: form.env,
    start_mode: form.start_mode,
    scheduled_at: form.scheduled_at,
    assigned_approver: form.assigned_approver,
    reviewer: form.assigned_approver,
    approver: form.assigned_approver,
    assigned_to: form.assigned_approver,
    source_instance_id: form.source_instance_id,
    target_instance_id: form.target_instance_id,
    source_db: form.objects[0]?.source_db_name || form.source_db,
    target_db: form.objects[0]?.target_db_name || form.target_db,
    tables: form.objects.map((o) => o.source_table_name),
    objects: form.objects,
    migration_mode: form.migration_mode,
    schema_strategy: form.schema_strategy,
    conflict_strategy: form.conflict_strategy,
    transfer_mode: form.transfer_mode,
    batch_size: form.batch_size,
    speed_limit: form.speed_limit,
  }
}

async function doPrecheck() {
  prechecking.value = true
  try {
    const { data } = await api.post('/migration/tasks/precheck', payload(), { timeout: 60000 })
    precheckItems.value = data.items || []
    ElMessage[data.ok ? 'success' : 'warning'](data.ok ? '预检通过' : '预检未通过')
  } finally {
    prechecking.value = false
  }
}

async function submit() {
  if (!form.objects?.length) {
    ElMessage.warning('请至少选择一张表')
    return
  }
  const objErr = validateObjects(form.objects)
  if (objErr) {
    ElMessage.warning(objErr)
    return
  }
  if (!form.assigned_approver) {
    ElMessage.warning('请选择审核人')
    return
  }
  submitting.value = true
  try {
    const { data } = await api.post('/migration/tasks', payload(), { timeout: 60000 })
    clearOnSubmitSuccess()
    if (data.ok === false) {
      ElMessage.warning(data.message || '已创建但预检未通过')
      const id = data.data?.id
      router.push(id ? `/ops/migration/${id}` : '/ops/migration')
      return
    }
    ElMessage.success('已提交审批，通过后自动执行')
    const row = data.data || {}
    if (row.issue_id) router.push('/ops/tickets/' + row.issue_id)
    else router.push(row.id ? `/ops/migration/${row.id}` : '/ops/migration')
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  const [instRes, envRes, setRes, argRes] = await Promise.all([
    api.get('/instances'),
    api.get('/environments').catch(() => ({ data: { data: [] } })),
    api.get('/migration/settings').catch(() => ({ data: { data: {} } })),
    api.get('/global-args').catch(() => ({ data: { data: [] } })),
  ])
  instances.value = instRes.data.data || []
  const envs = (envRes.data.data || []).map((e) => e.name || e.code || e.id).filter(Boolean)
  if (envs.length) envOptions.value = envs
  Object.assign(settings, setRes.data.data || {})
  const p = settings.performance || {}
  if (p.batch_size > 0 && form.batch_size === 1000) form.batch_size = p.batch_size
  if (Number.isFinite(p.default_speed_limit) && form.speed_limit === 0) form.speed_limit = p.default_speed_limit
  const args = argRes.data.data || []
  const extras = []
  String(args.find((a) => a.key === 'sync_default_approver')?.value || '').split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean).forEach((u) => extras.push(u))
  extraApprovers.value = extras
  if (!form.assigned_approver && approvers.value[0]) form.assigned_approver = approvers.value[0]
  restoreDraft()
})
</script>

<style scoped>
.mig-create { max-width: 1400px; margin: 0 auto; padding-bottom: 32px; }
.mig-create .ops-title { justify-content: space-between; }
.mb { margin-bottom: 16px; }
.mig-nav { margin-top: 20px; display: flex; gap: 8px; flex-wrap: wrap; }
.ok { margin-left: 12px; color: #16a34a; }
.bad { margin-left: 12px; color: #dc2626; }
.pre-actions { margin: 12px 0; }
.hint { color: #6b7280; font-size: 12px; }
.mt { margin-top: 8px; }
</style>
