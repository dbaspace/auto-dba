<template>
  <div class="dbz-tab">
    <div class="dbz-summary">
      <div class="dbz-summary-title">
        <strong>{{ detail.name || extra.connector_name || '—' }}</strong>
        <el-tag v-if="detail.state" size="small" :type="stateType(detail.state)">{{ detail.state }}</el-tag>
        <el-tag v-if="detail.debezium_mode || extra.debezium_mode" size="small" type="primary" effect="plain">
          模式：{{ detail.debezium_mode || extra.debezium_mode }}
        </el-tag>
      </div>
      <div class="dbz-summary-meta">
        <span>配置项：{{ rows.length }}</span>
        <span>模板：{{ detail.template || extra.debezium_template || '—' }}</span>
      </div>
    </div>

    <div class="dbz-toolbar">
      <el-input v-model="keyword" clearable size="small" placeholder="搜索 key 或 value" class="dbz-search" />
      <div class="dbz-actions">
        <el-button size="small" :disabled="editing" @click="load">刷新</el-button>
        <el-button size="small" @click="copyJSON">复制</el-button>
        <el-button size="small" @click="jsonOpen = true">JSON视图</el-button>
        <el-button v-if="!editing" size="small" type="primary" :disabled="pending" @click="startEdit">保存</el-button>
        <template v-else>
          <el-button size="small" @click="cancelEdit">取消</el-button>
          <el-button size="small" type="primary" :loading="saving" @click="save">确认保存</el-button>
        </template>
        <el-button size="small" @click="historyOpen = true">配置版本历史</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="filteredRows" size="small" empty-text="暂无配置" max-height="520">
      <el-table-column label="Key" min-width="240">
        <template #default="{ row }">
          <el-input v-if="editing && row.isNew" v-model="row.key" size="small" placeholder="配置项" />
          <code v-else>{{ row.key }}</code>
        </template>
      </el-table-column>
      <el-table-column label="Value" min-width="320">
        <template #default="{ row }">
          <el-input
            v-if="editing"
            v-model="row.value"
            size="small"
            :type="isSensitive(row.key) ? 'password' : (isLong(row) ? 'textarea' : 'text')"
            :show-password="isSensitive(row.key)"
            :autosize="isLong(row) ? { minRows: 2, maxRows: 6 } : undefined"
          />
          <span v-else class="dbz-val">
            {{ isSensitive(row.key) && !revealed[row.key] ? '••••••••' : row.value }}
            <el-button v-if="isSensitive(row.key)" link type="primary" @click="revealed[row.key] = !revealed[row.key]">
              {{ revealed[row.key] ? '隐藏' : '显示' }}
            </el-button>
          </span>
        </template>
      </el-table-column>
      <el-table-column v-if="editing" label="操作" width="80">
        <template #default="{ row }">
          <el-button link type="danger" @click="removeRow(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <div v-if="editing" class="dbz-add">
      <el-button size="small" @click="addRow">新增配置项</el-button>
    </div>

    <el-drawer v-model="historyOpen" title="配置版本历史" size="960px" destroy-on-close @open="loadVersions">
      <el-button class="mb" :disabled="versions.length < 2" @click="beginRestore(versions[1])">恢复上一版</el-button>
      <el-table v-loading="verLoading" :data="versions" size="small" empty-text="暂无历史版本">
        <el-table-column label="版本" width="80">
          <template #default="{ row }"><el-tag size="small" type="primary">v{{ row.version || row.version_no }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="created_at" label="保存时间" width="180">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="operator" label="操作人" width="120" />
        <el-table-column label="操作类型" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="operationTypeTag(row.change_source || row.operation_id)">
              {{ formatOperationType(row.change_source || row.operation_id) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="operator_ip" label="操作来源" width="130">
          <template #default="{ row }">{{ row.operator_ip || '—' }}</template>
        </el-table-column>
        <el-table-column label="来源" width="120">
          <template #default="{ row }"><el-tag size="small">{{ row.change_source || row.operation_id || '—' }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="config_key_count" label="配置项" width="80" />
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="showDetail(row)">查看</el-button>
            <el-button link type="primary" @click="showDiff(row)">对比</el-button>
            <el-button link type="danger" @click="beginRestore(row)">恢复</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">共 {{ verTotal }} 条
        <el-pagination
          v-model:current-page="verPage"
          v-model:page-size="verSize"
          :total="verTotal"
          :page-sizes="[10, 20, 50]"
          layout="prev, pager, next, sizes"
          small
          @current-change="loadVersions"
          @size-change="loadVersions"
        />
      </div>
    </el-drawer>

    <el-dialog v-model="jsonOpen" title="JSON 视图" width="720px">
      <pre class="cfg-pre">{{ prettyJSON }}</pre>
    </el-dialog>

    <el-dialog v-model="detailOpen" title="版本配置" width="720px">
      <el-table :data="detailRows" size="small" max-height="420">
        <el-table-column prop="key" label="Key" min-width="220" />
        <el-table-column prop="value" label="Value" min-width="280" />
      </el-table>
    </el-dialog>

    <el-dialog v-model="diffOpen" title="与当前配置对比" width="860px">
      <el-table :data="diffRows" size="small" max-height="420">
        <el-table-column prop="key" label="Key" min-width="200" />
        <el-table-column prop="kind" label="变化" width="80" />
        <el-table-column prop="current" label="当前" min-width="220" />
        <el-table-column prop="historical" label="历史版本" min-width="220" />
      </el-table>
    </el-dialog>

    <el-dialog v-model="previewOpen" title="恢复预览" width="640px">
      <div v-if="preview" class="preview-body">
        <p>将覆盖当前 Connector 配置为 <strong>v{{ preview.version }}</strong>。</p>
        <p v-if="preview.table_include_add?.length">将加回表：{{ preview.table_include_add.join('、') }}</p>
        <p v-if="preview.table_include_drop?.length">将移除表：{{ preview.table_include_drop.join('、') }}</p>
        <p>配置变更：新增 {{ preview.config_diff?.added?.length || 0 }}，删除 {{ preview.config_diff?.removed?.length || 0 }}，修改 {{ preview.config_diff?.changed?.length || 0 }}</p>
      </div>
      <template #footer>
        <el-button @click="previewOpen = false">取消</el-button>
        <el-button type="primary" :loading="restoring" @click="confirmRestore">确认恢复并覆盖</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { formatOperationType, isSensitiveConfigKey, operationTypeTag } from './cdc'

const props = defineProps({
  taskId: { type: String, required: true },
  extra: { type: Object, default: () => ({}) },
  pending: { type: Boolean, default: false },
})
const emit = defineEmits(['updated'])

const loading = ref(false)
const saving = ref(false)
const editing = ref(false)
const detail = ref({})
const rows = ref([])
const keyword = ref('')
const revealed = reactive({})
const jsonOpen = ref(false)
const historyOpen = ref(false)
const versions = ref([])
const verLoading = ref(false)
const verPage = ref(1)
const verSize = ref(10)
const verTotal = ref(0)
const detailOpen = ref(false)
const detailRows = ref([])
const diffOpen = ref(false)
const diffRows = ref([])
const previewOpen = ref(false)
const preview = ref(null)
const restoreTarget = ref(null)
const restoring = ref(false)

const filteredRows = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return rows.value
  return rows.value.filter((r) => String(r.key).toLowerCase().includes(q) || String(r.value).toLowerCase().includes(q))
})
const prettyJSON = computed(() => JSON.stringify(rowsToConfig(rows.value), null, 2))

