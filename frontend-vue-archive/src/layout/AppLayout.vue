<template>
  <el-container class="wrap" direction="vertical">
    <el-header class="header">
      <div class="brand" @click="goHome">auto-dba</div>
      <div class="header-divider" />
      <div class="spacer" />
      <UserAccountMenu ref="accountMenu" :user="currentUser" @updated="onProfileUpdated" />
    </el-header>
    <el-container class="shell">
      <el-aside width="220px" class="aside">
        <el-menu
          :key="activeMenu"
          class="menu-scroll"
          :default-active="activeMenu"
          background-color="#1d2b3a"
          text-color="#cfd8e3"
          active-text-color="#69c0ff"
          @select="onMenuSelect"
        >
          <NavMenuNodes :nodes="menus" />
        </el-menu>
      </el-aside>
      <el-container class="body" direction="vertical">
        <div class="crumb-bar">
          <el-breadcrumb separator="/">
            <el-breadcrumb-item v-for="(c, i) in crumbs" :key="i">{{ c }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <el-main class="main" :class="{ 'main--ticket': isTicketPage }">
          <div class="main-panel">
            <router-view v-slot="{ Component }">
              <keep-alive include="Query">
                <component :is="Component" :key="viewKey" />
              </keep-alive>
            </router-view>
          </div>
        </el-main>
      </el-container>
    </el-container>
  </el-container>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api/client'
import NavMenuNodes from './NavMenuNodes.vue'
import UserAccountMenu from './UserAccountMenu.vue'

const route = useRoute()
const router = useRouter()
const menus = ref([])
const currentUser = ref(readUser())
const accountMenu = ref(null)
const activeMenu = computed(() => {
  const p = route.path
  if (p.startsWith('/ops/flashback/history/')) return '/ops/flashback/history'
  return p
})
const crumbs = computed(() => {
  const trail = findMenuTrail(menus.value, activeMenu.value)
  if (trail.length) return trail.map((n) => n.title)
  if (route.meta.title) return [route.meta.title]
  return []
})
const viewKey = computed(() => {
  if (route.path.startsWith('/ops/flashback/cloud')) return route.name || route.path
  if (route.path.startsWith('/ops/flashback')) return 'Flashback'
  return route.name || route.path
})
const isTicketPage = computed(() => isTicketRoute(route.path))

function isTicketRoute(path) {
  return (
    path === '/ops/tickets' ||
    path.startsWith('/ops/tickets/') ||
    path === '/ops/ddl' ||
    path.startsWith('/ops/ddl/') ||
    path === '/ops/dml' ||
    path.startsWith('/ops/dml/') ||
    path === '/ops/dql' ||
    path.startsWith('/ops/dql/') ||
    path === '/ops/createdb' ||
    path.startsWith('/ops/createdb/') ||
    path === '/ops/dcl' ||
    path.startsWith('/ops/dcl/') ||
    path === '/ops/kafka' ||
    path.startsWith('/ops/kafka/')
  )
}

function readUser() {
  try {
    return JSON.parse(localStorage.getItem('user') || '{}')
  } catch {
    return {}
  }
}

function findMenuTrail(nodes, path, trail = []) {
  if (!path) return []
  let best = []
  for (const n of nodes || []) {
    const next = [...trail, n]
    if (n.path && (n.path === path || path.startsWith(n.path + '/'))) {
      const deeper = findMenuTrail(n.children, path, next)
      const hit = deeper.length ? deeper : next
      if (hit.length > best.length) best = hit
      continue
    }
    if (n.children?.length) {
      const hit = findMenuTrail(n.children, path, next)
      if (hit.length > best.length) best = hit
    }
  }
  return best
}

function firstPath(nodes) {
  for (const n of nodes || []) {
    if (n.path) return n.path
    const c = firstPath(n.children)
    if (c) return c
  }
  return '/403'
}

function goHome() {
  router.push(firstPath(menus.value))
}

function goPath(index) {
  if (!index || !String(index).startsWith('/')) return
  if (index === route.path) return
  router.push(index).catch(() => {
    if (route.path !== index) router.replace(index).catch(() => {})
  })
}

function onMenuSelect(index) {
  goPath(index)
}

async function refreshMe() {
  const { data } = await api.get('/auth/me')
  const u = data.data || {}
  localStorage.setItem('user', JSON.stringify(u))
  currentUser.value = u
  menus.value = u.menus || []
}

function onProfileUpdated(u) {
  const next = { ...currentUser.value, ...(u || {}) }
  currentUser.value = next
  localStorage.setItem('user', JSON.stringify(next))
  if (u?.menus) menus.value = u.menus
}

const stopAfterEach = router.afterEach(() => {
  accountMenu.value?.closeAll?.()
})

onMounted(() => {
  refreshMe()
  window.addEventListener('menus-changed', refreshMe)
})
onUnmounted(() => {
  window.removeEventListener('menus-changed', refreshMe)
  stopAfterEach()
})
</script>

<style scoped>
.wrap { height: 100%; width: 100%; overflow: hidden; }
.header {
  height: 56px;
  padding: 0 16px 0 20px;
  background: #1d2b3a;
  border-bottom: 1px solid #3d556c;
  display: flex;
  align-items: center;
  flex-shrink: 0;
  z-index: 40;
}
.brand {
  height: 56px;
  color: #fff;
  display: flex;
  align-items: center;
  font-weight: 700;
  letter-spacing: 0.5px;
  cursor: pointer;
  flex-shrink: 0;
  padding-right: 16px;
}
.header-divider {
  width: 1px;
  height: 22px;
  background: #3d556c;
  margin-right: 12px;
  flex-shrink: 0;
}
.spacer { flex: 1; }
.shell { flex: 1; min-height: 0; min-width: 0; overflow: hidden; }
.aside {
  position: relative;
  z-index: 20;
  background: #1d2b3a;
  border-right: 1px solid #3d556c;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  flex-shrink: 0;
}
.body {
  min-width: 0;
  min-height: 0;
  flex: 1;
  overflow: hidden;
  background: #f0f2f5;
}
.menu-scroll { flex: 1; min-height: 0; overflow-y: auto; border-right: none; }
.crumb-bar {
  background: #fff;
  padding: 10px 16px;
  border-bottom: 1px solid #e5e7eb;
  flex-shrink: 0;
}
.main {
  flex: 1 1 0;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
  padding: 16px;
  background: #f0f2f5;
  display: flex;
  flex-direction: column;
}
.main-panel {
  flex: 1 1 0;
  min-height: 0;
  min-width: 0;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 4px;
  overflow: auto;
  display: flex;
  flex-direction: column;
}
.main-panel :deep(> *) {
  width: 100%;
  min-height: 100%;
  box-sizing: border-box;
}
.main--ticket .main-panel {
  overflow: hidden;
}
.main--ticket .main-panel :deep(> *) {
  height: 100%;
  max-height: 100%;
  min-height: 0;
  overflow: hidden;
}
</style>
