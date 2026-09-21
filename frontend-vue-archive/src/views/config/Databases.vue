<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">登记库</el-button>
    </div>
    <div class="ops-filters">
      <div class="filter-item">
        <span class="label">实例：</span>
        <el-select v-model="instanceId" clearable filterable placeholder="请选择实例">
          <el-option v-for="i in tabInstances" :key="i.id" :label="i.name" :value="i.id" />
        </el-select>
      </div>
    </div>
    <el-tabs v-model="activeType" class="inst-tabs" @tab-change="onTabChange">
      <el-tab-pane v-for="t in tabs" :key="t.value" :name="t.value">
        <template #label>
          <span class="tab-label">
            <DbTypeIcon :type="t.value" :size="16" />
            <span>{{ t.label }} ({{ typeCount(t.value) }})</span>
          </span>
        </template>
      </el-tab-pane>
    </el-tabs>
    <div class="ops-table">
    <el-table :data="pagedRows" border height="100%">
      <el-table-column prop="db_name" label="库名" min-width="140" />
      <el-table-column label="绑定域名" min-width="180">
        <template #default="{ row }">{{ (row.domains || []).join(', ') || '—' }}</template>
      </el-table-column>
      <el-table-column label="标签" min-width="140">
        <template #default="{ row }">
          <el-tag v-for="t in (row.tags || [])" :key="t" size="small" class="cell-tag">{{ t }}</el-tag>
          <span v-if="!(row.tags || []).length">—</span>
        </template>
      </el-table-column>
      <el-table-column label="范围" width="80">
        <template #default="{ row }">{{ row.visibility === 'private' ? '私有' : '全局' }}</template>
      </el-table-column>
      <el-table-column label="可访问用户" min-width="140">
        <template #default="{ row }">{{ row.visibility === 'private' ? ((row.access_users || []).join(',') || '—') : '全部' }}</template>
      </el-table-column>
      <el-table-column label="绑定角色" min-width="140">
        <template #default="{ row }">{{ row.visibility === 'private' ? (roleNames(row.role_ids) || '—') : '—' }}</template>
      </el-table-column>
      <el-table-column label="实例" min-width="120">
        <template #default="{ row }">{{ instanceName(row.instance_id) }}</template>
      </el-table-column>
      <el-table-column label="审批人" min-width="120">
        <template #default="{ row }">{{ (row.approvers || []).join(',') }}</template>
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
      <span class="pager-total">共 {{ filteredRows.length }} 条</span>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="filteredRows.length"
        :page-sizes="[10, 20, 50, 100]"
        layout="sizes, prev, pager, next, jumper"
      />
    </div>
    <el-dialog v-model="visible" title="登记库" width="640px" append-to-body destroy-on-close align-center>
      <el-form :model="form" label-width="100px">
        <el-form-item label="实例">
          <el-select
            v-model="form.instance_id"
            filterable
            :disabled="!!form.id"
            style="width:100%"
            placeholder="请选择实例"
            @change="onInstanceChange"
          >
            <el-option v-for="i in formInstances" :key="i.id" :label="i.name" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="域名" class="db-name-item">
          <el-select
            v-model="form.domains"
            multiple
            filterable
            clearable
            :disabled="!form.instance_id"
            :placeholder="form.instance_id ? (instanceDomains.length ? '输入关键字搜索并选择域名，不默认填充' : '该实例尚未配置域名') : '请先选择实例'"
            style="width:100%"
          >
            <el-option v-for="d in instanceDomains" :key="d" :label="d" :value="d" />
          </el-select>
          <div class="type-hint">从该实例已配置的域名中按需选择，至少选一个，支持搜索</div>
        </el-form-item>
        <el-form-item v-if="form.id" label="库名">
          <el-input v-model="form.db_name" disabled />
        </el-form-item>
        <el-form-item v-else label="库名" class="db-name-item">
          <el-select
            v-model="form.db_names"
            multiple
            filterable
            allow-create
            default-first-option
            clearable
            :loading="dbLoading"
            :disabled="!form.instance_id"
            placeholder="输入关键字搜索库名，可多选，也可输入后回车"
            style="width:100%"
          >
            <el-option
              v-for="d in visibleDbs"
              :key="d.name"
              :label="d.registered ? `${d.name}（已登记）` : d.name"
              :value="d.name"
              :disabled="d.registered"
            />
          </el-select>
          <div class="type-hint">
            <el-checkbox v-model="showSystemDb">显示系统库</el-checkbox>
            <span v-if="liveHint" class="live-hint">{{ liveHint }}</span>
          </div>
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="form.tags"
            multiple
            filterable
            allow-create
            default-first-option
            collapse-tags
            collapse-tags-tooltip
            placeholder="输入后回车，同名库用标签区分"
            style="width:100%"
          >
            <el-option v-for="t in usedTags" :key="t" :label="t" :value="t" />
          </el-select>
          <div class="type-hint">工单选库时会展示为「库名 [标签]」，用于区分相同库名</div>
        </el-form-item>
        <el-form-item label="可见范围">
          <el-radio-group v-model="form.visibility">
            <el-radio-button value="global">全局</el-radio-button>
            <el-radio-button value="private">私有</el-radio-button>
          </el-radio-group>
          <div class="type-hint">全局：所有人可见。私有：仅可访问用户，以及绑定角色下的用户可见</div>
        </el-form-item>
        <template v-if="form.visibility === 'private'">
          <el-form-item label="可访问用户">
            <el-select
              v-model="form.access_users"
              multiple
              filterable
              allow-create
              default-first-option
              collapse-tags
              collapse-tags-tooltip
              placeholder="选择或输入用户名"
              style="width:100%"
            >
              <el-option v-for="u in userOptions" :key="u" :label="u" :value="u" />
            </el-select>
          </el-form-item>
          <el-form-item label="绑定角色">
            <el-select
              v-model="form.role_ids"
              multiple
              filterable
              collapse-tags
              collapse-tags-tooltip
              placeholder="绑定后，该角色下用户可见"
              style="width:100%"
            >
              <el-option v-for="r in roles" :key="r.id" :label="r.name || r.code" :value="r.id" />
            </el-select>
          </el-form-item>
        </template>
        <el-form-item label="审批人">
          <el-select
            v-model="form.approvers"
            multiple
            filterable
            allow-create
            default-first-option
            collapse-tags
            collapse-tags-tooltip
            placeholder="可多选，也可输入后回车"
            style="width:100%"
          >
            <el-option v-for="u in userOptions" :key="u" :label="u" :value="u" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible=false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'
