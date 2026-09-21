<template>
  <div class="fb-shell">
      <section v-if="page === 'dash'" class="dash">
        <div class="dash-kpis">
          <div class="card dash-kpi">
            <h3>总闪回次数</h3>
            <div class="num">{{ dash.total }}</div>
            <p class="foot">今日 {{ dash.today }} 条</p>
          </div>
          <div class="card dash-kpi">
            <h3>成功</h3>
            <div class="num">{{ dash.succeeded }}</div>
            <p class="foot up">✓ 成功率 {{ dash.rate }}%</p>
          </div>
          <div class="card dash-kpi">
            <h3>恢复数据量</h3>
            <div class="num">{{ fmtBytes(dash.walBytes) }}</div>
            <p class="foot">累计 WAL / binlog</p>
          </div>
          <div class="card dash-kpi">
            <h3>待处理任务</h3>
            <div class="num">{{ dash.pending }}</div>
            <p :class="['foot', dash.pending ? 'down' : 'up']">{{ dash.pending ? '队列积压' : '队列空闲' }}</p>
          </div>
        </div>
        <div class="dash-mid">
          <div class="card">
            <div class="dash-hd">
              <h3>闪回趋势（近 7 日）</h3>
              <span class="meta">按日统计</span>
            </div>
            <div v-if="dash.days.some((d) => d.ok || d.fail)" class="dash-bars">
              <div v-for="d in dash.days" :key="d.label" class="dash-col">
                <div class="stack">
                  <div class="seg ok" :style="{ height: barH(d.ok) }" />
                  <div v-if="d.fail" class="seg fail" :style="{ height: barH(d.fail) }" />
                </div>
                <div class="lab">{{ d.label }}</div>
              </div>
            </div>
            <div v-else class="empty">近 7 日暂无任务</div>
            <div class="dash-legend">
              <span><i class="ok" />成功 {{ dash.weekOk }}</span>
              <span><i class="fail" />失败 {{ dash.weekFail }}</span>
            </div>
          </div>
          <div class="card dash-quick">
            <div class="dash-hd"><h3>快速闪回</h3></div>
            <el-form label-position="top" @submit.prevent="quickStart">
              <el-form-item label="数据库类型">
                <div class="chips">
                  <button type="button" class="chip" :class="{ on: engine === 'postgres' }" @click="engine = 'postgres'; onEngineChange()">PG</button>
                  <button type="button" class="chip" :class="{ on: engine === 'mysql' }" @click="engine = 'mysql'; onEngineChange()">MySQL</button>
                </div>
              </el-form-item>
              <el-form-item label="实例">
                <el-select v-model="form.instance_id" filterable placeholder="选择实例" style="width:100%" @change="onInstChange">
                  <el-option v-for="i in engineInstances" :key="i.id" :label="instLabel(i)" :value="i.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="数据库">
                <el-select v-model="form.db_name" filterable allow-create placeholder="业务库名" style="width:100%" @change="onDbChange">
                  <el-option v-for="d in dbOptions" :key="d.id || d.db_name" :label="dbLabel(d)" :value="d.db_name" />
                </el-select>
              </el-form-item>
              <el-form-item label="表">
                <el-input v-model="quickTable" placeholder="schema.table，可空=整库" />
              </el-form-item>
              <el-form-item label="回退到">
                <el-select v-model="quickAgo" style="width:100%">
                  <el-option :value="5" label="5 分钟前" />
                  <el-option :value="10" label="10 分钟前" />
                  <el-option :value="30" label="30 分钟前" />
                  <el-option :value="60" label="1 小时前" />
                  <el-option :value="360" label="6 小时前" />
                </el-select>
              </el-form-item>
              <el-button class="go-btn" type="success" native-type="submit">执行闪回</el-button>
            </el-form>
            <p class="meta">将带入时间窗并进入预检查向导。实例地址来自「配置 → 实例」；云库还须关联云厂商/云实例 ID，并在「多云密钥」配置 AK/SK。</p>
          </div>
        </div>
        <div class="card">
          <div class="dash-hd">
            <h3>最近闪回任务</h3>
            <a class="link" @click.prevent="go('history')">查看全部</a>
          </div>
          <el-table v-if="recentTasks.length" :data="recentTasks" size="small" border>
            <el-table-column label="时间" width="180">
              <template #default="{ row }">
                <a class="link" @click.prevent="openTask(row)">{{ fmtTime(row.created_at) }}</a>
              </template>
            </el-table-column>
            <el-table-column label="表" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">{{ tableText(row) }}</template>
            </el-table-column>
            <el-table-column label="数据库" width="140">
              <template #default="{ row }">{{ row.db_name || row.database }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="rowStatusTag(row)">{{ rowStatusText(row) }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
          <div v-else class="empty">还没有闪回任务。</div>
        </div>
      </section>

      <section v-else-if="page === 'new'" class="card">
        <p class="lead">连接地址从「配置 → 实例」获取，这里只选实例并设定时间窗。云托管库请在实例上填写云厂商 / 云实例 ID / Region，并到「数据闪回 → 多云密钥」配置对应 AK/SK，才能从云平台下载 WAL。</p>
        <div class="steps">
          <div class="step" :class="{ on: wizardStep >= 1 }"><b>1</b> 配置连接</div>
          <div class="step" :class="{ on: wizardStep >= 2 }"><b>2</b> 预检查</div>
          <div class="step" :class="{ on: wizardStep >= 3 }"><b>3</b> 提交审批</div>
        </div>

        <el-form v-if="wizardStep === 1" ref="formRef" :model="form" :rules="rules" label-position="top">
          <div class="field">
            <label>模式</label>
            <div class="chips">
              <button type="button" class="chip" :class="{ on: engine === 'postgres' }" @click="engine = 'postgres'; onEngineChange()">在线 · PostgreSQL</button>
              <button type="button" class="chip" :class="{ on: engine === 'mysql' }" @click="engine = 'mysql'; onEngineChange()">在线 · MySQL</button>
            </div>
          </div>
          <div class="split">
            <div>
              <h3>连接配置</h3>
              <el-form-item label="环境" prop="env">
                <el-select v-model="form.env" clearable placeholder="可选，用于筛选实例" style="width:100%" @change="onEnvChange">
                  <el-option label="测试" value="test" />
                  <el-option label="生产" value="prod" />
                </el-select>
              </el-form-item>
              <el-form-item label="已配置实例 *" prop="instance_id">
                <el-select v-model="form.instance_id" filterable placeholder="请选择实例" style="width:100%" @change="onInstChange">
                  <el-option v-for="i in sourceOptions" :key="i.id" :label="instLabel(i)" :value="i.id" />
                </el-select>
              </el-form-item>
              <div class="addr">{{ addrText }}</div>
              <el-form-item label="数据库名 *" prop="db_name">
                <el-select v-model="form.db_name" filterable allow-create placeholder="业务库名" style="width:100%" @change="onDbChange">
                  <el-option v-for="d in dbOptions" :key="d.id || d.db_name" :label="dbLabel(d)" :value="d.db_name" />
                </el-select>
              </el-form-item>
              <el-form-item label="目标表（空=整库；可多选）">
                <el-select v-model="form.tables" multiple filterable allow-create clearable placeholder="schema.table" style="width:100%">
                  <el-option v-for="t in tableOptions" :key="t" :label="t" :value="t" />
                </el-select>
              </el-form-item>
              <div v-if="engine === 'postgres'" class="info tip">
                <strong>建议：误操作前对目标表设置 REPLICA IDENTITY FULL</strong>
                <p>默认 DEFAULT 时，WAL 里 DELETE/UPDATE 通常只记主键。行被 VACUUM 清掉后，非主键列无法从 WAL 还原。</p>
                <pre>ALTER TABLE schema.table REPLICA IDENTITY FULL;</pre>
              </div>
            </div>
            <div>
              <h3>闪回目标</h3>
              <el-form-item label="起始时间 *（须早于误操作 COMMIT）" prop="start_time">
                <el-date-picker v-model="form.start_time" type="datetime" value-format="YYYY-MM-DD HH:mm:ss" :disabled-date="disableFuture" style="width:100%" />
              </el-form-item>
              <div class="chips">
                <button v-for="c in relChips" :key="c.m" type="button" class="chip" :class="{ on: relMin === c.m }" @click="applyRel(c.m)">{{ c.label }}</button>
              </div>
              <el-form-item label="结束时间（须晚于误操作 COMMIT）" style="margin-top:12px">
                <el-date-picker v-model="form.end_time" type="datetime" value-format="YYYY-MM-DD HH:mm:ss" :disabled-date="disableFuture" style="width:100%" />
              </el-form-item>
              <el-form-item label="输出">
                <el-select v-model="form.output_kind" style="width:100%">
                  <el-option value="flashback" label="Undo SQL（反向闪回）" />
                  <el-option value="original" label="原始 Redo SQL" />
                </el-select>
              </el-form-item>
              <el-form-item label="SQL 类型">
                <div class="chips">
                  <label v-for="t in ['insert','update','delete','ddl']" :key="t" class="chip" :class="{ on: form.sql_types.includes(t) }">
                    <input type="checkbox" :value="t" v-model="form.sql_types" /> {{ t }}
                  </label>
                </div>
              </el-form-item>
              <el-form-item label="审核人" prop="reviewer">
                <el-select v-model="form.reviewer" filterable allow-create placeholder="审批通过后执行" style="width:100%">
                  <el-option v-for="a in reviewerOptions" :key="a" :label="a" :value="a" />
                </el-select>
              </el-form-item>
              <el-collapse>
                <el-collapse-item title="高级位点" name="adv">
                  <el-row :gutter="12">
                    <el-col :span="12"><el-form-item label="start_file"><el-input v-model="form.start_file" /></el-form-item></el-col>
                    <el-col :span="12"><el-form-item label="start_pos"><el-input v-model.number="form.start_pos" /></el-form-item></el-col>
                    <el-col :span="12"><el-form-item label="stop_file"><el-input v-model="form.stop_file" /></el-form-item></el-col>
                    <el-col :span="12"><el-form-item label="stop_pos"><el-input v-model.number="form.stop_pos" /></el-form-item></el-col>
                  </el-row>
                </el-collapse-item>
              </el-collapse>
            </div>
          </div>
          <div class="actions">
            <el-button type="primary" :loading="prechecking" @click="doPrecheck">下一步：预检查</el-button>
          </div>
        </el-form>

        <div v-else-if="wizardStep === 2">
          <div class="info">{{ precheck?.ok ? '预检查通过' : '预检查未通过' }} · {{ precheck?.host || '' }}:{{ precheck?.port || '' }} · {{ precheck?.parse_mode || '' }} · {{ precheck?.server_version || '' }}</div>
          <div class="checks">
            <div v-for="it in precheck?.items || []" :key="it.code || it.name" class="check">
              <el-tag size="small" :type="itemTag(it)">{{ it.status || (it.ok ? 'passed' : 'failed') }}</el-tag>
              <div>
                <strong>{{ it.name }}</strong>
                <div class="meta">{{ it.message }}</div>
              </div>
            </div>
          </div>
          <div class="actions">
            <el-button @click="wizardStep = 1">返回修改</el-button>
            <el-button type="primary" :disabled="!precheck?.ok" @click="goConfirm">下一步：提交审批</el-button>
          </div>
        </div>

        <div v-else>
          <div class="addr">
            实例 <strong>{{ instText({ instance_id: form.instance_id }) }}</strong> · 库 {{ form.db_name }} · 表 {{ form.tables.length ? form.tables.join(', ') : '整库' }}<br>
            时间窗 {{ form.start_time }} → {{ form.end_time || '现在' }} · {{ form.output_kind === 'original' ? '原始 Redo SQL' : 'Undo SQL' }}<br>
            审核人 {{ form.reviewer || '—' }} · 审批通过后按当前配置自动执行
          </div>
          <div class="actions">
            <el-button @click="wizardStep = 2">返回预检查</el-button>
            <el-button type="primary" :loading="creating" :disabled="!form.reviewer" @click="create">提交审批</el-button>
          </div>
        </div>
      </section>

      <section v-else-if="page === 'history'" class="card">
        <div class="toolbar">
          <div class="grow">
            <p class="lead">任务地址来自已配置实例，不在这里改连接。</p>
          </div>
          <el-button type="primary" @click="go('new')">新建任务</el-button>
        </div>
        <div class="toolbar">
          <el-select v-model="histStatus" clearable placeholder="全部状态" style="width:140px">
            <el-option value="awaiting_approval" label="待审批" />
            <el-option value="pending" label="排队" />
            <el-option value="running" label="执行中" />
            <el-option value="succeeded" label="成功" />
            <el-option value="failed" label="失败" />
            <el-option value="rejected" label="已驳回" />
          </el-select>
          <el-input v-model="keyword" clearable placeholder="库名 / 实例 / 任务 ID" style="width:260px" @keyup.enter="histPage = 1" />
          <el-button @click="load">刷新</el-button>
        </div>
        <el-table :data="histRows" v-loading="listLoading" size="small" border empty-text="还没有历史任务">
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="模式" width="90">
            <template #default>
              <span class="badge online">online</span>
            </template>
          </el-table-column>
          <el-table-column label="实例 / 地址" min-width="180">
            <template #default="{ row }">
              <div>{{ instText(row) }}</div>
              <div class="meta">{{ row.host ? `${row.host}:${row.port}` : '' }}</div>
            </template>
          </el-table-column>
          <el-table-column label="库 / 表" min-width="180">
            <template #default="{ row }">
              <div>{{ row.db_name || row.database }}</div>
              <div class="meta">{{ tableText(row) }}</div>
            </template>
          </el-table-column>
          <el-table-column label="闪回点" width="180">
            <template #default="{ row }">{{ row.start_time || row.target_time || '—' }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="rowStatusTag(row)">{{ rowStatusText(row) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="审批" width="100">
            <template #default="{ row }">
              <el-button v-if="row.issue_id" link type="primary" @click="$router.push('/ops/tickets/' + row.issue_id)">
                {{ approvalText(row.approval_status) }}
              </el-button>
              <span v-else class="meta">{{ approvalText(row.approval_status) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ row }">
              <el-button link type="primary" @click="openTask(row)">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <span>共 {{ histFiltered.length }} 条</span>
          <el-pagination v-model:current-page="histPage" :page-size="10" :total="histFiltered.length" layout="prev, pager, next" />
        </div>
      </section>

      <section v-else-if="page === 'task'" class="card" v-loading="detailLoading">
        <div class="toolbar">
          <div class="grow">
            <p class="mono">{{ detail?.id }}</p>
          </div>
          <el-tag v-if="detail" :type="rowStatusTag(detail)">{{ rowStatusText(detail) }}</el-tag>
          <el-button v-if="detail?.issue_id" link type="primary" @click="$router.push('/ops/tickets/' + detail.issue_id)">
            {{ approvalText(detail.approval_status) }}
          </el-button>
          <el-button @click="go('history')">返回历史</el-button>
        </div>
        <div v-if="detail" class="split">
          <div class="addr">
            实例 {{ instText(detail) }} · {{ detail.host }}:{{ detail.port }}<br>
            库 {{ detail.db_name || detail.database }} / {{ tableText(detail) }}<br>
            时间 {{ detail.start_time || detail.target_time }} → {{ detail.end_time || '现在' }}
            <div v-if="detail.error_message" class="err">{{ detail.error_message }}</div>
          </div>
          <div class="meters">
            <div>
              <div class="meta">获取日志 {{ detail.log_done || 0 }}/{{ detail.log_total || 0 }}</div>
              <div class="track"><div class="bar" :style="{ width: pct(detail.log_done, detail.log_total) + '%' }" /></div>
            </div>
            <div>
              <div class="meta">解析 {{ detail.parse_done || 0 }}/{{ detail.parse_total || 0 }}</div>
              <div class="track"><div class="bar parse" :style="{ width: pct(detail.parse_done, detail.parse_total) + '%' }" /></div>
            </div>
            <div class="meta">变更数 {{ detail.change_count ?? 0 }}</div>
          </div>
        </div>
        <div class="actions">
          <el-button @click="reloadSQL">刷新 SQL</el-button>
          <el-button @click="copySQL">复制选中 SQL</el-button>
          <el-button @click="downloadSQL">下载选中 .sql</el-button>
          <el-button type="primary" :disabled="detail?.status !== 'succeeded' || !selectedSQLs.length" @click="toDml(detail)">转 DML 工单</el-button>
          <span class="meta">{{ selectedSQLs.length ? `已选 ${selectedSQLs.length} 条` : '未选' }}</span>
        </div>
        <el-table :data="sqls" border size="small" max-height="360" @selection-change="onSqlSel">
          <el-table-column type="selection" width="40" />
          <el-table-column prop="seq" label="#" width="60" />
          <el-table-column prop="op" label="op" width="80" />
          <el-table-column prop="table_name" label="表" width="140" />
          <el-table-column label="SQL" min-width="320">
            <template #default="{ row }"><pre class="mono">{{ row.statement }}</pre></template>
          </el-table-column>
        </el-table>
        <h3>运行日志</h3>
        <div class="logs">
          <p v-for="(l, i) in logs" :key="i" class="log-line" :class="String(l.level || '').toLowerCase()">
            [{{ fmtTime(l.created_at) }}] {{ l.level }} {{ l.message }}
          </p>
          <p v-if="!logs.length" class="log-line">暂无日志</p>
        </div>
      </section>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { dbLabel } from '../../utils/db'

const PREFILL_KEY = 'auto-dba-dml-prefill'
const relChips = [
  { m: 5, label: '5m' },
  { m: 10, label: '10m' },
  { m: 60, label: '1h' },
  { m: 360, label: '6h' },
]

const route = useRoute()
const router = useRouter()
const formRef = ref()
const page = computed(() => {
  if (route.params.id) return 'task'
  if (route.path.endsWith('/tasks')) return 'new'
  if (route.path.includes('/history')) return 'history'
  return 'dash'
})
const wizardStep = ref(1)
const engine = ref('postgres')
const instances = ref([])
const databases = ref([])
const tableOptions = ref([])
const allRows = ref([])
const listLoading = ref(false)
const prechecking = ref(false)
const creating = ref(false)
const precheck = ref(null)
const keyword = ref('')
const histStatus = ref('')
const histPage = ref(1)
const detailLoading = ref(false)
const detail = ref(null)
const sqls = ref([])
const logs = ref([])
const selectedSQLs = ref([])
const quickTable = ref('')
const quickAgo = ref(10)
const relMin = ref(10)
const form = reactive(emptyForm())
let listTimer = null
let taskTimer = null

function emptyForm() {
  return {
    env: '', instance_id: '', db_name: '', tables: [],
    start_time: '', end_time: '', reviewer: '',
    sql_types: [], output_kind: 'flashback',
    start_file: '', start_pos: 0, stop_file: '', stop_pos: 0,
  }
}

const rules = {
  instance_id: [{ required: true, message: '请选择实例', trigger: 'change' }],
  db_name: [{ required: true, message: '请选择数据库', trigger: 'change' }],
  start_time: [{ required: true, message: '请设置起始时间', trigger: 'change' }],
  reviewer: [{ required: true, message: '请选择审核人', trigger: 'change' }],
}
const defaultReviewer = ref('')

function pad(n) { return String(n).padStart(2, '0') }
function toLocal(d) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function applyRel(m) {
  relMin.value = m
  const end = new Date()
  form.end_time = toLocal(end)
  form.start_time = toLocal(new Date(end.getTime() - m * 60 * 1000))
}
function disableFuture(d) { return d.getTime() > Date.now() }
function idcScope(idc) {
  const s = String(idc || '').toLowerCase().trim()
  if (!s || s === 'default' || s === 'unknown') return ''
  if (s.includes('test') || s.includes('dev') || s === 'uat') return 'test'
  return 'prod'
}
function isEngine(i) {
  const t = String(i.db_type || '').toLowerCase()
  return engine.value === 'postgres' ? (t === 'postgres' || t === 'postgresql') : t.includes('mysql') || t === engine.value
}

const engineInstances = computed(() => instances.value.filter(isEngine))
const sourceOptions = computed(() => {
  return engineInstances.value.filter((i) => {
    if (!form.env) return true
    const scope = idcScope(i.idc)
    return !scope || scope === form.env
  })
})
const currentInst = computed(() => instances.value.find((i) => i.id === form.instance_id))
const dbOptions = computed(() => databases.value.filter((d) => !d.instance_id || d.instance_id === form.instance_id))
const reviewerOptions = computed(() => {
  const set = new Set()
  for (const a of currentInst.value?.approvers || []) if (a) set.add(a)
  const db = dbOptions.value.find((d) => d.db_name === form.db_name)
  for (const a of db?.approvers || []) if (a) set.add(a)
  if (defaultReviewer.value) set.add(defaultReviewer.value)
  return [...set]
})
const instMap = computed(() => {
  const m = new Map()
  for (const i of instances.value) m.set(i.id, i)
  return m
})
const recentTasks = computed(() => allRows.value.slice(0, 5))
const dash = computed(() => {
  const rows = allRows.value
  const startOfToday = new Date(); startOfToday.setHours(0, 0, 0, 0)
  const days = []
  let max = 1
  for (let i = 6; i >= 0; i--) {
    const d = new Date(startOfToday); d.setDate(d.getDate() - i)
    const next = new Date(d); next.setDate(next.getDate() + 1)
    let ok = 0, fail = 0
    for (const r of rows) {
      const t = new Date(r.created_at).getTime()
      if (t >= d.getTime() && t < next.getTime()) {
        if (r.status === 'succeeded' || r.status === 'executed') ok++
        else if (r.status === 'failed') fail++
      }
    }
    max = Math.max(max, ok + fail)
    days.push({ label: `${d.getMonth() + 1}/${d.getDate()}`, ok, fail })
  }
  const succeeded = rows.filter((r) => r.status === 'succeeded' || r.status === 'executed').length
  const failed = rows.filter((r) => r.status === 'failed').length
  const pending = rows.filter((r) => r.approval_status === 'pending' || r.status === 'pending' || r.status === 'running').length
  const today = rows.filter((r) => new Date(r.created_at) >= startOfToday).length
  const walBytes = rows.reduce((s, r) => s + Number(r.wal_bytes || 0), 0)
  return {
    total: rows.length, succeeded, failed, pending, today, walBytes,
    rate: rows.length ? Math.round((succeeded * 100) / rows.length) : 0,
    days, max, weekOk: days.reduce((s, d) => s + d.ok, 0), weekFail: days.reduce((s, d) => s + d.fail, 0),
  }
})
const histFiltered = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return allRows.value.filter((r) => {
    if (histStatus.value === 'awaiting_approval') {
      if (r.approval_status !== 'pending') return false
    } else if (histStatus.value && r.status !== histStatus.value && r.approval_status !== histStatus.value) return false
    if (!q) return true
    return `${instText(r)} ${r.db_name || ''} ${r.id || ''}`.toLowerCase().includes(q)
  })
})
const histRows = computed(() => histFiltered.value.slice((histPage.value - 1) * 10, histPage.value * 10))
const addrText = computed(() => {
  const i = currentInst.value
  if (!i) return instances.value.length ? '请选择已配置的实例地址' : '还没有实例。请先到「配置 → 实例」添加。'
  const vendor = i.cloud_vendor || '自建'
  return `${i.name || i.id} · ${i.db_type} · ${i.host}:${i.port} · 用户 ${i.username || '—'} · vendor=${vendor} ${i.cloud_instance_id || ''} ${i.cloud_region || ''}`
})

