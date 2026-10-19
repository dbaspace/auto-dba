import React, { useEffect, useMemo, useState } from 'react'
import { Button, Form, Input, Modal, Popconfirm, Select, Space, Tag, Tooltip, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { useAppSelector } from '../../store'

function renderRoleTags(ids: string[], names: Record<string, string>, fallback?: string) {
  if (!ids.length) return fallback || '-'
  return (
    <Space size={[4, 4]} wrap>
      {ids.map((id) => (
        <Tag key={id}>{names[id] || id}</Tag>
      ))}
    </Space>
  )
}

function currentUserId(): string {
  try {
    return String(JSON.parse(localStorage.getItem('user') || '{}').id || '').trim()
  } catch {
    return ''
  }
}

function externalCooldownActive(row: any): boolean {
  const until = String(row?.external_cooldown_until || '').trim()
  if (!until) return false
  const t = Date.parse(until)
  return Number.isFinite(t) && t > Date.now()
}

function statusMeta(row: any): { text: string; color: string } {
  const st = String(row?.status || '').toLowerCase()
  if (st === 'disabled' || row?.login_disabled) return { text: '已禁用', color: 'default' }
  if (
    st === 'locked' ||
    (row?.failed_login_count || 0) >= 3 ||
    row?.locked_at ||
    externalCooldownActive(row)
  ) {
    return { text: '已锁定', color: 'error' }
  }
  return { text: '正常', color: 'success' }
}

function isBuiltinAdmin(row: any): boolean {
  return String(row?.username || '').toLowerCase() === 'admin'
}

function larkMeta(row: any): { text: string; color: string } | null {
  if (isBuiltinAdmin(row)) return null
  if (row?.lark_open_id || row?.lark_id) return { text: '已同步', color: 'success' }
  if (row?.has_logged_in) return { text: '未同步', color: 'warning' }
  return { text: '未登录', color: 'default' }
}

function larkSyncSummary(r: any): string {
  const parts = [`已同步 ${r.synced || 0}/${r.total || 0}`]
  if (r.unmatched?.length) parts.push(`未匹配：${r.unmatched.join('、')}`)
  if (r.no_key?.length) parts.push(`缺少邮箱和手机号：${r.no_key.join('、')}`)
  if (r.scanned_pages) parts.push(`通讯录读取 ${r.scanned_pages} 页`)
  if (r.skipped?.length) parts.push(`未处理 ${r.skipped.length} 人`)
  if (r.not_logged_in) parts.push(`未登录 ${r.not_logged_in} 人已跳过`)
  if (r.message) parts.push(r.message)
  return parts.join('；')
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
  const authUser = useAppSelector((s) => s.auth.user)
  const currentTenantId =
    (authUser as { current_tenant?: { id?: string } })?.current_tenant?.id || ''
  const [rows, setRows] = useState<any[]>([])
  const [roles, setRoles] = useState<any[]>([])
  /** 当前租户成员 role_ids：user_id → role_ids */
  const [tenantRoleMap, setTenantRoleMap] = useState<Record<string, string[]>>({})
  const [visible, setVisible] = useState(false)
  const [pwdOpen, setPwdOpen] = useState(false)
  const [pwdTarget, setPwdTarget] = useState<any>(null)
  const [pwdValue, setPwdValue] = useState('')
  const [pwdConfirm, setPwdConfirm] = useState('')
  const [pwdSaving, setPwdSaving] = useState(false)
  const [busyId, setBusyId] = useState('')
  const [larkSyncing, setLarkSyncing] = useState(false)
  const me = currentUserId()
  const [form, setForm] = useState({
    id: '',
    username: '',
    password: '',
    display_name: '',
    email: '',
    lark_email: '',
    role_ids: [] as string[],
  })

  async function syncLark() {
    setLarkSyncing(true)
    try {
      const { data } = await api.post('/users/lark-sync', {}, { silent: true })
      const r = data?.data || {}
      const text = larkSyncSummary(r)
      if (r.message || r.skipped?.length) message.warning(text, 8)
      else if (r.unmatched?.length || r.no_key?.length) message.info(text, 8)
      else message.success(text)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '同步失败')
    } finally {
      setLarkSyncing(false)
    }
  }

  async function load() {
    const [u, r] = await Promise.all([api.get('/users'), api.get('/roles')])
    setRows(u.data.data || [])
    setRoles(r.data.data || [])
    if (currentTenantId) {
      try {
        const { data } = await api.get(`/tenants/${currentTenantId}/members`, { silent: true })
        const map: Record<string, string[]> = {}
        for (const m of data?.data || []) {
          const uid = String(m.user_id || '').trim()
          if (!uid) continue
          map[uid] = Array.isArray(m.role_ids) ? m.role_ids.filter(Boolean) : []
        }
        setTenantRoleMap(map)
      } catch {
        setTenantRoleMap({})
      }
    } else {
      setTenantRoleMap({})
    }
  }

  const names = useMemo(() => {
    const m: Record<string, string> = {}
    for (const r of roles) {
      m[r.id] = r.name || r.code || r.id
    }
    return m
  }, [roles])

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
            lark_email: row.lark_email || '',
            role_ids: ids,
          }
        : {
            id: '',
            username: '',
            password: '',
            display_name: '',
            email: '',
            lark_email: '',
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
    if (form.id) await api.put('/users/' + form.id, { ...payload, lark_email: (form.lark_email || '').trim() })
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
  }, [currentTenantId])

  return (
    <div className="ops-page">
      <div className="ops-title">
        <Space>
          <Auth urn={Perm.users.create}>
            <Button type="primary" onClick={() => open()}>
              新建用户
            </Button>
          </Auth>
          <Auth urn={Perm.users.edit}>
            <Tooltip title="只同步登录过本平台的用户：按飞书邮箱、企业邮箱、手机号批量点查飞书身份，不新建账号">
              <Button loading={larkSyncing} onClick={syncLark}>
                同步 Lark
              </Button>
            </Tooltip>
          </Auth>
        </Space>
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
              title: '企业邮箱',
              dataIndex: 'email',
              ellipsis: true,
              render: (v: string) => v || '-',
            },
            {
              title: '飞书邮箱',
              dataIndex: 'lark_email',
              ellipsis: true,
              render: (v: string) => v || '-',
            },
            {
              title: '飞书',
              width: 90,
              render: (_, row) => {
                const m = larkMeta(row)
                if (!m) return '-'
                const tag = <Tag color={m.color}>{m.text}</Tag>
                return row.lark_sync_time ? (
                  <Tooltip title={`同步于 ${new Date(row.lark_sync_time).toLocaleString()}`}>{tag}</Tooltip>
                ) : (
                  tag
                )
              },
            },
            {
              title: '平台角色',
              width: 280,
              render: (_, row) => {
                const ids =
                  Array.isArray(row.role_ids) && row.role_ids.length > 0
                    ? row.role_ids
                    : row.role_id
                      ? [row.role_id]
                      : []
                return renderRoleTags(ids, names, row.role || '-')
              },
            },
            ...(currentTenantId
              ? [
                  {
                    title: '租户角色',
                    width: 280,
                    render: (_: unknown, row: any) => {
                      const ids = tenantRoleMap[row.id] || []
                      if (!ids.length) return <span style={{ color: '#999' }}>非本租户成员</span>
                      return renderRoleTags(ids, names)
                    },
                  },
                ]
              : []),
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
            label="企业邮箱"
            extra="用于邮件通知和忘记密码重置；平台/LDAP 账号建议填写"
          >
            <Input
              value={form.email}
              onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
              placeholder="user@example.com"
            />
          </Form.Item>
          {form.id ? (
            <Form.Item
              label="飞书邮箱"
              extra="飞书名片上的邮箱；企业邮箱在飞书查不到时填写，同步时优先用它。修改后需重新同步 Lark"
            >
              <Input
                value={form.lark_email}
                onChange={(e) => setForm((f) => ({ ...f, lark_email: e.target.value }))}
                placeholder="可留空"
              />
            </Form.Item>
          ) : null}
          <Form.Item
            label="平台角色"
            extra="可多选，列表全部展示；权限为并集。首位为主角色（JWT/兼容）。业务侧栏看「租户角色」（租户成员里配置）。"
          >
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
