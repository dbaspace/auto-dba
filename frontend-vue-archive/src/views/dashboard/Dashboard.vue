<template>
  <div class="dash">
    <p class="hint">总工单 / 分布为平台全量，默认不限时间；我的工单为本人相关。</p>

    <div class="ops-card">
      <div class="ops-title">
        工单运营数据
        <span v-if="stats" class="muted">统计范围内共 {{ stats.total }} 单</span>
      </div>
      <el-skeleton :loading="statsLoading" animated :rows="3">
        <template #default>
          <div v-if="loadError" class="empty-hint">统计接口不可用</div>
          <div v-else class="kpi-grid">
            <div class="kpi">
              <div class="kpi-top"><span>总工单</span><span class="kpi-ico" style="background:#1677ff">▣</span></div>
              <div class="kpi-num">{{ fmt(stats?.total) }}</div>
              <div class="kpi-foot up" v-if="stats?.today_count">+{{ stats.today_count }} 今日新增</div>
              <div class="kpi-foot" v-else>CDC 任务工单 {{ fmt(stats?.cdc_total) }}</div>
            </div>
            <div class="kpi">
              <div class="kpi-top"><span>我的工单</span><span class="kpi-ico" style="background:#597ef7">☺</span></div>
              <div class="kpi-num">{{ fmt(stats?.my_total) }}</div>
              <div class="kpi-foot">我创建/经办/审批</div>
            </div>
            <div class="kpi">
              <div class="kpi-top"><span>待审批</span><span class="kpi-ico" style="background:#fa8c16">⌛</span></div>
              <div class="kpi-num">{{ fmt(pendingCount ?? waitInWindow) }}</div>
              <div class="kpi-foot">平台分布 {{ fmt(waitInWindow) }}</div>
            </div>
            <div class="kpi">
              <div class="kpi-top"><span>已通过/完成</span><span class="kpi-ico" style="background:#52c41a">✓</span></div>
              <div class="kpi-num">{{ fmt(doneCount) }}</div>
              <div class="kpi-foot">{{ share(doneCount) }}</div>
            </div>
            <div class="kpi">
              <div class="kpi-top"><span>执行中</span><span class="kpi-ico" style="background:#13c2c2">▶</span></div>
              <div class="kpi-num">{{ fmt(runningCount) }}</div>
              <div class="kpi-foot">{{ share(runningCount) }}</div>
            </div>
            <div class="kpi">
              <div class="kpi-top"><span>已驳回</span><span class="kpi-ico" style="background:#f5222d">✕</span></div>
              <div class="kpi-num">{{ fmt(rejectedCount) }}</div>
              <div class="kpi-foot">{{ share(rejectedCount) }}</div>
            </div>
            <div class="kpi">
              <div class="kpi-top"><span>失败</span><span class="kpi-ico" style="background:#eb2f96">!</span></div>
              <div class="kpi-num">{{ fmt(failedCount) }}</div>
              <div class="kpi-foot">{{ share(failedCount) }}</div>
            </div>
          </div>
        </template>
      </el-skeleton>

      <div class="workbench">
        <div class="wb-card todo-card">
          <div class="wb-head">
            <span>待办工单 <span v-if="pendingCount" class="muted">（{{ pendingCount }}）</span></span>
            <el-button text type="primary" @click="$router.push('/ops/tickets')">查看全部 &gt;</el-button>
          </div>
          <el-table :data="todos" size="small" v-loading="todoLoading" empty-text="暂无待您审批的工单（含迁移）">
            <el-table-column label="工单" min-width="220">
              <template #default="{ row }">
                <span class="todo-id">{{ shortId(row.id) }}</span>
                {{ row.name || '-' }}
                <el-tag v-if="row.source === 'migration'" size="small" type="primary">数据迁移</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="申请人" width="100">
              <template #default="{ row }">{{ row.creator_name || row.created_by || '-' }}</template>
            </el-table-column>
            <el-table-column label="创建时间" width="150">
              <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120">
              <template #default="{ row }">
                <el-button v-if="canApprove(row)" text type="success" @click="goDetail(row)">通过</el-button>
                <el-button text type="primary" @click="goDetail(row)">查看</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div class="pager" v-if="pendingCount > 10">
            <el-pagination
              small
              layout="total, prev, pager, next"
              :total="pendingCount"
              :page-size="10"
              :current-page="todoPage"
              @current-change="(p) => { todoPage = p; loadTodos() }"
            />
          </div>
        </div>

        <div class="wb-side">
          <div class="wb-card">
            <div class="wb-head">
              <span>实时动态</span>
              <el-button text type="primary" @click="$router.push('/ops/tickets')">更多</el-button>
            </div>
            <div v-loading="actLoading">
              <ul v-if="activities.length" class="act-list">
                <li v-for="(a, i) in pagedActivities" :key="i" @click="goDetail(a)">
                  <span class="act-dot" :class="actTone(a)" />
                  <span class="act-msg">
                    <el-tag v-if="a.source === 'migration'" size="small" type="primary">数据迁移</el-tag>
                    {{ a.message || a.issue_name || '-' }}
                  </span>
                  <span class="act-time">{{ fmtActTime(a.at) }}</span>
                </li>
              </ul>
              <el-empty v-else description="近 3 天暂无动态" :image-size="64" />
              <div class="pager" v-if="activities.length > 10">
                <el-pagination
                  small
                  layout="prev, pager, next"
                  :total="activities.length"
                  :page-size="10"
                  :current-page="actPage"
                  @current-change="(p) => (actPage = p)"
                />
              </div>
            </div>
          </div>

          <div class="wb-card">
            <div class="wb-head"><span>快捷操作</span></div>
            <div class="quick-grid">
              <button type="button" class="quick" @click="createOpen = true">
                <span class="quick-ico">＋</span>
                <span>新建工单</span>
              </button>
              <button
                type="button"
                class="quick"
                :class="{ alert: draftCount > 0 }"
                :aria-label="draftCount > 0 ? `我的草稿（${draftCount} 条未提交本地缓存）` : '我的草稿'"
                @click="draftsOpen = true"
              >
                <span v-if="draftCount" class="quick-badge">{{ draftCount > 99 ? '99+' : draftCount }}</span>
                <span class="quick-ico">✎</span>
                <span>{{ draftCount > 0 ? `我的草稿（${draftCount}）` : '我的草稿' }}</span>
              </button>
              <button type="button" class="quick" @click="$router.push('/ops/tickets')">
                <span class="quick-ico">☰</span>
                <span>工单列表</span>
              </button>
              <button type="button" class="quick" @click="$router.push('/ops/sql-insight')">
                <span class="quick-ico">⌕</span>
                <span>SQL 洞察</span>
              </button>
              <button type="button" class="quick" @click="$router.push('/ops/migration')">
                <span class="quick-ico">⇄</span>
                <span>数据迁移</span>
              </button>
            </div>
          </div>
        </div>
      </div>

      <div class="chart-grid">
        <div class="chart-card">
          <div class="chart-title">状态分布</div>
          <ChartBox v-if="hasStatus" :option="statusPie" height="220px" @click="onStatusClick" />
          <el-empty v-else description="暂无数据" :image-size="56" />
          <p class="chart-tip">点击可筛选状态（再次点击取消）</p>
        </div>
        <div class="chart-card">
          <div class="chart-title">工单类型分布</div>
          <ChartBox v-if="hasType" :option="typeBar" height="220px" @click="onTypeClick" />
          <el-empty v-else description="暂无数据" :image-size="56" />
          <p class="chart-tip">点击可筛选工单类型</p>
        </div>
        <div class="chart-card">
          <div class="chart-title">
            环境概览
            <el-button text type="primary" @click="idcOpen = true">查看明细</el-button>
          </div>
          <ChartBox v-if="hasScope" :option="scopePie" height="220px" @click="onScopeClick" />
          <el-empty v-else description="暂无数据" :image-size="56" />
          <p class="chart-tip">测试 / 生产；点击可筛选环境范围</p>
        </div>
        <div class="chart-card" v-if="(stats?.top_creators || []).length">
          <div class="chart-title">
            申请人 Top10
            <el-button text type="primary" @click="creatorOpen = true">查看明细</el-button>
          </div>
          <ChartBox :option="creatorBar" height="240px" />
        </div>
      </div>

      <div class="chart-card">
        <div class="chart-title">申请趋势（近 30 天）</div>
        <ChartBox v-if="hasTrend" :option="trendLine" height="240px" />
        <el-empty v-else description="近 30 天无申请" :image-size="56" />
      </div>
    </div>

    <el-dialog v-model="createOpen" title="新建工单" width="420px" append-to-body destroy-on-close>
      <p class="muted">请选择工单申请类型</p>
      <div class="create-list">
        <button v-for="opt in createOptions" :key="opt.type || opt.path" type="button" class="create-item" @click="goCreate(opt)">
          {{ opt.label }}
        </button>
      </div>
    </el-dialog>
    <MyDraftsModal v-model:open="draftsOpen" @changed="refreshDraftCount" />
    <el-dialog v-model="idcOpen" title="机房分布" width="800px" append-to-body destroy-on-close>
      <ChartBox v-if="hasIdc" :option="idcBar" height="360px" @click="onIdcClick" />
      <el-empty v-else description="暂无机房分布数据" />
    </el-dialog>
    <el-dialog v-model="creatorOpen" title="申请人 Top10 · 工单分类" width="900px" append-to-body destroy-on-close>
      <ChartBox v-if="creatorStack" :option="creatorStack" height="400px" />
      <el-empty v-else description="暂无工单分类明细" />
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import api from '../../api/client'
import ChartBox from './ChartBox.vue'
import MyDraftsModal from '../../components/MyDraftsModal.vue'
import {
  DRAFTS_CHANGED_EVENT,
  WORK_ORDER_CREATE_OPTIONS,
  allowOpenTypeOrRestore,
  currentDraftOwnerId,
  loadAllWorkOrderDrafts,
} from '../../utils/workOrderDraft'

