<template>
  <div class="gray-page archive-scroll">
    <div class="page-head">
      <h1>任务列表</h1>
      <div class="toolbar">
        <el-button type="primary" @click="openCreate()">新建任务</el-button>
        <el-button @click="load">刷新</el-button>
      </div>
    </div>

    <el-card shadow="never" class="panel">
      <el-table :data="pageRows" v-loading="loading" empty-text="暂无归档任务">
        <el-table-column label="任务名称" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <button type="button" class="name-link" @click="openView(row)">{{ row.name }}</button>
          </template>
        </el-table-column>
        <el-table-column label="源 -> 目标" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">{{ sourceTarget(row, instances) }}</template>
        </el-table-column>
        <el-table-column label="策略" width="128">
          <template #default="{ row }">
            <el-tag size="small" type="primary">{{ strategyLabel(decodeMeta(row.condition).strategy) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="调度" width="120">
          <template #default="{ row }">{{ scheduleLabel(row) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="审批" width="100">
          <template #default="{ row }">
            <el-button v-if="row.issue_id" link type="primary" @click="$router.push('/ops/tickets/' + row.issue_id)">
              {{ approvalText(row.approval_status) }}
            </el-button>
            <span v-else class="muted">{{ approvalText(row.approval_status) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <div class="ops">
              <el-button link type="primary" @click="openView(row)">查看</el-button>
              <el-button link type="primary" :disabled="row.status === 'running'" @click="openView(row)">编辑</el-button>
              <el-button link type="primary" :disabled="!canRun(row)" @click="run(row)">执行</el-button>
              <el-button link type="warning" :disabled="row.status === 'paused' || !isApproved(row)" @click="pause(row)">停止</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="rows.length"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
        />
      </div>
    </el-card>

    <CreateTaskDialog
      v-model="visible"
      :instances="instances"
      :task="current"
      @saved="load"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'
import CreateTaskDialog from './CreateTaskDialog.vue'
import { approvalText, canRun, decodeMeta, isApproved, scheduleLabel, sourceTarget, statusText, statusType, strategyLabel } from './archive'

const rows = ref([])
const instances = ref([])
const loading = ref(false)
const visible = ref(false)
const current = ref(null)
const page = ref(1)
const pageSize = ref(10)
const pageRows = computed(() => rows.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))

function openCreate() {
  current.value = null
  visible.value = true
}
function openView(row) {
  current.value = row
  visible.value = true
}

async function load() {
  loading.value = true
  try {
    const [taskRes, instRes] = await Promise.all([api.get('/archive/tasks'), api.get('/instances')])
    rows.value = taskRes.data.data || []
    instances.value = instRes.data.data || []
  } finally {
    loading.value = false
  }
}
async function run(row) {
  if (!canRun(row)) {
    ElMessage.warning('归档任务须审批通过后才能执行')
    return
  }
  await api.post(`/archive/tasks/${row.id}/run`)
  ElMessage.success('已触发执行，可在控制台查看本次记录')
  await load()
}
async function pause(row) {
  await ElMessageBox.confirm(`仅停止后续触发，不影响已在执行的任务。确认停止「${row.name}」？`, '停止任务', {
    confirmButtonText: '停止后续触发',
    type: 'warning',
  })
  await api.post(`/archive/tasks/${row.id}/pause`)
  ElMessage.success('已停止后续触发')
  await load()
}
onMounted(load)
</script>

<style scoped>
.archive-scroll { overflow: auto; }
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h1 { margin: 0; font-size: 20px; font-weight: 600; color: #111827; }
.toolbar { display: flex; gap: 8px; }
.panel { border-radius: 8px; }
.name-link {
  border: 0;
  background: none;
  padding: 0;
  color: #1677ff;
  cursor: pointer;
}
.name-link:hover { text-decoration: underline; }
.muted { color: #9ca3af; }
.ops { display: inline-flex; flex-wrap: nowrap; gap: 4px; white-space: nowrap; }
.ops :deep(.el-button) { margin: 0; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
