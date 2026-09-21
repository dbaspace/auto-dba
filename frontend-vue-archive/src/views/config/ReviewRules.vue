<template>
  <div class="ops-page review-page">
    <div v-if="engine === 'mysql'" class="ops-title">
      <div class="title-actions">
        <el-input v-model="keyword" clearable placeholder="搜索参数名 / 中文名 / 当前值" style="width:260px" />
        <el-button :icon="Plus" :disabled="saving || adding" @click="openAdd">增加参数</el-button>
        <el-button :icon="Refresh" :loading="loading" :disabled="saving" @click="reload">刷新</el-button>
        <el-button type="primary" :icon="CircleCheck" :loading="saving" :disabled="dirtyItems.length === 0" @click="save">
          保存修改{{ dirtyItems.length ? `（${dirtyItems.length}）` : '' }}
        </el-button>
      </div>
    </div>

    <el-tabs v-model="engine" class="inst-tabs" @tab-change="onTabChange">
      <el-tab-pane label="MySQL" name="mysql" />
      <el-tab-pane label="PostgreSQL" name="postgres" />
      <el-tab-pane label="StarRocks" name="starrocks" />
    </el-tabs>

    <div v-show="engine === 'mysql'" class="review-body" v-loading="loading && engine === 'mysql'">
      <section class="gi-card">
        <div class="gi-head">
          <h2>goInception 连接</h2>
          <p>启用后，MySQL / TiDB 工单审核走 goInception（账号可空，连接时随机生成会话）。未启用则仍用本地规则。</p>
        </div>
        <el-form :model="giForm" inline class="gi-form" label-width="72px">
          <el-form-item label="启用">
            <el-switch v-model="giForm.enabled" :disabled="giSaving" />
          </el-form-item>
          <el-form-item label="地址">
            <el-input v-model="giForm.host" placeholder="如 10.0.0.8" style="width:180px" :disabled="giSaving" />
          </el-form-item>
          <el-form-item label="端口">
            <el-input-number v-model="giForm.port" :min="1" :max="65535" :disabled="giSaving" />
          </el-form-item>
          <el-form-item label="用户">
            <el-input v-model="giForm.user" placeholder="可空" style="width:140px" :disabled="giSaving" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="giForm.password" type="password" show-password :placeholder="giPwdPlaceholder" style="width:180px" autocomplete="off" :disabled="giSaving" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="giSaving" @click="saveGI">保存连接</el-button>
            <el-button :loading="giPinging" :disabled="giSaving" @click="pingGI">测试连接</el-button>
          </el-form-item>
        </el-form>
      </section>
      <div v-if="filteredRows.length === 0" class="empty">
        {{ keyword.trim() ? '无匹配参数' : '暂无参数' }}
      </div>
      <div v-else class="rule-grid">
        <div v-for="(col, i) in mysqlCols" :key="i" class="rule-col">
          <div v-for="row in col" :key="row.name" class="rule-row">
            <span class="rule-label" :title="row.label || row.name">{{ row.label || '—' }}</span>
            <div class="rule-ctrl">
              <el-switch
                v-if="isBoolText(row.value)"
                :model-value="isTrue(drafts[row.name])"
                :disabled="saving"
                size="small"
                @change="(on) => setDraft(row.name, on ? 'true' : 'false')"
              />
              <el-input
                v-else
                :model-value="drafts[row.name]"
                size="small"
                class="rule-input"
                placeholder="请输入参数值"
                :disabled="saving"
                @input="(v) => setDraft(row.name, v)"
              />
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-show="engine !== 'mysql'" class="review-body">
      <div class="panel-actions">
        <el-input v-model="keyword" clearable placeholder="搜索参数名 / 中文名 / 当前值" style="width:260px" />
        <el-button :icon="Plus" :disabled="saving || adding" @click="openAdd">增加参数</el-button>
        <el-button :icon="Refresh" :loading="loading" :disabled="saving" @click="reload">刷新</el-button>
        <el-button type="primary" :icon="CircleCheck" :loading="saving" :disabled="dirtyItems.length === 0" @click="save">
          保存修改{{ dirtyItems.length ? `（${dirtyItems.length}）` : '' }}
        </el-button>
      </div>
      <div v-loading="loading">
        <div v-if="ddlRows.length === 0 && dmlRows.length === 0" class="empty">
          {{ keyword.trim() ? '无匹配参数' : '暂无参数' }}
        </div>
        <template v-else>
          <section class="rule-section">
            <h2>DDL</h2>
            <div v-if="ddlRows.length === 0" class="empty-sm">{{ keyword.trim() ? '无匹配参数' : '暂无参数' }}</div>
            <div v-else class="rule-grid">
              <div v-for="row in grouped(ddlRows).switches" :key="row.name" class="rule-cell">
                <RuleField :row="row" :value="drafts[row.name]" :saving="saving" @change="setDraft" />
              </div>
              <div v-if="grouped(ddlRows).inputs.length" class="rule-wide-wrap">
                <div v-for="row in grouped(ddlRows).inputs" :key="row.name" class="rule-cell">
                  <RuleField :row="row" :value="drafts[row.name]" :saving="saving" wide @change="setDraft" />
                </div>
              </div>
              <div v-for="row in grouped(ddlRows).wides" :key="row.name" class="rule-span">
                <RuleField :row="row" :value="drafts[row.name]" :saving="saving" wide @change="setDraft" />
              </div>
            </div>
          </section>
          <section class="rule-section">
            <h2>DML</h2>
            <div v-if="dmlRows.length === 0" class="empty-sm">{{ keyword.trim() ? '无匹配参数' : '暂无参数' }}</div>
            <div v-else class="rule-grid">
              <div v-for="row in grouped(dmlRows).switches" :key="row.name" class="rule-cell">
                <RuleField :row="row" :value="drafts[row.name]" :saving="saving" @change="setDraft" />
              </div>
              <div v-if="grouped(dmlRows).inputs.length" class="rule-wide-wrap">
                <div v-for="row in grouped(dmlRows).inputs" :key="row.name" class="rule-cell">
                  <RuleField :row="row" :value="drafts[row.name]" :saving="saving" wide @change="setDraft" />
                </div>
              </div>
              <div v-for="row in grouped(dmlRows).wides" :key="row.name" class="rule-span">
                <RuleField :row="row" :value="drafts[row.name]" :saving="saving" wide @change="setDraft" />
              </div>
            </div>
          </section>
        </template>
      </div>
    </div>

    <el-dialog v-model="addOpen" title="增加参数" width="480px" append-to-body destroy-on-close :close-on-click-modal="!adding" @closed="resetAdd">
      <el-form ref="addRef" :model="addForm" :rules="addRules" label-width="80px">
        <el-form-item label="参数名" prop="name">
          <el-input v-model="addForm.name" :placeholder="engine === 'mysql' ? '如 check_dml_where' : '如 DDLMaxColumnCount'" />
        </el-form-item>
        <el-form-item label="中文名" prop="label">
          <el-input v-model="addForm.label" :placeholder="engine === 'mysql' ? '如 检查 DML WHERE 条件' : '如 建表最大列数'" />
        </el-form-item>
        <el-form-item label="当前值" prop="value">
          <el-input v-model="addForm.value" :placeholder="engine === 'mysql' ? '如 true' : '如 true 或 64'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="adding" @click="addOpen = false">取消</el-button>
        <el-button type="primary" :loading="adding" @click="onAddOk">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleCheck, Plus, Refresh } from '@element-plus/icons-vue'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'
