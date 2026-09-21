<template>
  <div class="ops-page ticket-fit">
    <div class="ops-filters">
      <div class="filter-item">
        <span class="label">工单类型：</span>
        <el-select v-model="filters.type" clearable placeholder="全部">
          <el-option v-for="t in types" :key="t.value" :label="t.label" :value="t.value" />
        </el-select>
      </div>
      <div class="filter-item">
        <span class="label">状态：</span>
        <el-select v-model="filters.status" clearable placeholder="请选择状态">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
      </div>
      <div class="filter-item">
        <span class="label">关键词：</span>
        <el-input v-model="filters.q" placeholder="请输入单号/库名" @keyup.enter="load" />
      </div>
      <div class="toolbar" style="margin:0; flex-shrink:0">
        <el-button type="primary" @click="load">查询</el-button>
        <el-button @click="reset">重置</el-button>
        <el-button @click="goCreate('ddl')">DDL 提单</el-button>
        <el-button @click="goCreate('dml')">DML 提单</el-button>
        <el-button @click="$router.push('/ops/dql')">DQL 导出</el-button>
      </div>
    </div>
    <p class="ticket-hint">已提交工单超过 8 小时仍未审核完成，系统将自动取消。</p>
    <div class="ops-table">
      <el-table :data="rows" border height="100%" v-loading="loading">
        <el-table-column prop="ticket_no" label="单号" width="160" />
        <el-table-column label="类型" width="110">
          <template #default="{ row }">{{ typeText(row.issue_type) }}</template>
        </el-table-column>
        <el-table-column prop="db_name" label="库" min-width="120" show-overflow-tooltip />
        <el-table-column prop="submitter_name" label="提交人" width="120" />
        <el-table-column prop="assigned_to" label="审批人" width="120" />
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="200" />
        <el-table-column label="操作" width="90" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="$router.push('/ops/tickets/' + row.id)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../../api/client'
import { navigateToWorkOrderCreate } from '../../utils/workOrderDraft'

const route = useRoute()
const router = useRouter()

const types = [
  { value: 'ddl', label: 'DDL' },
  { value: 'dml', label: '数据库操作' },
  { value: 'dql', label: '数据导出' },
  { value: 'dcl', label: '账号权限' },
  { value: 'kafka', label: 'Kafka' },
  { value: 'cdc_sync', label: 'CDC' },
  { value: 'archive', label: '数据归档' },
  { value: 'flashback', label: '闪回' },
  { value: 'migration', label: '数据迁移' },
  { value: 'mullang', label: '多语模版' },
  { value: 'createdb', label: '新增数据库' },
]
const statuses = [
  { value: 'pending', label: '待审批' },
  { value: 'approved', label: '已通过' },
  { value: 'rejected', label: '已驳回' },
  { value: 'canceled', label: '已撤销' },
  { value: 'executed', label: '已执行' },
  { value: 'failed', label: '执行失败' },
]
const filters = reactive({ type: String(route.query.type || ''), status: '', q: '' })
const rows = ref([])
const loading = ref(false)

function typeText(t) {
  return { ddl: 'DDL', dml: 'DML', dql: '数据导出', dcl: '权限申请', kafka: 'Kafka', cdc_sync: 'CDC', archive: '数据归档', flashback: '闪回', migration: '数据迁移', mullang: '多语模版', createdb: '新增数据库' }[t] || t || '-'
}
function statusText(s) {
  return { pending: '待审批', approved: '已通过', rejected: '已驳回', canceled: '已撤销', executed: '已执行', failed: '执行失败' }[s] || s || '-'
}
function statusType(s) {
  if (s === 'approved' || s === 'executed') return 'success'
  if (s === 'rejected' || s === 'failed') return 'danger'
  if (s === 'pending') return 'warning'
  return 'info'
}
async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/tickets', { params: filters })
    rows.value = data.data || []
  } finally {
    loading.value = false
  }
}
function reset() {
  filters.type = ''
  filters.status = ''
  filters.q = ''
  load()
}
function goCreate(type) {
  navigateToWorkOrderCreate(router, type)
}
onMounted(load)
</script>

<style scoped>
.ticket-hint {
  margin: 0 0 10px;
  color: #b45309;
  font-size: 13px;
}
</style>
