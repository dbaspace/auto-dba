import React, { useMemo } from 'react'
import { Input, Select, Switch } from 'antd'
import ReviewRuleJsonEditor, { canEditRuleJson, isCaseWord } from './ReviewRuleJsonEditor'
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

const CASE_OPTIONS = [
  { value: 'any', label: '不限' },
  { value: 'lower', label: '小写' },
  { value: 'upper', label: '大写' },
]

export default function ReviewRuleField({ row, value = '', saving = false, wide = false, onChange }: Props) {
  const origin = String(row?.value ?? '')
  const bool = useMemo(() => {
    const s = origin.trim().toLowerCase()
    return s === 'true' || s === 'false'
  }, [origin])
  const json = useMemo(() => canEditRuleJson(value) || canEditRuleJson(origin), [value, origin])
  const caseWord = useMemo(() => !json && (isCaseWord(origin) || isCaseWord(value)), [json, origin, value])

  const checked = useMemo(() => String(value ?? '').trim().toLowerCase() === 'true', [value])
  const hint = RULE_HINTS[row.name] || ''
  const caseValue = String(value || origin || 'any').trim().toLowerCase()

  return (
    <div className={`rule-row${wide || json ? ' is-wide' : ''}${hint ? ' has-hint' : ''}`}>
      <span className="rule-label" title={`${row.label || row.name}（${row.name}）`}>
        {row.label || '—'}
      </span>
      <div className={`rule-ctrl${wide || json || !bool ? ' grow' : ''}`}>
        {json && canEditRuleJson(value) ? (
          <ReviewRuleJsonEditor name={row.name} value={value} saving={saving} onChange={onChange} />
        ) : caseWord ? (
          <Select
            size="small"
            style={{ width: 100 }}
            disabled={saving}
            value={CASE_OPTIONS.some((o) => o.value === caseValue) ? caseValue : 'any'}
            options={CASE_OPTIONS}
            onChange={(v) => onChange?.(row.name, v)}
          />
        ) : bool ? (
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