import RuleField from './ReviewRuleField.vue'

const engines = ['mysql', 'postgres', 'starrocks']
const engine = ref('mysql')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const adding = ref(false)
const addOpen = ref(false)
const addRef = ref()
const addForm = reactive({ name: '', label: '', value: '' })
const giSaving = ref(false)
const giPinging = ref(false)
const giLoaded = ref(false)
const giPasswordSet = ref(false)
const giForm = reactive({
  enabled: false,
  host: '',
  port: 4000,
  user: '',
  password: '',
})
const giPwdPlaceholder = computed(() => (giPasswordSet.value ? '已配置，留空则不修改' : '可空'))

const caches = reactive({})
for (const e of engines) {
  caches[e] = { rows: [], drafts: {}, saved: {}, loaded: false }
}

const cache = computed(() => caches[engine.value])
const rows = computed(() => cache.value.rows)
const drafts = computed(() => cache.value.drafts)

const addRules = {
  name: [
    { required: true, message: '请输入参数名', trigger: 'blur' },
    {
      validator: (_, v, cb) => {
        const name = String(v ?? '').trim()
        if (!name) return cb()
        if (rows.value.some((row) => row.name.toLowerCase() === name.toLowerCase())) {
          return cb(new Error('该参数已在列表中'))
        }
        cb()
      },
      trigger: 'blur',
    },
  ],
  label: [{ required: true, message: '请输入中文名', trigger: 'blur' }],
  value: [{ required: true, message: '请输入参数值', trigger: 'blur' }],
}

