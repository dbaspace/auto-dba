import React from 'react'
import { Segmented } from 'antd'
import type { CheckLevelFilter } from '../hooks/useCheckResultView'

type Counts = { total: number; ok: number; warn: number; error: number }

type Props = {
  value: CheckLevelFilter
  onChange: (v: CheckLevelFilter) => void
  counts: Counts
  /** Hide bar when no results yet */
  visible?: boolean
}

export default function CheckResultFilterBar({ value, onChange, counts, visible = true }: Props) {
  if (!visible || counts.total <= 0) return null
  return (
    <div className="check-result-filter" style={{ marginBottom: 8 }}>
      <span style={{ marginRight: 8, color: '#6b7280', fontSize: 13 }}>状态</span>
      <Segmented
        size="small"
        value={value}
        onChange={(v) => onChange(v as CheckLevelFilter)}
        options={[
          { label: `全部(${counts.total})`, value: 'all' },
          { label: `通过(${counts.ok})`, value: 'ok' },
          { label: `警告(${counts.warn})`, value: 'warn' },
          { label: `错误(${counts.error})`, value: 'error' },
        ]}
      />
    </div>
  )
}