function isSensitive(key) {
  return isSensitiveConfigKey(key)
}
function isLong(row) {
  return String(row.value || '').length > 80 || /\.list$/.test(row.key || '')
}
function stateType(s) {
  const u = String(s || '').toUpperCase()
  if (u === 'RUNNING') return 'success'
  if (u === 'PAUSED' || u === 'UNASSIGNED') return 'warning'
  if (u === 'FAILED') return 'danger'
  return 'info'
}
function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return String(t).replace('T', ' ').slice(0, 19)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
function configToRows(cfg) {
  return Object.entries(cfg || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({ key, value: String(value ?? ''), isNew: false }))
}
function rowsToConfig(list) {
  const out = {}
  for (const r of list || []) {
    const k = String(r.key || '').trim()
    if (k) out[k] = r.value ?? ''
  }
  return out
}

async function load() {
  if (!props.taskId) return
  loading.value = true
  try {
    const { data } = await api.get(`/cdc-sync/tasks/${props.taskId}/connector`)
    detail.value = data.data || {}
    rows.value = configToRows(detail.value.config || {})
    editing.value = false
  } finally {
    loading.value = false
  }
}

function startEdit() {
  editing.value = true
}
function cancelEdit() {
  editing.value = false
  rows.value = configToRows(detail.value.config || {})
}
function addRow() {
  rows.value = [...rows.value, { key: '', value: '', isNew: true }]
}
function removeRow(row) {
  rows.value = rows.value.filter((r) => r !== row)
}

