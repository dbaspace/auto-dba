<template>
  <div class="account-wrap">
    <el-dropdown trigger="click" @command="onCmd">
      <span class="user-trigger">
        <el-avatar :size="24" :src="avatarSrc || undefined" class="user-avatar">
          <el-icon v-if="!avatarSrc" :size="14"><UserFilled /></el-icon>
        </el-avatar>
        <span class="user-name">{{ accountName }}</span>
        <el-icon class="caret"><ArrowDown /></el-icon>
      </span>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item command="profile">
            <el-icon><User /></el-icon>
            <span>个人设置</span>
          </el-dropdown-item>
          <el-dropdown-item command="security">
            <el-icon><Lock /></el-icon>
            <span>安全设置</span>
          </el-dropdown-item>
          <el-dropdown-item command="logout" divided>
            <el-icon><SwitchButton /></el-icon>
            <span>退出登录</span>
          </el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>

    <el-dialog v-model="profileOpen" title="编辑个人信息" width="560px" append-to-body destroy-on-close @open="fillProfile">
      <el-form :model="profile" label-width="100px">
        <el-form-item label="登录账号" required>
          <el-input :model-value="accountName" disabled />
        </el-form-item>
        <el-form-item label="头像">
          <div class="avatar-edit">
            <el-avatar :size="72" :src="profile.avatar || undefined" class="avatar-preview">
              <el-icon v-if="!profile.avatar" :size="28"><Plus /></el-icon>
            </el-avatar>
            <input ref="fileRef" type="file" accept="image/*" hidden @change="onAvatarFile" />
            <el-button size="small" @click="fileRef?.click()">上传头像</el-button>
            <el-button v-if="profile.avatar" size="small" @click="profile.avatar = ''">清除</el-button>
          </div>
        </el-form-item>
        <el-form-item label="姓名" required>
          <el-input v-model="profile.display_name" placeholder="请输入" />
        </el-form-item>
        <el-form-item label="性别" required>
          <el-select v-model="profile.gender" placeholder="请选择" style="width:100%">
            <el-option label="男" value="男" />
            <el-option label="女" value="女" />
          </el-select>
        </el-form-item>
        <el-form-item label="邮箱" required>
          <el-input v-model="profile.email" placeholder="请输入" />
        </el-form-item>
        <el-form-item label="手机号">
          <el-input v-model="profile.phone" placeholder="请输入" />
        </el-form-item>
        <el-form-item label="IM类型">
          <el-select v-model="profile.im_type" clearable placeholder="请选择" style="width:100%">
            <el-option label="飞书" value="feishu" />
            <el-option label="钉钉" value="dingding" />
            <el-option label="企业微信" value="wechat" />
            <el-option label="Lark" value="lark" />
          </el-select>
        </el-form-item>
        <el-form-item label="IM账号">
          <el-input v-model="profile.im_account" placeholder="请填写" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="profileOpen = false">取消</el-button>
        <el-button type="primary" :loading="profileSaving" @click="saveProfile">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="securityOpen" title="安全设置" width="560px" append-to-body destroy-on-close @closed="pwdOpen = false">
      <div class="sec-row">
        <span>密码</span>
        <el-button size="small" @click="openPwd">
          <el-icon class="btn-ico"><Edit /></el-icon>
          修改密码
        </el-button>
      </div>
      <div v-if="pwdOpen" class="pwd-box">
        <el-form :model="pwd" label-width="96px" @submit.prevent="savePwd">
          <el-form-item label="当前账号">{{ accountName }}</el-form-item>
          <el-form-item label="原密码">
            <el-input v-model="pwd.old_password" type="password" show-password autocomplete="current-password" />
          </el-form-item>
          <el-form-item label="新密码">
            <el-input v-model="pwd.new_password" type="password" show-password autocomplete="new-password" placeholder="至少 6 位" />
          </el-form-item>
          <el-form-item label="确认新密码">
            <el-input v-model="pwd.new_password2" type="password" show-password autocomplete="new-password" />
          </el-form-item>
          <el-form-item>
            <el-button @click="pwdOpen = false">取消</el-button>
            <el-button type="primary" :loading="pwdSaving" @click="savePwd">保存</el-button>
          </el-form-item>
        </el-form>
      </div>
      <div class="sec-row">
        <span>MFA认证</span>
        <el-button size="small" @click="ElMessage.info('独立部署暂未开放 MFA 认证')">
          <el-icon class="btn-ico"><Lock /></el-icon>
          开启MFA认证
        </el-button>
      </div>
      <div class="sec-row">
        <span>API Key管理</span>
        <el-button size="small" @click="ElMessage.info('独立部署暂未开放 API Key 管理')">
          <el-icon class="btn-ico"><Key /></el-icon>
          管理 API Key
        </el-button>
      </div>
      <template #footer>
        <el-button type="primary" link @click="securityOpen = false">关闭设置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Edit, Key, Lock, Plus, SwitchButton, User, UserFilled } from '@element-plus/icons-vue'
