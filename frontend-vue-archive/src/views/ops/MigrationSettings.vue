<template>
  <div class="gray-page mig-settings">
    <div class="ops-title">
      <h1>迁移设置</h1>
      <el-button @click="$router.push('/ops/migration')">返回列表</el-button>
    </div>

    <div class="form-narrow">
      <el-card shadow="never" class="panel">
        <template #header>
          <div class="card-head">
            <span>迁移审批（global-args）</span>
            <el-button text type="primary" :loading="loading" @click="load">刷新</el-button>
          </div>
        </template>
        <el-form label-position="top">
          <el-form-item label="开启迁移审批">
            <el-switch v-model="approval.approval_enabled" />
          </el-form-item>
          <el-form-item label="审批人">
            <div class="hint mb">值为登录用户名，逗号分隔。创建任务时从上述名单选择审核人。</div>
            <el-input
              v-model="approval.approver_user_ids"
              type="textarea"
              :rows="2"
              placeholder="如 xusong, chenjiachi"
            />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="savingApproval" @click="saveApproval">保存</el-button>
          </el-form-item>
        </el-form>
      </el-card>

      <el-card shadow="never" class="panel">
        <template #header><span>OSS 配置</span></template>
        <el-form label-position="top">
          <el-form-item label="OSS 访问地址">
            <el-input v-model="oss.endpoint" placeholder="https://oss-cn-hangzhou.aliyuncs.com" />
          </el-form-item>
          <el-form-item label="存储桶名称">
            <el-input v-model="oss.bucket" />
          </el-form-item>
          <el-form-item label="文件路径前缀">
            <el-input v-model="oss.prefix" placeholder="migration" />
          </el-form-item>
          <el-form-item label="AccessKey ID">
            <el-input v-model="oss.access_key" type="password" show-password placeholder="脱敏展示，修改请重新输入" autocomplete="new-password" />
          </el-form-item>
          <el-form-item label="AccessKey Secret">
            <el-input v-model="oss.secret_key" type="password" show-password placeholder="脱敏展示，修改请重新输入" autocomplete="new-password" />
          </el-form-item>
          <el-form-item label="临时文件保留天数">
            <el-input-number v-model="oss.retention_days" :min="1" :max="3650" style="width:100%" />
          </el-form-item>
          <el-form-item label="单文件分片大小 (MB)">
            <el-input-number v-model="oss.chunk_size_mb" :min="1" :max="10240" style="width:100%" />
          </el-form-item>
          <el-form-item>
            <div class="actions">
              <el-button :loading="testing" @click="testOSS">连接测试</el-button>
              <el-button type="primary" :loading="savingOSS" @click="saveOSS">保存</el-button>
            </div>
          </el-form-item>
        </el-form>
      </el-card>

      <el-card shadow="never" class="panel last">
        <template #header><span>全局性能参数</span></template>
        <el-form label-position="top">
          <el-form-item v-for="item in perfFields" :key="item.key">
            <template #label>
              <span>{{ item.label }}</span>
              <el-tooltip content="此为全局默认值，可在创建迁移任务时按需覆盖" placement="top">
                <span class="help-dot">?</span>
              </el-tooltip>
            </template>
            <el-input-number v-model="perf[item.key]" :min="item.min" :max="item.max" style="width:100%" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="savingPerf" @click="savePerf">保存</el-button>
          </el-form-item>
        </el-form>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../api/client'

const loading = ref(false)
const savingApproval = ref(false)
const savingOSS = ref(false)
const savingPerf = ref(false)
const testing = ref(false)
const testOk = ref(false)

const approval = reactive({ approval_enabled: true, approver_user_ids: '' })
const oss = reactive(emptyOSS())
const perf = reactive(emptyPerf())

