<template>
  <div class="ops-page">
    <div class="ops-title">
      <div class="toolbar" style="margin:0">
        <el-button @click="$router.push('/ops/archive/dashboard')">返回控制台</el-button>
        <el-button @click="load">刷新</el-button>
      </div>
    </div>
    <div class="ops-table">
    <el-table :data="runs" v-loading="loading" empty-text="暂无执行记录" border height="100%">
      <el-table-column prop="name" label="任务名称" min-width="160" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.status === 'failed' ? 'danger' : row.status === 'running' ? 'warning' : 'success'">
            {{ { running: '执行中', executed: '成功', success: '成功', failed: '失败', paused: '已停用', init: '待执行' }[row.status] || row.status }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="source_db" label="源库" width="140" />
      <el-table-column prop="table_name" label="表" width="140" />
      <el-table-column prop="volume" label="归档量" width="90" />
      <el-table-column prop="duration" label="耗时" width="90" />
      <el-table-column prop="created_at" label="创建时间" min-width="170" />
      <el-table-column prop="started_at" label="执行时间" min-width="170" />
    </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import api from '../../api/client'

const runs = ref([])
const loading = ref(false)
async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/archive/runs')
    const list = data.data || []
    runs.value = list.length ? list.map((r) => ({
      ...r,
      name: r.task_name || r.name,
      volume: r.volume || `${r.deleted_rows || 0} 行`,
    })) : ((await api.get('/archive/stats')).data.data?.recent_runs || [])
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>
