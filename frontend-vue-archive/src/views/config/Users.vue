<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">新建用户</el-button>
    </div>
    <div class="ops-table">
      <el-table :data="rows" border height="100%">
        <el-table-column prop="username" label="用户名" />
        <el-table-column prop="display_name" label="显示名" />
        <el-table-column prop="role" label="角色" width="120" />
        <el-table-column label="来源" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.source === 'ldap' ? 'warning' : 'info'">
              {{ row.source === 'ldap' ? 'LDAP' : '平台' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button text type="primary" @click="open(row)">改角色</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <el-dialog v-model="visible" :title="form.id ? '编辑用户' : '新建用户'" width="420px" append-to-body destroy-on-close>
      <el-form :model="form" label-width="90px">
        <el-form-item label="用户名">
          <el-input v-model="form.username" :disabled="!!form.id" />
        </el-form-item>
        <el-form-item v-if="!form.id" label="密码">
          <el-input v-model="form.password" type="password" show-password />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="form.display_name" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role_id" style="width:100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
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
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'

const rows = ref([])
const roles = ref([])
const visible = ref(false)
const form = reactive({ id: '', username: '', password: '', display_name: '', role_id: '' })

async function load() {
  rows.value = (await api.get('/users')).data.data || []
  roles.value = (await api.get('/roles')).data.data || []
}

function open(row) {
  Object.assign(form, row
    ? { id: row.id, username: row.username, password: '', display_name: row.display_name, role_id: row.role_id }
    : { id: '', username: '', password: '', display_name: '', role_id: roles.value[0]?.id || '' })
  showDialog(visible)
}

async function save() {
  if (!form.id && !form.username) {
    ElMessage.warning('请输入用户名')
    return
  }
  if (form.id) await api.put('/users/' + form.id, form)
  else await api.post('/users', form)
  ElMessage.success('已保存')
  visible.value = false
  await load()
}

onMounted(load)
</script>
