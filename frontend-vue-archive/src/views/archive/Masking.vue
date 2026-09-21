<template>
  <div class="ops-page">
    <el-steps :active="step" finish-status="success" align-center class="mb">
      <el-step title="选择数据" />
      <el-step title="脱敏规则" />
      <el-step title="调度策略" />
      <el-step title="提交" />
    </el-steps>
    <el-alert type="info" :closable="false" class="mb" title="按字段配置脱敏算法后随归档任务执行。当前为独立工作台配置向导。" />
    <el-table :data="rows" class="mb">
      <el-table-column label="源字段" width="180">
        <template #default="{ row }">
          <span>{{ row.field }}</span>
          <el-tag v-if="row.sensitive" size="small" type="danger" style="margin-left:8px">敏感</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="type" label="类型" width="120" />
      <el-table-column label="脱敏算法">
        <template #default="{ row }">
          <el-select v-model="row.algo" style="width:180px">
            <el-option label="哈希" value="hash" />
            <el-option label="掩码" value="mask" />
            <el-option label="替换" value="replace" />
            <el-option label="不处理" value="none" />
          </el-select>
        </template>
      </el-table-column>
    </el-table>
    <el-checkbox v-model="parallel">并行执行</el-checkbox>
    <el-checkbox v-model="compress" style="margin-left:12px">压缩归档</el-checkbox>
    <div class="toolbar" style="margin-top:16px">
      <el-button :disabled="step===0" @click="step--">上一步</el-button>
      <el-button v-if="step<3" type="primary" @click="step++">下一步</el-button>
      <el-button v-else type="primary" @click="submit">提交</el-button>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'

const step = ref(1)
const parallel = ref(true)
const compress = ref(false)
const rows = ref([
  { field: 'phone', type: 'varchar', sensitive: true, algo: 'mask' },
  { field: 'email', type: 'varchar', sensitive: true, algo: 'hash' },
  { field: 'id_card', type: 'varchar', sensitive: true, algo: 'mask' },
  { field: 'created_at', type: 'timestamp', sensitive: false, algo: 'none' },
])
function submit() {
  ElMessage.success('脱敏规则已保存到本地，可在任务管理中关联执行')
}
</script>

<style scoped>
.mb { margin-bottom: 16px; }
</style>
