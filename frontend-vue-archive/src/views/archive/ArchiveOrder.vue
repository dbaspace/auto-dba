<template>
  <div class="gray-page archive-scroll">
    <div class="form-wrap">
      <el-card shadow="never" class="panel">
        <template #header>
          <span class="card-title">数据库归档任务</span>
        </template>
        <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
          <el-form-item label="数据库类型" prop="db_type">
            <el-select v-model="form.db_type" filterable placeholder="选择类型" style="width:100%" @change="onTypeChange">
              <el-option v-for="t in dbTypes" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
          </el-form-item>

          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="源端域名" prop="source_instance_id">
                <el-select
                  v-model="form.source_instance_id"
                  filterable
                  :disabled="!form.db_type"
                  :placeholder="form.db_type ? '源端数据库域名' : '请先选择类型'"
                  style="width:100%"
                >
                  <el-option v-for="i in sourceInstances" :key="i.id" :label="domainLabel(i)" :value="i.id" />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="源端库名" prop="source_db">
                <el-input v-model="form.source_db" placeholder="源端库名" />
              </el-form-item>
            </el-col>
          </el-row>

          <el-form-item label="源端表名" prop="source_table">
            <el-input v-model="form.source_table" placeholder="源端表名" />
          </el-form-item>

          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="目标域名">
                <el-select v-model="form.target_instance_id" filterable clearable placeholder="目标数据库域名" style="width:100%">
                  <el-option v-for="i in instances" :key="i.id" :label="domainLabel(i)" :value="i.id" />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="目标库名">
                <el-input v-model="form.target_db" placeholder="目标库名" />
              </el-form-item>
            </el-col>
          </el-row>

          <el-form-item label="目标表名">
            <el-input v-model="form.target_table" placeholder="目标表名" />
          </el-form-item>

          <el-form-item label="库表类型" prop="table_type">
            <el-select v-model="form.table_type" placeholder="请选择" style="width:100%">
              <el-option label="全量归档" value="full" />
              <el-option label="分区表归档" value="partition" />
              <el-option label="逻辑归档" value="logical" />
            </el-select>
          </el-form-item>

          <el-form-item label="执行方式" prop="run_mode">
            <el-radio-group v-model="form.run_mode">
              <el-radio value="immediate">立即执行</el-radio>
              <el-radio value="scheduled">定时任务</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="form.run_mode === 'scheduled'" label="调度配置" prop="schedule_expr">
            <el-input v-model="form.schedule_expr" placeholder="如: 0 2 * * * 或 每日 02:00" />
          </el-form-item>
          <el-form-item label="审核人" prop="approver">
            <el-select v-model="form.approver" filterable allow-create placeholder="请选择审核人" style="width:100%">
              <el-option v-for="u in approvers" :key="u" :label="u" :value="u" />
            </el-select>
          </el-form-item>
          <el-form-item label="归档条件">
            <el-input
              v-model="form.archive_condition"
              type="textarea"
              :rows="4"
              placeholder="如: create_time < '2024-01-01'"
            />
            <div class="hint">可选，用于指定归档数据的 WHERE 条件，如时间范围</div>
          </el-form-item>

          <p class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
          <el-form-item>
            <div class="actions">
              <el-button type="primary" :loading="saving" @click="submit">提交工单</el-button>
              <el-button @click="reset">重置</el-button>
            </div>
          </el-form-item>
        </el-form>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { encodeCondition } from './archive'

