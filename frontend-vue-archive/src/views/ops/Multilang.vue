<template>
  <div class="ops-page">
    <div class="ops-title">
      <el-button type="primary" @click="create">新建模版任务</el-button>
    </div>
    <div class="ops-table">
      <el-table :data="rows" border height="100%">
        <el-table-column prop="name" label="名称" min-width="180" />
        <el-table-column prop="status" label="状态" width="110" />
        <el-table-column prop="template" label="模版" min-width="140" />
        <el-table-column prop="created_at" label="时间" width="200" />
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import api from '../../api/client'

const rows = ref([])
async function load() {
  rows.value = (await api.get('/multilang-template/tasks')).data.data || []
}
async function create() {
  await api.post('/multilang-template/tasks', { name: 'multilang-' + Date.now(), template: 'default', instance_id: '' })
  await load()
}
onMounted(load)
</script>
