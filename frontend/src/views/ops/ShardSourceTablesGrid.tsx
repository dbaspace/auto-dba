import React from 'react'
import { Typography } from 'antd'

/**
 * 分表组源表名网格：全部成员表名，每行 2 个，超出自动换行。
 * 对齐 universe-fe ShardSourceTablesGrid，避免分表较多时单行 join 撑破列布局。
 */
export default function ShardSourceTablesGrid({ tables }: { tables: string[] }) {
  if (!tables.length) return <span style={{ color: '#c0c4cc' }}>—</span>
  return (
    <div className="shard-src-grid">
      {tables.map((t) => (
        <Typography.Text code key={t} className="shard-src-item">
          {t}
        </Typography.Text>
      ))}
    </div>
  )
}
