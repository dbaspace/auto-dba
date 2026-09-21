<template>
  <!-- 结构 / class 与 universe-fe PipelineVisualization.tsx 一致；w-max 适配新项目更宽的主内容区 -->
  <div class="w-full overflow-auto">
    <div class="flex flex-col gap-3 w-max min-w-[1080px]">
      <div class="flex items-stretch flex-wrap gap-y-3">
        <template v-for="(n, i) in head" :key="n.id">
          <span v-if="i" class="text-gray-400 text-base mx-1 flex items-center">→</span>
          <div
            class="pv-antd-card min-w-[180px] max-w-[220px] shrink-0"
            :title="n.message || ''"
          >
            <div class="pv-antd-card-body" :style="{ background: bgOf(n.tone) }">
              <div class="flex items-center gap-2 mb-1">
                <el-icon :style="{ color: colorOf(n.tone), fontSize: '16px' }">
                  <component :is="iconOf(n.tone)" />
                </el-icon>
                <span class="font-medium text-sm">{{ n.name }}</span>
              </div>
              <div v-if="n.detail" class="text-xs text-gray-600 mb-1 break-all">{{ n.detail }}</div>
              <div v-if="n.extras?.length" class="flex flex-col gap-0.5 text-xs">
                <div
                  v-for="e in n.extras"
                  :key="e.label"
                  class="flex justify-between gap-2"
                >
                  <span class="text-gray-400">{{ e.label }}</span>
                  <span class="text-gray-700 truncate">
                    <el-tag
                      v-if="isTagValue(e)"
                      size="small"
                      :type="tagType(e.value)"
                      effect="light"
                    >{{ e.value }}</el-tag>
                    <template v-else>{{ e.value }}</template>
                  </span>
                </div>
              </div>
              <div v-if="n.id === 'debezium'" class="mt-2 pt-2 border-t border-gray-200">
                <el-button
                  link
                  type="primary"
                  class="!px-0 !h-auto text-xs"
                  :disabled="!n.ready"
                  @click="$emit('open-connector')"
                >查看运行状态</el-button>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div class="flex items-center pl-[1080px]">
        <span class="text-gray-400 text-base mx-1 flex items-center">↓</span>
      </div>

      <div class="flex items-stretch flex-wrap gap-y-3 justify-end">
        <template v-for="(n, i) in tail" :key="n.id">
          <span v-if="i" class="text-gray-400 text-base mx-1 flex items-center">→</span>
          <div
            class="pv-antd-card min-w-[180px] max-w-[220px] shrink-0"
            :title="n.message || ''"
          >
            <div class="pv-antd-card-body" :style="{ background: bgOf(n.tone) }">
              <div class="flex items-center gap-2 mb-1">
                <el-icon :style="{ color: colorOf(n.tone), fontSize: '16px' }">
                  <component :is="iconOf(n.tone)" />
                </el-icon>
                <span class="font-medium text-sm">{{ n.name }}</span>
              </div>
              <div v-if="n.detail" class="text-xs text-gray-600 mb-1 break-all">{{ n.detail }}</div>
              <div v-if="n.extras?.length" class="flex flex-col gap-0.5 text-xs">
                <div
                  v-for="e in n.extras"
                  :key="e.label"
                  class="flex justify-between gap-2"
                >
                  <span class="text-gray-400">{{ e.label }}</span>
                  <span class="text-gray-700 truncate">
                    <el-tag
                      v-if="isTagValue(e)"
                      size="small"
                      :type="tagType(e.value)"
                      effect="light"
                    >{{ e.value }}</el-tag>
                    <template v-else>{{ e.value }}</template>
                  </span>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { CircleCheckFilled, CircleCloseFilled, Clock, Loading, WarningFilled } from '@element-plus/icons-vue'

const props = defineProps({
  nodes: { type: Array, default: () => [] },
})
defineEmits(['open-connector'])

const head = computed(() => (props.nodes || []).slice(0, 5))
const tail = computed(() => (props.nodes || []).slice(5))

const META = {
  'is-ok': { color: '#52c41a', bg: '#f6ffed' },
  'is-run': { color: '#1677ff', bg: '#e6f4ff' },
  'is-error': { color: '#ff4d4f', bg: '#fff2f0' },
  'is-warn': { color: '#d48806', bg: '#fffbe6' },
  pending: { color: '#8c8c8c', bg: '#fafafa' },
}

function bgOf(tone) {
  return (META[tone] || META.pending).bg
}
function colorOf(tone) {
  return (META[tone] || META.pending).color
}
function iconOf(tone) {
  if (tone === 'is-ok') return CircleCheckFilled
  if (tone === 'is-error') return CircleCloseFilled
  if (tone === 'is-run') return Loading
  if (tone === 'is-warn') return WarningFilled
  return Clock
}
function isTagValue(e) {
  const v = String(e?.value ?? '')
  if (['是', '否', 'active', 'inactive'].includes(v)) return true
  if (['Publication 表', 'topics', '配置表数'].includes(e?.label) && /^\d+$/.test(v)) return true
  return false
}
function tagType(v) {
  const s = String(v)
  if (s === '是' || s === 'active') return 'success'
  if (s === '否' || s === 'inactive') return 'info'
  return 'success'
}
</script>

<style scoped>
/* 对齐 Ant Design Card size=small（老项目 PipelineNode 外壳） */
.pv-antd-card {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
  color: rgba(0, 0, 0, 0.88);
  font-size: 14px;
  line-height: 1.5714285714285714;
  list-style: none;
  background: #ffffff;
  border-radius: 8px;
  border: 1px solid #f0f0f0;
  overflow: hidden;
}
.pv-antd-card-body {
  padding: 12px;
}
</style>