function instLabel(i) {
  const addr = i.host ? `${i.host}:${i.port}` : ''
  return i.name ? `${i.name} — ${addr}` : addr
}
function instText(row) {
  const i = instMap.value.get(row.instance_id)
  return i ? instLabel(i) : (row.host ? `${row.host}:${row.port}` : row.instance_id || '—')
}
function tableText(row) {
  const t = row.tables || row.table_list || []
  return Array.isArray(t) && t.length ? t.join(', ') : '整库'
}
function statusText(s) {
  return { pending: '排队', init: '排队', running: '执行中', succeeded: '完成', executed: '完成', failed: '失败', cancelled: '已取消', canceled: '已取消', rejected: '已驳回' }[s] || s || '—'
}
function statusTag(s) {
  if (s === 'succeeded' || s === 'executed') return 'success'
  if (s === 'running' || s === 'pending') return 'warning'
  if (s === 'failed' || s === 'rejected') return 'danger'
  return 'info'
}
function approvalText(s) {
  return { pending: '待审批', approved: '已通过', rejected: '已驳回', canceled: '已撤销', cancelled: '已撤销' }[s] || (s ? s : '—')
}
function rowStatusText(row) {
  const a = row?.approval_status
  if (a === 'pending') return '待审批'
  if (a === 'rejected') return '已驳回'
  if (a === 'canceled' || a === 'cancelled') return '已撤销'
  return statusText(row?.status)
}
function rowStatusTag(row) {
  const a = row?.approval_status
  if (a === 'pending') return 'warning'
  if (a === 'rejected') return 'danger'
  if (a === 'canceled' || a === 'cancelled') return 'info'
  return statusTag(row?.status)
}
function itemTag(it) {
  if (it.status === 'passed' || it.ok) return 'success'
  if (it.status === 'warning') return 'warning'
  return 'danger'
}
function pct(done, total) {
  const t = Number(total || 0)
  if (t <= 0) return detail.value?.status === 'succeeded' ? 100 : 0
  return Math.min(100, Math.round((Number(done || 0) * 100) / t))
}
function barH(n) {
  const m = dash.value.max || 1
  return `${Math.max(n ? 8 : 2, Math.round((n / m) * 100))}%`
}
function fmtBytes(n) {
  const v = Number(n) || 0
  if (v < 1024) return v + ' B'
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + ' KB'
  if (v < 1024 * 1024 * 1024) return (v / (1024 * 1024)).toFixed(1) + ' MB'
  return (v / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}
function fmtTime(s) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return String(s).replace('T', ' ').slice(0, 19)
  return toLocal(d)
}

