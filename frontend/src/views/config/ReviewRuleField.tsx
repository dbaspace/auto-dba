import React, { useMemo } from 'react'
import { Input, Switch } from 'antd'
import './ReviewRuleField.css'

export interface RuleRow {
  name: string
  label?: string
  value?: string
}

interface Props {
  row: RuleRow
  value?: string
  saving?: boolean
  wide?: boolean
  onChange?: (name: string, value: string) => void
}

const RULE_HINTS: Record<string, string> = {
  DDLRequireUpdateTime: 'column 多个用逗号分隔，满足任一即可（也支持 JSON 数组）',
}

export default function ReviewRuleField({ row, value = '', saving = false, wide = false, onChange }: Props) {
  const bool = useMemo(() => {
    const s = String(row?.value ?? '').trim().toLowerCase()
    return s === 'true' || s === 'false'
  }, [row])

  const checked = useMemo(() => String(value ?? '').trim().toLowerCase() === 'true', [value])
  const hint = RULE_HINTS[row.name] || ''

  return (
    <div className={`rule-row${wide ? ' is-wide' : ''}${hint ? ' has-hint' : ''}`}>
      <span className="rule-label" title={`${row.label || row.name}（${row.name}）`}>
        {row.label || '—'}
      </span>
      <div className={`rule-ctrl${wide || !bool ? ' grow' : ''}`}>
        {bool ? (
          <Switch
            checked={checked}
            disabled={saving}
            size="small"
            onChange={(on) => onChange?.(row.name, on ? 'true' : 'false')}
          />
        ) : (
          <Input
            value={value}
            size="small"
            className={wide ? 'rule-input-wide' : 'rule-input'}
            placeholder={hint || '请输入参数值'}
            disabled={saving}
            onChange={(e) => onChange?.(row.name, e.target.value)}
          />
        )}
        {hint ? <div className="rule-hint">{hint}</div> : null}
      </div>
    </div>
  )
}
