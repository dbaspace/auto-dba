<template>
  <img
    v-if="src"
    class="db-icon"
    :src="src"
    :alt="name"
    :title="name"
    :width="size"
    :height="size"
  />
  <span
    v-else
    class="db-icon"
    :title="name"
    :style="{ width: size + 'px', height: size + 'px' }"
    v-html="fallback"
  />
</template>

<script setup>
import { computed } from 'vue'
import postgres from '../assets/db/postgres.png'
import mysql from '../assets/db/mysql.png'
import tidb from '../assets/db/tidb.png'
import starrocks from '../assets/db/starrocks.png'
import clickhouse from '../assets/db/clickhouse.png'
import kafka from '../assets/db/kafka.png'

const props = defineProps({
  type: { type: String, default: '' },
  size: { type: Number, default: 16 },
})

const ICONS = {
  postgres,
  postgresql: postgres,
  pg: postgres,
  pgsql: postgres,
  mysql,
  tidb,
  starrocks,
  clickhouse,
  kafka,
}

const name = computed(() => String(props.type || '').toLowerCase().trim())
const src = computed(() => ICONS[name.value] || '')
const fallback = computed(() => {
  const ch = (name.value[0] || '?').toUpperCase()
  return `<svg viewBox="0 0 24 24"><rect width="24" height="24" rx="5" fill="#6B7280"/><text x="12" y="16" text-anchor="middle" fill="#fff" font-size="12" font-family="sans-serif">${ch}</text></svg>`
})
</script>

<style scoped>
.db-icon {
  display: inline-block;
  flex-shrink: 0;
  object-fit: contain;
  vertical-align: middle;
  line-height: 0;
}
.db-icon :deep(svg) { width: 100%; height: 100%; display: block; }
</style>
