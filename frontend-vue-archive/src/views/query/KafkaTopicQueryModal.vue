<template>
  <el-dialog
    :model-value="visible"
    :title="`Topic: ${topicName}`"
    width="1100px"
    top="6vh"
    append-to-body
    destroy-on-close
    @close="emit('close')"
  >
    <el-tabs v-model="activeTab">
      <el-tab-pane label="Data" name="data" />
      <el-tab-pane label="Partitions" name="partitions" />
      <el-tab-pane label="Consumer Groups" name="consumerGroups" />
      <el-tab-pane label="Configs" name="configs" />
      <el-tab-pane label="Logs" name="logs" />
    </el-tabs>

    <template v-if="activeTab === 'data'">
      <div class="toolbar">
        <span class="lab">Sort</span>
        <el-select v-model="sortOrder" style="width:110px">
          <el-option label="Oldest" value="oldest" />
          <el-option label="Newest" value="newest" />
        </el-select>
        <span class="lab">Partition</span>
        <el-select v-model="partition" style="width:110px">
          <el-option label="All" value="" />
          <el-option v-for="p in partitionOptions" :key="p" :label="String(p)" :value="String(p)" />
        </el-select>
        <span class="lab">Offset</span>
        <el-input v-model="offsetInput" style="width:90px" placeholder="-1" />
        <span class="lab">Time</span>
        <el-select v-model="timeFormat" style="width:120px">
          <el-option label="RELATIVE" value="RELATIVE" />
          <el-option label="ABSOLUTE" value="ABSOLUTE" />
        </el-select>
        <el-button @click="showSearch = !showSearch">Search</el-button>
        <el-button type="primary" :loading="loading" @click="loadMessages">查询</el-button>
        <el-button @click="downloadResult">下载 JSON</el-button>
      </div>

      <div v-if="showSearch" class="search-panel">
        <div v-for="row in searchFields" :key="row.key" class="search-row">
          <span class="lab">{{ row.label }}</span>
          <el-input v-model="row.value" style="flex:1" />
          <el-select v-model="row.operator" style="width:140px">
            <el-option label="Contains" value="Contains" />
            <el-option label="Equals" value="Equals" />
            <el-option label="Starts With" value="Starts With" />
            <el-option label="Ends With" value="Ends With" />
            <el-option label="Not Contains" value="Not Contains" />
          </el-select>
        </div>
        <div class="search-actions">
          <el-button type="primary" @click="showSearch = false">应用过滤</el-button>
          <el-button @click="clearSearch">清空</el-button>
        </div>
      </div>

      <el-table :data="sortedMessages" size="small" height="380" v-loading="loading" empty-text="No data available">
        <el-table-column type="selection" width="42" />
        <el-table-column prop="key" label="Key" min-width="140" show-overflow-tooltip />
        <el-table-column label="Value" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ row.value || '-' }}</template>
        </el-table-column>
        <el-table-column label="Timestamp" width="160">
          <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
        </el-table-column>
        <el-table-column prop="partition" label="Partition" width="90" />
        <el-table-column prop="offset" label="Offset" width="100" />
        <el-table-column label="Headers" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ formatHeaders(row.headers) }}</template>
        </el-table-column>
      </el-table>
    </template>
    <div v-else class="placeholder">{{ tabLabel }} 功能开发中</div>
  </el-dialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'

const props = defineProps({
  visible: { type: Boolean, default: false },
  instanceId: { type: String, default: '' },
  topicName: { type: String, default: '' },
  initialTab: { type: String, default: 'data' },
})
const emit = defineEmits(['close'])

const activeTab = ref('data')
const sortOrder = ref('oldest')
const partition = ref('')
const partitionOptions = ref([])
const offsetInput = ref('-1')
const timeFormat = ref('RELATIVE')
const showSearch = ref(false)
const loading = ref(false)
const messages = ref([])
const searchFields = ref([
  { key: 'key', label: 'Key', value: '', operator: 'Contains' },
  { key: 'value', label: 'Value', value: '', operator: 'Contains' },
  { key: 'headerKey', label: 'Header Key', value: '', operator: 'Contains' },
  { key: 'headerValue', label: 'Header Value', value: '', operator: 'Contains' },
])

