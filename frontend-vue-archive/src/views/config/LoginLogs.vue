<template>
  <div class="ops-page">
    <p class="hint">记录每次登录尝试的时间、用户名、来源 IP 与结果。仅管理员可查看。</p>
    <div class="ops-filters">
      <div class="filter-item">
        <el-input v-model="username" clearable placeholder="用户名" style="width:180px" @keyup.enter="reload" />
      </div>
      <div class="filter-item">
        <el-select v-model="success" clearable placeholder="结果" style="width:140px" @change="reload">
          <el-option label="成功" value="true" />
          <el-option label="失败" value="false" />
        </el-select>
      </div>
      <el-button type="primary" @click="reload">查询</el-button>
    </div>
    <div class="ops-table">
      <el-table :data="rows" border height="100%" v-loading="loading">
        <el-table-column prop="logged_at" label="登录时间" width="190">
          <template #default="{ row }">{{ fmtTime(row.logged_at) }}</template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" width="140" />
        <el-table-column prop="display_name" label="显示名" width="140" />
        <el-table-column prop="ip" label="登录 IP" width="160" />
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.success ? 'success' : 'danger'">{{ row.success ? '成功' : '失败' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="说明" min-width="160" show-overflow-tooltip />
        <el-table-column prop="user_agent" label="User-Agent" min-width="240" show-overflow-tooltip />
      </el-table>
    </div>
    <div class="pager">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="load"
      />
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import api from '../../api/client'

const rows = ref([])
const total = ref(0)
const loading = ref(false)
const username = ref('')
const success = ref('')
const page = ref(1)
const pageSize = 20

function fmtTime(v) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/login-logs', {
      params: {
        username: username.value.trim() || undefined,
        success: success.value || undefined,
        page: page.value,
        page_size: pageSize,
      },
    })
    rows.value = data.data || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

onMounted(load)
</script>

<style scoped>
.hint { margin: 0 0 12px; color: #6b7280; font-size: 13px; }
.pager { margin-top: 12px; display: flex; justify-content: flex-end; }
</style>