function payload() {
  return {
    instance_id: form.instance_id,
    database: form.db_name,
    db_name: form.db_name,
    tables: form.tables,
    target_time: form.start_time,
    start_time: form.start_time,
    end_time: form.end_time,
    sql_type: (form.sql_types || []).join(','),
    sql_types: form.sql_types,
    output_kind: form.output_kind,
    reviewer: form.reviewer,
    approver: form.reviewer,
    assigned_to: form.reviewer,
    start_file: form.start_file,
    start_pos: Number(form.start_pos || 0),
    stop_file: form.stop_file,
    stop_pos: Number(form.stop_pos || 0),
  }
}

function go(p) {
  if (p === 'new' && wizardStep.value > 1 && !precheck.value) wizardStep.value = 1
  if (p !== 'task') stopTaskPoll()
  const map = {
    dash: '/ops/flashback/dashboard',
    new: '/ops/flashback/tasks',
    history: '/ops/flashback/history',
  }
  router.push(map[p] || '/ops/flashback/dashboard')
}

function onEngineChange() {
  form.instance_id = ''
  form.db_name = ''
  form.tables = []
  form.reviewer = ''
  tableOptions.value = []
  databases.value = []
  precheck.value = null
  wizardStep.value = 1
}
function onEnvChange() {
  form.instance_id = ''
  form.db_name = ''
  form.tables = []
  form.reviewer = ''
  tableOptions.value = []
  precheck.value = null
}
async function onInstChange() {
  form.db_name = ''
  form.tables = []
  form.reviewer = ''
  tableOptions.value = []
  precheck.value = null
  await loadDbs()
}
async function onDbChange() {
  form.tables = []
  form.reviewer = reviewerOptions.value[0] || ''
  precheck.value = null
  await loadTables()
}

