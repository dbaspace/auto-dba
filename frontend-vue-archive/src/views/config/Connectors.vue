<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click.stop="open()">增加</el-button>
    </div>
    <p class="hint">维护 Debezium Connect 集群。环境来自「环境管理」，Kafka 地址来自已登记的 Kafka 实例。</p>
    <div class="ops-table">
      <el-table :data="pagedRows" border height="100%" v-loading="loading">
        <el-table-column prop="dbz_host" label="dbz_host" min-width="240" show-overflow-tooltip />
        <el-table-column label="env" width="180">
          <template #default="{ row }">{{ envLabel(row.env) }}</template>
        </el-table-column>
        <el-table-column label="消息格式" width="120">
          <template #default="{ row }">
            <el-tag :type="row.message_schema_mode === 'with_schema' ? 'primary' : 'info'" size="small">
              {{ schemaLabel(row.message_schema_mode) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="kafka_host" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">{{ kafkaLabel(row.kafka_host) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="open(row)">编辑</el-button>
            <el-button text type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="pager">
      <span class="pager-total">共 {{ rows.length }} 条</span>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="rows.length"
        :page-sizes="[10, 20, 50]"
        layout="sizes, prev, pager, next, jumper"
      />
    </div>

    <el-dialog
      v-model="visible"
      :title="form.id ? '编辑连接器' : '增加连接器'"
      width="560px"
      append-to-body
      destroy-on-close
      align-center
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item label="dbz_host" prop="dbz_host">
          <el-input v-model="form.dbz_host" placeholder="如 10.100.112.17:8084 或逗号分隔多地址" />
        </el-form-item>
        <el-form-item label="env" prop="env">
          <el-select v-model="form.env" filterable clearable placeholder="请选择环境" style="width:100%">
            <el-option v-for="e in envOptions" :key="e.value" :label="e.label" :value="e.value" />
          </el-select>
          <div class="form-extra">数据来自「环境管理」；保存为环境编码（如 test / prod），非 id</div>
        </el-form-item>
        <el-form-item label="kafka_host" prop="kafka_host">
          <el-select v-model="form.kafka_host" filterable clearable placeholder="请选择 Kafka 资源（address）" style="width:100%">
            <el-option v-for="k in kafkaOptions" :key="k.value" :label="k.label" :value="k.value" />
          </el-select>
          <div class="form-extra">数据来自已登记的 Kafka 实例（db_type=kafka）</div>
        </el-form-item>
        <el-form-item label="Kafka 消息格式" prop="message_schema_mode">
          <el-radio-group v-model="form.message_schema_mode">
            <el-radio value="without_schema">不带 Schema</el-radio>
            <el-radio value="with_schema">带 Schema</el-radio>
          </el-radio-group>
          <div class="form-extra">with_schema=带字段描述；without_schema=纯 KV JSON（CDC 默认）</div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'

const rows = ref([])
const envs = ref([])
const kafkas = ref([])
const loading = ref(false)
const visible = ref(false)
const saving = ref(false)
const page = ref(1)
const pageSize = ref(10)
const formRef = ref()
const form = reactive({
  id: '',
  dbz_host: '',
  env: '',
  kafka_host: '',
  message_schema_mode: 'without_schema',
  remark: '',
})
const rules = {
  dbz_host: [{ required: true, message: '请输入 dbz_host', trigger: 'blur' }],
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  kafka_host: [{ required: true, message: '请选择 Kafka 资源地址', trigger: 'change' }],
  message_schema_mode: [{ required: true, message: '请选择 Kafka 消息格式', trigger: 'change' }],
}

const pagedRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return rows.value.slice(start, start + pageSize.value)
})

const envOptions = computed(() => {
  const base = envs.value.map((e) => ({
    value: e.code,
    label: e.name ? `${e.name} (${e.code})` : e.code,
  }))
  if (form.env && !base.some((o) => o.value === form.env)) {
    base.push({ value: form.env, label: `未在接口列表中: ${form.env}` })
  }
  return base
})

const kafkaOptions = computed(() => {
  const base = kafkas.value.map((k) => {
    const addr = kafkaAddress(k)
    return { value: addr, label: k.name ? `${k.name} — ${addr}` : addr }
  }).filter((o) => o.value)
  if (form.kafka_host && !base.some((o) => o.value === form.kafka_host)) {
    base.push({ value: form.kafka_host, label: `未在资源列表: ${form.kafka_host}` })
  }
  return base
})

function kafkaAddress(i) {
  const host = String(i?.host || i?.domain_name || '').trim()
  const port = Number(i?.port)
  if (!host) return ''
  const cleaned = host.replace(/:(\d+):\1$/, ':$1')
  if (/:\d+$/.test(cleaned) || cleaned.includes(',')) return cleaned
  if (Number.isFinite(port) && port > 0) return `${cleaned}:${port}`
  return cleaned
}

function envLabel(code) {
  const hit = envs.value.find((e) => e.code === code || e.id === code || e.name === code)
  if (hit?.name) return hit.code ? `${hit.name} (${hit.code})` : hit.name
  return code || '—'
}

function schemaLabel(v) {
  return v === 'with_schema' ? '带 Schema' : '不带 Schema'
}

function kafkaLabel(addr) {
  const hit = kafkas.value.find((k) => kafkaAddress(k) === addr || k.id === addr || k.name === addr)
  if (hit?.name) return `${hit.name} — ${kafkaAddress(hit) || addr}`
  return addr || '—'
}

async function load() {
  loading.value = true
  try {
    const [connRes, envRes, kafkaRes] = await Promise.all([
      api.get('/connectors'),
      api.get('/environments'),
      api.get('/instances', { params: { db_type: 'kafka' } }),
    ])
    rows.value = connRes.data.data || []
    envs.value = envRes.data.data || []
    kafkas.value = kafkaRes.data.data || []
  } finally {
    loading.value = false
  }
}

function open(row) {
  form.id = row?.id || ''
  form.dbz_host = row?.dbz_host || ''
  form.env = row?.env || ''
  form.kafka_host = row?.kafka_host || ''
  form.message_schema_mode = row?.message_schema_mode === 'with_schema' ? 'with_schema' : 'without_schema'
  form.remark = row?.remark || ''
  showDialog(visible)
  nextTick(() => formRef.value?.clearValidate())
}

async function save() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = {
      dbz_host: form.dbz_host.trim(),
      env: form.env,
      kafka_host: form.kafka_host,
      message_schema_mode: form.message_schema_mode,
      remark: form.remark.trim(),
    }
    if (form.id) await api.put('/connectors/' + form.id, payload)
    else await api.post('/connectors', payload)
    ElMessage.success(form.id ? '编辑成功' : '增加成功')
    visible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm('确定删除该条连接器？', '提示', { type: 'warning' })
  await api.delete('/connectors/' + row.id)
  ElMessage.success('删除成功')
  await load()
}

onMounted(load)
</script>

<style scoped>
.pager {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding-top: 12px;
  flex-shrink: 0;
}
.pager-total { color: #6b7280; font-size: 13px; }
.form-extra { color: #9ca3af; font-size: 12px; line-height: 1.4; margin-top: 4px; }
</style>
