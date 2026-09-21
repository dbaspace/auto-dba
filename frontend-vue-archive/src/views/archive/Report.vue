<template>
  <div class="gray-page">
    <div class="toolbar">
      <el-radio-group v-model="range">
        <el-radio-button label="today">今日</el-radio-button>
        <el-radio-button label="week">本周</el-radio-button>
        <el-radio-button label="month">本月</el-radio-button>
      </el-radio-group>
      <el-button @click="load">刷新</el-button>
    </div>
    <el-row :gutter="16" class="mb">
      <el-col :xs="24" :sm="12" :lg="6" v-for="k in kpis" :key="k.label">
        <div class="kpi"><div class="kpi-ico" :style="{ background: k.bg }">{{ k.mark }}</div><div><div class="kpi-num">{{ k.value }}</div><div class="kpi-label">{{ k.label }}</div></div></div>
      </el-col>
    </el-row>
    <el-card shadow="never">
      <template #header>任务明细</template>
      <el-table :data="tasks" empty-text="暂无数据">
        <el-table-column prop="name" label="任务" />
        <el-table-column prop="source_db" label="源库" width="140" />
        <el-table-column prop="table_name" label="表" width="140" />
        <el-table-column prop="status" label="状态" width="100" />
        <el-table-column prop="created_at" label="时间" width="180" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import api from '../../api/client'

const range = ref('week')
const stats = ref({})
const tasks = ref([])
const kpis = computed(() => [
  { label: '总任务', value: stats.value.total_tasks ?? 0, bg: '#1677ff', mark: '☰' },
  { label: '成功率', value: stats.value.success_rate ?? '-', bg: '#52c41a', mark: '✓' },
  { label: '失败', value: stats.value.failed_today ?? 0, bg: '#f5222d', mark: '!' },
  { label: '执行中', value: stats.value.running ?? 0, bg: '#fa8c16', mark: '▶' },
])
async function load() {
  const data = (await api.get('/archive/stats')).data.data || {}
  stats.value = data
  tasks.value = (await api.get('/archive/tasks')).data.data || []
}
onMounted(load)
</script>

<style scoped>
.mb { margin-bottom: 16px; }
.kpi { display: flex; align-items: center; gap: 12px; background: #fff; border: 1px solid #f0f0f0; border-radius: 8px; padding: 16px; margin-bottom: 16px; }
.kpi-ico { width: 44px; height: 44px; border-radius: 8px; color: #fff; display: flex; align-items: center; justify-content: center; }
.kpi-num { font-size: 22px; font-weight: 600; }
.kpi-label { color: #909399; font-size: 13px; }
</style>
