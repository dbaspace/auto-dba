<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click.stop="openRole()">新建角色</el-button>
    </div>
    <el-row :gutter="16" style="flex:1; min-height:0">
      <el-col :span="8" style="height:100%">
        <div class="white-card" style="height:100%; overflow:auto">
        <el-table :data="roles" highlight-current-row @current-change="pickRole" border>
          <el-table-column prop="name" label="名称" />
          <el-table-column prop="code" label="编码" width="120" />
        </el-table>
        </div>
      </el-col>
      <el-col :span="16" style="height:100%">
        <div class="white-card" style="height:100%; overflow:auto">
        <div v-if="current">
          <p class="hint">为「{{ current.name }}」勾选可访问菜单，保存后对应用户重新登录或刷新即可生效。</p>
          <el-tree
            ref="treeRef"
            :data="tree"
            node-key="id"
            show-checkbox
            default-expand-all
            :props="{ label: 'title', children: 'children' }"
          />
          <div class="toolbar" style="margin-top:12px">
            <el-button type="primary" @click="saveMenus">保存授权</el-button>
            <el-button @click="openRole(current)">编辑角色</el-button>
          </div>
        </div>
        <p v-else class="hint">请选择左侧角色。</p>
        </div>
      </el-col>
    </el-row>
    <el-dialog v-model="visible" :title="roleForm.id ? '编辑角色' : '新建角色'" width="420px" append-to-body destroy-on-close align-center>
      <el-form :model="roleForm" label-width="80px">
        <el-form-item label="名称"><el-input v-model="roleForm.name" /></el-form-item>
        <el-form-item label="编码"><el-input v-model="roleForm.code" :disabled="!!roleForm.id" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="roleForm.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button type="primary" @click="saveRole">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'

const roles = ref([])
const tree = ref([])
const current = ref(null)
const treeRef = ref()
const visible = ref(false)
const roleForm = reactive({ id: '', name: '', code: '', description: '' })

async function load() {
  roles.value = (await api.get('/roles')).data.data || []
  tree.value = (await api.get('/menus/tree')).data.data || []
}

function openRole(row) {
  Object.assign(roleForm, row ? { ...row } : { id: '', name: '', code: '', description: '' })
  showDialog(visible)
}

async function saveRole() {
  if (roleForm.id) await api.put('/roles/' + roleForm.id, roleForm)
  else await api.post('/roles', roleForm)
  visible.value = false
  ElMessage.success('已保存')
  await load()
}

async function pickRole(row) {
  if (!row) return
  current.value = row
  const { data } = await api.get('/roles/' + row.id + '/menus')
  await nextTick()
  treeRef.value?.setCheckedKeys(data.data || [], false)
}

async function saveMenus() {
  const ids = [
    ...(treeRef.value?.getCheckedKeys() || []),
    ...(treeRef.value?.getHalfCheckedKeys() || []),
  ]
  await api.put('/roles/' + current.value.id + '/menus', { menu_ids: ids })
  ElMessage.success('授权已保存')
}

onMounted(load)
</script>
