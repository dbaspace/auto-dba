<template>
  <div class="ops-page">
    <div class="ops-title">
      <div class="toolbar" style="margin:0">
        <el-button @click="load">重新检查</el-button>
        <el-button v-if="url" type="primary" @click="openUrl">打开控制台</el-button>
      </div>
    </div>
    <div v-if="url" class="ops-table">
      <iframe :src="url" class="embed" title="external-console" />
    </div>
    <el-result v-else :icon="ok ? 'success' : 'info'" :title="ok ? '已配置' : '未配置'" :sub-title="message" />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import api from '../../api/client'

const props = defineProps({ kind: { type: String, default: 'dolphin' } })
const ok = ref(false)
const message = ref('')
const url = ref('')

async function load() {
  const path = props.kind === 'dolphin' ? '/dsadmin/status' : '/feadmin/status'
  const { data } = await api.get(path)
  ok.value = !!data.ok
  message.value = data.message || ''
  url.value = data.url || ''
}
function openUrl() {
  if (url.value) window.open(url.value, '_blank')
}
onMounted(load)
</script>

<style scoped>
.embed {
  width: 100%;
  height: 100%;
  min-height: 480px;
  border: 1px solid #f3f4f6;
  border-radius: 4px;
  background: #fff;
}
</style>
