<template>
  <el-icon v-if="comp" :size="size"><component :is="comp" /></el-icon>
</template>

<script setup>
import { computed } from 'vue'
import { resolveMenuIcon } from '../constants/menuIcons'

const props = defineProps({
  name: { type: String, default: '' },
  size: { type: [Number, String], default: 16 },
})

const comp = computed(() => resolveMenuIcon(props.name))
</script>
