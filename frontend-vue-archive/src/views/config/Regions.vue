<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">新增区域</el-button>
    </div>
    <p class="hint">区域编码唯一。同一区域（如 beijing）请编辑后勾选多个环境，不要重复新增。</p>
    <div class="ops-table">
      <el-table :data="rows" border height="100%">
        <el-table-column prop="code" label="编码" width="140" />
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column label="绑定环境" min-width="220">
          <template #default="{ row }">{{ envText(row.id) }}</template>
        </el-table-column>
        <el-table-column prop="description" label="说明" min-width="180" show-overflow-tooltip />
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="openBind(row)">绑定环境</el-button>
            <el-button text type="primary" @click="open(row)">编辑</el-button>
            <el-button text type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <el-dialog v-model="visible" :title="form.id ? '编辑区域' : '新增区域'" width="480px" append-to-body destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="编码" prop="code">
          <el-input v-model="form.code" :disabled="!!form.id" placeholder="如 default / beijing" />
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如 默认 / 北京" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="bindVisible" title="绑定环境" width="480px" append-to-body destroy-on-close>
      <p class="hint">区域「{{ bindForm.name }}」可同时绑定测试、生产等多个环境。</p>
      <el-select v-model="bindForm.env_ids" multiple filterable placeholder="选择环境" style="width:100%">
        <el-option v-for="e in envs" :key="e.id" :label="e.name + ' (' + e.code + ')'" :value="e.id" />
      </el-select>
      <template #footer>
        <el-button @click="bindVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveBind">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'

const rows = ref([])
const envs = ref([])
const bindMap = ref({})
const visible = ref(false)
const bindVisible = ref(false)
const saving = ref(false)
const formRef = ref()
const form = reactive({ id: '', code: '', name: '', description: '' })
const bindForm = reactive({ id: '', name: '', env_ids: [] })
const rules = {
  code: [{ required: true, message: '请填写编码', trigger: 'blur' }],
  name: [{ required: true, message: '请填写名称', trigger: 'blur' }],
}

function envText(regionId) {
  const list = bindMap.value[regionId] || []
  return list.map((e) => e.name || e.code).join('、') || '—'
}

async function load() {
  const [r, e] = await Promise.all([api.get('/regions'), api.get('/environments')])
  rows.value = r.data.data || []
  envs.value = e.data.data || []
  const map = {}
  await Promise.all(rows.value.map(async (row) => {
    map[row.id] = (await api.get(`/regions/${row.id}/envs`)).data.data || []
  }))
  bindMap.value = map
}

function open(row) {
  form.id = row?.id || ''
  form.code = row?.code || ''
  form.name = row?.name || ''
  form.description = row?.description || ''
  showDialog(visible)
}

function openBind(row) {
  bindForm.id = row.id
  bindForm.name = row.name || row.code
  bindForm.env_ids = (bindMap.value[row.id] || []).map((e) => e.id)
  showDialog(bindVisible)
}

async function save() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = { code: form.code, name: form.name, description: form.description }
    if (form.id) await api.put('/regions/' + form.id, payload)
    else await api.post('/regions', payload)
    ElMessage.success('已保存')
    visible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function saveBind() {
  saving.value = true
  try {
    await api.put(`/regions/${bindForm.id}/envs`, { env_ids: bindForm.env_ids })
    ElMessage.success('已绑定')
    bindVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`删除区域「${row.name}」？`, '提示', { type: 'warning' })
  await api.delete('/regions/' + row.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>