import DbTypeIcon from '../../components/DbTypeIcon.vue'

const OTHER = '__other__'
const typeOptions = ref([])
const activeType = ref('')
const instances = ref([])
const rows = ref([])
const instanceId = ref('')
const visible = ref(false)
const form = ref({})
const liveDbs = ref([])
const dbLoading = ref(false)
const liveHint = ref('')
const showSystemDb = ref(false)
const userOptions = ref(['admin'])
const roles = ref([])
const usedTags = ref([])
const saving = ref(false)
const page = ref(1)
const pageSize = ref(10)

const visibleDbs = computed(() => {
  if (showSystemDb.value) return liveDbs.value
  return liveDbs.value.filter((d) => !d.system || d.registered)
})

const instanceDomains = computed(() => {
  const inst = instances.value.find((i) => i.id === form.value.instance_id)
  if (!inst) return []
  const list = [...(inst.domains || [])]
  if (inst.domain_name && !list.includes(inst.domain_name)) list.unshift(inst.domain_name)
  return [...new Set(list.filter(Boolean))]
})

function parseInstanceTypes(raw) {
  try {
    const arr = JSON.parse(String(raw || '').trim() || '[]')
    if (!Array.isArray(arr)) return []
    const seen = new Set()
    const out = []
    for (const it of arr) {
      const value = String(it?.value ?? '').trim()
      if (!value || seen.has(value) || value === 'kafka' || value === 'redis') continue
      seen.add(value)
      out.push({ value, label: String(it?.label || value).trim() || value })
    }
    return out
  } catch {
    return []
  }
}

function instanceType(id) {
  return instances.value.find((i) => i.id === id)?.db_type || ''
}

