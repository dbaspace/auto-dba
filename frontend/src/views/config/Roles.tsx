import React, { useEffect, useMemo, useState } from 'react'
import { Button, Form, Input, Modal, Space, Table, Tree, message } from 'antd'
import type { DataNode } from 'antd/es/tree'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { ActionVerbTag, resolveMenuKind, MenuKindTag } from '../../utils/menuKind'

const BUILTIN_CODES = new Set(['admin', 'operator', 'viewer', 'developer'])

type RoleForm = {
  id: string
  name: string
  code: string
  description: string
}

type MenuNodeRaw = {
  id: string
  title: string
  urn?: string
  permission_desc?: string
  children?: MenuNodeRaw[]
}

function collectExpandableKeys(nodes: DataNode[]): React.Key[] {
  const keys: React.Key[] = []
  const walk = (list: DataNode[]) => {
    for (const n of list) {
      if (n.children?.length) {
        keys.push(n.key)
        walk(n.children)
      }
    }
  }
  walk(nodes)
  return keys
}

function findNodePath(nodes: DataNode[], target: string, path: string[] = []): string[] | null {
  for (const n of nodes) {
    const key = String(n.key)
    const next = [...path, key]
    if (key === target) return next
    if (n.children?.length) {
      const hit = findNodePath(n.children, target, next)
      if (hit) return hit
    }
  }
  return null
}

function collectDescendantKeys(node: DataNode): string[] {
  const out: string[] = []
  const walk = (list?: DataNode[]) => {
    if (!list?.length) return
    for (const n of list) {
      out.push(String(n.key))
      walk(n.children)
    }
  }
  walk(node.children)
  return out
}

function findNode(nodes: DataNode[], target: string): DataNode | null {
  for (const n of nodes) {
    if (String(n.key) === target) return n
    if (n.children?.length) {
      const hit = findNode(n.children, target)
      if (hit) return hit
    }
  }
  return null
}

/** 勾选子项 → 勾上祖先；取消父项 → 去掉全部子孙。允许「仅页面」不带操作。 */
function applyPermCheck(tree: DataNode[], prev: React.Key[], nodeKey: string, checked: boolean): React.Key[] {
  const set = new Set(prev.map(String))
  if (checked) {
    set.add(nodeKey)
    const path = findNodePath(tree, nodeKey)
    if (path) {
      for (const k of path) set.add(k)
    }
  } else {
    set.delete(nodeKey)
    const node = findNode(tree, nodeKey)
    if (node) {
      for (const k of collectDescendantKeys(node)) set.delete(k)
    }
  }
  return [...set]
}

function filterTreeByTitle(nodes: DataNode[], keyword: string): DataNode[] {
  const q = keyword.trim().toLowerCase()
  if (!q) return nodes
  const walk = (list: DataNode[]): DataNode[] => {
    const out: DataNode[] = []
    for (const n of list) {
      const titleText = extractTitleText(n.title)
      const kids = n.children?.length ? walk(n.children) : []
      if (titleText.toLowerCase().includes(q) || kids.length) {
        out.push({ ...n, children: kids.length ? kids : n.children })
      }
    }
    return out
  }
  return walk(nodes)
}

function extractTitleText(title: DataNode['title']): string {
  if (typeof title === 'string') return title
  if (React.isValidElement(title)) {
    const props = title.props as { title?: string; children?: React.ReactNode }
    if (typeof props.title === 'string') return props.title
    if (typeof props.children === 'string') return props.children
    if (Array.isArray(props.children)) {
      return props.children.map((c) => (typeof c === 'string' ? c : '')).join('')
    }
  }
  return ''
}

