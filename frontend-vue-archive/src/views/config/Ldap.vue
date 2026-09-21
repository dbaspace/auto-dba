<template>
  <div class="ops-page">
    <p class="hint">
      启用后，非 admin 账号优先走 LDAP 认证；首次登录会自动创建本地用户并赋予「运维」角色，管理员可再调整权限。
      admin 始终使用平台密码。
    </p>
    <div class="jumper">
      <strong>按 JumpServer 对照：</strong>
      「服务器」拆为本页的服务器地址 + 端口（<code>ldap://host:389</code> → 地址 host、端口 389；
      <code>ldaps://host:636</code> → 勾选 TLS、端口 636）。
      「绑定 DN」→ Bind DN，「密码」→ Bind 密码，「用户 OU」→ Base DN。
      用户过滤器可填 <code>(cn=%(user)s)</code>、<code>(uid=%s)</code> 或 AD 的 <code>(sAMAccountName=%s)</code>。
      仅点「测试连接」只验证 Bind；要确认能登录，请同时填测试账号/密码。
    </div>
    <el-form :model="form" label-width="160px" class="ldap-form" v-loading="loading">
      <el-form-item label="启用 LDAP">
        <el-switch v-model="form.enabled" />
      </el-form-item>
      <el-form-item label="服务器地址">
        <el-input v-model="form.host" placeholder="如 ldap.example.com，可带 ldap:// 前缀" />
      </el-form-item>
      <el-form-item label="端口">
        <el-input-number v-model="form.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item label="使用 TLS">
        <el-switch v-model="form.use_tls" />
        <span class="field-hint">LDAPS 时勾选，默认端口 636</span>
      </el-form-item>
      <el-form-item label="Base DN">
        <el-input v-model="form.base_dn" placeholder="如 ou=users,dc=example,dc=com" />
      </el-form-item>
      <el-form-item label="Bind DN">
        <el-input v-model="form.bind_dn" placeholder="如 cn=admin,dc=example,dc=com" />
      </el-form-item>
      <el-form-item label="Bind 密码">
        <el-input v-model="form.bind_password" type="password" show-password :placeholder="pwdPlaceholder" autocomplete="off" />
      </el-form-item>
      <el-form-item label="用户搜索 Filter">
        <el-input v-model="form.user_filter" placeholder="(uid=%s) 或 (cn=%(user)s)" />
        <div class="field-hint">支持 %s 或 %(user)s，会替换为登录用户名</div>
      </el-form-item>
      <el-form-item label="显示名属性">
        <el-input v-model="form.display_name_attr" placeholder="如 cn 或 displayName" />
      </el-form-item>
      <el-form-item label="测试账号（可选）">
        <el-input v-model="form.test_username" placeholder="填写后测试会做一次完整用户验证" />
      </el-form-item>
      <el-form-item label="测试密码（可选）">
        <el-input v-model="form.test_password" type="password" show-password autocomplete="off" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        <el-button :loading="saving" @click="test">测试连接</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../../api/client'

const loading = ref(false)
const saving = ref(false)
const bindSet = ref(false)
const form = reactive({
  enabled: false,
  host: '',
  port: 389,
  use_tls: false,
  base_dn: '',
  bind_dn: '',
  bind_password: '',
  user_filter: '(uid=%s)',
  display_name_attr: '',
  test_username: '',
  test_password: '',
})
const pwdPlaceholder = computed(() => (bindSet.value ? '已设置，留空则保持原密码' : '请输入 Bind 密码'))

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/settings/ldap')
    const d = data.data || {}
    form.enabled = !!d.enabled
    form.host = d.host || ''
    form.port = d.port > 0 ? d.port : 389
    form.use_tls = !!d.use_tls
    form.base_dn = d.base_dn || ''
    form.bind_dn = d.bind_dn || ''
    form.bind_password = ''
    form.user_filter = d.user_filter || '(uid=%s)'
    form.display_name_attr = d.display_name_attr || ''
    bindSet.value = !!d.bind_password_set
  } finally {
    loading.value = false
  }
}

function payload() {
  return {
    enabled: form.enabled,
    host: form.host.trim(),
    port: form.port || 389,
    use_tls: form.use_tls,
    base_dn: form.base_dn.trim(),
    bind_dn: form.bind_dn.trim(),
    bind_password: form.bind_password || undefined,
    user_filter: form.user_filter.trim() || '(uid=%s)',
    display_name_attr: form.display_name_attr.trim(),
    test_username: form.test_username.trim(),
    test_password: form.test_password,
  }
}

async function save() {
  saving.value = true
  try {
    await api.put('/settings/ldap', payload())
    ElMessage.success('已保存')
    form.bind_password = ''
    await load()
  } finally {
    saving.value = false
  }
}

async function test() {
  saving.value = true
  try {
    const { data } = await api.post('/settings/ldap/test', payload())
    ElMessage.success(data.message || '连接成功')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.hint { margin: 0 0 12px; color: #6b7280; font-size: 13px; line-height: 1.6; }
.jumper {
  margin-bottom: 16px;
  padding: 10px 12px;
  background: #f8fafc;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  color: #4b5563;
  font-size: 13px;
  line-height: 1.6;
}
.ldap-form { max-width: 720px; }
.field-hint { margin-left: 8px; color: #9ca3af; font-size: 12px; }
</style>