async function loadDbs() {
  if (!form.instance_id) { databases.value = []; return }
  const [reg, live] = await Promise.all([
    api.get('/databases', { params: { instance_id: form.instance_id } }),
    api.get(`/instances/${form.instance_id}/databases`).catch(() => ({ data: { data: [] } })),
  ])
  const registered = reg.data.data || []
  const liveNames = (live.data.data || []).map((d) => (typeof d === 'string' ? { db_name: d } : { db_name: d.name || d.db_name, system: d.system }))
    .filter((d) => d.db_name && !d.system)
  const seen = new Set(registered.map((d) => d.db_name))
  databases.value = [...registered, ...liveNames.filter((d) => !seen.has(d.db_name))]
}
async function loadTables() {
  tableOptions.value = []
  const rec = databases.value.find((d) => d.id && d.db_name === form.db_name)
  if (rec?.id) {
    const { data } = await api.get(`/databases/${rec.id}/tables`)
    tableOptions.value = data.data || []
  }
}

async function doPrecheck() {
  await formRef.value?.validate()
  prechecking.value = true
  try {
    const { data } = await api.post('/flashback/tasks/precheck', payload(), { timeout: 60000 })
    const items = (data.items || []).map((it) => ({
      ...it,
      ok: it.status === 'passed' || it.status === 'warning' || it.ok,
    }))
    precheck.value = { ...data, ok: !!data.ok, items }
    wizardStep.value = 2
    ElMessage[data.ok ? 'success' : 'warning'](data.message || (data.ok ? '预检查通过' : '预检查未通过'))
  } finally {
    prechecking.value = false
  }
}

