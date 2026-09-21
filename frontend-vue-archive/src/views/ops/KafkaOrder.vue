<template>
  <div class="kafka-page">
    <div class="white-card form-narrow kafka-form-card">
      <el-alert
        v-if="form.env && !instLoading && !kafkaOptions.length"
        type="warning"
        show-icon
        :closable="false"
        class="mb"
        title="当前环境暂无可用 Kafka 资源，请先在「实例」中登记 db_type=kafka 的实例"
      />
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="140px"
        label-position="right"
        class="kafka-form"
      >
        <el-form-item label="环境" prop="env">
          <el-select v-model="form.env" filterable clearable placeholder="请选择环境" style="width:100%" @change="onEnvChange">
            <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="Kafka 地址" prop="instance_id">
          <el-select
            v-model="form.instance_id"
            filterable
            clearable
            :disabled="!form.env"
            :loading="instLoading"
            placeholder="选择环境的 kafka 地址"
            style="width:100%"
            @change="onInstChange"
          >
            <el-option v-for="i in kafkaOptions" :key="i.id" :label="instLabel(i)" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="工单类型" prop="action">
          <el-select v-model="form.action" style="width:100%">
            <el-option label="Create Topic" value="create_topic" />
            <el-option label="Delete Topic" value="delete_topic" />
          </el-select>
        </el-form-item>
        <el-form-item label="Topic 名" prop="topic">
          <el-input v-model="form.topic" clearable placeholder="多个 topic 用逗号隔开" />
        </el-form-item>
        <template v-if="form.action === 'create_topic'">
          <el-form-item label="Partition" prop="partition">
            <el-input-number v-model="form.partition" :min="1" :max="100000" :precision="0" controls-position="right" style="width:100%" placeholder="分区" />
          </el-form-item>
          <el-form-item label="Replication Factor" prop="replication">
            <el-input-number v-model="form.replication" :min="1" :max="32" :precision="0" controls-position="right" style="width:100%" placeholder="副本数" />
          </el-form-item>
          <el-form-item label="Cleanup Policy" prop="cleanup_policy">
            <el-select v-model="form.cleanup_policy" style="width:100%">
              <el-option label="Delete" value="delete" />
              <el-option label="Compact" value="compact" />
            </el-select>
          </el-form-item>
          <el-form-item label="Retention" prop="retention">
            <el-input-number v-model="form.retention" :min="1" :precision="0" controls-position="right" style="width:100%" placeholder="毫秒，如 86400000" />
          </el-form-item>
        </template>
        <el-form-item label="审核人">
          <el-select v-model="form.assigned_to" filterable clearable allow-create style="width:100%" :placeholder="approverPlaceholder">
            <el-option v-for="a in approvers" :key="a" :label="a" :value="a" />
          </el-select>
        </el-form-item>
        <el-form-item label="工单说明" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="4" placeholder="请输入业务背景及申请原因" />
        </el-form-item>
        <el-form-item label="定时执行">
          <el-date-picker
            v-model="form.scheduled_at"
            type="datetime"
            format="YYYY-MM-DD HH:mm"
            value-format="YYYY-MM-DD HH:mm"
            style="width:100%"
            placeholder="选择时间"
          />
        </el-form-item>
        <p class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
        <div class="ticket-form-actions">
          <el-button type="primary" :loading="submitting" @click="submit">提交工单</el-button>
          <el-button @click="saveDraft">保存草稿</el-button>
          <el-button @click="reset">重置</el-button>
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
import { useWorkOrderDraft } from '../../composables/useWorkOrderDraft'
const TOPIC_RE = /^[a-zA-Z0-9._-]+$/

const router = useRouter()
const formRef = ref()
const instances = ref([])
const instLoading = ref(false)
const submitting = ref(false)
const form = reactive(emptyForm())

function emptyForm() {
  return {
    env: '',
    instance_id: '',
    action: 'create_topic',
    topic: '',
    partition: undefined,
    replication: undefined,
    cleanup_policy: 'delete',
    retention: 86400000,
    assigned_to: '',
    description: '',
    scheduled_at: '',
  }
}

