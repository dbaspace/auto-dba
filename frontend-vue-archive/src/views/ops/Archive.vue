<template>
  <div class="page-card">
    <div class="toolbar">
      <el-button type="primary" @click="visible=true">新建归档</el-button>
    </div>
    <el-table :data="rows">
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="status" label="状态" width="110" />
      <el-table-column prop="source_db" label="源库" />
      <el-table-column prop="table_name" label="表" />
      <el-table-column prop="condition" label="条件" />
      <el-table-column label="操作" width="120">
        <template #default="{ row }">
          <el-button text @click="run(row)">运行</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-dialog v-model="visible" title="归档任务" width="480px" append-to-body destroy-on-close>
      <el-form :model="form" label-width="90px">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="实例">
          <el-select v-model="form.source_instance_id" style="width:100%">
            <el-option v-for="i in instances" :key="i.id" :label="i.name" :value="i.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="库"><el-input v-model="form.source_db" /></el-form-item>
        <el-form-item label="表"><el-input v-model="form.table_name" /></el-form-item>
        <el-form-item label="条件"><el-input v-model="form.condition" placeholder="created_at < now() - interval '90 day'" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button type="primary" @click="create">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import api from '../../api/client'

const rows = ref([])
const instances = ref([])
const visible = ref(false)
const form = reactive({ name: '', source_instance_id: '', source_db: '', table_name: '', condition: '' })

async function load() {
  rows.value = (await api.get('/archive/tasks')).data.data || []
  instances.value = (await api.get('/instances')).data.data || []
}
async function create() {
  await api.post('/archive/tasks', form)
  visible.value = false
  await load()
}
async function run(row) {
  await api.post(`/archive/tasks/${row.id}/run`)
  await load()
}
onMounted(load)
</script>
