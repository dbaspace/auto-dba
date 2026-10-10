import React, { useEffect, useMemo, useState } from 'react'
import { Button, Input, InputNumber, Modal, Radio, Select, Switch, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { CaretRightOutlined, CaretDownOutlined } from '@ant-design/icons'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { PAGE_ROUTES } from '../../constants/routes'
import { resolveMenuIcon } from '../../components/MenuIcon'
import { resolveMenuKind, MenuKindTag, isActionMenu } from '../../utils/menuKind'
import type { MenuKind } from '../../utils/menuKind'
import './Menus.css'

interface MenuNode {
  id: string
  parent_id?: string
  parent_title?: string
  title: string
  urn?: string
  path?: string
  permission_desc?: string
  icon?: string
  sort_order?: number
  enabled?: boolean
  children?: MenuNode[]
}

interface VisibleRow extends MenuNode {
  _depth: number
  _hasChildren: boolean
}

interface MenuForm {
  id: string
  parent_id: string
  title: string
  urn: string
  path: string
  permission_desc: string
  icon: string
  sort_order: number
  enabled: boolean
  kind: MenuKind
}

const MENU_ICON_OPTIONS = [
  'dashboard', 'odometer', 'databoard', 'tickets', 'list', 'document', 'download',
  'edit', 'share', 'key', 'plus', 'refresh', 'connection', 'upload', 'sort', 'lock',
  'clock', 'folder', 'location', 'monitor', 'search', 'cpu', 'grid', 'files',
  'histogram', 'coin', 'setting', 'menu', 'user', 'user-filled', 'team', 'login',
  'safety', 'audit', 'warning', 'todo-list',
]

function emptyForm(): MenuForm {
  return {
    id: '',
    parent_id: '',
    title: '',
    urn: '',
    path: '',
    permission_desc: '',
    icon: '',
    sort_order: 0,
    enabled: true,
    kind: 'page',
  }
}

function flatten(nodes: MenuNode[] | undefined, out: { id: string; label: string }[] = [], depth = 0) {
  for (const n of nodes || []) {
    out.push({ id: n.id, label: `${'\u3000'.repeat(depth)}${n.title}` })
    if (n.children?.length) flatten(n.children, out, depth + 1)
  }
  return out
}

function collectIds(node: MenuNode, set: Set<string>) {
  set.add(node.id)
  for (const c of node.children || []) collectIds(c, set)
}

function collectParentIds(nodes: MenuNode[] | undefined, out: string[] = []) {
  for (const n of nodes || []) {
    if (n.children?.length) {
      out.push(n.id)
      collectParentIds(n.children, out)
    }
  }
  return out
}

export default function Menus() {
  const [tree, setTree] = useState<MenuNode[]>([])
  const [collapsedIds, setCollapsedIds] = useState<string[]>([])
  const [ready, setReady] = useState(false)
  const [visible, setVisible] = useState(false)
  const [form, setForm] = useState<MenuForm>(emptyForm())
  const [kindFilter, setKindFilter] = useState<'all' | 'page' | 'action'>('all')

  const parentOptions = useMemo(() => {
    const all = flatten(tree)
    if (!form.id) return all
    const blocked = new Set<string>()
    const walk = (nodes: MenuNode[] | undefined) => {
      for (const n of nodes || []) {
        if (n.id === form.id) collectIds(n, blocked)
        else walk(n.children)
      }
    }
    walk(tree)
    return all.filter((n) => !blocked.has(n.id))
  }, [tree, form.id])

  const visibleRows = useMemo(() => {
    const collapsed = new Set(collapsedIds)
    const out: VisibleRow[] = []
    function walk(nodes: MenuNode[] | undefined, depth: number, hidden: boolean) {
      for (const n of nodes || []) {
        const hasChildren = !!n.children?.length
        if (!hidden) out.push({ ...n, _depth: depth, _hasChildren: hasChildren })
        if (hasChildren) walk(n.children, depth + 1, hidden || collapsed.has(n.id))
      }
    }
    walk(tree, 0, false)
    if (kindFilter === 'all') return out
    return out.filter((row) => resolveMenuKind(row) === kindFilter)
  }, [tree, collapsedIds, kindFilter])

  function isCollapsed(id: string) {
    return collapsedIds.includes(id)
  }

  function toggle(row: MenuNode) {
    setCollapsedIds((prev) => (prev.includes(row.id) ? prev.filter((id) => id !== row.id) : [...prev, row.id]))
  }

  function expandAll() {
    setCollapsedIds([])
  }

  function collapseAll() {
    setCollapsedIds(collectParentIds(tree))
  }

  async function load() {
    setReady(false)
    setTree((await api.get('/menus/tree')).data.data || [])
    setCollapsedIds([])
    setReady(true)
  }

  function openCreate() {
    setForm(emptyForm())
    setVisible(true)
  }

  function openEdit(row: MenuNode) {
    const kind = resolveMenuKind(row)
    setForm({
      id: row.id,
      parent_id: row.parent_id || '',
      title: row.title,
      urn: row.urn || '',
      path: row.path || '',
      permission_desc: row.permission_desc || '',
      icon: row.icon || '',
      sort_order: row.sort_order || 0,
      enabled: !!row.enabled,
      kind,
    })
    setVisible(true)
  }

  function onKindChange(kind: MenuKind) {
    setForm((f) => {
      if (kind === 'action') {
        return { ...f, kind, path: '' }
      }
      return { ...f, kind }
    })
  }

  async function save() {
    const path = form.kind === 'action' ? '' : form.path
    const { kind: _kind, ...rest } = form
    const payload = {
      ...rest,
      parent_id: form.parent_id || '',
      path,
    }
    if (form.id) await api.put('/menus/' + form.id, payload)
    else await api.post('/menus', payload)
    message.success('已保存')
    setVisible(false)
    setForm(emptyForm())
    await load()
    window.dispatchEvent(new Event('menus-changed'))
  }

  async function move(row: MenuNode, dir: 'up' | 'down') {
    await api.post('/menus/' + row.id + '/move', { dir })
    await load()
  }

  function remove(row: MenuNode) {
    Modal.confirm({
      title: '确认删除',
      content: `删除「${row.title}」及其子菜单？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/menus/' + row.id)
        message.success('已删除')
        await load()
        window.dispatchEvent(new Event('menus-changed'))
      },
    })
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <div className="ops-page menus-page">
      <div className="ops-title">
        <div className="toolbar" style={{ margin: 0 }}>
          <Auth urn={Perm.menus.create}>
            <Button type="primary" onClick={openCreate}>
              新建菜单
            </Button>
          </Auth>
          <Button onClick={expandAll}>全部展开</Button>
          <Button onClick={collapseAll}>全部收起</Button>
          <Radio.Group
            value={kindFilter}
            optionType="button"
            buttonStyle="solid"
            onChange={(e) => setKindFilter(e.target.value)}
            options={[
              { value: 'all', label: '全部' },
              { value: 'page', label: '仅页面' },
              { value: 'action', label: '仅操作' },
            ]}
          />
        </div>
      </div>
      <div className="table-wrap">
        {ready && (
          <ResponsiveTable
            rowKey="id"
            dataSource={visibleRows}
            bordered
            pagination={false}
            scroll={{ x: 1200, y: '100%' }}
            style={{ height: '100%' }}
            rowClassName={(row) => `menu-row lv-${Math.min(row._depth || 0, 4)}`}
            columns={[
              {
                title: '菜单名称',
                width: 280,
                render: (_, row) => (
                  <div className="name-cell" style={{ paddingLeft: 8 + row._depth * 28 }}>
                    {row._hasChildren ? (
                      <button type="button" className="toggle" onClick={() => toggle(row)}>
                        {isCollapsed(row.id) ? <CaretRightOutlined /> : <CaretDownOutlined />}
                      </button>
                    ) : (
                      <span className="toggle-placeholder" />
                    )}
                    {row.icon ? (
                      <span className="menu-icon">{resolveMenuIcon(row.icon)}</span>
                    ) : (
                      <span className="icon-placeholder" />
                    )}
                    <span className="name-text">{row.title}</span>
                    <MenuKindTag kind={resolveMenuKind(row)} />
                  </div>
                ),
              },
              { title: '菜单 URN', width: 180, ellipsis: true, render: (_, row) => row.urn || '-' },
              {
                title: '页面路径',
                width: 140,
                ellipsis: true,
                render: (_, row) => (isActionMenu(row) ? '—（不进侧栏）' : row.path || '—'),
              },
              { title: '权限说明', width: 120, ellipsis: true, render: (_, row) => row.permission_desc || '-' },
              { title: '父级', width: 80, ellipsis: true, render: (_, row) => row.parent_title || '-' },
              { title: '排序', dataIndex: 'sort_order', width: 72, align: 'center' },
              { title: '可见', width: 64, align: 'center', render: (_, row) => (row.enabled ? '是' : '否') },
              {
                title: '操作',
                width: 220,
                render: (_, row) => (
                  <div className="ops">
                    <Button type="link" onClick={() => move(row, 'up')}>
                      上移
                    </Button>
                    <Button type="link" onClick={() => move(row, 'down')}>
                      下移
                    </Button>
                    <Auth urn={Perm.menus.edit}>
                      <Button type="link" onClick={() => openEdit(row)}>
                        编辑
                      </Button>
                    </Auth>
                    <Auth urn={Perm.menus.delete}>
                      <Button type="link" danger onClick={() => remove(row)}>
                        删除
                      </Button>
                    </Auth>
                  </div>
                ),
              },
            ]}
          />
        )}
      </div>

      <Modal
        open={visible}
        title={form.id ? '编辑菜单' : '新建菜单'}
        width={520}
        destroyOnClose
        onCancel={() => setVisible(false)}
        footer={[
          <Button key="cancel" onClick={() => setVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" onClick={save}>
            保存
          </Button>,
        ]}
      >
        <div className="menu-form">
          <div className="fi">
            <label>类型</label>
            {form.id ? (
              <div>
                <MenuKindTag kind={form.kind} />
                <div className="hint" style={{ marginTop: 6, marginBottom: 0 }}>
                  {form.kind === 'action'
                    ? '操作权限不进侧栏，用于角色授权与接口鉴权；如需变更类型请删除后重建。'
                    : form.kind === 'page'
                      ? '页面入口会出现在侧栏并可打开对应路径；如需变更类型请删除后重建。'
                      : '目录分组仅作侧栏折叠容器，无页面路径；如需变更类型请删除后重建。'}
                </div>
              </div>
            ) : (
              <div>
                <Radio.Group
                  value={form.kind}
                  onChange={(e) => onKindChange(e.target.value)}
                  options={[
                    { value: 'page', label: '页面入口' },
                    { value: 'action', label: '操作权限' },
                    { value: 'group', label: '目录分组' },
                  ]}
                />
                <div className="hint" style={{ marginTop: 6, marginBottom: 0 }}>
                  新建时可选择类型；保存后类型不可在此切换。
                </div>
              </div>
            )}
          </div>
          <div className="fi">
            <label>上级</label>
            <Select
              value={form.parent_id || ''}
              allowClear
              style={{ width: '100%' }}
              onChange={(v) => setForm((f) => ({ ...f, parent_id: v || '' }))}
              options={[
                { value: '', label: '无（一级分组）' },
                ...parentOptions.map((g) => ({ value: g.id, label: g.label })),
              ]}
            />
          </div>
          <div className="fi">
            <label>
              名称<span className="req">*</span>
            </label>
            <Input value={form.title} onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))} />
          </div>
          <div className="fi">
            <label>图标</label>
            <Select
              value={form.icon ? [form.icon] : []}
              allowClear
              showSearch
              mode="tags"
              maxCount={1}
              style={{ width: '100%' }}
              placeholder="如 dashboard"
              onChange={(v) => setForm((f) => ({ ...f, icon: (Array.isArray(v) ? v[0] : v) || '' }))}
              options={MENU_ICON_OPTIONS.map((v) => ({
                value: v,
                label: (
                  <span className="icon-opt">
                    <span className="menu-icon">{resolveMenuIcon(v)}</span>
                    <span>{v}</span>
                  </span>
                ),
              }))}
            />
          </div>
          <div className="fi">
            <label>菜单 URN</label>
            <Input
              value={form.urn}
              placeholder={
                form.kind === 'action'
                  ? '留空由后端生成，或填写 um.action.*'
                  : '留空则按路径自动生成 um.sidebar.*'
              }
              onChange={(e) => setForm((f) => ({ ...f, urn: e.target.value }))}
            />
          </div>
          {form.kind !== 'action' && (
            <div className="fi">
              <label>页面路径</label>
              <Select
                value={form.path ? [form.path] : []}
                allowClear
                showSearch
                mode="tags"
                maxCount={1}
                style={{ width: '100%' }}
                placeholder={form.kind === 'group' ? '目录分组可留空' : '选择或填写页面路径'}
                optionFilterProp="label"
                onChange={(v) => setForm((f) => ({ ...f, path: (Array.isArray(v) ? v[0] : v) || '' }))}
                options={PAGE_ROUTES.map((r) => ({ value: r.path, label: `${r.title} (${r.path})` }))}
              />
            </div>
          )}
          <div className="fi">
            <label>权限说明</label>
            <Input
              value={form.permission_desc}
              placeholder="如：查看工单列表"
              onChange={(e) => setForm((f) => ({ ...f, permission_desc: e.target.value }))}
            />
          </div>
          <div className="fi">
            <label>排序</label>
            <InputNumber min={0} value={form.sort_order} onChange={(v) => setForm((f) => ({ ...f, sort_order: v ?? 0 }))} />
          </div>
          <div className="fi">
            <label>可见</label>
            <Switch checked={form.enabled} onChange={(v) => setForm((f) => ({ ...f, enabled: v }))} />
          </div>
        </div>
      </Modal>
    </div>
  )
}
