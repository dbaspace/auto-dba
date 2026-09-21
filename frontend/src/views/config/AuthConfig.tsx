import React, { useEffect, useMemo, useState } from 'react'
import { Button, Divider, Form, Input, Radio, Space, Spin, Switch, Tabs, message } from 'antd'
import api from '../../api/request'
import './Ldap.css'

interface LdapForm {
  enabled: boolean
  ldap_url: string
  bind_dn: string
  bind_password: string
  base_dn: string
  user_filter: string
  display_name_attr: string
  test_username: string
  test_password: string
}

interface SsoForm {
  enabled: boolean
  grant_mode: 'authorization_code' | 'password'
  base_url: string
  callback_url: string
  authorize_path: string
  token_path: string
  userinfo_path: string
  jwks_uri: string
  client_id: string
  client_secret: string
  scope: string
  username_claim: string
  display_name_claim: string
  email_claim: string
  test_username: string
  test_password: string
}

interface SmtpForm {
  enabled: boolean
  host: string
  port: number
  username: string
  password: string
  from: string
  use_tls: boolean
  frontend_base_url: string
  test_to: string
}

const DEFAULT_LDAP: LdapForm = {
  enabled: false,
  ldap_url: '',
  bind_dn: '',
  bind_password: '',
  base_dn: '',
  user_filter: '(uid=%s)',
  display_name_attr: 'displayName',
  test_username: '',
  test_password: '',
}

const DEFAULT_SSO: SsoForm = {
  enabled: false,
  grant_mode: 'authorization_code',
  base_url: '',
  callback_url: '',
  authorize_path: '/oauth2/authorize',
  token_path: '/oauth2/token',
  userinfo_path: '/oauth2/userinfo',
  jwks_uri: '',
  client_id: '',
  client_secret: '',
  scope: '',
  username_claim: 'nickname',
  display_name_claim: 'name',
  email_claim: 'email',
  test_username: '',
  test_password: '',
}

const DEFAULT_SMTP: SmtpForm = {
  enabled: false,
  host: '',
  port: 465,
  username: '',
  password: '',
  from: '',
  use_tls: true,
  frontend_base_url: 'http://127.0.0.1:5174',
  test_to: '',
}

function composeLdapURL(host: string, port: number, useTLS: boolean): string {
  let h = (host || '').trim()
  if (!h) return ''
  const lower = h.toLowerCase()
  if (lower.startsWith('ldap://') || lower.startsWith('ldaps://')) {
    return h
  }
  const scheme = useTLS ? 'ldaps' : 'ldap'
  const p = port > 0 ? port : useTLS ? 636 : 389
  return `${scheme}://${h}:${p}`
}

function parseLdapURL(raw: string): { host: string; port: number; use_tls: boolean } {
  let s = (raw || '').trim()
  if (!s) {
    return { host: '', port: 389, use_tls: false }
  }
  let useTLS = false
  const lower = s.toLowerCase()
  if (lower.startsWith('ldaps://')) {
    useTLS = true
    s = s.slice('ldaps://'.length)
  } else if (lower.startsWith('ldap://')) {
    s = s.slice('ldap://'.length)
  }
  const slash = s.indexOf('/')
  if (slash >= 0) s = s.slice(0, slash)
  let host = s
  let port = useTLS ? 636 : 389
  const colon = s.lastIndexOf(':')
  if (colon > 0 && !s.includes(']')) {
    const p = parseInt(s.slice(colon + 1), 10)
    if (p > 0) {
      port = p
      host = s.slice(0, colon)
    }
  } else if (s.startsWith('[') && s.includes(']:')) {
    const end = s.indexOf(']:')
    host = s.slice(1, end)
    const p = parseInt(s.slice(end + 2), 10)
    if (p > 0) port = p
  }
  return { host, port, use_tls: useTLS }
}

