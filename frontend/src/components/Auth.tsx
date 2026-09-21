import React from 'react'
import { usePermission } from '../hooks/usePermission'

/** 无指定 URN 时不渲染子节点（隐藏按钮，避免点了才发现无权限） */
export default function Auth({
  urn,
  children,
}: {
  urn: string
  children: React.ReactNode
}) {
  const { hasUrn } = usePermission()
  if (!hasUrn(urn)) return null
  return <>{children}</>
}
