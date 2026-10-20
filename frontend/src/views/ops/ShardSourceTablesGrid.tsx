import React from 'react'
import { Typography } from 'antd'

/**
 * 分表组源表名网格：按容器宽度 auto-fill 自适应列数，字号偏小以容纳长表名。
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