function matchType(dbType, tab) {
  if (tab === OTHER) return !!dbType && !knownTypes.value.has(dbType)
  return dbType === tab
}

const knownTypes = computed(() => new Set(typeOptions.value.map((t) => t.value)))
const tabs = computed(() => {
  const list = typeOptions.value.map((t) => ({ value: t.value, label: t.label }))
  const hasOther = instances.value.some((i) => i.db_type && !knownTypes.value.has(i.db_type))
  if (hasOther) list.push({ value: OTHER, label: '其他' })
  return list
})
const tabInstances = computed(() => {
  if (!activeType.value) return instances.value
  return instances.value.filter((i) => matchType(i.db_type, activeType.value))
})
const formInstances = computed(() => {
  if (form.value.id) {
    const cur = instances.value.find((i) => i.id === form.value.instance_id)
    return cur ? [cur] : tabInstances.value
  }
  return tabInstances.value
})
const filteredRows = computed(() => {
  return rows.value.filter((r) => {
    const dbType = instanceType(r.instance_id)
    if (activeType.value && !matchType(dbType, activeType.value)) return false
    if (instanceId.value && r.instance_id !== instanceId.value) return false
    return true
  })
})
const pagedRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredRows.value.slice(start, start + pageSize.value)
})

watch([activeType, instanceId], () => { page.value = 1 })
watch(filteredRows, (list) => {
  const maxPage = Math.max(1, Math.ceil(list.length / pageSize.value) || 1)
  if (page.value > maxPage) page.value = maxPage
})

function typeCount(value) {
  return rows.value.filter((r) => matchType(instanceType(r.instance_id), value)).length
}

function ensureActiveTab() {
  const names = tabs.value.map((t) => t.value)
  if (!names.includes(activeType.value)) activeType.value = names[0] || ''
}

function onTabChange() {
  if (instanceId.value && !matchType(instanceType(instanceId.value), activeType.value)) {
    instanceId.value = ''
  }
}

function instanceName(id) {
  return instances.value.find((i) => i.id === id)?.name || id
}

function roleNames(ids) {
  return (ids || []).map((id) => roles.value.find((r) => r.id === id)?.name || id).join(',')
}

function defaultApprovers(inst) {
  const fromInst = (inst?.approvers || []).map((s) => String(s).trim()).filter(Boolean)
  return fromInst.length ? fromInst : ['admin']
}

function emptyMeta() {
  return { visibility: 'global', access_users: [], role_ids: [], tags: [], domains: [] }
}

async function loadMeta() {
  const [{ data: instRes }, { data: apprRes }, { data: roleRes }, { data: argRes }] = await Promise.all([
    api.get('/instances'),
    api.get('/create-db/approvers'),
    api.get('/role-options'),
    api.get('/global-args'),
  ])
  instances.value = instRes.data || []
  const names = (apprRes.data || []).map((a) => a.name || a).filter(Boolean)
  userOptions.value = [...new Set(['admin', ...names])]
  roles.value = roleRes.data || []
  const raw = (argRes.data || []).find((a) => a.key === 'ticket_instance_types')?.value || ''
  typeOptions.value = parseInstanceTypes(raw)
  ensureActiveTab()
}

async function load() {
  const { data } = await api.get('/databases')
  rows.value = data.data || []
  const tags = new Set(usedTags.value)
  for (const row of rows.value) {
    for (const t of row.tags || []) if (t) tags.add(t)
  }
  usedTags.value = [...tags]
  ensureActiveTab()
}

async function loadLiveDbs(id) {
  liveDbs.value = []
  liveHint.value = ''
  if (!id) return
  dbLoading.value = true
  try {
    const { data } = await api.get(`/instances/${id}/databases`)
    liveDbs.value = data.data || []
    liveHint.value = data.live ? '' : (data.message || '未能拉取实例库，可手动输入库名')
  } catch (e) {
    liveHint.value = e.response?.data?.message || '未能拉取实例库，可手动输入库名'
  } finally {
    dbLoading.value = false
  }
}

function onInstanceChange() {
  form.value.db_names = []
  form.value.domains = []
  const inst = instances.value.find((i) => i.id === form.value.instance_id)
  if (!form.value.id) form.value.approvers = defaultApprovers(inst)
  loadLiveDbs(form.value.instance_id)
}

