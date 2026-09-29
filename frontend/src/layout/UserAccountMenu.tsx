import React, { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Avatar,
  Button,
  Dropdown,
  Form,
  Input,
  Modal,
  Select,
  message,
} from 'antd'
import {
  DownOutlined,
  EditOutlined,
  KeyOutlined,
  LockOutlined,
  UserOutlined,
  PoweroffOutlined,
} from '@ant-design/icons'
import api from '../api/request'
import { useAppDispatch } from '../store'
import { patchUser, clearAuth, CurrentUser } from '../store/authSlice'
import { resetTabs } from '../store/tabsSlice'
import { clearLastActivity } from '../hooks/useIdleLogout'
import { clearSharedLegacyDrafts } from '../utils/workOrderDraft'

interface Props {
  user: CurrentUser
}

export default function UserAccountMenu({ user }: Props) {
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const fileRef = useRef<HTMLInputElement>(null)
  const [profileOpen, setProfileOpen] = useState(false)
  const [profileSaving, setProfileSaving] = useState(false)
  const [securityOpen, setSecurityOpen] = useState(false)
  const [pwdOpen, setPwdOpen] = useState(false)
  const [pwdSaving, setPwdSaving] = useState(false)
  const [profileForm] = Form.useForm()
  const [pwdForm] = Form.useForm()
  const [avatar, setAvatar] = useState('')

  const accountName = user?.username || user?.display_name || '未登录'
  const avatarSrc = user?.avatar || ''

  function openProfile() {
    setAvatar(user?.avatar || '')
    profileForm.setFieldsValue({
      display_name: user?.display_name || '',
      gender: (user?.gender as string) || '男',
      email: (user?.email as string) || '',
      phone: (user?.phone as string) || '',
      im_type: (user?.im_type as string) || undefined,
      im_account: (user?.im_account as string) || '',
    })
    setProfileOpen(true)
  }

  function openPwd() {
    if (user?.source === 'sso') {
      message.warning('SSO 账号请在统一认证中心修改密码')
      return
    }
    pwdForm.resetFields()
    setPwdOpen(true)
  }

  function onAvatarFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    if (file.size > 200 * 1024) {
      message.warning('头像请小于 200KB')
      return
    }
    const reader = new FileReader()
    reader.onload = () => setAvatar(String(reader.result || ''))
    reader.readAsDataURL(file)
  }

  async function saveProfile() {
    const values = await profileForm.validateFields()
    setProfileSaving(true)
    try {
      const payload = { ...values, avatar }
      const { data } = await api.put('/auth/profile', payload)
      message.success('已保存')
      dispatch(patchUser(data.data || { ...user, ...payload }))
      setProfileOpen(false)
    } catch (e: any) {
      if (e?.errorFields) return
      message.error(e?.response?.data?.message || '保存失败')
    } finally {
      setProfileSaving(false)
    }
  }

  async function savePwd() {
    const values = await pwdForm.validateFields()
    if (values.new_password !== values.new_password2) {
      message.warning('两次输入的新密码不一致')
      return
    }
    setPwdSaving(true)
    try {
      await api.put('/auth/password', {
        old_password: values.old_password,
        new_password: values.new_password,
      })
      message.success('密码已更新')
      setPwdOpen(false)
    } catch (e: any) {
      if (e?.errorFields) return
      message.error(e?.response?.data?.message || '修改失败')
    } finally {
      setPwdSaving(false)
    }
  }

  function logout() {
    Modal.confirm({
      title: '退出登陆',
      content: '请确定退出登陆',
      okText: '确定',
      cancelText: '取消',
      onOk() {
        clearLastActivity()
        dispatch(clearAuth())
        dispatch(resetTabs({}))
        sessionStorage.removeItem('auto-dba-dml-prefill')
        sessionStorage.removeItem('auto-dba-query-workspace')
        clearSharedLegacyDrafts()
        navigate('/login')
      },
    })
  }

  return (
    <div className="account-wrap">
      <Dropdown
        trigger={['click']}
        menu={{
          items: [
            { key: 'profile', icon: <UserOutlined />, label: '个人设置' },
            { key: 'security', icon: <LockOutlined />, label: '安全设置' },
            { type: 'divider' },
            { key: 'logout', icon: <PoweroffOutlined />, label: '退出登录' },
          ],
          onClick: ({ key }) => {
            if (key === 'profile') openProfile()
            else if (key === 'security') setSecurityOpen(true)
            else if (key === 'logout') logout()
          },
        }}
      >
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            cursor: 'pointer',
            color: '#1f2937',
          }}
        >
          <Avatar size={24} src={avatarSrc || undefined} style={{ background: '#7883cb' }}>
            {!avatarSrc && <UserOutlined />}
          </Avatar>
          <span style={{ fontSize: 14 }}>{accountName}</span>
          <DownOutlined style={{ fontSize: 12 }} />
        </span>
      </Dropdown>

      <Modal
        title="编辑个人信息"
        open={profileOpen}
        width={560}
        onCancel={() => setProfileOpen(false)}
        onOk={saveProfile}
        confirmLoading={profileSaving}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={profileForm} labelCol={{ span: 5 }}>
          <Form.Item label="登录账号">
            <Input value={accountName} disabled />
          </Form.Item>
          <Form.Item label="头像">
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <Avatar size={72} src={avatar || undefined} style={{ background: '#f3f4f6', color: '#9ca3af' }}>
                {!avatar && '+'}
              </Avatar>
              <input ref={fileRef} type="file" accept="image/*" hidden onChange={onAvatarFile} />
              <Button size="small" onClick={() => fileRef.current?.click()}>上传头像</Button>
              {avatar && <Button size="small" onClick={() => setAvatar('')}>清除</Button>}
            </div>
          </Form.Item>
          <Form.Item label="姓名" name="display_name" rules={[{ required: true, message: '请输入姓名' }]}>
            <Input placeholder="请输入" />
          </Form.Item>
          <Form.Item label="性别" name="gender" rules={[{ required: true, message: '请选择性别' }]}>
            <Select
              options={[
                { label: '男', value: '男' },
                { label: '女', value: '女' },
              ]}
            />
          </Form.Item>
          <Form.Item
            label="邮箱"
            name="email"
            rules={[{ required: true, type: 'email', message: '请输入正确邮箱' }]}
          >
            <Input placeholder="请输入" />
          </Form.Item>
          <Form.Item label="手机号" name="phone">
            <Input placeholder="请输入" />
          </Form.Item>
          <Form.Item label="IM类型" name="im_type">
            <Select
              allowClear
              placeholder="请选择"
              options={[
                { label: '飞书', value: 'feishu' },
                { label: '钉钉', value: 'dingding' },
                { label: '企业微信', value: 'wechat' },
                { label: 'Lark', value: 'lark' },
              ]}
            />
          </Form.Item>
          <Form.Item label="IM账号" name="im_account">
            <Input placeholder="请填写" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="安全设置"
        open={securityOpen}
        width={560}
        onCancel={() => {
          setSecurityOpen(false)
          setPwdOpen(false)
        }}
        footer={<Button type="link" onClick={() => setSecurityOpen(false)}>关闭设置</Button>}
        destroyOnClose
      >
        <div style={secRow}>
          <span>密码</span>
          <Button size="small" icon={<EditOutlined />} onClick={openPwd}>修改密码</Button>
        </div>
        {user?.source === 'ldap' && !pwdOpen ? (
          <p style={{ margin: '0 0 8px', color: '#909399', fontSize: 12 }}>
            LDAP 账号将同步修改目录服务中当前登录账号（{accountName}）的密码，无法修改他人账号。
          </p>
        ) : null}
        {pwdOpen && (
          <div style={{ padding: '12px 4px 4px', borderBottom: '1px solid #f0f0f0' }}>
            <Form form={pwdForm} labelCol={{ span: 6 }}>
              <Form.Item label="当前账号">{accountName}</Form.Item>
              {user?.source === 'ldap' ? (
                <p style={{ margin: '0 0 12px 25%', color: '#909399', fontSize: 12 }}>
                  将同步修改目录服务中当前登录账号的密码
                </p>
              ) : null}
              <Form.Item label="原密码" name="old_password" rules={[{ required: true, message: '请输入原密码' }]}>
                <Input.Password autoComplete="current-password" />
              </Form.Item>
              <Form.Item
                label="新密码"
                name="new_password"
                rules={[{ required: true, min: 6, message: '新密码至少 6 位' }]}
              >
                <Input.Password autoComplete="new-password" placeholder="至少 6 位" />
              </Form.Item>
              <Form.Item label="确认新密码" name="new_password2" rules={[{ required: true, message: '请确认新密码' }]}>
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Form.Item>
                <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                  <Button onClick={() => setPwdOpen(false)}>取消</Button>
                  <Button type="primary" loading={pwdSaving} onClick={savePwd}>保存</Button>
                </div>
              </Form.Item>
            </Form>
          </div>
        )}
        <div style={secRow}>
          <span>MFA认证</span>
          <Button size="small" icon={<LockOutlined />} onClick={() => message.info('独立部署暂未开放 MFA 认证')}>
            开启MFA认证
          </Button>
        </div>
        <div style={secRow}>
          <span>API Key管理</span>
          <Button size="small" icon={<KeyOutlined />} onClick={() => message.info('独立部署暂未开放 API Key 管理')}>
            管理 API Key
          </Button>
        </div>
      </Modal>
    </div>
  )
}

const secRow: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '14px 4px',
  borderBottom: '1px solid #f0f0f0',
  fontSize: 14,
}