const TYPE_LABEL = {
  dql: '数据导出', dml: 'DML', ddl: 'DDL', dcl: '权限申请', kafka: 'Kafka',
  mullang: '多语模版', cdc_sync: 'CDC同步', cdc: 'CDC同步', migration: '数据迁移',
  archive: '数据归档', flashback: '闪回', createdb: '新增数据库',
}
const STATUS_TEXT = {
  pending: '待审批', approved: '已通过', executed: '已完成', rejected: '已驳回',
  canceled: '已取消', failed: '失败', running: '执行中', init: '待审批',
}
const STATUS_COLOR = {
  pending: '#faad14', approved: '#1677ff', executed: '#52c41a', rejected: '#cf1322',
  canceled: '#8c8c8c', failed: '#eb2f96', running: '#13c2c2',
}
const STACK_COLORS = ['#1677ff', '#52c41a', '#722ed1', '#fa8c16', '#13c2c2', '#eb2f96', '#2f54eb', '#a0d911']
const createOptions = WORK_ORDER_CREATE_OPTIONS

const router = useRouter()
const filters = reactive({ status: '', issue_type: '', idc: '', idc_scope: '' })
const stats = ref(null)
const statsLoading = ref(false)
const loadError = ref(false)
const todos = ref([])
const pendingCount = ref(0)
const todoPage = ref(1)
const todoLoading = ref(false)
const activities = ref([])
const actLoading = ref(false)
const actPage = ref(1)
const createOpen = ref(false)
const draftsOpen = ref(false)
const draftCount = ref(0)
const idcOpen = ref(false)
const creatorOpen = ref(false)

