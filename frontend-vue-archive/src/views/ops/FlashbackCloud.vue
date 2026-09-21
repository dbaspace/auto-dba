<template>
  <div class="gray-page fb-cloud">
    <div class="ops-title">
      <h1>数据闪回 · 多云密钥</h1>
      <el-button @click="$router.push('/ops/flashback/dashboard')">返回仪表盘</el-button>
    </div>
    <p class="lead">
      密钥按云厂商各存一套，写入全局参数后立即生效。下载 WAL 时按「配置 → 实例」上的云厂商匹配对应 AK/SK；
      云实例 ID / Region 也填在实例上。密钥类脱敏展示，留空保存表示保留原值。
    </p>

    <el-alert type="info" show-icon :closable="false" class="mb">
      <template #title>与实例的关联方式</template>
      在「配置 → 实例」选择云厂商并填写云实例 ID、Region。闪回预检/执行时用该厂商的密钥从云平台拉 WAL。
      腾讯云 PostgreSQL 已支持云日志下载；阿里云 / 华为云 / AWS 可先配密钥，下载能力按厂商逐步接入。
    </el-alert>

    <div class="checks" v-loading="loading">
      <div v-for="v in vendors" :key="v.vendor" class="check">
        <span class="dot" :class="v.configured ? 'ok' : 'bad'" />
        <div>
          <strong>{{ v.name }}</strong>
          <div class="mono">{{ v.id_key }}</div>
          <div class="meta">{{ v.configured ? '已配置密钥' : '未配置密钥' }}</div>
        </div>
      </div>
    </div>

    <el-card shadow="never" class="panel">
      <template #header>
        <div class="card-head">
          <span>云厂商密钥与下载参数</span>
          <el-button text type="primary" :loading="loading" @click="load">刷新</el-button>
        </div>
      </template>
      <el-form label-position="top" class="grid">
        <el-form-item v-for="a in args" :key="a.key" :label="argLabel(a)">
          <el-input
            v-model="a.value"
            :type="a.secret ? 'password' : 'text'"
            :show-password="a.secret"
            :placeholder="a.secret ? '库中已保存则脱敏，留空保留' : a.description"
            autocomplete="new-password"
          />
        </el-form-item>
      </el-form>
      <div class="actions">
        <el-button type="primary" :loading="saving" @click="save">保存到数据库</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'

const loading = ref(false)
const saving = ref(false)
const vendors = ref([])
const args = ref([])

function sourceLabel(src) {
  return { db: '已存数据库', env: '环境变量', yaml: 'YAML' }[src] || '未配置'
}

function argLabel(a) {
  const src = a.source ? ` · ${sourceLabel(a.source)}` : ''
  return `${a.description || a.key}${src}`
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/flashback/cloud-settings')
    const row = data.data || {}
    vendors.value = row.vendors || []
    args.value = (row.args || []).map((a) => ({ ...a, value: a.value || '' }))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await api.put('/flashback/cloud-settings', {
      args: args.value.map((a) => ({ key: a.key, value: a.value, description: a.description })),
    })
    ElMessage.success('已保存')
    await load()
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.fb-cloud { max-width: 960px; }
.lead { color: #606266; font-size: 13px; line-height: 1.6; margin: 0 0 12px; }
.mb { margin-bottom: 16px; }
.checks { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; margin-bottom: 16px; }
.check { display: flex; gap: 10px; align-items: flex-start; padding: 12px; border: 1px solid #f0f0f0; border-radius: 8px; background: #fff; }
.dot { width: 10px; height: 10px; border-radius: 50%; margin-top: 6px; flex-shrink: 0; }
.dot.ok { background: #52c41a; }
.dot.bad { background: #ff4d4f; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; color: #909399; margin-top: 2px; word-break: break-all; }
.meta { font-size: 12px; color: #8c8c8c; margin-top: 4px; }
.panel { margin-bottom: 16px; }
.card-head { display: flex; justify-content: space-between; align-items: center; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 16px; }
.actions { margin-top: 8px; }
@media (max-width: 800px) {
  .grid { grid-template-columns: 1fr; }
}
</style>