const rules = {
  env: [{ required: true, message: '请选择环境', trigger: 'change' }],
  instance_id: [{ required: true, message: '请选择 Kafka 地址', trigger: 'change' }],
  action: [{ required: true, message: '请选择工单类型', trigger: 'change' }],
  topic: [
    { required: true, message: '请输入 Topic 名', trigger: 'blur' },
    {
      validator: (_, val, cb) => {
        const parts = String(val || '').split(',').map((s) => s.trim()).filter(Boolean)
        if (!parts.length) return cb(new Error('请输入 Topic 名'))
        for (const t of parts) {
          if (!TOPIC_RE.test(t)) return cb(new Error(`Topic 名称仅支持字母、数字、点、下划线、横线：${t}`))
        }
        cb()
      },
      trigger: 'blur',
    },
  ],
  description: [{ required: true, message: '请填写工单说明', trigger: 'blur' }],
  partition: [{ required: true, message: '请输入分区数', trigger: 'change' }],
  replication: [{ required: true, message: '请输入副本数', trigger: 'change' }],
  cleanup_policy: [{ required: true, message: '请选择 Cleanup Policy', trigger: 'change' }],
  retention: [{ required: true, message: '请输入 Retention（毫秒）', trigger: 'change' }],
}

const envOptions = computed(() => {
  const set = new Set(['test', 'prod'])
  for (const i of instances.value) {
    if (i.idc) set.add(i.idc)
  }
  return [...set]
})

const kafkaOptions = computed(() => {
  if (!form.env) return []
  return instances.value.filter((i) => !i.idc || i.idc === form.env)
})

const currentInst = computed(() => instances.value.find((i) => i.id === form.instance_id))
const approvers = computed(() => currentInst.value?.approvers || [])
const approverPlaceholder = computed(() => {
  if (!form.instance_id) return '请先选择 Kafka 地址'
  if (!approvers.value.length) return '该 Kafka 未配置审批人员，可手动填写或到实例管理维护'
  return '选填，来自实例中配置的审批人员'
})

function instLabel(i) {
  const addr = i.host ? `${i.host}:${i.port}` : (i.domain_name || '')
  return i.name ? `${i.name} — ${addr}` : addr
}

function onEnvChange() {
  form.instance_id = ''
  form.assigned_to = ''
}
function onInstChange() {
  form.assigned_to = ''
  if (approvers.value[0]) form.assigned_to = approvers.value[0]
}

async function loadInst() {
  instLoading.value = true
  try {
    const { data } = await api.get('/kafka/instances')
    instances.value = data.data || []
  } finally {
    instLoading.value = false
  }
}

const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess } = useWorkOrderDraft({
  type: 'kafka',
  label: 'Kafka工单',
  path: '/ops/kafka',
  form,
  applyValues: (d) => {
    Object.assign(form, { ...emptyForm(), ...d })
  },
})

function reset() {
  Object.assign(form, emptyForm())
  formRef.value?.clearValidate()
}

async function submit() {
  await formRef.value?.validate()
  const inst = currentInst.value
  const extra = []
  if (form.action === 'create_topic') {
    extra.push(`partition=${form.partition}`, `rf=${form.replication}`, `cleanup=${form.cleanup_policy}`, `retention=${form.retention}`)
  }
  if (form.scheduled_at) extra.push(`scheduled=${form.scheduled_at}`)
  submitting.value = true
  try {
    await api.post('/kafka/orders', {
      cluster: inst ? instLabel(inst) : form.instance_id,
      topic: form.topic,
      action: form.action,
      assigned_to: form.assigned_to,
      description: extra.length ? `${form.description}\n${extra.join(' ')}` : form.description,
    })
    ElMessage.success('工单创建成功')
    clearOnSubmitSuccess()
    reset()
    router.push('/ops/tickets')
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  await loadInst()
  restoreDraft()
})
</script>

<style scoped>
.kafka-page {
  width: 100%;
  height: 100%;
  max-height: 100%;
  min-height: 0;
  padding: 16px;
  background: #fafafa;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.kafka-title {
  text-align: center;
  font-size: 18px;
  font-weight: 500;
  color: #111827;
  margin: 0 0 32px;
}
.kafka-form-card {
  margin: 0 auto;
  flex: 1 1 0;
  min-height: 0;
  width: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-sizing: border-box;
}
.kafka-form {
  flex: 1 1 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.kafka-form :deep(.el-form-item) { margin-bottom: 12px; flex-shrink: 0; }
.mb { margin-bottom: 16px; }
</style>
