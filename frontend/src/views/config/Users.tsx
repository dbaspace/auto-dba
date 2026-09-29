import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Popconfirm, Select, Space, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'

function currentUserId(): string {
  try {
    return String(JSON.parse(localStorage.getItem('user') || '{}').id || '').trim()
  } catch {
    return ''
  }
}

function statusMeta(row: any): { text: string; color: string } {
  const st = String(row?.status || '').toLowerCase()
  if (st === 'disabled' || row?.login_disabled) return { text: '已禁用', color: 'default' }
  if (st === 'locked' || (row?.failed_login_count || 0) >= 3 || row?.locked_at) {
    return { text: '已锁定', color: 'error' }
  }
  return { text: '正常', color: 'success' }
}

function isBuiltinAdmin(row: any): boolean {
  return String(row?.username || '').toLowerCase() === 'admin'
}

function canAdminResetPassword(row: any): boolean {
  const src = String(row?.source || 'platform').toLowerCase()
  return src === 'platform' || src === '' || src === 'ldap'
}

function deleteConfirmCopy(row: any): { title: string; description: string } {
  const src = String(row?.source || 'platform').toLowerCase()
  if (src === 'ldap' || src === 'sso') {
    const dir = src === 'ldap' ? 'LDAP' : 'SSO'
    return {
      title: `确认删除该${dir}本地账号？`,
      description: `仅删除本系统中的本地账号与权限，不会删除${dir}目录中的账号；之后仍可用目录密码登录，并可能自动重建本地账号。`,
    }
  }
  return {
    title: '确认删除该平台账号？',
    description: '仅删除本系统用户管理中的账号，不会影响 LDAP/SSO 或外部任务服务器上的账号。',
  }
}

