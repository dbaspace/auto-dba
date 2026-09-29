/**
 * 响应式表格：默认开启横向滚动（x: max-content），窄屏不挤压成一字竖排。
 * 调用方可覆盖 scroll；若只传 y / 未传 x，会自动补上 x。
 */
import React from 'react'
import { Table, type TableProps } from 'antd'

function mergeScroll(scroll: TableProps['scroll']): TableProps['scroll'] {
  if (scroll === undefined || scroll === null) {
    return { x: 'max-content' }
  }
  if (typeof scroll === 'object' && (scroll as { x?: unknown }).x == null) {
    return { ...scroll, x: 'max-content' }
  }
  return scroll
}

export default function ResponsiveTable<RecordType extends object = any>(props: TableProps<RecordType>) {
  const { scroll, className, dataSource, ...rest } = props
  const safeData = Array.isArray(dataSource) ? dataSource : []
  return (
    <Table<RecordType>
      {...rest}
      dataSource={safeData}
      className={['app-responsive-table', className].filter(Boolean).join(' ')}
      scroll={mergeScroll(scroll)}
    />
  )
}
