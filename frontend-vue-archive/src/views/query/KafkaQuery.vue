<template>
  <div class="ops-page kafka-query">
    <div class="layout">
      <aside class="side">
        <div class="side-head">Kafka 实例</div>
        <el-input v-model="keyword" clearable placeholder="搜索实例" class="mb" />
        <el-select v-model="envFilter" clearable placeholder="全部环境" class="mb" style="width:100%">
          <el-option v-for="e in envOptions" :key="e" :label="e" :value="e" />
        </el-select>
        <div v-loading="instLoading" class="inst-list">
          <div
            v-for="i in filteredInstances"
            :key="i.id"
            class="inst-item"
            :class="{ active: selectedId === i.id }"
            @click="selectInstance(i)"
          >
            <span class="name">{{ i.name || instAddr(i) }}</span>
            <span class="env">{{ instEnv(i) }}</span>
          </div>
          <p v-if="!instLoading && !filteredInstances.length" class="empty">暂无 Kafka 实例，请先在「配置 → 实例」登记 db_type=kafka</p>
        </div>
      </aside>
      <section class="main">
        <div v-if="!selectedId" class="placeholder">请从左侧选择 Kafka 实例查看 Topic 列表</div>
        <template v-else>
          <div class="toolbar">
            <strong>{{ selected?.name || instAddr(selected) }}</strong>
            <span class="muted">{{ instAddr(selected) }}</span>
            <el-button :loading="topicsLoading" @click="loadTopics">刷新 Topic</el-button>
            <el-input v-model="topicSearch" clearable placeholder="搜索 Topic" style="width:200px" />
            <el-checkbox v-model="hideInternal">隐藏内部 Topic（__ 前缀）</el-checkbox>
          </div>
          <el-table
            :data="pagedTopics"
            size="small"
            v-loading="topicsLoading"
            height="100%"
            empty-text="该实例暂无 Topic"
          >
            <el-table-column prop="name" label="Name" min-width="220" show-overflow-tooltip />
            <el-table-column label="Count" width="90">
              <template #default="{ row }">{{ formatCount(row.message_count) }}</template>
            </el-table-column>
            <el-table-column label="Size" width="90">
              <template #default="{ row }">{{ formatSize(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column label="Last Record" width="160">
              <template #default="{ row }">{{ formatLastRecord(row.last_record) }}</template>
            </el-table-column>
            <el-table-column prop="partition_count" label="Partitions" width="110" />
            <el-table-column prop="replication_factor" label="Replications" width="120" />
            <el-table-column label="操作" width="160" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openTopic(row.name, 'data')">查看</el-button>
                <el-button link @click="openTopic(row.name, 'configs')">配置</el-button>
                <el-button link type="danger" @click="confirmDelete(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div class="pager">
            <el-pagination
              v-model:current-page="page"
              v-model:page-size="pageSize"
              :total="filteredTopics.length"
              :page-sizes="[10, 25, 50, 100]"
              layout="total, sizes, prev, pager, next"
              small
            />
          </div>
        </template>
      </section>
    </div>
    <KafkaTopicQueryModal
      :visible="modalOpen"
      :instance-id="selectedId"
      :topic-name="modalTopic"
      :initial-tab="modalTab"
      @close="modalOpen = false"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import KafkaTopicQueryModal from './KafkaTopicQueryModal.vue'

const instLoading = ref(false)
const topicsLoading = ref(false)
const instances = ref([])
const envs = ref([])
const keyword = ref('')
const envFilter = ref('')
const selectedId = ref('')
const topics = ref([])
const topicSearch = ref('')
const hideInternal = ref(false)
const page = ref(1)
const pageSize = ref(25)
const modalOpen = ref(false)
const modalTopic = ref('')
const modalTab = ref('data')

const selected = computed(() => instances.value.find((i) => i.id === selectedId.value))

const envOptions = computed(() => {
  const set = new Set()
  for (const e of envs.value) {
    if (e.code) set.add(e.code)
    if (e.name) set.add(e.name)
  }
  for (const i of instances.value) {
    if (i.idc) set.add(i.idc)
  }
  return [...set]
})

function instEnv(i) {
  return i?.idc || i?.env_id || ''
}