async function goConfirm() {
  if (!form.reviewer) {
    ElMessage.warning('请先选择审核人')
    return
  }
  wizardStep.value = 3
}

async function create() {
  if (!form.reviewer) {
    ElMessage.warning('请先选择审核人')
    return
  }
  creating.value = true
  try {
    const { data } = await api.post('/flashback/tasks', payload(), { timeout: 60000 })
    ElMessage.success('已提交审批，通过后自动执行')
    precheck.value = null
    wizardStep.value = 1
    await load()
    const row = data.data || data
    if (row?.issue_id) router.push('/ops/tickets/' + row.issue_id)
    else if (row?.id) openTask(row)
    else router.push('/ops/tickets?type=flashback')
  } finally {
    creating.value = false
  }
}

function quickStart() {
  if (!form.instance_id) { ElMessage.warning('请先选择实例'); return }
  if (!form.db_name) { ElMessage.warning('请填写数据库名'); return }
  applyRel(Number(quickAgo.value) || 10)
  form.tables = quickTable.value.trim() ? [quickTable.value.trim()] : []
  form.reviewer = reviewerOptions.value[0] || form.reviewer
  wizardStep.value = 1
  precheck.value = null
  go('new')
}

async function load() {
  listLoading.value = true
  try {
    const [tasks, insts, args] = await Promise.all([
      api.get('/flashback/tasks'),
      api.get('/instances'),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
    ])
    allRows.value = tasks.data.data || []
    instances.value = insts.data.data || []
    const list = args.data.data || []
    defaultReviewer.value = String(list.find((a) => a.key === 'sync_default_approver')?.value || '').trim()
    if (!form.reviewer && reviewerOptions.value[0]) form.reviewer = reviewerOptions.value[0]
  } finally {
    listLoading.value = false
  }
}

