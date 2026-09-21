<template>
  <div class="ops-page menus-page">
    <div class="ops-title">
      <div class="toolbar" style="margin:0">
        <el-button type="primary" @click="openCreate()">新建菜单</el-button>
        <el-button @click="expandAll">全部展开</el-button>
        <el-button @click="collapseAll">全部收起</el-button>
      </div>
    </div>
    <div class="table-wrap">
      <el-table
        v-if="ready"
        :key="tableKey"
        :data="visibleRows"
        row-key="id"
        border
        height="100%"
        :row-class-name="rowClass"
      >
        <el-table-column label="菜单名称" min-width="280">
          <template #default="{ row }">
            <div class="name-cell" :style="{ paddingLeft: (8 + row._depth * 28) + 'px' }">
              <button
                v-if="row._hasChildren"
                type="button"
                class="toggle"
                @click="toggle(row)"
              >
                <el-icon>
                  <CaretRight v-if="isCollapsed(row.id)" />
                  <CaretBottom v-else />
                </el-icon>
              </button>
              <span v-else class="toggle-placeholder" />
              <MenuIcon v-if="row.icon" :name="row.icon" :size="16" />
              <span v-else class="icon-placeholder" />
              <span class="name-text">{{ row.title }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="菜单 URN" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ row.urn || '-' }}</template>
        </el-table-column>
        <el-table-column label="页面路径" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.path || '-' }}</template>
        </el-table-column>
        <el-table-column label="权限说明" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">{{ row.permission_desc || '-' }}</template>
        </el-table-column>
        <el-table-column label="父级" min-width="80" show-overflow-tooltip>
          <template #default="{ row }">{{ row.parent_title || '-' }}</template>
        </el-table-column>
        <el-table-column prop="sort_order" label="排序" width="72" align="center" />
        <el-table-column label="可见" width="64" align="center">
          <template #default="{ row }">{{ row.enabled ? '是' : '否' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="220">
          <template #default="{ row }">
            <div class="ops">
              <el-button link type="primary" @click="move(row, 'up')">上移</el-button>
              <el-button link type="primary" @click="move(row, 'down')">下移</el-button>
              <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
              <el-button link type="danger" @click="remove(row)">删除</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="visible" :title="form.id ? '编辑菜单' : '新建菜单'" width="520px" append-to-body destroy-on-close>
      <el-form :model="form" label-width="100px">
        <el-form-item label="上级">
          <el-select v-model="form.parent_id" clearable style="width:100%">
            <el-option label="无（一级分组）" value="" />
            <el-option v-for="g in parentOptions" :key="g.id" :label="g.label" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="form.title" />
        </el-form-item>
        <el-form-item label="图标">
          <el-select v-model="form.icon" clearable filterable allow-create style="width:100%" placeholder="如 dashboard">
            <el-option v-for="opt in MENU_ICON_OPTIONS" :key="opt.value" :label="opt.value" :value="opt.value">
              <span class="icon-opt">
                <el-icon><component :is="opt.icon" /></el-icon>
                <span>{{ opt.label }}</span>
              </span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="菜单 URN">
          <el-input v-model="form.urn" placeholder="留空则按路径自动生成 um.sidebar.*" />
        </el-form-item>
        <el-form-item label="页面路径">
          <el-select v-model="form.path" clearable filterable allow-create style="width:100%" placeholder="一级分组可留空">
            <el-option v-for="r in PAGE_ROUTES" :key="r.path" :label="`${r.title} (${r.path})`" :value="r.path" />
          </el-select>
        </el-form-item>
        <el-form-item label="权限说明">
          <el-input v-model="form.permission_desc" placeholder="如：查看工单列表" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort_order" :min="0" />
        </el-form-item>
        <el-form-item label="可见">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { CaretBottom, CaretRight } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { PAGE_ROUTES } from '../../constants/routes'
import { MENU_ICON_OPTIONS } from '../../constants/menuIcons'
import MenuIcon from '../../components/MenuIcon.vue'
import { showDialog } from '../../utils/overlay'

const tree = ref([])
const collapsedIds = ref([])
const ready = ref(false)
const tableKey = ref(0)
const visible = ref(false)
const form = reactive({
  id: '',
  parent_id: '',
  title: '',
  urn: '',
  path: '',
  permission_desc: '',
  icon: '',
  sort_order: 0,
  enabled: true,
})

function flatten(nodes, out = [], depth = 0) {
  for (const n of nodes || []) {
    out.push({ ...n, label: `${'　'.repeat(depth)}${n.title}` })
    if (n.children?.length) flatten(n.children, out, depth + 1)
  }
  return out
}

function collectIds(node, set) {
  set.add(node.id)
  for (const c of node.children || []) collectIds(c, set)
}

