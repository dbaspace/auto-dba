<template>
  <div class="gray-page archive-scroll">
    <div class="page-head">
      <h1>系统设置</h1>
      <p>全局配置、邮件告警、Webhook、归档存储（写入全局参数 archive_settings）</p>
    </div>

    <el-card shadow="never" class="panel">
      <ul class="set-list">
        <li v-for="item in rows" :key="item.key" @click="edit(item.key)">
          <span>{{ item.label }}</span>
          <span class="status" :class="item.ok ? 'ok' : 'pending'">{{ item.statusText }}</span>
        </li>
      </ul>
    </el-card>

    <el-dialog
      v-model="visible"
      :title="title"
      :width="key === 'mail' ? '560px' : '520px'"
      append-to-body
      destroy-on-close
    >
      <el-form v-if="key === 'mail'" :model="form" label-width="120px">
        <el-form-item label="邮件协议"><el-input model-value="smtp" disabled /></el-form-item>
        <el-form-item label="邮件类型"><el-input model-value="html" disabled /></el-form-item>
        <el-form-item label="SMTP主机" required><el-input v-model="form.host" placeholder="请输入 SMTP 主机" /></el-form-item>
        <el-form-item label="SMTP端口" required><el-input-number v-model="form.port" :min="1" :max="65535" style="width:100%" /></el-form-item>
        <el-form-item label="SMTP账号" required><el-input v-model="form.account" placeholder="请输入 SMTP 账号" /></el-form-item>
        <el-form-item label="SMTP密码" required>
          <el-input v-model="form.password" type="password" show-password placeholder="请输入 SMTP 密码" />
        </el-form-item>
        <el-form-item label="SMTP超时时间" required>
          <el-input-number v-model="form.timeout" :min="1" style="width:100%" />
        </el-form-item>
        <el-form-item label="邮件发件人" required><el-input v-model="form.sender" placeholder="请输入邮件发件人" /></el-form-item>
      </el-form>
      <el-form v-else-if="key === 'webhook'" :model="form" label-position="top">
        <el-form-item label="Webhook URL">
          <el-input v-model="form.url" placeholder="https://；留空则为待配置" />
        </el-form-item>
        <el-form-item label="Secret">
          <el-input v-model="form.secret" type="password" show-password placeholder="可选；已设置时显示掩码" />
        </el-form-item>
      </el-form>
      <el-form v-else :model="form" label-position="top">
        <el-form-item label="可用容量（TB）" required>
          <el-input-number v-model="form.availableTb" :min="0" :step="0.1" style="width:100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button v-if="key === 'mail' || key === 'webhook'" :loading="testing" @click="test">测试</el-button>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

const MASK = '••••••••'
const settings = ref(defaultSettings())
const visible = ref(false)
const testing = ref(false)
const key = ref('mail')
const form = reactive({
  host: '', port: 25, account: '', password: '', timeout: 10, sender: '',
  url: '', secret: '', availableTb: 10,
})
const titles = { mail: '邮件告警服务器', webhook: 'Webhook 集成', storage: '归档存储池' }
const title = computed(() => titles[key.value])
const rows = computed(() => {
  const mailOk = !!(settings.value.mail.host && settings.value.mail.account && settings.value.mail.sender && settings.value.mail.password)
  const hookOk = !!settings.value.webhook.url
  const n = settings.value.storage.availableTb
  const storageOk = n != null && Number.isFinite(n)
  return [
    { key: 'mail', label: '邮件告警服务器', ok: mailOk, statusText: mailOk ? '已配置' : '待配置' },
    { key: 'webhook', label: 'Webhook 集成', ok: hookOk, statusText: hookOk ? '已配置' : '待配置' },
    { key: 'storage', label: '归档存储池', ok: storageOk, statusText: storageOk ? `可用 ${n} TB` : '待配置' },
  ]
})