async function copyJSON() {
  try {
    await navigator.clipboard.writeText(prettyJSON.value)
    ElMessage.success(`已复制 ${rows.value.length} 项配置`)
  } catch {
    ElMessage.error('复制失败')
  }
}

async function save() {
  saving.value = true
  try {
    await api.put(`/cdc-sync/tasks/${props.taskId}/connector`, { config: rowsToConfig(rows.value) })
    ElMessage.success('已保存并写入版本历史')
    await load()
    emit('updated')
  } catch (e) {
    ElMessage.error(e.response?.data?.message || e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function loadVersions() {
  verLoading.value = true
  try {
    const { data } = await api.get(`/cdc-sync/tasks/${props.taskId}/connector/versions`, {
      params: { current: verPage.value, page_size: verSize.value },
    })
    versions.value = data.data?.list || []
    verTotal.value = data.data?.pagination?.total || 0
  } catch {
    versions.value = []
    verTotal.value = 0
  } finally {
    verLoading.value = false
  }
}

async function fetchVersion(row) {
  const { data } = await api.get(`/cdc-sync/tasks/${props.taskId}/connector/versions/${row.id}`)
  return data.data
}

async function showDetail(row) {
  const d = await fetchVersion(row)
  detailRows.value = configToRows(d?.config || {})
  detailOpen.value = true
}

async function showDiff(row) {
  const d = await fetchVersion(row)
  const hist = d?.config || {}
  const cur = rowsToConfig(rows.value)
  const keys = new Set([...Object.keys(cur), ...Object.keys(hist)])
  diffRows.value = [...keys].sort().map((key) => {
    const c = cur[key]
    const h = hist[key]
    let kind = '相同'
    if (c == null) kind = '删除'
    else if (h == null) kind = '新增'
    else if (c !== h) kind = '修改'
    return { key, kind, current: c ?? '—', historical: h ?? '—' }
  }).filter((r) => r.kind !== '相同')
  diffOpen.value = true
}

async function beginRestore(row) {
  if (!row?.id) return
  restoreTarget.value = row
  const { data } = await api.post(`/cdc-sync/tasks/${props.taskId}/connector/versions/${row.id}/restore/preview`, {})
  preview.value = data.data
  previewOpen.value = true
}

async function confirmRestore() {
  if (!restoreTarget.value?.id) return
  restoring.value = true
  try {
    await api.post(`/cdc-sync/tasks/${props.taskId}/connector/versions/${restoreTarget.value.id}/restore`, { sync_pipeline: true })
    ElMessage.success('已恢复并覆盖当前配置')
    previewOpen.value = false
    historyOpen.value = false
    await load()
    emit('updated')
  } catch (e) {
    ElMessage.error(e.response?.data?.message || e.message || '恢复失败')
  } finally {
    restoring.value = false
  }
}

watch(() => props.taskId, load, { immediate: true })
</script>

<style scoped>
.dbz-summary { padding: 12px; background: #f8fafc; border: 1px solid #e5e7eb; border-radius: 8px; margin-bottom: 12px; }
.dbz-summary-title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.dbz-summary-meta { margin-top: 6px; color: #6b7280; font-size: 12px; display: flex; gap: 16px; }
.dbz-toolbar { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-bottom: 10px; }
.dbz-search { width: 260px; }
.dbz-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.dbz-val { word-break: break-all; font-size: 12px; }
.dbz-add { margin-top: 8px; }
.mb { margin-bottom: 12px; }
.pager { display: flex; justify-content: space-between; align-items: center; margin-top: 12px; color: #6b7280; font-size: 12px; }
.cfg-pre {
  margin: 0; padding: 12px; background: #f8fafc; border-radius: 6px;
  font-family: Menlo, Consolas, Monaco, monospace; font-size: 12px; line-height: 1.5;
  overflow: auto; max-height: 480px; white-space: pre-wrap;
}
.preview-body p { margin: 0 0 8px; }
</style>