function open(row) {
  if (row) {
    form.value = {
      ...emptyMeta(),
      ...row,
      approvers: [...(row.approvers || [])],
      access_users: [...(row.access_users || [])],
      role_ids: [...(row.role_ids || [])],
      tags: [...(row.tags || [])],
      domains: [...(row.domains || [])],
      visibility: row.visibility === 'private' ? 'private' : 'global',
    }
    liveDbs.value = []
    liveHint.value = ''
  } else {
    const pref = instanceId.value && matchType(instanceType(instanceId.value), activeType.value)
      ? instanceId.value
      : ''
    const inst = instances.value.find((i) => i.id === pref)
    form.value = {
      instance_id: pref,
      db_names: [],
      approvers: defaultApprovers(inst),
      ...emptyMeta(),
    }
    loadLiveDbs(form.value.instance_id)
  }
  showDialog(visible)
}

function payloadMeta() {
  const approvers = (form.value.approvers || []).map((s) => String(s).trim()).filter(Boolean)
  const visibility = form.value.visibility === 'private' ? 'private' : 'global'
  const tags = (form.value.tags || []).map((s) => String(s).trim()).filter(Boolean)
  const domains = (form.value.domains || []).map((s) => String(s).trim()).filter(Boolean)
  const access_users = visibility === 'private'
    ? (form.value.access_users || []).map((s) => String(s).trim()).filter(Boolean)
    : []
  const role_ids = visibility === 'private' ? [...(form.value.role_ids || [])] : []
  return { approvers, visibility, access_users, role_ids, tags, domains }
}

async function save() {
  const meta = payloadMeta()
  if (!meta.domains.length) {
    ElMessage.warning('请绑定域名')
    return
  }
  if (!meta.approvers.length) {
    ElMessage.warning('请至少选择一名审批人')
    return
  }
  if (meta.visibility === 'private' && !meta.access_users.length && !meta.role_ids.length) {
    ElMessage.warning('私有库请至少选择可访问用户或绑定角色')
    return
  }
  saving.value = true
  try {
    if (form.value.id) {
      await api.put('/databases/' + form.value.id, { ...form.value, ...meta })
      ElMessage.success('已保存')
    } else {
      const dbNames = (form.value.db_names || []).map((s) => String(s).trim()).filter(Boolean)
      if (!form.value.instance_id || !dbNames.length) {
        ElMessage.warning('请选择实例并至少选择一个库')
        return
      }
      const { data } = await api.post('/databases/batch', {
        instance_id: form.value.instance_id,
        db_names: dbNames,
        ...meta,
      })
      const created = data.data?.created || []
      const skipped = data.data?.skipped || []
      ElMessage.success(skipped.length ? `已登记 ${created.length} 个库，跳过已存在 ${skipped.length} 个` : `已登记 ${created.length} 个库`)
    }
    const savedType = instanceType(form.value.instance_id)
    visible.value = false
    await load()
    if (savedType) activeType.value = savedType
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm('删除该登记库？')
  await api.delete('/databases/' + row.id)
  await load()
}

onMounted(async () => { await loadMeta(); await load() })
</script>

<style scoped>
.type-hint {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 6px;
  font-size: 12px;
  color: #6b7280;
}
.live-hint { color: #d97706; }
.inst-tabs { flex-shrink: 0; }
.inst-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
.inst-tabs :deep(.el-tabs__content) { display: none; }
.tab-label { display: inline-flex; align-items: center; gap: 6px; }
.cell-tag { margin-right: 4px; }
.db-name-item :deep(.el-select__tags),
.db-name-item :deep(.el-select__selection) {
  flex-wrap: wrap;
  align-items: center;
  max-width: 100%;
}
.db-name-item :deep(.el-tag) {
  max-width: 100%;
}
.db-name-item :deep(.el-select__wrapper) {
  min-height: 32px;
  height: auto;
  align-items: center;
}
.db-name-item :deep(.el-select__placeholder) {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  line-height: 22px;
}
.pager {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding-top: 12px;
  flex-shrink: 0;
}
.pager-total { color: #6b7280; font-size: 13px; }
</style>