function defaultSettings() {
  return {
    mail: { host: '', port: 25, account: '', password: '', timeout: 10, sender: '' },
    webhook: { url: '', secret: '' },
    storage: { availableTb: undefined },
  }
}
function mergeSettings(raw) {
  const base = defaultSettings()
  return {
    mail: { ...base.mail, ...(raw?.mail || {}) },
    webhook: { ...base.webhook, ...(raw?.webhook || {}) },
    storage: { ...base.storage, ...(raw?.storage || {}) },
  }
}
async function loadRemote() {
  try {
    const { data } = await api.get('/archive/settings')
    settings.value = mergeSettings(data.data || {})
  } catch { /* keep default */ }
}
async function persist(next) {
  settings.value = next
  await api.patch('/archive/settings', next)
  visible.value = false
  ElMessage.success('已保存')
}
function edit(k) {
  key.value = k
  if (k === 'mail') {
    Object.assign(form, { ...settings.value.mail, password: settings.value.mail.password ? MASK : '' })
  } else if (k === 'webhook') {
    Object.assign(form, { url: settings.value.webhook.url || '', secret: settings.value.webhook.secret ? MASK : '' })
  } else {
    form.availableTb = settings.value.storage.availableTb
  }
  visible.value = true
}
function resolveSecret(next, prev) {
  const v = String(next || '').trim()
  if (!v) return ''
  if (v === MASK) return prev || ''
  return v
}
function save() {
  if (key.value === 'mail') {
    persist({
      ...settings.value,
      mail: {
        host: form.host.trim(),
        port: form.port,
        account: form.account.trim(),
        password: resolveSecret(form.password, settings.value.mail.password),
        timeout: form.timeout,
        sender: form.sender.trim(),
      },
    })
    return
  }
  if (key.value === 'webhook') {
    persist({
      ...settings.value,
      webhook: {
        url: form.url.trim(),
        secret: resolveSecret(form.secret, settings.value.webhook.secret),
      },
    })
    return
  }
  persist({ ...settings.value, storage: { availableTb: form.availableTb } })
}
async function test() {
  if (key.value === 'mail') {
    if (!form.host || !form.account || !resolveSecret(form.password, settings.value.mail.password)) {
      ElMessage.error('请完整填写 SMTP 主机、账号和密码')
      return
    }
    const { value } = await ElMessageBox.prompt('请输入测试收件人', '发送测试邮件', {
      inputPlaceholder: '请输入测试收件人',
      confirmButtonText: '发送测试',
    })
    if (!String(value || '').trim()) {
      ElMessage.error('请输入测试收件人')
      return
    }
    testing.value = true
    try {
      const { data } = await api.post('/archive/settings/mail/test', { to: value.trim() })
      ElMessage.success(data.message || `已向 ${value.trim()} 发起测试`)
    } finally {
      testing.value = false
    }
    return
  }
  if (!form.url.trim()) {
    ElMessage.error('请输入 Webhook URL')
    return
  }
  testing.value = true
  try {
    const { data } = await api.post('/archive/settings/webhook/test', { url: form.url.trim() })
    ElMessage.success(data.message || 'Webhook 测试已提交')
  } finally {
    testing.value = false
  }
}
onMounted(loadRemote)
</script>

<style scoped>
.archive-scroll { overflow: auto; }
.page-head { margin-bottom: 16px; }
.page-head h1 { margin: 0; font-size: 20px; font-weight: 600; color: #111827; }
.page-head p { margin: 6px 0 0; color: #6b7280; font-size: 13px; }
.panel { border-radius: 8px; max-width: 896px; }
.set-list { list-style: none; margin: 0; padding: 0; }
.set-list li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 8px;
  cursor: pointer;
  border-bottom: 1px solid #f3f4f6;
  color: #1f2937;
}
.set-list li:last-child { border-bottom: 0; }
.set-list li:hover { background: #fafafa; }
.status {
  font-size: 12px;
  font-weight: 500;
  border-radius: 4px;
  padding: 2px 10px;
}
.status.ok { background: #f0fdf4; color: #15803d; }
.status.pending { background: #fffbeb; color: #b45309; }
</style>