const tabLabel = computed(() => {
  const map = { partitions: 'Partitions', consumerGroups: 'Consumer Groups', configs: 'Configs', logs: 'Logs' }
  return map[activeTab.value] || activeTab.value
})

const sortedMessages = computed(() => applySearchFilter(messages.value))

function applySearchFilter(list) {
  return list.filter((m) => searchFields.value.every((row) => {
    const v = (row.value || '').trim().toLowerCase()
    if (!v) return true
    let text = ''
    if (row.key === 'key') text = (m.key || '').toLowerCase()
    else if (row.key === 'value') text = (m.value || '').toLowerCase()
    else if (row.key === 'headerKey') text = Object.keys(m.headers || {}).join(' ').toLowerCase()
    else if (row.key === 'headerValue') text = Object.values(m.headers || {}).join(' ').toLowerCase()
    else return true
    if (row.operator === 'Equals') return text === v
    if (row.operator === 'Starts With') return text.startsWith(v)
    if (row.operator === 'Ends With') return text.endsWith(v)
    if (row.operator === 'Not Contains') return !text.includes(v)
    return text.includes(v)
  }))
}

function topicPath() {
  return encodeURIComponent(props.topicName)
}

async function loadPartitions() {
  if (!props.instanceId || !props.topicName) return
  try {
    const { data } = await api.get(`/instances/${props.instanceId}/kafka/topics/${topicPath()}/partitions`)
    partitionOptions.value = data.data || []
  } catch {
    partitionOptions.value = []
  }
}

async function loadMessages() {
  if (!props.instanceId || !props.topicName) return
  loading.value = true
  try {
    const params = { limit: 100, sort: sortOrder.value }
    if (offsetInput.value !== '' && offsetInput.value !== '-1') params.offset = offsetInput.value
    if (partition.value !== '') params.partition = partition.value
    const { data } = await api.get(`/instances/${props.instanceId}/kafka/topics/${topicPath()}/messages`, { params })
    messages.value = data.data || []
  } catch {
    messages.value = []
  } finally {
    loading.value = false
  }
}

function clearSearch() {
  searchFields.value.forEach((r) => { r.value = ''; r.operator = 'Contains' })
}

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '-'
  if (timeFormat.value === 'RELATIVE') {
    const diff = Date.now() - d.getTime()
    if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
    return `${Math.floor(diff / 86400000)}d ago`
  }
  return d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
}

function formatHeaders(h) {
  if (!h || !Object.keys(h).length) return '-'
  return Object.entries(h).map(([k, v]) => `${k}: ${v}`).join('; ')
}

function downloadResult() {
  const list = sortedMessages.value
  if (!list.length) {
    ElMessage.info('没有可下载的数据')
    return
  }
  const blob = new Blob([JSON.stringify(list, null, 2)], { type: 'application/json' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `kafka-${props.topicName}-${Date.now()}.json`
  a.click()
  URL.revokeObjectURL(a.href)
}

watch(() => props.visible, (v) => {
  if (v && props.instanceId && props.topicName) {
    activeTab.value = props.initialTab || 'data'
    loadPartitions()
    if ((props.initialTab || 'data') === 'data') loadMessages()
  }
})

watch([sortOrder, partition], () => {
  if (props.visible && props.instanceId && props.topicName && activeTab.value === 'data') loadMessages()
})
</script>

<style scoped>
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 12px; }
.lab { font-size: 13px; color: #6b7280; }
.search-panel { background: #f5f7fa; border: 1px solid #e5e7eb; border-radius: 6px; padding: 12px; margin-bottom: 12px; }
.search-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.search-actions { display: flex; gap: 8px; }
.placeholder { padding: 48px 0; text-align: center; color: #9ca3af; }
</style>
