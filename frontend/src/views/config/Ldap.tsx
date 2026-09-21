import React, { useEffect, useMemo, useState } from 'react'
import { Button, Divider, Form, Input, Space, Spin, Switch, message } from 'antd'
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

const DEFAULT_FORM: LdapForm = {
  enabled: false,
  ldap_url: '',
  bind_dn: '',
  bind_password: '',
  base_dn: '',
  user_filter: '(uid=%s)',
  display_name_attr: 'cn',
  test_username: '',
  test_password: '',
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
  // strip path if any
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

export default function Ldap() {
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
        display_name_attr: d.display_name_attr || 'cn',
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

  function reset() {
    load()
    message.info('已恢复为已保存配置')
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <div className="ops-page ldap-page">
      <div className="ops-title">
        <h1>LDAP 配置</h1>
      </div>
      <Spin spinning={loading}>
        <Form
          form={form}
          className="ldap-form"
          labelCol={{ flex: '140px' }}
          wrapperCol={{ flex: 1 }}
          labelAlign="right"
          colon={false}
          initialValues={DEFAULT_FORM}
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
            extra={
              <span className="field-hint">可能的选项是 (cn=%(user)s) 或 (uid=%(user)s) 等</span>
            }
          >
            <Input placeholder="(cn=%s)" />
          </Form.Item>

          <Form.Item
            label="用户名属性"
            name="display_name_attr"
            extra={<span className="field-hint">LDAP 中代表用户名的属性字段，如 uid 或 cn</span>}
          >
            <Input placeholder="cn" />
          </Form.Item>

          <Divider className="ldap-divider" />

          <div className="ldap-section-title">连接测试（可选）</div>
          <Form.Item
            label="测试账号"
            name="test_username"
            extra={<span className="field-hint">填写后测试会做一次完整用户验证</span>}
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
              <Button onClick={reset} disabled={loading || saving}>
                重置
              </Button>
            </Space>
          </div>
        </Form>
      </Spin>
    </div>
  )
}