const parentOptions = computed(() => {
  const all = flatten(tree.value)
  if (!form.id) return all
  const blocked = new Set()
  for (const n of all) {
    if (n.id === form.id) collectIds(n, blocked)
  }
  return all.filter((n) => !blocked.has(n.id))
})

function collectParentIds(nodes, out = []) {
  for (const n of nodes || []) {
    if (n.children?.length) {
      out.push(n.id)
      collectParentIds(n.children, out)
    }
  }
  return out
}

const visibleRows = computed(() => {
  const collapsed = new Set(collapsedIds.value)
  const out = []
  function walk(nodes, depth, hidden) {
    for (const n of nodes || []) {
      const hasChildren = !!n.children?.length
      if (!hidden) {
        out.push({ ...n, _depth: depth, _hasChildren: hasChildren })
      }
      if (hasChildren) walk(n.children, depth + 1, hidden || collapsed.has(n.id))
    }
  }
  walk(tree.value, 0, false)
  return out
})

function rowClass({ row }) {
  return `menu-row lv-${Math.min(row._depth || 0, 4)}`
}

function isCollapsed(id) {
  return collapsedIds.value.includes(id)
}

function toggle(row) {
  if (isCollapsed(row.id)) {
    collapsedIds.value = collapsedIds.value.filter((id) => id !== row.id)
  } else {
    collapsedIds.value = [...collapsedIds.value, row.id]
  }
}

function expandAll() {
  collapsedIds.value = []
}

function collapseAll() {
  collapsedIds.value = collectParentIds(tree.value)
}

async function load() {
  ready.value = false
  tree.value = (await api.get('/menus/tree')).data.data || []
  collapsedIds.value = []
  tableKey.value += 1
  ready.value = true
}

function reset() {
  Object.assign(form, {
    id: '',
    parent_id: '',
    title: '',
    urn: '',
    path: '',
    permission_desc: '',
    icon: '',
    sort_order: 0,
    enabled: true,
  })
}

function openCreate() {
  reset()
  showDialog(visible)
}

function openEdit(row) {
  Object.assign(form, {
    id: row.id,
    parent_id: row.parent_id || '',
    title: row.title,
    urn: row.urn || '',
    path: row.path || '',
    permission_desc: row.permission_desc || '',
    icon: row.icon || '',
    sort_order: row.sort_order || 0,
    enabled: !!row.enabled,
  })
  showDialog(visible)
}

async function save() {
  const payload = { ...form, parent_id: form.parent_id || '' }
  if (form.id) await api.put('/menus/' + form.id, payload)
  else await api.post('/menus', payload)
  ElMessage.success('已保存')
  visible.value = false
  reset()
  await load()
  window.dispatchEvent(new Event('menus-changed'))
}

async function move(row, dir) {
  await api.post('/menus/' + row.id + '/move', { dir })
  await load()
}

async function remove(row) {
  await ElMessageBox.confirm(`删除「${row.title}」及其子菜单？`, '确认删除', { type: 'warning' })
  await api.delete('/menus/' + row.id)
  ElMessage.success('已删除')
  await load()
  window.dispatchEvent(new Event('menus-changed'))
}

onMounted(load)
</script>

<style scoped>
.menus-page { overflow: hidden; height: 100%; }
.table-wrap { flex: 1; min-height: 0; min-width: 0; }
.ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  white-space: nowrap;
  gap: 4px;
}
.ops :deep(.el-button) {
  margin: 0;
  padding: 0 2px;
}
.muted { color: #c0c4cc; }
.icon-opt { display: inline-flex; align-items: center; gap: 8px; }
.name-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  background: transparent;
  color: #909399;
  cursor: pointer;
  flex-shrink: 0;
}
.toggle:hover { color: #409eff; }
.toggle-placeholder { width: 18px; height: 18px; flex-shrink: 0; }
.icon-placeholder { width: 16px; height: 16px; flex-shrink: 0; }
.name-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.table-wrap :deep(.menu-row.lv-0) { background: #f5f7fa; }
.table-wrap :deep(.menu-row.lv-0 .name-text) { font-weight: 600; color: #303133; }
.table-wrap :deep(.menu-row.lv-1) { background: #fff; }
.table-wrap :deep(.menu-row.lv-1 .name-text) { color: #303133; }
.table-wrap :deep(.menu-row.lv-2) { background: #fafafa; }
.table-wrap :deep(.menu-row.lv-2 .name-text) { color: #606266; }
.table-wrap :deep(.menu-row.lv-3),
.table-wrap :deep(.menu-row.lv-4) { background: #fcfcfc; }
.table-wrap :deep(.menu-row.lv-3 .name-text),
.table-wrap :deep(.menu-row.lv-4 .name-text) { color: #909399; }
</style>
