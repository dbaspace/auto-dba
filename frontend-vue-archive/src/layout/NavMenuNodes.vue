<template>
  <template v-for="node in nodes" :key="node.id">
    <el-sub-menu v-if="node.children?.length" :index="node.id">
      <template #title>
        <MenuIcon :name="node.icon" />
        <span>{{ node.title }}</span>
      </template>
      <NavMenuNodes :nodes="node.children" />
    </el-sub-menu>
    <el-menu-item v-else-if="node.path" :index="node.path" :data-path="node.path">
      <MenuIcon :name="node.icon" />
      <span>{{ node.title }}</span>
    </el-menu-item>
  </template>
</template>

<script setup>
import MenuIcon from '../components/MenuIcon.vue'

defineOptions({ name: 'NavMenuNodes' })

defineProps({
  nodes: { type: Array, default: () => [] },
})
</script>