function fmt(n) {
  return Number(n || 0).toLocaleString('en-US')
}
function sumStatus(keys) {
  const set = new Set(keys)
  return (stats.value?.by_status || []).reduce((a, r) => (set.has(r.status) ? a + (r.count || 0) : a), 0)
}
const waitInWindow = computed(() => sumStatus(['pending']))
const doneCount = computed(() => sumStatus(['executed']))
const runningCount = computed(() => sumStatus(['approved', 'running']))
const rejectedCount = computed(() => sumStatus(['rejected']))
const failedCount = computed(() => sumStatus(['failed']))
function share(part) {
  const t = stats.value?.total || 0
  if (!t) return '0% 占比'
  return `${Math.round((part / t) * 100)}% 占比`
}
function typeLabel(t) {
  return TYPE_LABEL[String(t || '').toLowerCase()] || (t || '').toUpperCase() || t
}
function scopeLabel(s) {
  const k = String(s || '').toLowerCase()
  if (k === 'prod') return '生产'
  if (k === 'test') return '测试'
  return s || '未知'
}
function statusText(s) { return STATUS_TEXT[s] || s || '-' }
function statusType(s) {
  if (s === 'pending' || s === 'init') return 'warning'
  if (s === 'executed') return 'success'
  if (s === 'rejected' || s === 'failed') return 'danger'
  return 'info'
}
function shortId(id) {
  const s = String(id || '')
  if (!s) return '-'
  return s.length <= 8 ? `#${s}` : `#${s.slice(0, 4)}…${s.slice(-4)}`
}
function fmtTime(at) {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
function fmtActTime(at) {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return '-'
  const now = new Date()
  const p = (n) => String(n).padStart(2, '0')
  if (d.toDateString() === now.toDateString()) return `${p(d.getHours())}:${p(d.getMinutes())}`
  const y = new Date(now); y.setDate(now.getDate() - 1)
  if (d.toDateString() === y.toDateString()) return '昨天'
  return `${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
function canApprove(row) {
  return row.status === 'pending' || (row.source === 'migration' && (row.status === 'pending' || row.status === 'init'))
}
function goDetail(row) {
  const id = row.id || row.issue_id
  if (!id) return
  if (row.source === 'migration' || row.issue_type === 'migration') {
    router.push(id ? `/ops/migration/${id}` : '/ops/migration')
    return
  }
  router.push('/ops/tickets/' + id)
}
function actTone(a) {
  const s = String(a.status || '')
  if (a.event_type === 'created') return 'primary'
  if (s === 'failed' || s === 'rejected' || s === 'canceled') return 'danger'
  if (s === 'executed' || s === 'approved') return 'success'
  return 'primary'
}
const pagedActivities = computed(() => activities.value.slice((actPage.value - 1) * 10, actPage.value * 10))

function queryParams() {
  const q = {}
  if (filters.status) q.status = filters.status
  if (filters.issue_type) q.issue_type = filters.issue_type
  if (filters.idc) q.idc = filters.idc
  if (filters.idc_scope) q.idc_scope = filters.idc_scope
  return q
}
function toggle(key, value) {
  if (!value) return
  filters[key] = filters[key] === value ? '' : value
  todoPage.value = 1
  loadAll()
}

async function loadStats() {
  statsLoading.value = true
  loadError.value = false
  try {
    stats.value = (await api.get('/tickets/dashboard/stats', { params: queryParams() })).data.data
  } catch {
    stats.value = null
    loadError.value = true
  } finally {
    statsLoading.value = false
  }
}
async function loadTodos() {
  todoLoading.value = true
  try {
    const data = (await api.get('/tickets/todos', { params: { ...queryParams(), current: todoPage.value, page_size: 10 } })).data.data
    todos.value = data?.list || []
    pendingCount.value = data?.pending_count || 0
  } catch {
    todos.value = []
    pendingCount.value = 0
  } finally {
    todoLoading.value = false
  }
}
async function loadActs() {
  actLoading.value = true
  try {
    activities.value = (await api.get('/tickets/activities', { params: { ...queryParams(), limit: 20 } })).data.data?.list || []
  } catch {
    activities.value = []
  } finally {
    actLoading.value = false
  }
}
function loadAll() {
  loadStats()
  loadTodos()
  loadActs()
}
function refreshDraftCount() {
  draftCount.value = loadAllWorkOrderDrafts(currentDraftOwnerId()).length
}

async function goCreate(opt) {
  const go = () => {
    createOpen.value = false
    router.push(opt.path)
  }
  if (!opt.type) {
    go()
    return
  }
  const ok = await allowOpenTypeOrRestore(opt.type, opt.label, go)
  if (ok) go()
}

const hasStatus = computed(() => (stats.value?.by_status || []).some((x) => x.count > 0))
const hasType = computed(() => (stats.value?.by_issue_type || []).some((x) => x.count > 0))
const hasScope = computed(() => (stats.value?.by_idc_scope || []).some((x) => x.count > 0))
const hasIdc = computed(() => (stats.value?.by_idc || []).some((x) => x.count > 0))
const hasTrend = computed(() => (stats.value?.trend || []).some((x) => x.count > 0))

const statusPie = computed(() => ({
  tooltip: { trigger: 'item' },
  legend: { bottom: 0, type: 'scroll' },
  series: [{
    type: 'pie', radius: ['42%', '68%'], center: ['50%', '42%'],
    label: { formatter: '{b}\n{c}' },
    data: (stats.value?.by_status || []).filter((x) => x.count > 0).map((x) => ({
      name: statusText(x.status), value: x.count, status: x.status,
      itemStyle: { color: STATUS_COLOR[x.status], opacity: filters.status && filters.status !== x.status ? 0.35 : 1 },
    })),
  }],
}))
const typeBar = computed(() => {
  const rows = [...(stats.value?.by_issue_type || [])].sort((a, b) => (b.count || 0) - (a.count || 0))
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: 8, right: 12, top: 12, bottom: 8, containLabel: true },
    xAxis: { type: 'category', data: rows.map((r) => typeLabel(r.issue_type)), axisLabel: { interval: 0, rotate: rows.length > 4 ? 28 : 0, fontSize: 11 } },
    yAxis: { type: 'value', minInterval: 1 },
    series: [{
      type: 'bar', barMaxWidth: 36,
      data: rows.map((r) => ({
        value: r.count, issue_type: r.issue_type,
        itemStyle: { color: '#1677ff', opacity: filters.issue_type && filters.issue_type !== r.issue_type ? 0.3 : 1 },
      })),
    }],
  }
})
const scopePie = computed(() => ({
  tooltip: { trigger: 'item' },
  legend: { bottom: 0, type: 'scroll' },
  series: [{
    type: 'pie', radius: ['35%', '62%'], center: ['50%', '42%'],
    label: { formatter: '{b}\n{c}' },
    data: (stats.value?.by_idc_scope || []).filter((x) => x.count > 0).map((x) => ({
      name: scopeLabel(x.idc_scope), value: x.count, idc_scope: x.idc_scope,
      itemStyle: { opacity: filters.idc_scope && filters.idc_scope !== x.idc_scope ? 0.35 : 1 },
    })),
  }],
}))
const creatorBar = computed(() => {
  const rows = [...(stats.value?.top_creators || [])].sort((a, b) => (b.count || 0) - (a.count || 0)).slice(0, 10)
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: 8, right: 24, top: 8, bottom: 8, containLabel: true },
    xAxis: { type: 'value', minInterval: 1 },
    yAxis: { type: 'category', data: rows.map((r) => r.username || '-').reverse(), axisLabel: { width: 100, overflow: 'truncate', fontSize: 11 } },
    series: [{ type: 'bar', barMaxWidth: 16, itemStyle: { color: '#722ed1' }, data: [...rows].reverse().map((r) => r.count) }],
  }
})
const trendLine = computed(() => {
  const points = stats.value?.trend || []
  return {
    tooltip: { trigger: 'axis' },
    grid: { left: 8, right: 16, top: 16, bottom: 8, containLabel: true },
    xAxis: { type: 'category', data: points.map((p) => p.date), axisLabel: { fontSize: 10, rotate: points.length > 14 ? 30 : 0 } },
    yAxis: { type: 'value', minInterval: 1 },
    series: [{ type: 'line', smooth: true, symbolSize: 6, areaStyle: { opacity: 0.12 }, itemStyle: { color: '#1677ff' }, data: points.map((p) => p.count) }],
  }
})
const idcBar = computed(() => {
  const rows = [...(stats.value?.by_idc || [])].filter((x) => x.count > 0).sort((a, b) => (b.count || 0) - (a.count || 0))
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: 8, right: 16, top: 12, bottom: 8, containLabel: true },
    xAxis: { type: 'category', data: rows.map((r) => r.idc), axisLabel: { interval: 0, rotate: rows.length > 4 ? 28 : 0, fontSize: 11 } },
    yAxis: { type: 'value', minInterval: 1 },
    series: [{
      type: 'bar', barMaxWidth: 36,
      data: rows.map((r) => ({ value: r.count, idc: r.idc, itemStyle: { color: '#13c2c2', opacity: filters.idc && filters.idc !== r.idc ? 0.35 : 1 } })),
    }],
  }
})
const creatorStack = computed(() => {
  const top = [...(stats.value?.top_creators || [])].sort((a, b) => (b.count || 0) - (a.count || 0)).slice(0, 10)
  const types = new Map()
  for (const c of top) for (const t of c.by_issue_type || []) types.set(t.issue_type, (types.get(t.issue_type) || 0) + (t.count || 0))
  const issueTypes = [...types.entries()].sort((a, b) => b[1] - a[1]).map(([t]) => t)
  if (!top.length || !issueTypes.length) return null
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { type: 'scroll', bottom: 0 },
    grid: { left: 8, right: 16, top: 24, bottom: 48, containLabel: true },
    xAxis: { type: 'category', data: top.map((c) => c.username || '-'), axisLabel: { interval: 0, rotate: top.length > 5 ? 28 : 0, fontSize: 11 } },
    yAxis: { type: 'value', minInterval: 1 },
    series: issueTypes.map((issueType, idx) => ({
      name: typeLabel(issueType), type: 'bar', stack: 'issue_type', barMaxWidth: 36,
      itemStyle: { color: STACK_COLORS[idx % STACK_COLORS.length] },
      data: top.map((c) => Number((c.by_issue_type || []).find((t) => t.issue_type === issueType)?.count) || 0),
    })),
  }
})

function onStatusClick(p) { toggle('status', p?.data?.status) }
function onTypeClick(p) { toggle('issue_type', p?.data?.issue_type) }
function onScopeClick(p) {
  if (!p?.data?.idc_scope) return
  filters.idc = ''
  toggle('idc_scope', p.data.idc_scope)
}
function onIdcClick(p) {
  if (!p?.data?.idc) return
  filters.idc_scope = ''
  toggle('idc', p.data.idc)
}

onMounted(() => {
  loadAll()
  refreshDraftCount()
  window.addEventListener(DRAFTS_CHANGED_EVENT, refreshDraftCount)
})
onUnmounted(() => {
  window.removeEventListener(DRAFTS_CHANGED_EVENT, refreshDraftCount)
})
</script>

<style scoped>
.dash { display: flex; flex-direction: column; gap: 12px; min-width: 0; overflow: auto; }
.dash-head { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.dash-title { font-size: 16px; font-weight: 600; color: #303133; }
.dash-sub, .muted { color: #909399; font-size: 12px; font-weight: 400; }
.ops-card { background: #fff; border: 1px solid #f0f0f0; border-radius: 8px; padding: 12px; }
.ops-title { font-weight: 600; margin-bottom: 12px; display: flex; align-items: center; gap: 8px; }
.kpi-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 10px; }
.kpi { background: #fff; border: 1px solid #f0f0f0; border-radius: 12px; padding: 12px; box-shadow: 0 2px 8px rgba(15, 23, 42, 0.06); min-height: 100px; }
.kpi-top { display: flex; justify-content: space-between; color: #909399; font-size: 12px; }
.kpi-ico { width: 28px; height: 28px; border-radius: 50%; color: #fff; display: inline-flex; align-items: center; justify-content: center; font-size: 12px; }
.kpi-num { font-size: 22px; font-weight: 600; color: #303133; margin-top: 8px; }
.kpi-foot { font-size: 11px; color: #8c8c8c; margin-top: 4px; }
.kpi-foot.up { color: #52c41a; }
.workbench { display: grid; grid-template-columns: minmax(0, 2fr) minmax(260px, 1fr); gap: 12px; margin: 12px 0; }
.wb-side { display: flex; flex-direction: column; gap: 12px; }
.wb-card { background: #fff; border: 1px solid #f0f0f0; border-radius: 12px; padding: 12px; box-shadow: 0 2px 8px rgba(15, 23, 42, 0.06); }
.wb-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; font-weight: 600; font-size: 14px; }
.todo-id { color: #909399; margin-right: 6px; }
.pager { display: flex; justify-content: flex-end; margin-top: 8px; }
.act-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
.act-list li { display: flex; gap: 8px; cursor: pointer; font-size: 12px; color: #4e5969; }
.act-dot { width: 22px; height: 22px; border-radius: 50%; flex-shrink: 0; }
.act-dot.primary { background: #1677ff; }
.act-dot.success { background: #52c41a; }
.act-dot.danger { background: #ff4d4f; }
.act-msg { flex: 1; min-width: 0; line-height: 20px; }
.act-time { color: #c0c4cc; flex-shrink: 0; }
.quick-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.quick { position: relative; border: 1px solid #f0f0f0; background: #f7f9fc; border-radius: 8px; padding: 16px 8px; cursor: pointer; display: flex; flex-direction: column; align-items: center; gap: 8px; font-size: 12px; color: #303133; }
.quick:hover { background: #eef3fb; }
.quick.alert { border-color: #ff4d4f; background: #fff1f0; color: #cf1322; font-weight: 500; }
.quick.alert:hover { background: #ffe7e6; }
.quick.alert .quick-ico { background: #ff4d4f; }
.quick-badge { position: absolute; top: 4px; right: 4px; min-width: 16px; height: 16px; padding: 0 4px; border-radius: 8px; background: #ff4d4f; color: #fff; font-size: 10px; line-height: 16px; text-align: center; }
.quick-ico { width: 36px; height: 36px; border-radius: 50%; background: #1677ff; color: #fff; display: flex; align-items: center; justify-content: center; font-size: 16px; }
.chart-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 12px; }
.chart-card { background: #fff; border: 1px solid #f0f0f0; border-radius: 8px; padding: 12px; }
.chart-title { font-weight: 600; font-size: 14px; margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center; }
.chart-tip { color: #909399; font-size: 12px; margin: 4px 0 0; }
.create-list { display: flex; flex-direction: column; gap: 8px; }
.create-item { text-align: left; padding: 10px 12px; border: 1px solid #f0f0f0; background: #f7f9fc; border-radius: 8px; cursor: pointer; }
.create-item:hover { background: #eef3fb; border-color: #91caff; }
.empty-hint { color: #909399; padding: 24px; text-align: center; }
@media (max-width: 1100px) {
  .workbench, .chart-grid { grid-template-columns: 1fr; }
}
</style>
