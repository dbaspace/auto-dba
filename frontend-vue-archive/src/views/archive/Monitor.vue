<template>
  <div class="gray-page archive-scroll" v-loading="loading">
    <div class="page-head">
      <h1>监控与告警</h1>
      <p>实时任务状态、性能指标与告警规则。</p>
    </div>

    <el-row :gutter="16" class="mb">
      <el-col v-for="item in kpis" :key="item.label" :xs="24" :sm="8">
        <div class="kpi-card">
          <div class="kpi-ico" :style="{ background: item.bg }">{{ item.mark }}</div>
          <div>
            <div class="kpi-num">{{ item.value }}</div>
            <div class="kpi-label">{{ item.label }}</div>
          </div>
        </div>
      </el-col>
    </el-row>

    <el-card shadow="never" class="panel mb">
      <template #header>
        <div class="card-head">
          <span class="card-title">告警规则</span>
          <el-button text type="primary" @click="openCreate">新增规则</el-button>
        </div>
      </template>
      <div v-if="!rules.length" class="empty">暂无告警规则</div>
      <ul v-else class="rule-list">
        <li v-for="rule in rules" :key="rule.id">
          <span class="rule-left">
            <i class="dot" :class="rule.status === 'triggered' ? 'dot-danger' : 'dot-ok'" />
            <span class="rule-title">{{ ruleTitle(rule) }}</span>
          </span>
          <span class="rule-ops">
            <el-button link type="primary" @click="openEdit(rule)">编辑</el-button>
            <el-button link type="danger" @click="removeRule(rule)">删除</el-button>
            <el-tag size="small" :type="rule.status === 'triggered' ? 'danger' : 'success'">
              {{ rule.status === 'triggered' ? '已触发' : '正常' }}
            </el-tag>
          </span>
        </li>
      </ul>
    </el-card>

    <el-card shadow="never" class="panel">
      <template #header>
        <span class="card-title">最近告警</span>
      </template>
      <div v-if="!alerts.length" class="empty">暂无最近告警</div>
      <ul v-else class="rule-list">
        <li v-for="alert in alerts" :key="alert.id">
          <span class="alert-text">{{ [alert.time, alert.message].filter(Boolean).join(' ') }}</span>
          <span class="rule-ops">
            <el-button v-if="!alert.acked" link type="primary" @click="ack(alert)">确认</el-button>
            <el-tag size="small" :type="severityType(alert.severity)">{{ severityText(alert.severity) }}</el-tag>
          </span>
        </li>
      </ul>
    </el-card>

    <el-dialog
      v-model="ruleOpen"
      :title="editing ? '编辑规则' : '新增规则'"
      width="480px"
      append-to-body
      destroy-on-close
    >
      <el-form :model="form" label-position="top">
        <el-form-item label="规则名称" required>
          <el-input v-model="form.title" maxlength="64" placeholder="如：任务执行超时" />
        </el-form-item>
        <el-form-item label="阈值" required>
          <el-input-number v-model="form.threshold" :min="0" style="width:100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ruleOpen = false">取消</el-button>
        <el-button type="primary" @click="saveRule">{{ editing ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

const KEY = 'auto-dba-archive-monitor'
const loading = ref(false)
const ov = ref({})
const rules = ref([])
const alerts = ref([])
const ruleOpen = ref(false)
const editing = ref(null)
const form = reactive({ title: '', threshold: 3 })
const kpis = computed(() => [
  { label: '运行中', value: ov.value.running ?? 0, bg: '#1677ff', mark: '▶' },
  { label: '今日完成', value: ov.value.completed_today ?? 0, bg: '#52c41a', mark: '✓' },
  { label: '今日失败', value: ov.value.failed_today ?? 0, bg: '#f5222d', mark: '✕' },
])

function ruleTitle(rule) {
  if (rule.title && rule.threshold != null) return `${rule.title} > ${rule.threshold}`
  return rule.title || rule.id
}
function severityText(s) {
  return { critical: '严重', warning: '警告', info: '提示' }[s] || '提示'
}
function severityType(s) {
  return { critical: 'danger', warning: 'warning', info: '' }[s] || ''
}
function persist() {
  localStorage.setItem(KEY, JSON.stringify({ rules: rules.value, alerts: alerts.value }))
}

function openCreate() {
  editing.value = null
  form.title = ''
  form.threshold = 3
  ruleOpen.value = true
}
function openEdit(rule) {
  editing.value = rule
  form.title = rule.title
  form.threshold = Number(rule.threshold) || 0
  ruleOpen.value = true
}
function saveRule() {
  if (!form.title.trim()) {
    ElMessage.warning('请输入规则名称')
    return
  }
  if (editing.value) {
    Object.assign(editing.value, { title: form.title.trim(), threshold: form.threshold })
    ElMessage.success('规则已更新')
  } else {
    rules.value.push({
      id: String(Date.now()),
      title: form.title.trim(),
      rule_type: form.title.trim(),
      threshold: form.threshold,
      status: 'normal',
    })
    ElMessage.success('规则已创建')
  }
  persist()
  ruleOpen.value = false
}
async function removeRule(rule) {
  await ElMessageBox.confirm(`确定删除「${rule.title || rule.id}」？`, '删除规则', { type: 'warning' })
  rules.value = rules.value.filter((r) => r.id !== rule.id)
  persist()
  ElMessage.success('规则已删除')
}
function ack(alert) {
  alert.acked = true
  persist()
  ElMessage.success('告警已确认')
}

onMounted(async () => {
  loading.value = true
  try {
    const st = (await api.get('/archive/stats')).data.data || {}
    ov.value = { running: st.running, completed_today: st.completed_today, failed_today: st.failed_today }
    try {
      const saved = JSON.parse(localStorage.getItem(KEY) || '{}')
      rules.value = saved.rules || []
      alerts.value = saved.alerts || []
    } catch { /* ignore */ }
    if ((st.failed_today || 0) > 0 && !alerts.value.some((a) => a.message?.includes('今日失败'))) {
      alerts.value = [{
        id: String(Date.now()),
        time: new Date().toLocaleString(),
        message: `今日失败任务 ${st.failed_today} 个`,
        severity: 'warning',
        acked: false,
      }, ...alerts.value].slice(0, 20)
      persist()
    }
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.archive-scroll { overflow: auto; }
.page-head { margin-bottom: 16px; }
.page-head h1 { margin: 0; font-size: 20px; font-weight: 600; color: #111827; }
.page-head p { margin: 6px 0 0; color: #6b7280; font-size: 13px; }
.mb { margin-bottom: 16px; }
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
  flex-shrink: 0;
}
.kpi-num { font-size: 22px; font-weight: 600; line-height: 1; }
.kpi-label { color: #6b7280; font-size: 13px; margin-top: 6px; }
.panel { border-radius: 8px; }
.card-head { display: flex; justify-content: space-between; align-items: center; }
.card-title { font-weight: 500; }
.empty { color: #9ca3af; font-size: 13px; padding: 8px 0; }
.rule-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
.rule-list li { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.rule-left { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
.dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
.dot-ok { background: #3b82f6; }
.dot-danger { background: #ef4444; }
.rule-title, .alert-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #1f2937; }
.rule-ops { display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0; }
</style>
