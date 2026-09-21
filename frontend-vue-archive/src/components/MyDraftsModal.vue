<template>
  <el-dialog
    :model-value="open"
    :title="drafts.length ? `我的草稿（${drafts.length}）` : '我的草稿'"
    width="520px"
    append-to-body
    destroy-on-close
    @update:model-value="onVisible"
  >
    <el-empty
      v-if="!drafts.length"
      :description="userId ? '暂无本机草稿（清理浏览器缓存后也会为空）' : '未登录，无法读取本机草稿'"
    />
    <div v-else class="drafts">
      <div class="drafts-head">
        <p class="drafts-hint">
          仅保存在本机浏览器、仅当前登录账号可见，不跨设备、不上报平台。每种工单类型最多 1 条；再次进入同类型创建入口须继续编辑或先清空。
          当前共 {{ drafts.length }} 条。
        </p>
        <el-button v-if="drafts.length > 1" type="danger" link @click="onClearAll">清空全部</el-button>
      </div>
      <div v-for="d in drafts" :key="d.type" class="draft-card">
        <div class="draft-title">{{ d.label }}</div>
        <div class="draft-time">更新于 {{ fmtTime(d.updatedAt) }}</div>
        <p class="draft-summary">{{ summarizeWorkOrderDraft(d) }}</p>
        <div class="draft-actions">
          <el-button @click="onClearOne(d.type)">清空</el-button>
          <el-button type="primary" @click="onContinue(d)">继续编辑</el-button>
        </div>
      </div>
    </div>
  </el-dialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  clearWorkOrderDraft,
  currentDraftOwnerId,
  loadAllWorkOrderDrafts,
  summarizeWorkOrderDraft,
} from '../utils/workOrderDraft'

const props = defineProps({
  open: { type: Boolean, default: false },
})
const emit = defineEmits(['update:open', 'changed'])

const router = useRouter()
const tick = ref(0)
const userId = computed(() => currentDraftOwnerId())
const drafts = computed(() => {
  void tick.value
  if (!userId.value) return []
  return loadAllWorkOrderDrafts(userId.value)
})

function refresh() {
  tick.value += 1
  emit('changed')
}

function onVisible(v) {
  emit('update:open', v)
  if (v) refresh()
}

function fmtTime(at) {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function onContinue(d) {
  emit('update:open', false)
  router.push(d.path || '/ops/tickets')
}

function onClearOne(type) {
  if (!userId.value) return
  clearWorkOrderDraft(type, userId.value)
  refresh()
}

function onClearAll() {
  if (!userId.value) return
  clearWorkOrderDraft(undefined, userId.value)
  refresh()
}

watch(() => props.open, (v) => {
  if (v) refresh()
})
</script>

<style scoped>
.drafts { display: flex; flex-direction: column; gap: 12px; }
.drafts-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; }
.drafts-hint { margin: 0; color: #909399; font-size: 12px; line-height: 1.6; }
.draft-card { border: 1px solid #f0f0f0; background: #f7f9fc; border-radius: 8px; padding: 12px; }
.draft-title { font-size: 14px; font-weight: 500; color: #303133; margin-bottom: 4px; }
.draft-time { font-size: 12px; color: #c0c4cc; margin-bottom: 8px; }
.draft-summary {
  margin: 0 0 8px;
  font-size: 12px; color: #606266;
  white-space: pre-wrap; word-break: break-all;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.draft-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
