<template>
  <div class="login">
    <el-card class="box">
      <h2>auto-dba</h2>
      <p class="hint">运维自助独立工作台</p>
      <el-form :model="form" @submit.prevent="onSubmit">
        <el-form-item v-if="ldapEnabled">
          <el-radio-group v-model="authMode" @change="onModeChange">
            <el-radio-button value="platform">平台账号</el-radio-button>
            <el-radio-button value="ldap">LDAP 账号</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item>
          <el-input v-model="form.username" :placeholder="authMode === 'ldap' ? 'LDAP 用户名' : '用户名'" />
        </el-form-item>
        <el-form-item>
          <el-input v-model="form.password" type="password" placeholder="密码" show-password />
        </el-form-item>
        <el-button type="primary" style="width:100%" :loading="loading" @click="onSubmit">登录</el-button>
      </el-form>
      <p v-if="error" class="err">{{ error }}</p>
      <p class="hint">{{ ldapEnabled ? (authMode === 'ldap' ? '使用目录账号登录，首次会自动创建本地运维账号' : '平台本地账号，admin 请走此项') : '默认账号 admin / admin' }}</p>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../api/client'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const ldapEnabled = ref(false)
const authMode = ref('platform')
const error = ref('')
const form = reactive({ username: 'admin', password: 'admin' })

onMounted(async () => {
  try {
    const { data } = await api.get('/info')
    ldapEnabled.value = Array.isArray(data.auth_modes) && data.auth_modes.includes('ldap')
  } catch { /* keep default hint */ }
})

function onModeChange() {
  error.value = ''
  if (authMode.value === 'ldap') {
    if (form.username === 'admin') form.username = ''
    if (form.password === 'admin') form.password = ''
  } else if (!form.username) {
    form.username = 'admin'
  }
}

async function onSubmit() {
  loading.value = true
  error.value = ''
  try {
    localStorage.removeItem('token')
    const { data } = await api.post('/auth/login', {
      username: form.username,
      password: form.password,
      auth_mode: ldapEnabled.value ? authMode.value : 'platform',
    })
    localStorage.setItem('token', data.token)
    localStorage.setItem('user', JSON.stringify(data.user))
    ElMessage.success('登录成功')
    const first = (data.user?.menus || []).flatMap(function walk(n) {
      return n.path ? [n.path] : (n.children || []).flatMap(walk)
    })[0]
    router.replace(route.query.redirect || first || '/403')
  } catch (e) {
    error.value = e.response?.data?.message || e.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background:
    linear-gradient(180deg, rgba(12, 24, 48, 0.38), rgba(12, 24, 48, 0.52)),
    url('../assets/login-bg.jpg') center / cover no-repeat;
}
.box {
  width: min(400px, 100%);
  background: rgba(255, 255, 255, 0.94);
  border: 1px solid rgba(255, 255, 255, 0.55);
  border-radius: 16px;
  box-shadow: 0 18px 48px rgba(8, 18, 40, 0.28);
  backdrop-filter: blur(10px);
}
.box :deep(.el-card__body) { padding: 28px 26px 24px; }
h2 { margin: 0 0 8px; text-align: center; }
.err { color: #c0352b; font-size: 13px; margin: 8px 0 0; text-align: center; }
</style>