import api from '../api/client'

const props = defineProps({
  user: { type: Object, default: () => ({}) },
})
const emit = defineEmits(['updated'])

const router = useRouter()
const fileRef = ref(null)
const profileOpen = ref(false)
const profileSaving = ref(false)
const securityOpen = ref(false)
const pwdOpen = ref(false)
const pwdSaving = ref(false)
const profile = reactive({
  display_name: '',
  avatar: '',
  gender: '男',
  email: '',
  phone: '',
  im_type: '',
  im_account: '',
})
const pwd = reactive({ old_password: '', new_password: '', new_password2: '' })

const accountName = computed(() => props.user?.username || props.user?.display_name || '未登录')
const avatarSrc = computed(() => props.user?.avatar || '')

function fillProfile() {
  profile.display_name = props.user?.display_name || ''
  profile.avatar = props.user?.avatar || ''
  profile.gender = props.user?.gender || '男'
  profile.email = props.user?.email || ''
  profile.phone = props.user?.phone || ''
  profile.im_type = props.user?.im_type || ''
  profile.im_account = props.user?.im_account || ''
}

function onCmd(cmd) {
  if (cmd === 'profile') {
    profileOpen.value = true
    return
  }
  if (cmd === 'security') {
    securityOpen.value = true
    return
  }
  if (cmd === 'logout') logout()
}

function openPwd() {
  if (props.user?.source === 'ldap') {
    ElMessage.warning('LDAP 账号请在目录服务中修改密码')
    return
  }
  pwd.old_password = ''
  pwd.new_password = ''
  pwd.new_password2 = ''
  pwdOpen.value = true
}

function onAvatarFile(e) {
  const file = e.target.files?.[0]
  e.target.value = ''
  if (!file) return
  if (file.size > 200 * 1024) {
    ElMessage.warning('头像请小于 200KB')
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    profile.avatar = String(reader.result || '')
  }
  reader.readAsDataURL(file)
}

async function saveProfile() {
  if (!profile.display_name.trim()) {
    ElMessage.warning('请输入姓名')
    return
  }
  if (!profile.gender) {
    ElMessage.warning('请选择性别')
    return
  }
  if (!profile.email.trim() || !profile.email.includes('@')) {
    ElMessage.warning('请输入正确邮箱')
    return
  }
  profileSaving.value = true
  try {
    const { data } = await api.put('/auth/profile', { ...profile })
    ElMessage.success('已保存')
    emit('updated', data.data || { ...props.user, ...profile })
    profileOpen.value = false
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '保存失败')
  } finally {
    profileSaving.value = false
  }
}

async function savePwd() {
  if (!pwd.old_password) {
    ElMessage.warning('请输入原密码')
    return
  }
  if (!pwd.new_password || pwd.new_password.length < 6) {
    ElMessage.warning('新密码至少 6 位')
    return
  }
  if (pwd.new_password !== pwd.new_password2) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }
  pwdSaving.value = true
  try {
    await api.put('/auth/password', { old_password: pwd.old_password, new_password: pwd.new_password })
    ElMessage.success('密码已更新')
    pwdOpen.value = false
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '修改失败')
  } finally {
    pwdSaving.value = false
  }
}

async function logout() {
  try {
    await ElMessageBox.confirm('请确定退出登陆', '退出登陆', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  sessionStorage.removeItem('auto-dba-query-workspace')
  sessionStorage.removeItem('auto-dba-ddl-draft')
  sessionStorage.removeItem('auto-dba-dml-draft')
  sessionStorage.removeItem('auto-dba-dml-prefill')
  sessionStorage.removeItem('auto-dba-createdb-draft')
  router.push('/login')
}

defineExpose({ closeAll() {
  profileOpen.value = false
  securityOpen.value = false
  pwdOpen.value = false
} })
</script>

<style scoped>
.user-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  color: #cfd8e3;
  outline: none;
}
.user-trigger:hover { color: #69c0ff; }
.user-avatar { background: #7883cb; color: #fff; flex-shrink: 0; }
.user-name { font-size: 14px; }
.caret { font-size: 12px; color: #cfd8e3; }
.avatar-edit { display: flex; align-items: center; gap: 12px; }
.avatar-preview { background: #f3f4f6; color: #9ca3af; cursor: pointer; }
.sec-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 4px;
  border-bottom: 1px solid #f0f0f0;
  font-size: 14px;
}
.pwd-box { padding: 12px 4px 4px; border-bottom: 1px solid #f0f0f0; }
.btn-ico { margin-right: 4px; }
</style>