export default function Users() {
  const [rows, setRows] = useState<any[]>([])
  const [roles, setRoles] = useState<any[]>([])
  const [visible, setVisible] = useState(false)
  const [pwdOpen, setPwdOpen] = useState(false)
  const [pwdTarget, setPwdTarget] = useState<any>(null)
  const [pwdValue, setPwdValue] = useState('')
  const [pwdConfirm, setPwdConfirm] = useState('')
  const [pwdSaving, setPwdSaving] = useState(false)
  const [busyId, setBusyId] = useState('')
  const me = currentUserId()
  const [form, setForm] = useState({
    id: '',
    username: '',
    password: '',
    display_name: '',
    email: '',
    role_ids: [] as string[],
  })

  async function load() {
    const [u, r] = await Promise.all([api.get('/users'), api.get('/roles')])
    setRows(u.data.data || [])
    setRoles(r.data.data || [])
  }

  function roleNameMap() {
    const m: Record<string, string> = {}
    for (const r of roles) {
      m[r.id] = r.name || r.code || r.id
    }
    return m
  }

  function open(row?: any) {
    const ids =
      Array.isArray(row?.role_ids) && row.role_ids.length > 0
        ? row.role_ids
        : row?.role_id
          ? [row.role_id]
          : roles[0]?.id
            ? [roles[0].id]
            : []
    setForm(
      row
        ? {
            id: row.id,
            username: row.username,
            password: '',
            display_name: row.display_name,
            email: row.email || '',
            role_ids: ids,
          }
        : {
            id: '',
            username: '',
            password: '',
            display_name: '',
            email: '',
            role_ids: roles[0]?.id ? [roles[0].id] : [],
          },
    )
    setVisible(true)
  }

  async function save() {
    if (!form.id && !form.username) {
      message.warning('请输入用户名')
      return
    }
    if (!form.role_ids?.length) {
      message.warning('请至少选择一个角色')
      return
    }
    const payload = {
      username: form.username,
      password: form.password,
      display_name: form.display_name,
      email: (form.email || '').trim(),
      role_ids: form.role_ids,
      role_id: form.role_ids[0],
    }
    if (form.id) await api.put('/users/' + form.id, payload)
    else await api.post('/users', payload)
    message.success('已保存')
    setVisible(false)
    await load()
  }

  async function runAction(id: string, action: () => Promise<void>, okMsg: string) {
    setBusyId(id)
    try {
      await action()
      message.success(okMsg)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '操作失败')
    } finally {
      setBusyId('')
    }
  }

  function openResetPwd(row: any) {
    setPwdTarget(row)
    setPwdValue('')
    setPwdConfirm('')
    setPwdOpen(true)
  }

  async function submitResetPwd() {
    if (!pwdTarget?.id) return
    const pwd = pwdValue.trim()
    if (pwd.length < 6) {
      message.warning('新密码至少 6 位')
      return
    }
    if (pwd !== pwdConfirm) {
      message.warning('两次输入的密码不一致')
      return
    }
    setPwdSaving(true)
    try {
      const { data } = await api.put('/users/' + pwdTarget.id + '/password', { password: pwd })
      message.success(data.message || '密码已更新')
      setPwdOpen(false)
      setPwdTarget(null)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '修改失败')
    } finally {
      setPwdSaving(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const names = roleNameMap()

  return (
    <div className="ops-page">
      <div className="ops-title">
        <Auth urn={Perm.users.create}>
          <Button type="primary" onClick={() => open()}>
            新建用户
          </Button>
        </Auth>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || r.username}
          dataSource={rows}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '用户名', dataIndex: 'username' },
            { title: '显示名', dataIndex: 'display_name' },
            {
              title: '邮箱',
              dataIndex: 'email',
              ellipsis: true,
              render: (v: string) => v || '-',
            },
            {
              title: '角色',
              width: 220,
              render: (_, row) => {
                const ids =
                  Array.isArray(row.role_ids) && row.role_ids.length > 0
                    ? row.role_ids
                    : row.role_id
                      ? [row.role_id]
                      : []
                if (!ids.length) {
                  return row.role || '-'
                }
                return (
                  <Space size={[4, 4]} wrap>
                    {ids.map((id: string) => (
                      <Tag key={id}>{names[id] || row.role || id}</Tag>
                    ))}
                  </Space>
                )
              },
            },
            {
              title: '来源',
              width: 100,
              render: (_, row) => (
                <Tag color={row.source === 'ldap' ? 'warning' : row.source === 'sso' ? 'processing' : 'default'}>
                  {row.source === 'ldap' ? 'LDAP' : row.source === 'sso' ? 'SSO' : '平台'}
                </Tag>
              ),
            },
            {
              title: '状态',
              width: 100,
              render: (_, row) => {
                const m = statusMeta(row)
                return <Tag color={m.color}>{m.text}</Tag>
              },
            },
            {
              title: '操作',
              width: 340,
              render: (_, row) => {
                const st = statusMeta(row)
                const self = me && row.id === me
                const builtin = isBuiltinAdmin(row)
                const canPwd = canAdminResetPassword(row)
                const loading = busyId === row.id
                const delCopy = deleteConfirmCopy(row)
                return (
                  <Space size={0} wrap>
                    <Auth urn={Perm.users.edit}>
                      <Button type="link" onClick={() => open(row)}>
                        编辑
                      </Button>
                    </Auth>
                    {canPwd ? (
                      <Auth urn={Perm.users.edit}>
                        <Button type="link" onClick={() => openResetPwd(row)}>
                          改密
                        </Button>
                      </Auth>
                    ) : null}
                    {!builtin && !self ? (
                      <Auth urn={Perm.users.edit}>
                        {st.text === '已禁用' ? (
                          <Button
                            type="link"
                            loading={loading}
                            onClick={() =>
                              runAction(row.id, () => api.put('/users/' + row.id + '/enable'), '已启用')
                            }
                          >
                            启用
                          </Button>
                        ) : (
                          <Popconfirm
                            title="确认禁用该账号？禁用后将无法登录。"
                            onConfirm={() =>
                              runAction(row.id, () => api.put('/users/' + row.id + '/disable'), '已禁用')
                            }
                          >
                            <Button type="link" danger loading={loading}>
                              禁用
                            </Button>
                          </Popconfirm>
                        )}
                      </Auth>
                    ) : null}
                    {st.text === '已锁定' && !builtin ? (
                      <Auth urn={Perm.users.edit}>
                        <Button
                          type="link"
                          loading={loading}
                          onClick={() =>
                            runAction(row.id, () => api.put('/users/' + row.id + '/unlock'), '已解锁')
                          }
                        >
                          解锁
                        </Button>
                      </Auth>
                    ) : null}
                    {!builtin && !self ? (
                      <Auth urn={Perm.users.delete}>
                        <Popconfirm
                          title={delCopy.title}
                          description={delCopy.description}
                          onConfirm={() =>
                            runAction(row.id, () => api.delete('/users/' + row.id), '已删除')
                          }
                        >
                          <Button type="link" danger loading={loading}>
                            删除
                          </Button>
                        </Popconfirm>
                      </Auth>
                    ) : null}
                  </Space>
                )
              },
            },
          ]}
        />
      </div>
      <Modal
        open={visible}
        title={form.id ? '编辑用户' : '新建用户'}
        width={420}
        destroyOnClose
        onCancel={() => setVisible(false)}
        footer={
          <Space>
            <Button onClick={() => setVisible(false)}>取消</Button>
            <Button type="primary" onClick={save}>
              保存
            </Button>
          </Space>
        }
      >
        <Form labelCol={{ style: { width: 90 } }}>
          <Form.Item label="用户名">
            <Input
              value={form.username}
              disabled={!!form.id}
              onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))}
            />
          </Form.Item>
          {!form.id ? (
            <Form.Item label="密码">
              <Input.Password
                value={form.password}
                onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
              />
            </Form.Item>
          ) : null}
          <Form.Item label="显示名">
            <Input
              value={form.display_name}
              onChange={(e) => setForm((f) => ({ ...f, display_name: e.target.value }))}
            />
          </Form.Item>
          <Form.Item
            label="邮箱"
            extra="用于忘记密码重置；平台/LDAP 账号建议填写"
          >
            <Input
              value={form.email}
              onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
              placeholder="user@example.com"
            />
          </Form.Item>
          <Form.Item label="角色" extra="可多选；权限为各角色并集。列表首位为主角色（展示/JWT）。">
            <Select
              mode="multiple"
              style={{ width: '100%' }}
              value={form.role_ids}
              onChange={(v) => setForm((f) => ({ ...f, role_ids: v }))}
              options={roles.map((r) => ({ value: r.id, label: r.name }))}
              placeholder="请选择角色"
            />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        open={pwdOpen}
        title={
          pwdTarget
            ? `修改密码 · ${pwdTarget.username}${
                String(pwdTarget.source || '').toLowerCase() === 'ldap' ? '（LDAP）' : ''
              }`
            : '修改密码'
        }
        width={420}
        destroyOnClose
        confirmLoading={pwdSaving}
        onCancel={() => {
          setPwdOpen(false)
          setPwdTarget(null)
        }}
        onOk={submitResetPwd}
        okText="确定"
        cancelText="取消"
      >
        <Form labelCol={{ style: { width: 90 } }} style={{ marginTop: 8 }}>
          {String(pwdTarget?.source || '').toLowerCase() === 'ldap' ? (
            <p className="hint" style={{ marginBottom: 12 }}>
              将通过 Bind 账号写入 LDAP 目录密码，需目录支持改密（建议 LDAPS）。
            </p>
          ) : null}
          <Form.Item label="新密码" required>
            <Input.Password
              value={pwdValue}
              autoComplete="new-password"
              placeholder="至少 6 位"
              onChange={(e) => setPwdValue(e.target.value)}
            />
          </Form.Item>
          <Form.Item label="确认密码" required>
            <Input.Password
              value={pwdConfirm}
              autoComplete="new-password"
              placeholder="再次输入新密码"
              onChange={(e) => setPwdConfirm(e.target.value)}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