function instAddr(i) {
  if (!i) return ''
  const host = i.host || i.domain_name || ''
  return host ? `${host}:${i.port || 9092}` : ''
}

const filteredInstances = computed(() => {
  let list = instances.value
  if (envFilter.value) {
    list = list.filter((i) => instEnv(i) === envFilter.value)
  }
  const q = keyword.value.trim().toLowerCase()
  if (q) {
    list = list.filter((i) => (i.name || '').toLowerCase().includes(q) || instAddr(i).toLowerCase().includes(q))
  }
  return list
})

const filteredTopics = computed(() => {
  let list = topics.value
  if (hideInternal.value) list = list.filter((t) => !(t.name || '').startsWith('__'))
  const q = topicSearch.value.trim().toLowerCase()
  if (q) list = list.filter((t) => (t.name || '').toLowerCase().includes(q))
  return list
})

const pagedTopics = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredTopics.value.slice(start, start + pageSize.value)
})

function formatCount(n) {
  if (n == null || Number(n) === 0) return '-'
  return Number(n)
}

function formatSize(bytes) {
  if (bytes == null || Number(bytes) === 0) return '-'
  const b = Number(bytes)
  if (b >= 1073741824) return `${(b / 1073741824).toFixed(1)} GB`
  if (b >= 1048576) return `${(b / 1048576).toFixed(1)} MB`
  if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${b} B`
}

function formatLastRecord(iso) {
  if (!iso) return '-'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
}

function selectInstance(inst) {
  selectedId.value = inst.id
  page.value = 1
  loadTopics()
}

function openTopic(name, tab) {
  modalTopic.value = name
  modalTab.value = tab
  modalOpen.value = true
}

async function confirmDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除 Topic「${row.name}」？此操作不可恢复。`, '删除 Topic', { type: 'warning' })
  } catch {
    return
  }
  await api.delete(`/instances/${selectedId.value}/kafka/topics/${encodeURIComponent(row.name)}`)
  ElMessage.success('已删除')
  loadTopics()
}

async function loadTopics() {
  if (!selectedId.value) return
  topicsLoading.value = true
  try {
    const { data } = await api.get(`/instances/${selectedId.value}/kafka/topics`)
    topics.value = data.data || []
  } catch {
    topics.value = []
  } finally {
    topicsLoading.value = false
  }
}

async function loadMeta() {
  instLoading.value = true
  try {
    const [instRes, envRes] = await Promise.all([
      api.get('/instances', { params: { db_type: 'kafka' } }),
      api.get('/environments'),
    ])
    instances.value = instRes.data.data || []
    envs.value = envRes.data.data || []
  } finally {
    instLoading.value = false
  }
}

watch([topicSearch, hideInternal, pageSize], () => { page.value = 1 })
onMounted(loadMeta)
</script>

<style scoped>
.kafka-query { height: 100%; }
.layout { display: flex; flex: 1; min-height: 0; background: #fff; border: 1px solid #e5e7eb; border-radius: 8px; overflow: hidden; }
.side { width: 260px; flex-shrink: 0; border-right: 1px solid #e5e7eb; display: flex; flex-direction: column; padding: 12px; }
.side-head { font-weight: 600; margin-bottom: 10px; }
.mb { margin-bottom: 8px; }
.inst-list { flex: 1; overflow: auto; }
.inst-item { display: flex; justify-content: space-between; gap: 8px; padding: 8px 10px; border-radius: 6px; cursor: pointer; font-size: 13px; }
.inst-item:hover { background: #f0f7ff; }
.inst-item.active { background: #e6f4ff; color: #1677ff; }
.inst-item .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.inst-item .env { color: #9ca3af; flex-shrink: 0; }
.main { flex: 1; min-width: 0; display: flex; flex-direction: column; padding: 12px; }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: 10px; }
.muted { color: #9ca3af; font-size: 13px; }
.placeholder, .empty { color: #9ca3af; font-size: 13px; padding: 24px 8px; }
.placeholder { flex: 1; display: flex; align-items: center; justify-content: center; }
.pager { margin-top: 10px; display: flex; justify-content: flex-end; }
</style>