function isBoolText(v) {
  const s = String(v ?? '').trim().toLowerCase()
  return s === 'true' || s === 'false'
}

function isTrue(v) {
  return String(v ?? '').trim().toLowerCase() === 'true'
}

function isDml(row) {
  return /^dml/i.test(String(row?.name ?? '').trim()) || String(row?.category ?? '').toLowerCase() === 'dml'
}

function isWide(row) {
  const v = String(row?.value ?? '').trim()
  return v.startsWith('{') || v.startsWith('[')
}

function setDraft(name, value) {
  cache.value.drafts[name] = value
}

const filteredRows = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return rows.value
  return rows.value.filter((row) => {
    const current = String(drafts.value[row.name] ?? row.value ?? '')
    return (
      row.name.toLowerCase().includes(q) ||
      String(row.label ?? '').toLowerCase().includes(q) ||
      current.toLowerCase().includes(q)
    )
  })
})

const dirtyItems = computed(() => {
  return rows.value
    .map((row) => {
      const next = String(drafts.value[row.name] ?? '')
      const prev = String(cache.value.saved[row.name] ?? '')
      return next !== prev ? { name: row.name, value: next } : null
    })
    .filter(Boolean)
})

const mysqlCols = computed(() => {
  const list = filteredRows.value
  const size = Math.ceil(list.length / 3) || 0
  return [0, 1, 2].map((i) => list.slice(i * size, (i + 1) * size))
})

const ddlRows = computed(() => filteredRows.value.filter((r) => !isDml(r)))
const dmlRows = computed(() => filteredRows.value.filter((r) => isDml(r)))

function grouped(list) {
  const switches = []
  const inputs = []
  const wides = []
  for (const row of list) {
    if (isWide(row)) wides.push(row)
    else if (isBoolText(row.value)) switches.push(row)
    else inputs.push(row)
  }
  return { switches, inputs, wides }
}

function normalizeList(data) {
  if (Array.isArray(data)) return data
  if (data && typeof data === 'object') {
    return [...(data.ddl || []), ...(data.dml || [])]
  }
  return []
}

function applyRows(list) {
  const c = cache.value
  c.rows = list
  c.drafts = {}
  c.saved = {}
  for (const row of list) {
    c.drafts[row.name] = String(row.value ?? '')
    c.saved[row.name] = String(row.value ?? '')
  }
  c.loaded = true
}

function reload() {
  return load(true)
}

function onTabChange() {
  keyword.value = ''
  load()
}

async function save() {
  if (!dirtyItems.value.length) {
    ElMessage.info('没有需要保存的修改')
    return
  }
  saving.value = true
  try {
    const { data } = await api.put('/review-rules', { engine: engine.value, items: dirtyItems.value })
    if (data.gi_synced === false && data.message) {
      ElMessage.warning(data.message)
    } else {
      ElMessage.success('已保存')
    }
    await reload()
  } finally {
    saving.value = false
  }
}