function openTask(row) {
  if (!row?.id) return
  router.push(`/ops/flashback/history/${row.id}`)
}

async function loadTask(id) {
  if (!id) {
    stopTaskPoll()
    return
  }
  detail.value = { ...(detail.value || {}), id }
  detailLoading.value = true
  try {
    await refreshTask(id)
  } finally {
    detailLoading.value = false
  }
  stopTaskPoll()
  if (detail.value && (detail.value.status === 'pending' || detail.value.status === 'running')) {
    taskTimer = setInterval(() => refreshTask(id).catch(() => {}), 3000)
  }
}

async function refreshTask(id) {
  const [s, l, t] = await Promise.all([
    api.get(`/flashback/tasks/${id}/sql`),
    api.get(`/flashback/tasks/${id}/logs`),
    api.get(`/flashback/tasks/${id}`),
  ])
  sqls.value = s.data.data || []
  logs.value = l.data.data || []
  detail.value = t.data.data || detail.value
  if (detail.value && !(detail.value.status === 'pending' || detail.value.status === 'running')) stopTaskPoll()
}

function reloadSQL() {
  if (detail.value?.id) refreshTask(detail.value.id)
}

function onSqlSel(rows) {
  selectedSQLs.value = rows.map((r) => r.statement).filter(Boolean)
}