const perfFields = [
  { key: 'max_concurrent_tasks', label: '最大并发任务数', min: 1, max: 10 },
  { key: 'table_parallelism', label: '默认多表并行度', min: 1, max: 16 },
  { key: 'batch_size', label: '默认批次大小', min: 100, max: 100000 },
  { key: 'single_table_timeout_min', label: '单表迁移超时（分钟）', min: 1, max: 1440 },
  { key: 'default_speed_limit', label: '默认限速（行/秒）', min: 0, max: 1000000 },
  { key: 'schedule_tolerance_min', label: '定时触发容忍窗口（分钟）', min: 1, max: 60 },
]

function emptyOSS() {
  return {
    endpoint: '',
    bucket: '',
    prefix: 'migration',
    access_key: '',
    secret_key: '',
    retention_days: 7,
    chunk_size_mb: 100,
  }
}

function emptyPerf() {
  return {
    max_concurrent_tasks: 3,
    table_parallelism: 4,
    batch_size: 10000,
    single_table_timeout_min: 30,
    default_speed_limit: 0,
    schedule_tolerance_min: 10,
  }
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/migration/settings')
    const row = data.data || {}
    approval.approval_enabled = row.approval_enabled !== false
    approval.approver_user_ids = row.approver_user_ids || ''
    Object.assign(oss, emptyOSS(), row.oss || {})
    Object.assign(perf, emptyPerf(), row.performance || {})
    testOk.value = false
  } finally {
    loading.value = false
  }
}

async function saveApproval() {
  savingApproval.value = true
  try {
    await api.put('/migration/settings', {
      approval_enabled: approval.approval_enabled,
      approver_user_ids: approval.approver_user_ids,
    })
    ElMessage.success('已保存')
    await load()
  } finally {
    savingApproval.value = false
  }
}

function ossBody() {
  return {
    endpoint: oss.endpoint,
    bucket: oss.bucket,
    prefix: oss.prefix,
    access_key: oss.access_key,
    secret_key: oss.secret_key,
    retention_days: oss.retention_days,
    chunk_size_mb: oss.chunk_size_mb,
  }
}

async function testOSS() {
  testing.value = true
  try {
    const { data } = await api.post('/migration/oss/test', ossBody(), { timeout: 20000 })
    testOk.value = Boolean(data.ok || data.success || data.connected)
    ElMessage[testOk.value ? 'success' : 'error'](data.message || data.error_message || (testOk.value ? 'OSS 连接成功' : '连接失败'))
  } catch (e) {
    testOk.value = false
    ElMessage.error(e?.response?.data?.message || e.message || '连接失败')
  } finally {
    testing.value = false
  }
}

async function saveOSS() {
  if (!testOk.value) {
    try {
      await ElMessageBox.confirm('尚未通过连接测试，仍要保存当前 OSS 配置吗？', '尚未通过连接测试', {
        confirmButtonText: '仍要保存',
        cancelButtonText: '取消',
        type: 'warning',
      })
    } catch {
      return
    }
  }
  savingOSS.value = true
  try {
    await api.put('/migration/settings', { oss: ossBody() })
    ElMessage.success('已保存')
    testOk.value = false
    await load()
  } finally {
    savingOSS.value = false
  }
}

async function savePerf() {
  savingPerf.value = true
  try {
    await api.put('/migration/settings', { performance: { ...perf } })
    ElMessage.success('已保存')
    await load()
  } finally {
    savingPerf.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.mig-settings { overflow: auto !important; }
.ops-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}
.ops-title h1 { margin: 0; font-size: 20px; font-weight: 600; color: #111827; }
.panel { margin-bottom: 16px; border-radius: 8px; }
.panel.last { margin-bottom: 0; }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.hint { color: #6b7280; font-size: 12px; line-height: 1.6; }
.mb { margin-bottom: 8px; }
.actions { display: flex; gap: 8px; }
.help-dot {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  margin-left: 6px;
  border-radius: 50%;
  background: #d1d5db;
  color: #fff;
  font-size: 10px;
  cursor: help;
  vertical-align: middle;
}
</style>