async function loadGI() {
  if (giLoaded.value) return
  try {
    const { data } = await api.get('/review-rules/goinception')
    const d = data.data || {}
    giForm.enabled = !!d.enabled
    giForm.host = d.host || ''
    giForm.port = d.port || 4000
    giForm.user = d.user || ''
    giForm.password = ''
    giPasswordSet.value = !!d.password_set
    giLoaded.value = true
  } catch {
    giLoaded.value = true
  }
}

async function saveGI() {
  giSaving.value = true
  try {
    const { data } = await api.put('/review-rules/goinception', {
      enabled: giForm.enabled,
      host: giForm.host.trim(),
      port: giForm.port,
      user: giForm.user.trim(),
      password: giForm.password,
    })
    const d = data.data || {}
    giPasswordSet.value = !!d.password_set
    giForm.password = ''
    ElMessage.success('goInception 连接已保存')
  } finally {
    giSaving.value = false
  }
}

async function pingGI() {
  giPinging.value = true
  try {
    const { data } = await api.post('/review-rules/goinception/ping', {
      host: giForm.host.trim(),
      port: giForm.port,
      user: giForm.user.trim(),
      password: giForm.password,
    })
    ElMessage.success(data.message || '连接成功')
  } finally {
    giPinging.value = false
  }
}

function openAdd() {
  resetAdd()
  showDialog(addOpen)
}

function resetAdd() {
  addForm.name = ''
  addForm.label = ''
  addForm.value = ''
  addRef.value?.clearValidate?.()
}

async function onAddOk() {
  await addRef.value.validate()
  adding.value = true
  try {
    const { data } = await api.post('/review-rules', {
      engine: engine.value,
      name: addForm.name.trim(),
      label: addForm.label.trim(),
      value: addForm.value.trim(),
    })
    if (data.gi_synced === false && data.message) {
      ElMessage.warning(data.message)
    } else {
      ElMessage.success('已增加')
    }
    addOpen.value = false
    await reload()
  } finally {
    adding.value = false
  }
}

async function load(force) {
  if (engine.value === 'mysql') await loadGI()
  if (cache.value.loaded && !force) return
  loading.value = true
  try {
    const { data } = await api.get('/review-rules', { params: { engine: engine.value } })
    applyRows(normalizeList(data.data))
  } finally {
    loading.value = false
  }
}

onMounted(() => load())
</script>

<style scoped>
.review-page { background: #fff; }
.gi-card {
  margin-bottom: 20px;
  padding: 16px 16px 4px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fafafa;
}
.gi-head h2 {
  margin: 0 0 4px;
  font-size: 14px;
  font-weight: 600;
  color: #1f2937;
}
.gi-head p {
  margin: 0 0 8px;
  font-size: 12px;
  color: #6b7280;
  line-height: 1.5;
}
.gi-form { margin-bottom: 0; }
.title-actions,
.panel-actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.panel-actions { margin-bottom: 16px; }
.inst-tabs { flex-shrink: 0; }
.inst-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
.inst-tabs :deep(.el-tabs__content) { display: none; }
.review-body { flex: 1; min-height: 0; overflow: auto; }
.empty {
  color: #9ca3af;
  text-align: center;
  padding: 64px 0;
  font-size: 14px;
}
.empty-sm {
  color: #9ca3af;
  text-align: center;
  padding: 24px 0;
  font-size: 13px;
}
.rule-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  column-gap: 32px;
  align-items: start;
}
.rule-col { min-width: 0; }
.rule-row {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  min-width: 0;
  overflow: hidden;
  padding: 0 4px;
  border-bottom: 1px solid #f3f4f6;
}
.rule-label {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
  color: #1f2937;
  line-height: 32px;
}
.rule-ctrl { flex-shrink: 0; }
.rule-input { width: 80px; }
.rule-section { margin-bottom: 32px; }
.rule-section:last-child { margin-bottom: 0; }
.rule-section h2 {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: #1f2937;
}
.rule-cell { min-width: 0; }
.rule-wide-wrap {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  column-gap: 32px;
}
.rule-span { grid-column: 1 / -1; min-width: 0; }
</style>
