<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="open()">新增参数</el-button>
    </div>
    <p class="hint">CDC / 建库 / 工单约束与实例类型等。修改后立即生效。</p>
    <div class="ops-filters">
      <div class="filter-item">
        <span class="label">关键词：</span>
        <el-input v-model="q" clearable placeholder="搜索键 / 值 / 说明" @keyup.enter="page = 1" />
      </div>
    </div>
    <div class="ops-table">
      <el-table :data="pageRows" border height="100%">
        <el-table-column prop="key" label="键" width="260" show-overflow-tooltip />
        <el-table-column prop="value" label="值" min-width="240" show-overflow-tooltip />
        <el-table-column prop="description" label="说明" min-width="200" show-overflow-tooltip />
        <el-table-column prop="updated_at" label="更新时间" width="180" />
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="open(row)">编辑</el-button>
            <el-button text type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="pager">
      <el-pagination v-model:current-page="page" :page-size="20" :total="filtered.length" layout="total, prev, pager, next" />
    </div>
    <el-dialog v-model="visible" :title="editing ? '编辑参数' : '新增参数'" width="560px" append-to-body destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="键" prop="key">
          <el-input v-model="form.key" :disabled="editing" placeholder="如 ticket_instance_types" />
        </el-form-item>
        <el-form-item label="值" prop="value">
          <el-input v-model="form.value" type="textarea" :rows="4" placeholder="字符串或 JSON" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="可选" />
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
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import { showDialog } from '../../utils/overlay'

const rows = ref([])
const q = ref('')
const page = ref(1)
const visible = ref(false)
const editing = ref(false)
const saving = ref(false)
const formRef = ref()
const form = reactive({ key: '', value: '', description: '' })
const rules = {
  key: [{ required: true, message: '请填写键', trigger: 'blur' }],
}

const filtered = computed(() => {
  const s = q.value.trim().toLowerCase()
  if (!s) return rows.value
  return rows.value.filter((r) => `${r.key} ${r.value} ${r.description}`.toLowerCase().includes(s))
})
const pageRows = computed(() => filtered.value.slice((page.value - 1) * 20, page.value * 20))

async function load() {
  const { data } = await api.get('/global-args')
  rows.value = data.data || []
}

function open(row) {
  editing.value = !!row
  form.key = row?.key || ''
  form.value = row?.value || ''
  form.description = row?.description || ''
  showDialog(visible)
}

async function save() {
  await formRef.value?.validate()
  saving.value = true
  try {
    if (editing.value) await api.put('/global-args', { ...form })
    else await api.post('/global-args', { ...form })
    ElMessage.success('已保存')
    visible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`删除参数「${row.key}」？`, '提示', { type: 'warning' })
  await api.delete('/global-args/' + encodeURIComponent(row.key))
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>

<style scoped>
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
