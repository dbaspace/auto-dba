<template>
  <div class="db-picker" :class="{ disabled }">
    <div class="panel">
      <div class="panel-head">
        <span>请选择数据库</span>
        <button type="button" class="link" :disabled="disabled" @click="hideSystemDb = !hideSystemDb">
          {{ hideSystemDb ? '显示系统库' : '隐藏系统库' }}
        </button>
      </div>
      <div class="panel-search">
        <el-input v-model="searchQ" clearable :disabled="disabled" placeholder="请输入数据库名称" />
      </div>
      <div class="panel-checkall">
        <el-checkbox
          :model-value="leftAllChecked"
          :indeterminate="leftIndeterminate"
          :disabled="disabled || !leftVisible.length"
          @change="onCheckAll"
        />
        <span>数据库</span>
      </div>
      <div class="panel-list">
        <div v-if="!leftVisible.length" class="empty">{{ loading ? '加载中…' : '无匹配库' }}</div>
        <label
          v-for="(opt, index) in leftVisible"
          :key="opt.value"
          class="row"
          @click.prevent="onRowClick(index, opt.value, $event)"
        >
          <el-checkbox :model-value="selectedSet.has(opt.value)" :disabled="disabled" />
          <span class="name">{{ opt.label }}</span>
        </label>
      </div>
      <div class="panel-foot">支持按住 shift 键进行多选</div>
    </div>

    <div class="swap">⇄</div>

    <div class="panel">
      <div class="panel-head"><span>已选择</span></div>
      <div class="panel-checkall"><span>数据库</span></div>
      <div class="panel-list">
        <div v-if="!selectedOptions.length" class="empty">暂无已选数据库</div>
        <div v-for="opt in selectedOptions" :key="opt.value" class="row between">
          <span class="name">{{ opt.label }}</span>
          <button type="button" class="remove" :disabled="disabled" @click="remove(opt.value)">×</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'

const SYSTEM_DB = new Set([
  'information_schema', 'performance_schema', 'sys', 'mysql',
  'template0', 'template1', 'postgres', '_statistics_',
])

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  options: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const searchQ = ref('')
const hideSystemDb = ref(true)
const lastIndex = ref(null)

const selectedSet = computed(() => new Set(props.modelValue || []))
const optionById = computed(() => new Map(props.options.map((o) => [o.value, o])))

const selectedOptions = computed(() =>
  (props.modelValue || []).map((id) => optionById.value.get(id) || { value: id, label: id }),
)

const leftVisible = computed(() => {
  const q = searchQ.value.trim().toLowerCase()
  return props.options.filter((opt) => {
    if (hideSystemDb.value && SYSTEM_DB.has(String(opt.label).trim().toLowerCase()) && !selectedSet.value.has(opt.value)) {
      return false
    }
    if (!q) return true
    return String(opt.label).toLowerCase().includes(q)
  })
})

const leftCheckedCount = computed(() => leftVisible.value.filter((o) => selectedSet.value.has(o.value)).length)
const leftAllChecked = computed(() => leftVisible.value.length > 0 && leftCheckedCount.value === leftVisible.value.length)
const leftIndeterminate = computed(() => leftCheckedCount.value > 0 && leftCheckedCount.value < leftVisible.value.length)

function emitChange(next) {
  emit('update:modelValue', next)
}

function toggleOne(id, checked) {
  if (props.disabled) return
  if (checked) {
    if (selectedSet.value.has(id)) return
    emitChange([...(props.modelValue || []), id])
    return
  }
  emitChange((props.modelValue || []).filter((x) => x !== id))
}

function toggleRange(from, to, checked) {
  const start = Math.min(from, to)
  const end = Math.max(from, to)
  const rangeIds = leftVisible.value.slice(start, end + 1).map((o) => o.value)
  if (checked) emitChange([...new Set([...(props.modelValue || []), ...rangeIds])])
  else {
    const removeSet = new Set(rangeIds)
    emitChange((props.modelValue || []).filter((id) => !removeSet.has(id)))
  }
}

function onRowClick(index, id, e) {
  const checked = !selectedSet.value.has(id)
  if (e?.shiftKey && lastIndex.value != null && lastIndex.value !== index) {
    toggleRange(lastIndex.value, index, checked)
  } else {
    toggleOne(id, checked)
  }
  lastIndex.value = index
}

function onCheckAll(checked) {
  const visibleIds = leftVisible.value.map((o) => o.value)
  if (checked) emitChange([...new Set([...(props.modelValue || []), ...visibleIds])])
  else {
    const vis = new Set(visibleIds)
    emitChange((props.modelValue || []).filter((id) => !vis.has(id)))
  }
}

function remove(id) {
  if (props.disabled) return
  emitChange((props.modelValue || []).filter((x) => x !== id))
}
</script>

<style scoped>
.db-picker {
  display: flex;
  align-items: stretch;
  gap: 12px;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1 1 0;
}
.db-picker.disabled { opacity: 0.6; pointer-events: none; }
.panel {
  flex: 1;
  min-width: 0;
  min-height: 0;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  background: #fff;
  display: flex;
  flex-direction: column;
}
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  border-bottom: 1px solid #f3f4f6;
  font-size: 13px;
  font-weight: 500;
  color: #1f2937;
  flex-shrink: 0;
}
.link {
  border: 0;
  background: none;
  color: #1677ff;
  cursor: pointer;
  font-size: 13px;
  padding: 0;
}
.panel-search { padding: 6px 12px; border-bottom: 1px solid #f3f4f6; flex-shrink: 0; }
.panel-checkall {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-bottom: 1px solid #f3f4f6;
  background: #f9fafb;
  font-size: 13px;
  color: #4b5563;
  flex-shrink: 0;
}
.panel-list {
  flex: 1 1 0;
  min-height: 0;
  overflow-y: auto;
}
.empty { padding: 16px 12px; text-align: center; color: #9ca3af; font-size: 13px; }
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid #f9fafb;
  cursor: pointer;
}
.row:hover { background: #eff6ff; }
.row.between { justify-content: space-between; cursor: default; }
.row.between:hover { background: #f9fafb; }
.name { font-size: 13px; color: #1f2937; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.panel-foot { padding: 6px 12px; border-top: 1px solid #f3f4f6; font-size: 12px; color: #9ca3af; flex-shrink: 0; }
.swap { display: flex; align-items: center; color: #9ca3af; flex-shrink: 0; }
.remove {
  width: 20px;
  height: 20px;
  border: 1px solid #d1d5db;
  border-radius: 50%;
  background: #fff;
  color: #6b7280;
  cursor: pointer;
  line-height: 16px;
}
.remove:hover { color: #ef4444; border-color: #fca5a5; }
@media (max-width: 640px) {
  .db-picker { flex-direction: column; }
}
</style>
