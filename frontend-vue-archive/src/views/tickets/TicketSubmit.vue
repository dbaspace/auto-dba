<template>
  <div class="order-page">
    <div class="order-split">
      <aside class="order-aside">
        <el-form :model="form" label-position="top">
          <el-form-item label="实例" required>
            <el-select v-model="form.instance_id" filterable style="width:100%" @change="onInst">
              <el-option v-for="i in instances" :key="i.id" :label="`${i.name} (${i.host}:${i.port})`" :value="i.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="数据库" required>
            <el-select v-model="form.db_name" filterable allow-create style="width:100%" @change="loadApprovers">
              <el-option v-for="d in dbs" :key="d.id" :label="dbLabel(d)" :value="d.db_name" />
            </el-select>
          </el-form-item>
          <el-form-item label="审核人">
            <el-select v-model="form.assigned_to" clearable style="width:100%">
              <el-option v-for="a in approvers" :key="a.name" :label="a.name" :value="a.name" />
            </el-select>
          </el-form-item>
          <el-form-item label="描述" required>
            <el-input v-model="form.description" placeholder="如：业务上线" />
          </el-form-item>
          <p class="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
          <el-form-item>
            <el-button type="primary" @click="submit">提交</el-button>
          </el-form-item>
        </el-form>
      </aside>
      <main class="order-main">
        <el-tabs v-model="mainTab">
          <el-tab-pane label="填写SQL" name="sql">
            <el-input
              v-model="form.sql_content"
              type="textarea"
              class="sql-editor"
              :autosize="{ minRows: 16 }"
              :placeholder="placeholder"
            />
            <div class="sql-actions">
              <el-button type="primary" @click="check">SQL检测</el-button>
              <el-button @click="beautify">SQL美化</el-button>
            </div>
            <p class="hint">编辑框内可检测、美化 SQL</p>
            <el-table :data="checkRows" size="small" border empty-text="暂无检测结果">
              <el-table-column prop="error_level" label="级别" width="90" />
              <el-table-column prop="message" label="说明" min-width="180" />
              <el-table-column prop="sql" label="SQL" min-width="240" show-overflow-tooltip />
            </el-table>
          </el-tab-pane>
          <el-tab-pane label="检测结果" name="result">
            <el-table :data="checkRows" size="small" border empty-text="暂无检测结果">
              <el-table-column prop="error_level" label="级别" width="90" />
              <el-table-column prop="message" label="说明" min-width="180" />
              <el-table-column prop="sql" label="SQL" min-width="240" show-overflow-tooltip />
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../../api/client'
import { dbLabel } from '../../utils/db'

const props = defineProps({ ticketType: { type: String, default: 'ddl' } })
const router = useRouter()
const instances = ref([])
const dbs = ref([])
const approvers = ref([])
const checkRows = ref([])
const mainTab = ref('sql')
const form = reactive({ instance_id: '', db_name: '', assigned_to: '', description: '', sql_content: '' })
const placeholder = props.ticketType === 'dml'
  ? 'UPDATE t SET c=1 WHERE id=1;'
  : 'ALTER TABLE t ADD COLUMN c varchar(32);'
const selectedInst = computed(() => instances.value.find((i) => i.id === form.instance_id) || {})
const dbType = computed(() => selectedInst.value.db_type || '')

async function loadInst() {
  const { data } = await api.get('/tickets/instances')
  instances.value = data.data || []
}
async function onInst() {
  form.db_name = ''
  const { data } = await api.get(`/tickets/instances/${form.instance_id}/databases`)
  dbs.value = data.data || []
  await loadApprovers()
}
async function loadApprovers() {
  const { data } = await api.get('/tickets/target-approvers', { params: { instance_id: form.instance_id, db_name: form.db_name } })
  approvers.value = data.data || []
  if (!form.assigned_to && approvers.value[0]) form.assigned_to = approvers.value[0].name
}
async function check() {
  const { data } = await api.post('/tickets/check-sql', {
    sql_content: form.sql_content,
    type: props.ticketType,
    db_type: dbType.value,
    instance_id: form.instance_id,
    db_name: form.db_name,
  })
  checkRows.value = data.statements || []
  mainTab.value = 'result'
  ElMessage[data.ok ? 'success' : 'warning'](data.ok ? '检测通过' : '检测未通过')
}
function beautify() {
  form.sql_content = form.sql_content
    .replace(/\s+/g, ' ')
    .replace(/\s*;\s*/g, ';\n')
    .replace(/\b(SELECT|FROM|WHERE|AND|OR|JOIN|LEFT|RIGHT|INNER|GROUP BY|ORDER BY|LIMIT|UPDATE|SET|INSERT INTO|VALUES|ALTER TABLE|ADD|DROP|CREATE|GRANT|TO)\b/gi, '\n$1')
    .replace(/\n+/g, '\n')
    .trim()
}
async function submit() {
  const { data } = await api.post('/tickets', { ...form, type: props.ticketType, db_type: dbType.value })
  ElMessage.success('已提交 ' + (data.data?.ticket_no || ''))
  router.push('/ops/tickets')
}
onMounted(loadInst)
</script>
