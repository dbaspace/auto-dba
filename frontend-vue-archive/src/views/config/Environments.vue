<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">新增环境</el-button>
    </div>
    <p class="hint">环境可被多个区域绑定。实例上的环境编码会同步到 idc，供工单按环境筛选。</p>
    <div class="ops-table">
      <el-table :data="rows" border height="100%">
        <el-table-column prop="code" label="编码" width="160" />
        <el-table-column prop="name" label="名称" width="180" />
        <el-table-column prop="description" label="说明" min-width="220" show-overflow-tooltip />
        <el-table-column prop="updated_at" label="更新时间" width="180" />
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="open(row)">编辑</el-button>
            <el-button text type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <el-dialog v-model="visible" :title="form.id ? '编辑环境' : '新增环境'" width="480px" append-to-body destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="编码" prop="code">
          <el-input v-model="form.code" :disabled="!!form.id" placeholder="如 test / prod" />
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如 测试 / 生产" />
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
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'

const rows = ref([])
const visible = ref(false)
const saving = ref(false)
const formRef = ref()
const form = reactive({ id: '', code: '', name: '', description: '' })
const rules = {
  code: [{ required: true, message: '请填写编码', trigger: 'blur' }],
  name: [{ required: true, message: '请填写名称', trigger: 'blur' }],
}

async function load() {
  rows.value = (await api.get('/environments')).data.data || []
}

function open(row) {
  form.id = row?.id || ''
  form.code = row?.code || ''
  form.name = row?.name || ''
  form.description = row?.description || ''
  showDialog(visible)
}

async function save() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = { code: form.code, name: form.name, description: form.description }
    if (form.id) await api.put('/environments/' + form.id, payload)
    else await api.post('/environments', payload)
    ElMessage.success('已保存')
    visible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`删除环境「${row.name}」？`, '提示', { type: 'warning' })
  await api.delete('/environments/' + row.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>