function selectedText() {
  const stmts = selectedSQLs.value.length ? selectedSQLs.value : sqls.value.map((r) => r.statement).filter(Boolean)
  return stmts.map((s) => String(s).replace(/;\s*$/, '') + ';').join('\n') + (stmts.length ? '\n' : '')
}

async function copySQL() {
  const text = selectedText()
  if (!text) { ElMessage.warning('没有可复制的 SQL'); return }
  await navigator.clipboard.writeText(text)
  ElMessage.success('已复制')
}

function downloadSQL() {
  const text = selectedText()
  if (!text) { ElMessage.warning('没有可下载的 SQL'); return }
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `flashback-${detail.value?.id || 'sql'}.sql`
  a.click()
  URL.revokeObjectURL(a.href)
}

function toDml(row) {
  if (!row) return
  const inst = instMap.value.get(row.instance_id)
  sessionStorage.setItem(PREFILL_KEY, JSON.stringify({
    env: form.env || idcScope(inst?.idc) || 'test',
    instance_type: inst?.db_type || engine.value,
    instance_id: row.instance_id,
    db_name: row.db_name || row.database,
    description: `闪回任务 ${row.id}`,
    sql_content: selectedText(),
  }))
  router.push('/ops/dml')
}

function stopTaskPoll() {
  if (taskTimer) { clearInterval(taskTimer); taskTimer = null }
}

watch(() => route.params.id, (id) => {
  if (id) loadTask(id)
  else stopTaskPoll()
}, { immediate: true })

onMounted(async () => {
  applyRel(10)
  await load()
  listTimer = setInterval(() => {
    if (allRows.value.some((r) => r.status === 'running' || r.status === 'pending')) load()
  }, 2000)
})
onUnmounted(() => {
  if (listTimer) clearInterval(listTimer)
  stopTaskPoll()
})
</script>