const router = useRouter()
const formRef = ref()
const saving = ref(false)
const instances = ref([])
const approvers = ref([])
const dbTypes = ref([
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'oracle', label: 'Oracle' },
  { value: 'sqlserver', label: 'SQL Server' },
])
const form = reactive(emptyForm())
const rules = {
  db_type: [{ required: true, message: '请选择数据库类型', trigger: 'change' }],
  source_instance_id: [{ required: true, message: '请输入源端数据库域名', trigger: 'change' }],
  source_table: [{ required: true, message: '请输入源端表名', trigger: 'blur' }],
  table_type: [{ required: true, message: '请选择库表类型', trigger: 'change' }],
  run_mode: [{ required: true, message: '请选择执行方式', trigger: 'change' }],
  schedule_expr: [{
    validator: (_, v, cb) => {
      if (form.run_mode === 'scheduled' && !String(v || '').trim()) cb(new Error('请填写调度配置'))
      else cb()
    },
    trigger: 'blur',
  }],
  approver: [{ required: true, message: '请选择审核人', trigger: 'change' }],
}

const sourceInstances = computed(() => {
  if (!form.db_type) return instances.value
  const want = typeKey(form.db_type)
  return instances.value.filter((i) => typeKey(i.db_type) === want)
})

function emptyForm() {
  return {
    db_type: '',
    source_instance_id: '',
    source_db: '',
    source_table: '',
    target_instance_id: '',
    target_db: '',
    target_table: '',
    table_type: '',
    archive_condition: '',
    run_mode: 'immediate',
    schedule_expr: '',
    approver: '',
  }
}

function typeKey(t) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'mssql') return 'sqlserver'
  return n
}

function domainLabel(i) {
  const domain = (i.domains && i.domains[0]) || i.domain_name || ''
  const addr = i.host ? `${i.host}:${i.port}` : ''
  const main = domain || addr || i.name
  return i.name && main !== i.name ? `${i.name} — ${main}` : main
}

function onTypeChange() {
  const still = sourceInstances.value.some((i) => i.id === form.source_instance_id)
  if (!still) form.source_instance_id = ''
}

function reset() {
  Object.assign(form, emptyForm())
  formRef.value?.clearValidate()
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

async function submit() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const src = instances.value.find((i) => i.id === form.source_instance_id)
    const tgt = instances.value.find((i) => i.id === form.target_instance_id)
    const name = [form.source_db, form.source_table].filter(Boolean).join('.') || form.source_table
    const extra = [
      `-- table_type:${form.table_type}`,
      `-- target:${tgt ? domainLabel(tgt) : ''}/${form.target_db || ''}/${form.target_table || ''}`,
      form.archive_condition.trim(),
    ].filter(Boolean).join('\n')
    await api.post('/archive/tasks', {
      name: `${name}_归档`,
      source_instance_id: form.source_instance_id,
      source_db: form.source_db,
      table_name: form.source_table,
      schedule_expr: form.run_mode === 'scheduled' ? form.schedule_expr : '',
      approver: form.approver,
      condition: encodeCondition(extra, form.table_type === 'full' ? 'backup_target' : 'delete_only', form.run_mode, form.schedule_expr),
    })
    ElMessage.success('工单已提交，审批通过后按任务执行方式运行')
    reset()
    router.push('/ops/tickets?type=archive')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  const [instRes, argRes] = await Promise.all([
    api.get('/instances'),
    api.get('/global-args'),
  ])
  instances.value = instRes.data.data || []
  const args = argRes.data.data || []
  const raw = args.find((a) => a.key === 'ticket_instance_types')?.value || ''
  const parsed = parseTypes(raw)
  if (parsed.length) dbTypes.value = parsed
  const set = new Set()
  String(args.find((a) => a.key === 'migration_approver_user_ids')?.value || '').split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean).forEach((u) => set.add(u))
  const def = String(args.find((a) => a.key === 'sync_default_approver')?.value || '').trim()
  if (def) set.add(def)
  approvers.value = [...set]
  if (!form.approver && approvers.value.length) form.approver = approvers.value[0]
})
</script>

<style scoped>
.archive-scroll { overflow: auto; }
.form-wrap { max-width: 672px; }
.panel { border-radius: 8px; }
.card-title { font-weight: 500; color: #111827; }
.hint { margin-top: 6px; color: #6b7280; font-size: 12px; }
.actions { display: flex; gap: 12px; }
</style>