function formatTime(raw: string): string {
  if (!raw) return '—'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export default function Roles() {
  const [roles, setRoles] = useState<any[]>([])
  const [tree, setTree] = useState<DataNode[]>([])
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [roleForm, setRoleForm] = useState<RoleForm>({ id: '', name: '', code: '', description: '' })
  const [checkedKeys, setCheckedKeys] = useState<React.Key[]>([])
  const [permSearch, setPermSearch] = useState('')
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([])
  const [membersOpen, setMembersOpen] = useState(false)
  const [membersLoading, setMembersLoading] = useState(false)
  const [members, setMembers] = useState<any[]>([])
  const [membersRole, setMembersRole] = useState<any>(null)

  async function load() {
    const [r, t] = await Promise.all([api.get('/roles'), api.get('/menus/tree')])
    setRoles(r.data.data || [])
    const mapped = mapTree((t.data.data || []) as MenuNodeRaw[])
    setTree(mapped)
    setExpandedKeys(collectExpandableKeys(mapped))
  }

  function mapTree(nodes: MenuNodeRaw[]): DataNode[] {
    return (nodes || []).map((n) => ({
      key: n.id,
      title: (
        <span title={n.permission_desc || n.title}>
          {n.title}
          <MenuKindTag kind={resolveMenuKind(n)} />
          <ActionVerbTag urn={n.urn} />
        </span>
      ),
      children: n.children?.length ? mapTree(n.children) : undefined,
    }))
  }

  const filteredTree = useMemo(() => filterTreeByTitle(tree, permSearch), [tree, permSearch])

  async function openCreate() {
    setRoleForm({ id: '', name: '', code: '', description: '' })
    setCheckedKeys([])
    setPermSearch('')
    setExpandedKeys(collectExpandableKeys(tree))
    setVisible(true)
  }

  async function openEdit(row: any) {
    setRoleForm({
      id: row.id,
      name: row.name || '',
      code: row.code || '',
      description: row.description || '',
    })
    setCheckedKeys([])
    setPermSearch('')
    setExpandedKeys(collectExpandableKeys(tree))
    setVisible(true)
    try {
      const { data } = await api.get('/roles/' + row.id + '/menus')
      setCheckedKeys(data.data || [])
    } catch {
      message.error('加载角色权限失败')
    }
  }

  async function saveAll() {
    const name = roleForm.name.trim()
    if (!name) {
      message.warning('请填写角色名称')
      return
    }
    const code = roleForm.code.trim()
    if (!roleForm.id && !code) {
      message.warning('请填写角色编码')
      return
    }
    setSaving(true)
    try {
      let roleId = roleForm.id
      const payload = {
        name,
        code: code || roleForm.code,
        description: roleForm.description.trim(),
      }
      if (roleId) {
        await api.put('/roles/' + roleId, payload)
      } else {
        const { data } = await api.post('/roles', payload)
        roleId = data.data?.id
        if (!roleId) {
          throw new Error('创建角色失败')
        }
      }
      const menuIds = checkedKeys.map(String)
      await api.put('/roles/' + roleId + '/menus', { menu_ids: menuIds })
      message.success('已保存')
      setVisible(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function removeRole(row: any) {
    if (BUILTIN_CODES.has(String(row.code || '').toLowerCase())) {
      message.warning('内置角色不可删除')
      return
    }
    Modal.confirm({
      title: `确认删除角色「${row.name}」？`,
      content: '删除后不可恢复，已绑定该角色的用户关联将被清除。',
      okType: 'danger',
      onOk: async () => {
        await api.delete('/roles/' + row.id)
        message.success('已删除')
        await load()
      },
    })
  }

  async function openMembers(row: any) {
    setMembersRole(row)
    setMembersOpen(true)
    setMembersLoading(true)
    setMembers([])
    try {
      const { data } = await api.get('/roles/' + row.id + '/users')
      setMembers(data.data || [])
    } catch {
      message.error('加载成员失败')
    } finally {
      setMembersLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <div className="ops-page">
      <div className="ops-title" style={{ justifyContent: 'space-between' }}>
        <h1 style={{ margin: 0, fontSize: 18 }}>角色授权</h1>
        <Auth urn={Perm.roles.create}>
          <Button type="primary" onClick={openCreate}>
            新建角色
          </Button>
        </Auth>
      </div>

      <div className="white-card" style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
        <Table
          rowKey="id"
          dataSource={roles}
          pagination={false}
          size="middle"
          columns={[
            { title: '角色名称', dataIndex: 'name', width: 200 },
            {
              title: '角色描述',
              dataIndex: 'description',
              ellipsis: true,
              render: (v: string) => v || '—',
            },
            {
              title: '成员数',
              dataIndex: 'member_count',
              width: 100,
              render: (v: number, row) => (
                <Button type="link" style={{ padding: 0 }} onClick={() => openMembers(row)}>
                  {v ?? 0}
                </Button>
              ),
            },
            {
              title: '更新时间',
              dataIndex: 'updated_at',
              width: 180,
              render: (v: string, row) => formatTime(v || row.created_at),
            },
            {
              title: '操作',
              width: 140,
              render: (_, row) => {
                const builtin = BUILTIN_CODES.has(String(row.code || '').toLowerCase())
                return (
                  <Space size="middle">
                    <Auth urn={Perm.roles.edit}>
                      <Button type="link" style={{ padding: 0 }} onClick={() => openEdit(row)}>
                        编辑
                      </Button>
                    </Auth>
                    <Auth urn={Perm.roles.delete}>
                      <Button
                        type="link"
                        danger={!builtin}
                        disabled={builtin}
                        style={{ padding: 0, color: builtin ? undefined : undefined }}
                        onClick={() => removeRole(row)}
                      >
                        删除
                      </Button>
                    </Auth>
                  </Space>
                )
              },
            },
          ]}
        />
      </div>

      <Modal
        open={visible}
        title={roleForm.id ? '编辑角色' : '新建角色'}
        width={760}
        destroyOnClose
        confirmLoading={saving}
        onCancel={() => setVisible(false)}
        onOk={saveAll}
        okText="确定"
        cancelText="取消"
      >
        <Form labelCol={{ style: { width: 88 } }} style={{ marginTop: 8 }}>
          <Form.Item label="角色名称" required>
            <Input
              value={roleForm.name}
              maxLength={64}
              onChange={(e) => setRoleForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="请输入角色名称"
            />
          </Form.Item>
          <Form.Item label="角色编码" required={!roleForm.id}>
            <Input
              value={roleForm.code}
              disabled={!!roleForm.id}
              maxLength={64}
              onChange={(e) => setRoleForm((f) => ({ ...f, code: e.target.value }))}
              placeholder={roleForm.id ? '' : '唯一编码，如 ops_readonly'}
            />
          </Form.Item>
          <Form.Item label="角色描述">
            <Input.TextArea
              value={roleForm.description}
              rows={3}
              maxLength={256}
              onChange={(e) => setRoleForm((f) => ({ ...f, description: e.target.value }))}
              placeholder="可选，说明该角色用途"
            />
          </Form.Item>
        </Form>

        <div className="role-perm-box">
          <div className="role-perm-toolbar">
            <Input.Search
              allowClear
              placeholder="搜索权限名称"
              value={permSearch}
              onChange={(e) => setPermSearch(e.target.value)}
              style={{ maxWidth: 280 }}
            />
            <Space size="middle">
              <Button
                type="link"
                style={{ padding: 0 }}
                onClick={() => setExpandedKeys(collectExpandableKeys(filteredTree))}
              >
                全部展开
              </Button>
              <Button type="link" style={{ padding: 0 }} onClick={() => setExpandedKeys([])}>
                全部收起
              </Button>
            </Space>
          </div>
          <div className="role-perm-tree">
            <Tree
              checkable
              checkStrictly
              treeData={filteredTree}
              checkedKeys={{ checked: checkedKeys, halfChecked: [] }}
              expandedKeys={expandedKeys}
              onExpand={(keys) => setExpandedKeys(keys)}
              onCheck={(_keys, info) => {
                const nodeKey = String(info.node.key)
                setCheckedKeys((prev) => applyPermCheck(tree, prev, nodeKey, info.checked))
              }}
            />
          </div>
          <p className="role-perm-hint">
            勾选子权限会自动勾选父权限；取消父权限会自动取消其所有子权限。
          </p>
        </div>
      </Modal>

      <Modal
        open={membersOpen}
        title={membersRole ? `角色成员 · ${membersRole.name}` : '角色成员'}
        width={560}
        footer={null}
        onCancel={() => setMembersOpen(false)}
        destroyOnClose
      >
        <Table
          rowKey="id"
          loading={membersLoading}
          dataSource={members}
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无成员' }}
          columns={[
            { title: '用户名', dataIndex: 'username' },
            { title: '显示名', dataIndex: 'display_name', render: (v: string) => v || '—' },
          ]}
        />
      </Modal>

      <style>{`
        .role-perm-box {
          border: 1px solid #f0f0f0;
          border-radius: 8px;
          padding: 12px;
          background: #fafafa;
        }
        .role-perm-toolbar {
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 12px;
          margin-bottom: 8px;
        }
        .role-perm-tree {
          max-height: 360px;
          overflow: auto;
          background: #fff;
          border: 1px solid #f0f0f0;
          border-radius: 6px;
          padding: 8px 12px;
        }
        .role-perm-hint {
          margin: 8px 0 0;
          color: #909399;
          font-size: 12px;
        }
      `}</style>
    </div>
  )
}
