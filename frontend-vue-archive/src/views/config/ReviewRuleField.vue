<template>
  <div class="rule-row" :class="{ 'is-wide': wide }">
    <span class="rule-label" :title="`${row.label || row.name}（${row.name}）`">{{ row.label || '—' }}</span>
    <div class="rule-ctrl" :class="{ grow: wide || !bool }">
      <el-switch
        v-if="bool"
        :model-value="checked"
        :disabled="saving"
        size="small"
        @change="(on) => $emit('change', row.name, on ? 'true' : 'false')"
      />
      <el-input
        v-else
        :model-value="value"
        size="small"
        :class="wide ? 'rule-input-wide' : 'rule-input'"
        placeholder="请输入参数值"
        :disabled="saving"
        @input="(v) => $emit('change', row.name, v)"
      />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  row: { type: Object, required: true },
  value: { type: String, default: '' },
  saving: { type: Boolean, default: false },
  wide: { type: Boolean, default: false },
})
defineEmits(['change'])

const bool = computed(() => {
  const s = String(props.row?.value ?? '').trim().toLowerCase()
  return s === 'true' || s === 'false'
})
const checked = computed(() => String(props.value ?? '').trim().toLowerCase() === 'true')
</script>

<style scoped>
.rule-row {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  min-width: 0;
  overflow: hidden;
  padding: 0 4px;
  border-bottom: 1px solid #f3f4f6;
}
.rule-row.is-wide { height: auto; min-height: 32px; padding: 6px 4px; }
.rule-label {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
  color: #1f2937;
  line-height: 32px;
}
.rule-ctrl { flex-shrink: 0; }
.rule-ctrl.grow { flex: 1; min-width: 0; }
.rule-input { width: 80px; }
.rule-input-wide { width: 100%; }
</style>
