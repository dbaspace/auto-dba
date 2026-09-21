import React from 'react'
import { Tag } from 'antd'

export type MenuKind = 'page' | 'action' | 'group'

export interface MenuKindInput {
  id?: string
  path?: string
  urn?: string
}

/** 与后端 IsActionMenu 约定一致：自动识别，无需改库 */
export function resolveMenuKind(n: MenuKindInput): MenuKind {
  const urn = String(n.urn || '').trim()
  const path = String(n.path || '').trim()
  const id = String(n.id || '').trim()
  if (urn.startsWith('um.action.') || (!path && id.startsWith('m-act-'))) {
    return 'action'
  }
  if (path) return 'page'
  return 'group'
}

export function isActionMenu(n: MenuKindInput): boolean {
  return resolveMenuKind(n) === 'action'
}

/** 由 URN 后缀推导能力 Tag 文案 */
export function actionVerbLabel(urn?: string): string | null {
  const u = String(urn || '').trim()
  if (!u) return null
  if (u.startsWith('um.sidebar.')) return '查看'
  if (u.endsWith('.create')) return '新建'
  if (u.endsWith('.edit')) return '编辑'
  if (u.endsWith('.delete')) return '删除'
  if (u.endsWith('.collect')) return '采集'
  if (u.endsWith('.run')) return '执行'
  if (u.endsWith('.stop')) return '停止'
  if (u.endsWith('.approve')) return '审批'
  if (u.startsWith('um.action.')) return '操作'
  return null
}

export function MenuKindTag({ kind }: { kind: MenuKind }) {
  if (kind === 'page') return <Tag color="blue">页面</Tag>
  if (kind === 'action') return <Tag color="orange">操作</Tag>
  return <Tag>目录</Tag>
}

export function ActionVerbTag({ urn }: { urn?: string }) {
  const label = actionVerbLabel(urn)
  if (!label) return null
  const color =
    label === '查看'
      ? 'blue'
      : label === '新建'
        ? 'green'
        : label === '编辑' || label === '采集' || label === '执行' || label === '审批'
          ? 'orange'
          : label === '删除' || label === '停止'
            ? 'red'
            : 'default'
  return (
    <Tag color={color} style={{ marginLeft: 6 }}>
      {label}
    </Tag>
  )
}
