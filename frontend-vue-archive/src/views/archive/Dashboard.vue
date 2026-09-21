<template>
  <div class="gray-page archive-scroll">
    <el-row :gutter="16" class="mb">
      <el-col v-for="item in kpis" :key="item.label" :xs="24" :sm="12" :lg="6">
        <div class="kpi-card">
          <div class="kpi-ico" :style="{ background: item.bg }">{{ item.mark }}</div>
          <div class="kpi-text">
            <div class="kpi-num">{{ item.value }}</div>
            <div class="kpi-label">{{ item.label }}</div>
          </div>
        </div>
      </el-col>
    </el-row>

    <el-row :gutter="16" class="mb">
      <el-col :xs="24" :lg="16">
        <el-card shadow="never" class="panel">
          <template #header>
            <div class="card-head">
              <span class="card-title">最近执行任务</span>
              <div class="card-extra">
                <el-button text type="primary" @click="load">刷新</el-button>
                <el-button text type="primary" @click="$router.push('/ops/archive/runs')">查看全部 →</el-button>
              </div>
            </div>
          </template>
          <el-table :data="runs" size="small" v-loading="loading" empty-text="暂无最近执行记录">
            <el-table-column label="任务名称" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.task_name || row.name || '-' }}</template>
            </el-table-column>
            <el-table-column label="任务策略" width="128">
              <template #default="{ row }">
                <el-tag size="small" type="primary">{{ strategyLabel(decodeMeta(row.condition).strategy) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="执行方式" width="110">
              <template #default="{ row }">{{ runModeLabel(decodeMeta(row.condition).run_mode) }}</template>
            </el-table-column>
            <el-table-column prop="volume" label="归档量" width="88" />
            <el-table-column prop="duration" label="耗时" width="80" />
            <el-table-column label="工单创建时间" width="168">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="执行时间" width="168">
              <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="80" fixed="right">
              <template #default>
                <el-button text type="primary" @click="$router.push('/ops/archive/tasks')">查看</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
      <el-col :xs="24" :lg="8">
        <el-card shadow="never" class="panel">
          <template #header>
            <div class="card-head">
              <span class="card-title">系统健康</span>
              <el-button text type="primary" @click="$router.push('/ops/archive/monitor')">详情</el-button>
            </div>
          </template>
          <div class="health-row">
            <span>存储使用率</span>
            <span>{{ health.storage_usage > 0 ? health.storage_usage + '%' : '-' }}</span>
          </div>
          <el-progress v-if="health.storage_usage > 0" :percentage="health.storage_usage" :show-text="false" class="mb-sm" />
          <div class="health-row">
            <span>数据库连接</span>
            <el-tag size="small" type="success">{{ health.db_connection || '-' }}</el-tag>
          </div>
          <div class="health-row">
            <span>待审批任务</span>
            <span class="badge badge-warn">{{ health.pending_approval || 0 }}</span>
          </div>
          <div class="health-row">
            <span>调度队列</span>
            <span class="badge badge-info">{{ health.schedule_queue || 0 }}</span>
          </div>
          <div class="health-actions">
            <el-button type="primary" @click="openCreate()">新建任务</el-button>
            <el-button @click="load">刷新</el-button>
            <el-button @click="$router.push('/ops/archive/report')">报表</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="panel">
      <template #header>
        <span class="card-title">快速开始</span>
      </template>
      <div class="quick-grid">
        <button type="button" class="quick-btn" @click="openCreate('backup_target', true)">按时间归档</button>
        <button type="button" class="quick-btn" @click="openCreate('delete_only', true)">清理过期数据</button>
        <button type="button" class="quick-btn" @click="$router.push('/ops/migration/create')">全库迁移</button>
        <button type="button" class="quick-btn" @click="$router.push('/ops/archive/masking')">敏感数据脱敏</button>
      </div>
    </el-card>

    <CreateTaskDialog
      v-model="createOpen"
      :instances="instances"
      :initial-strategy="createStrategy"
      :lock-strategy="lockStrategy"
      @saved="load"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import api from '../../api/client'
import CreateTaskDialog from './CreateTaskDialog.vue'
import { decodeMeta, formatTime, runModeLabel, statusText, statusType, strategyLabel } from './archive'

const loading = ref(false)
const stats = ref({})
const instances = ref([])
const createOpen = ref(false)
const createStrategy = ref('')
const lockStrategy = ref(false)
const runs = computed(() => stats.value.recent_runs || [])
const health = computed(() => ({
  storage_usage: stats.value.storage_usage || 0,
  db_connection: stats.value.db_connection === 'ok' ? '正常' : (stats.value.db_connection || '-'),
  pending_approval: stats.value.pending_approval || 0,
  schedule_queue: stats.value.schedule_queue || 0,
}))
const kpis = computed(() => [
  { label: '总任务数', value: stats.value.total_tasks ?? '-', bg: '#1677ff', mark: '☰' },
  { label: '归档数据量', value: stats.value.archived_volume ?? '-', bg: '#52c41a', mark: '▣' },
  { label: '成功率', value: stats.value.success_rate ?? '-', bg: '#fa8c16', mark: '✓' },
  { label: '待处理告警', value: stats.value.pending_alerts ?? 0, bg: '#f5222d', mark: '!' },
])

function openCreate(strategy = '', lock = false) {
  createStrategy.value = strategy
  lockStrategy.value = lock
  createOpen.value = true
}

async function load() {
  loading.value = true
  try {
    const [st, inst] = await Promise.all([api.get('/archive/stats'), api.get('/instances')])
    stats.value = st.data.data || {}
    instances.value = inst.data.data || []
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<style scoped>
.archive-scroll { overflow: auto; }
.mb { margin-bottom: 16px; }
.mb-sm { margin-bottom: 12px; }
.kpi-card {
  display: flex;
  align-items: center;
  gap: 16px;
  background: #fff;
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 16px;
  box-shadow: 0 1px 2px rgba(0,0,0,.04);
}
.kpi-ico {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
}
.kpi-num { font-size: 22px; font-weight: 600; line-height: 1; color: #111827; }
.kpi-label { color: #6b7280; font-size: 13px; margin-top: 6px; }
.panel { border-radius: 8px; }
.card-head { display: flex; justify-content: space-between; align-items: center; }
.card-title { font-weight: 500; color: #111827; }
.health-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  color: #4b5563;
  margin: 10px 0;
}
.badge {
  min-width: 22px;
  height: 22px;
  padding: 0 7px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
}
.badge-warn { background: #fef3c7; color: #b45309; }
.badge-info { background: #dbeafe; color: #1d4ed8; }
.health-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
.quick-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}
.quick-btn {
  border: 0;
  background: #f5f5f5;
  border-radius: 6px;
  padding: 14px 16px;
  text-align: left;
  cursor: pointer;
  color: #1f2937;
  font-size: 14px;
}
.quick-btn:hover { background: #e5e7eb; }
@media (max-width: 900px) {
  .quick-grid { grid-template-columns: 1fr 1fr; }
}
</style>
