<template>
  <div class="ops-page">
    <div class="ops-title">
      <div class="toolbar" style="margin:0">
        <el-button type="primary" @click="collect">采集</el-button>
        <el-button @click="load">刷新</el-button>
      </div>
    </div>
    <p class="hint">独立部署按本地 postgres 实例写入占位槽；连接真实库后可替换为 pg_replication_slots 采集。</p>
    <div class="ops-table">
      <el-table :data="rows" border height="100%">
        <el-table-column prop="host" label="主机" min-width="160" />
        <el-table-column prop="port" label="端口" width="80" />
        <el-table-column prop="slot_name" label="槽名" min-width="160" />
        <el-table-column prop="database" label="库" min-width="120" />
        <el-table-column prop="active" label="活跃" width="80" />
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'

const rows = ref([])
async function load() {
  rows.value = (await api.get('/sync-slots')).data.data || []
}
async function collect() {
  const { data } = await api.post('/sync-slots/collect')
  ElMessage.info(data.message || '已采集')
  await load()
}
async function remove(row) {
  await api.post('/sync-slots/delete', { id: row.id })
  await load()
}
onMounted(load)
</script>