function LdapPanel() {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [bindSet, setBindSet] = useState(false)
  const [form] = Form.useForm<LdapForm>()

  const pwdPlaceholder = useMemo(
    () => (bindSet ? '已保存则留空不修改' : '请输入绑定密码'),
    [bindSet],
  )

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/ldap')
      const d = data.data || {}
      form.setFieldsValue({
        enabled: !!d.enabled,
        ldap_url: composeLdapURL(d.host || '', d.port > 0 ? d.port : 389, !!d.use_tls),
        bind_dn: d.bind_dn || '',
        bind_password: '',
        base_dn: d.base_dn || '',
        user_filter: d.user_filter || '(uid=%s)',
        display_name_attr: d.display_name_attr || 'displayName',
        test_username: '',
        test_password: '',
      })
      setBindSet(!!d.bind_password_set)
    } finally {
      setLoading(false)
    }
  }

  function payload() {
    const f = form.getFieldsValue()
    const parsed = parseLdapURL(f.ldap_url || '')
    return {
      enabled: f.enabled,
      host: parsed.host,
      port: parsed.port,
      use_tls: parsed.use_tls,
      base_dn: (f.base_dn || '').trim(),
      bind_dn: (f.bind_dn || '').trim(),
      bind_password: f.bind_password || undefined,
      user_filter: (f.user_filter || '').trim() || '(uid=%s)',
      display_name_attr: (f.display_name_attr || '').trim(),
      test_username: (f.test_username || '').trim(),
      test_password: f.test_password,
    }
  }

  async function save() {
    try {
      await form.validateFields(['ldap_url', 'user_filter'])
    } catch {
      return
    }
    const parsed = parseLdapURL(form.getFieldValue('ldap_url') || '')
    if (!parsed.host) {
      message.warning('请填写 LDAP 地址')
      return
    }
    setSaving(true)
    try {
      await api.put('/settings/ldap', payload())
      message.success('已保存')
      form.setFieldsValue({ bind_password: '' })
      await load()
    } finally {
      setSaving(false)
    }
  }

  async function test() {
    try {
      await form.validateFields(['ldap_url'])
    } catch {
      return
    }
    setSaving(true)
    try {
      const { data } = await api.post('/settings/ldap/test', payload())
      message.success(data.message || '连接成功')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Spin spinning={loading}>
      <Form
        form={form}
        className="ldap-form"
        labelCol={{ flex: '140px' }}
        wrapperCol={{ flex: 1 }}
        labelAlign="right"
        colon={false}
        initialValues={DEFAULT_LDAP}
      >
        <div className="ldap-section-title">LDAP 服务器</div>
        <Form.Item label="启用 LDAP" name="enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item
          label="LDAP 地址"
          name="ldap_url"
          required
          rules={[{ required: true, message: '请填写 LDAP 地址' }]}
          extra={<span className="field-hint">如: ldap://localhost:389</span>}
        >
          <Input placeholder="ldap://ldap.example.com:389" />
        </Form.Item>
        <Form.Item label="绑定 DN" name="bind_dn">
          <Input placeholder="cn=admin,ou=users,dc=example,dc=com" />
        </Form.Item>
        <Form.Item label="密码" name="bind_password">
          <Input.Password placeholder={pwdPlaceholder} autoComplete="off" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">LDAP 用户</div>
        <Form.Item
          label="用户 OU"
          name="base_dn"
          extra={<span className="field-hint">多个 OU 使用 | 分割</span>}
        >
          <Input placeholder="dc=example,dc=com" />
        </Form.Item>
        <Form.Item
          label="用户过滤器"
          name="user_filter"
          required
          rules={[{ required: true, message: '请填写用户过滤器' }]}
          extra={<span className="field-hint">如 (uid=%s) 或 (cn=%s)</span>}
        >
          <Input placeholder="(uid=%s)" />
        </Form.Item>
        <Form.Item
          label="显示名属性"
          name="display_name_attr"
          extra={<span className="field-hint">中文名一般用 displayName；若与登录名相同会自动尝试其它属性</span>}
        >
          <Input placeholder="displayName" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">连接测试（可选）</div>
        <Form.Item label="测试账号" name="test_username">
          <Input placeholder="可选" />
        </Form.Item>
        <Form.Item label="测试密码" name="test_password">
          <Input.Password autoComplete="off" placeholder="可选" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-actions">
          <Space size="middle">
            <Button type="primary" loading={saving} onClick={save}>
              保存
            </Button>
            <Button loading={saving} onClick={test}>
              测试连接
            </Button>
            <Button onClick={load} disabled={loading || saving}>
              重置
            </Button>
          </Space>
        </div>
      </Form>
    </Spin>
  )
}

function SsoPanel() {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [secretSet, setSecretSet] = useState(false)
  const [form] = Form.useForm<SsoForm>()

  const secretPlaceholder = useMemo(
    () => (secretSet ? '已保存则留空不修改' : '请输入 Client Secret'),
    [secretSet],
  )

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/sso')
      const d = data.data || {}
      form.setFieldsValue({
        enabled: !!d.enabled,
        grant_mode: d.grant_mode === 'password' ? 'password' : 'authorization_code',
        base_url: d.base_url || '',
        callback_url: d.callback_url || '',
        authorize_path: d.authorize_path || '/oauth2/authorize',
        token_path: d.token_path || '/oauth2/token',
        userinfo_path: d.userinfo_path || '/oauth2/userinfo',
        jwks_uri: d.jwks_uri || '',
        client_id: d.client_id || '',
        client_secret: '',
        scope: d.scope || '',
        username_claim: d.username_claim || 'nickname',
        display_name_claim: d.display_name_claim || 'name',
        email_claim: d.email_claim || 'email',
        test_username: '',
        test_password: '',
      })
      setSecretSet(!!d.client_secret_set)
    } finally {
      setLoading(false)
    }
  }

  function payload() {
    const f = form.getFieldsValue()
    return {
      enabled: f.enabled,
      grant_mode: f.grant_mode === 'password' ? 'password' : 'authorization_code',
      base_url: (f.base_url || '').trim(),
      callback_url: (f.callback_url || '').trim(),
      authorize_path: (f.authorize_path || '').trim() || '/oauth2/authorize',
      token_path: (f.token_path || '').trim() || '/oauth2/token',
      userinfo_path: (f.userinfo_path || '').trim() || '/oauth2/userinfo',
      jwks_uri: (f.jwks_uri || '').trim(),
      client_id: (f.client_id || '').trim(),
      client_secret: f.client_secret || undefined,
      scope: (f.scope || '').trim(),
      username_claim: (f.username_claim || '').trim() || 'nickname',
      display_name_claim: (f.display_name_claim || '').trim() || 'name',
      email_claim: (f.email_claim || '').trim() || 'email',
      test_username: (f.test_username || '').trim(),
      test_password: f.test_password,
    }
  }

  async function save() {
    try {
      await form.validateFields(['base_url', 'client_id'])
    } catch {
      return
    }
    setSaving(true)
    try {
      await api.put('/settings/sso', payload())
      message.success('已保存')
      form.setFieldsValue({ client_secret: '' })
      await load()
    } finally {
      setSaving(false)
    }
  }

  async function test() {
    try {
      await form.validateFields(['base_url', 'client_id'])
    } catch {
      return
    }
    setSaving(true)
    try {
      const { data } = await api.post('/settings/sso/test', payload())
      message.success(data.message || '连接成功')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Spin spinning={loading}>
      <Form
        form={form}
        className="ldap-form"
        labelCol={{ flex: '140px' }}
        wrapperCol={{ flex: 1 }}
        labelAlign="right"
        colon={false}
        initialValues={DEFAULT_SSO}
      >
        <div className="ldap-section-title">SSO 服务器</div>
        <Form.Item
          label="启用 SSO"
          name="enabled"
          valuePropName="checked"
          extra={<span className="field-hint">启用后，登录页「认证方式」可选 SSO</span>}
        >
          <Switch />
        </Form.Item>
        <Form.Item
          label="授权模式"
          name="grant_mode"
          extra={
            <span className="field-hint">
              授权码：登录页选 SSO 后点登录跳转 IdP。密码：登录页选 SSO 后用账号密码换 token（需 IdP 支持
              grant_type=password）。
            </span>
          }
        >
          <Radio.Group
            options={[
              { value: 'authorization_code', label: '授权码' },
              { value: 'password', label: '密码' },
            ]}
          />
        </Form.Item>
        <Form.Item
          label="SSO 地址"
          name="base_url"
          required
          rules={[{ required: true, message: '请填写 SSO 地址' }]}
          extra={<span className="field-hint">IdP 服务根地址，如 https://sso.example.com（用于拼 Authorize / Token / UserInfo）</span>}
        >
          <Input placeholder="https://sso.example.com" />
        </Form.Item>
        <Form.Item
          label="SSO 回调地址"
          name="callback_url"
          dependencies={['grant_mode']}
          rules={[
            ({ getFieldValue }) => ({
              validator(_, value) {
                if (getFieldValue('grant_mode') === 'authorization_code' && !String(value || '').trim()) {
                  return Promise.reject(new Error('授权码模式下请填写 SSO 回调地址（须与 IdP 白名单逐字相同）'))
                }
                return Promise.resolve()
              },
            }),
          ]}
          extra={
            <span className="field-hint">
              授权码模式必填，须与 IdP 白名单逐字相同。注意：IdP 通常不把 localhost 与 127.0.0.1
              视为等价。本地 Saturn 应用请用 http://127.0.0.1:5174/auth/callback（不要用
              localhost）；若 IdP 已登记后端则填 http://127.0.0.1:8800/api/v1/auth/sso/callback
            </span>
          }
        >
          <Input placeholder="http://127.0.0.1:5174/auth/callback" />
        </Form.Item>
        <Form.Item
          label="Authorize 路径"
          name="authorize_path"
          extra={<span className="field-hint">授权码模式使用；相对 SSO 地址或完整 URL</span>}
        >
          <Input placeholder="/oauth2/authorize" />
        </Form.Item>
        <Form.Item
          label="Token 路径"
          name="token_path"
          extra={<span className="field-hint">相对 SSO 地址，或填完整 URL</span>}
        >
          <Input placeholder="/oauth2/token" />
        </Form.Item>
        <Form.Item
          label="UserInfo 路径"
          name="userinfo_path"
          extra={<span className="field-hint">相对 SSO 地址，或填完整 URL</span>}
        >
          <Input placeholder="/oauth2/userinfo" />
        </Form.Item>
        <Form.Item
          label="JWKS URI"
          name="jwks_uri"
          extra={
            <span className="field-hint">
              可选。填写后优先用 id_token（JWKS 校验）取用户信息；相对 SSO 地址或完整 URL，如 /oauth2/jwks / .well-known/jwks.json
            </span>
          }
        >
          <Input placeholder="/oauth2/jwks" />
        </Form.Item>
        <Form.Item
          label="Client ID"
          name="client_id"
          required
          rules={[{ required: true, message: '请填写 Client ID' }]}
        >
          <Input placeholder="应用 Client ID" />
        </Form.Item>
        <Form.Item label="Client Secret" name="client_secret">
          <Input.Password placeholder={secretPlaceholder} autoComplete="off" />
        </Form.Item>
        <Form.Item label="Scope" name="scope">
          <Input placeholder="可选，如 openid profile" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">用户字段映射</div>
        <Form.Item
          label="用户名 Claim"
          name="username_claim"
          extra={<span className="field-hint">本地登录名映射字段，推荐 nickname；不要用 sub（数字 ID）</span>}
        >
          <Input placeholder="nickname" />
        </Form.Item>
        <Form.Item label="显示名 Claim" name="display_name_claim">
          <Input placeholder="name" />
        </Form.Item>
        <Form.Item
          label="邮箱 Claim"
          name="email_claim"
          extra={<span className="field-hint">用于用户邮箱与工单通知等，推荐 email</span>}
        >
          <Input placeholder="email" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">连接测试（可选）</div>
        <Form.Item
          label="测试账号"
          name="test_username"
          extra={
            <span className="field-hint">
              仅密码模式可测账号。授权码模式请留空只测连通，完整登录到登录页选择 SSO。
            </span>
          }
        >
          <Input placeholder="可选" />
        </Form.Item>
        <Form.Item label="测试密码" name="test_password">
          <Input.Password autoComplete="off" placeholder="可选" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-actions">
          <Space size="middle">
            <Button type="primary" loading={saving} onClick={save}>
              保存
            </Button>
            <Button loading={saving} onClick={test}>
              测试连接
            </Button>
            <Button onClick={load} disabled={loading || saving}>
              重置
            </Button>
          </Space>
        </div>
      </Form>
    </Spin>
  )
}

function SmtpPanel() {
  const [form] = Form.useForm<SmtpForm>()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [passwordSet, setPasswordSet] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/smtp')
      const d = data?.data || {}
      setPasswordSet(!!d.password_set)
      form.setFieldsValue({
        ...DEFAULT_SMTP,
        enabled: !!d.enabled,
        host: d.host || '',
        port: d.port || 465,
        username: d.username || '',
        password: '',
        from: d.from || '',
        use_tls: d.use_tls !== false,
        frontend_base_url: d.frontend_base_url || 'http://127.0.0.1:5174',
        test_to: '',
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function payload() {
    const f = form.getFieldsValue()
    return {
      enabled: !!f.enabled,
      host: (f.host || '').trim(),
      port: Number(f.port) || 465,
      username: (f.username || '').trim(),
      password: (f.password || '').trim(),
      from: (f.from || '').trim(),
      use_tls: !!f.use_tls,
      frontend_base_url: (f.frontend_base_url || '').trim(),
    }
  }

  async function onSave() {
    setSaving(true)
    try {
      await api.put('/settings/smtp', payload())
      message.success('已保存')
      form.setFieldValue('password', '')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function onTest() {
    setTesting(true)
    try {
      const f = form.getFieldsValue()
      const { data } = await api.post('/settings/smtp/test', {
        ...payload(),
        test_to: (f.test_to || '').trim(),
      })
      message.success(data?.message || '测试邮件已发送')
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '测试失败')
    } finally {
      setTesting(false)
    }
  }

  return (
    <Spin spinning={loading}>
      <Form form={form} layout="vertical" initialValues={DEFAULT_SMTP} style={{ maxWidth: 640 }}>
        <Form.Item
          label="启用 SMTP"
          name="enabled"
          valuePropName="checked"
          extra={<span className="field-hint">忘记密码发信前必须开启；仅「发送测试邮件」可不依赖此开关</span>}
        >
          <Switch />
        </Form.Item>
        <Form.Item
          label="SMTP 服务器"
          name="host"
          extra={<span className="field-hint">如 smtp.example.com</span>}
        >
          <Input placeholder="smtp.example.com" />
        </Form.Item>
        <Form.Item label="端口" name="port">
          <Input type="number" placeholder="465" />
        </Form.Item>
        <Form.Item label="启用 TLS" name="use_tls" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item label="账号" name="username">
          <Input placeholder="发信账号 / 邮箱" />
        </Form.Item>
        <Form.Item
          label="密码"
          name="password"
          extra={
            <span className="field-hint">
              {passwordSet ? '已保存，留空表示不修改' : 'SMTP 认证密码或授权码'}
            </span>
          }
        >
          <Input.Password placeholder={passwordSet ? '********' : '请输入'} />
        </Form.Item>
        <Form.Item
          label="发件人"
          name="from"
          extra={<span className="field-hint">From 地址，须为合法邮箱</span>}
        >
          <Input placeholder="noreply@example.com" />
        </Form.Item>
        <Form.Item
          label="前端地址"
          name="frontend_base_url"
          extra={
            <span className="field-hint">
              用于拼密码重置链接，如 http://127.0.0.1:5174
            </span>
          }
        >
          <Input placeholder="http://127.0.0.1:5174" />
        </Form.Item>
        <Divider />
        <Form.Item label="测试收件人" name="test_to">
          <Input placeholder="留空则发到发件人地址" />
        </Form.Item>
        <Space>
          <Button type="primary" loading={saving} onClick={onSave}>
            保存
          </Button>
          <Button loading={testing} onClick={onTest}>
            发送测试邮件
          </Button>
        </Space>
      </Form>
    </Spin>
  )
}

function NotifyPanel() {
  const [form] = Form.useForm()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/notify')
      const d = data?.data || {}
      form.setFieldsValue({
        email_on_ticket_create: !!d.email_on_ticket_create,
        webhook_enabled: !!d.webhook_enabled,
        webhook_url: d.webhook_url || '',
        wecom_enabled: !!d.wecom_enabled,
        wecom_corp_id: d.wecom_corp_id || '',
        wecom_agent_id: d.wecom_agent_id || '',
        wecom_secret: '',
        phone_enabled: !!d.phone_enabled,
        phone_api_url: d.phone_api_url || '',
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function onSave() {
    setSaving(true)
    try {
      const f = form.getFieldsValue()
      await api.put('/settings/notify', {
        email_on_ticket_create: !!f.email_on_ticket_create,
        webhook_enabled: !!f.webhook_enabled,
        webhook_url: (f.webhook_url || '').trim(),
        wecom_enabled: !!f.wecom_enabled,
        wecom_corp_id: (f.wecom_corp_id || '').trim(),
        wecom_agent_id: (f.wecom_agent_id || '').trim(),
        wecom_secret: (f.wecom_secret || '').trim(),
        phone_enabled: !!f.phone_enabled,
        phone_api_url: (f.phone_api_url || '').trim(),
      })
      message.success('已保存')
      form.setFieldValue('wecom_secret', '')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Spin spinning={loading}>
      <Form form={form} layout="vertical" style={{ maxWidth: 640 }}>
        <Form.Item
          label="工单创建邮件通知"
          name="email_on_ticket_create"
          valuePropName="checked"
          extra={
            <span className="field-hint">
              开启后，工单创建成功将邮件通知审批人（需「邮件」Tab 已启用 SMTP，且审批人已绑定邮箱）
            </span>
          }
        >
          <Switch />
        </Form.Item>
        <Divider />
        <p className="field-hint" style={{ marginBottom: 12 }}>
          以下渠道本期仅保存配置，发送能力后续开放。
        </p>
        <Form.Item label="启用 Webhook" name="webhook_enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item label="Webhook URL" name="webhook_url">
          <Input placeholder="https://example.com/hooks/..." />
        </Form.Item>
        <Form.Item label="启用企业微信" name="wecom_enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item label="企业微信 CorpID" name="wecom_corp_id">
          <Input placeholder="wwxxxxxxxx" />
        </Form.Item>
        <Form.Item label="企业微信 AgentID" name="wecom_agent_id">
          <Input placeholder="1000002" />
        </Form.Item>
        <Form.Item label="企业微信 Secret" name="wecom_secret">
          <Input.Password placeholder="留空表示不修改" />
        </Form.Item>
        <Form.Item label="启用电话通知" name="phone_enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item label="电话通知 API" name="phone_api_url">
          <Input placeholder="https://..." />
        </Form.Item>
        <Button type="primary" loading={saving} onClick={onSave}>
          保存
        </Button>
      </Form>
    </Spin>
  )
}

export default function AuthConfig() {
  return (
    <div className="ops-page ldap-page">
      <div className="ops-title">
        <h1>认证配置</h1>
      </div>
      <p className="field-hint" style={{ marginBottom: 16 }}>
        开启哪个模块，登录页即可选择对应认证方式。邮件用于忘记密码与工单通知；通知 Tab 可开关工单创建邮件。
      </p>
      <Tabs
        defaultActiveKey="ldap"
        items={[
          { key: 'ldap', label: 'LDAP', children: <LdapPanel /> },
          { key: 'sso', label: 'SSO', children: <SsoPanel /> },
          { key: 'smtp', label: '邮件', children: <SmtpPanel /> },
          { key: 'notify', label: '通知', children: <NotifyPanel /> },
        ]}
      />
    </div>
  )
}
