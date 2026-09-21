<template>
  <div ref="el" class="chart-box" :style="{ height }" />
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts'

const props = defineProps({
  option: { type: Object, default: () => ({}) },
  height: { type: String, default: '220px' },
})
const emit = defineEmits(['click'])
const el = ref()
let chart

function render() {
  if (!chart) return
  chart.setOption(props.option || {}, true)
}

function resize() {
  chart?.resize()
}

onMounted(() => {
  chart = echarts.init(el.value)
  chart.on('click', (p) => emit('click', p))
  render()
  window.addEventListener('resize', resize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', resize)
  chart?.dispose()
  chart = null
})

watch(() => props.option, render, { deep: true })
</script>

<style scoped>
.chart-box { width: 100%; min-width: 0; }
</style>