<style scoped>
.fb-shell {
  --bg: #eef2f7; --card: #fff; --ink: #1d2a3a; --muted: #6b7a8d; --line: #e4eaf2;
  --blue: #2f6fed; --blue-soft: #e8f0fe; --ok: #1f8a4c; --fail: #c0352b;
  box-sizing: border-box; min-height: 100%; height: auto; overflow: auto;
  padding: 18px 22px 36px; background: var(--bg); color: var(--ink);
}
.card { background: var(--card); border: 1px solid var(--line); border-radius: 16px; box-shadow: 0 10px 28px rgba(30, 58, 110, 0.07); padding: 22px; }
.card h2 { margin: 0 0 6px; font-size: 20px; }
.card h3 { margin: 0 0 12px; font-size: 15px; }
.lead, .meta { color: var(--muted); font-size: 13px; }
.toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: end; margin-bottom: 16px; }
.grow { flex: 1; }
.steps { display: flex; gap: 22px; margin: 16px 0 20px; }
.step { display: flex; align-items: center; gap: 8px; color: var(--muted); }
.step b { width: 26px; height: 26px; border-radius: 50%; display: grid; place-items: center; background: #e8edf5; font-size: 13px; }
.step.on { color: var(--blue); }
.step.on b { background: var(--blue); color: #fff; }
.split { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 8px; }
.field label { font-size: 12px; color: var(--muted); }
.addr { background: #f6f8fc; border: 1px dashed #cfd8e6; border-radius: 12px; padding: 12px; font-size: 13px; margin-bottom: 12px; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 5px 10px; cursor: pointer; font-size: 13px; display: inline-flex; align-items: center; gap: 4px; }
.chip.on { background: var(--blue-soft); color: var(--blue); border-color: #c5d6fb; }
.chip input { margin: 0; }
.info { background: #eef5ff; border: 1px dashed #b7ccf5; border-radius: 12px; padding: 12px; color: #335285; font-size: 13px; }
.tip { margin-top: 12px; line-height: 1.55; }
.tip strong { display: block; margin-bottom: 6px; color: #1e3a6e; }
.tip p { margin: 0 0 8px; }
.tip pre { margin: 0 0 8px; padding: 8px 10px; background: #fff; border: 1px solid #d5e2f7; border-radius: 8px; }
.actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; align-items: center; }
.checks { display: grid; gap: 8px; margin-top: 14px; }
.check { display: grid; grid-template-columns: 88px 1fr; gap: 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; align-items: start; }
.err { color: var(--fail); font-size: 13px; margin-top: 8px; }
.logs { max-height: 340px; overflow: auto; background: #152033; color: #e8eef7; border-radius: 12px; padding: 12px; margin-top: 8px; }
.log-line { margin: 0 0 8px; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
.log-line.warn { color: #f0c36a; }
.log-line.error { color: #ff9b8c; }
.meters { display: grid; gap: 12px; }
.track { height: 10px; background: #e8edf5; border-radius: 999px; overflow: hidden; }
.bar { height: 100%; background: linear-gradient(90deg, var(--blue), #7aa4f5); }
.bar.parse { background: linear-gradient(90deg, #1f8a4c, #6ecf93); }
.mono { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all; margin: 0; }
.empty { padding: 40px 16px; text-align: center; color: var(--muted); border: 1px dashed var(--line); border-radius: 12px; }
.pager { display: flex; justify-content: space-between; align-items: center; margin-top: 14px; color: var(--muted); font-size: 13px; }
.badge { display: inline-flex; padding: 3px 8px; border-radius: 999px; font-size: 12px; background: #edf1f6; }
.badge.online { background: #edf1f6; }
.dash { display: grid; gap: 16px; }
.dash-kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
.dash-kpi { padding: 18px 20px; }
.dash-kpi h3 { margin: 0; font-size: 13px; color: var(--muted); }
.dash-kpi .num { margin: 10px 0 8px; font-size: 32px; font-weight: 700; line-height: 1; }
.dash-kpi .foot { margin: 0; font-size: 13px; }
.dash-kpi .foot.up { color: var(--ok); }
.dash-kpi .foot.down { color: var(--fail); }
.dash-mid { display: grid; grid-template-columns: minmax(0, 1.7fr) minmax(280px, 1fr); gap: 16px; }
.dash-hd { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.dash-hd h3 { margin: 0; }
.dash-bars { display: flex; align-items: stretch; gap: 14px; height: 168px; }
.dash-col { flex: 1; display: flex; flex-direction: column; align-items: center; min-width: 0; }
.dash-col .stack { flex: 1; width: 100%; display: flex; flex-direction: column; justify-content: flex-end; }
.dash-col .seg { width: 100%; border-radius: 6px 6px 0 0; }
.dash-col .seg.ok { background: #5b8def; }
.dash-col .seg.fail { background: #e36b63; }
.dash-col .lab { margin-top: 8px; color: var(--muted); font-size: 12px; }
.dash-legend { display: flex; gap: 16px; margin-top: 12px; font-size: 13px; color: #4a5b70; }
.dash-legend i { width: 8px; height: 8px; border-radius: 50%; display: inline-block; margin-right: 6px; }
.dash-legend .ok { background: #5b8def; }
.dash-legend .fail { background: #e36b63; }
.go-btn { width: 100%; }
.link { color: var(--blue); cursor: pointer; }
@media (max-width: 1100px) {
  .split, .dash-kpis, .dash-mid { grid-template-columns: 1fr; }
}
</style>
