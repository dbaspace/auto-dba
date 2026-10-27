import React, { useEffect, useMemo, useState } from 'react'
import { Input, InputNumber, Select, Switch } from 'antd'

type Obj = Record<string, unknown>

const CASE_OPTIONS = [
  { value: 'any', label: '不限' },
  { value: 'lower', label: '小写' },
  { value: 'upper', label: '大写' },
]

const DROP_OPTIONS = [
  { value: 'forbid', label: '禁止 DROP TABLE' },
  { value: 'require_del_suffix', label: '只允许删除带后缀的表' },
]

const FK_OPTIONS = [
  { value: 'off', label: '不检查' },
  { value: 'forbid', label: '禁止外键' },
  { value: 'require', label: '必须声明外键' },
]

export function canEditRuleJson(value: string): boolean {
  return parseObj(value) != null
}

export function isCaseWord(value: string): boolean {
  return ['any', 'lower', 'upper'].includes(String(value ?? '').trim().toLowerCase())
}

function parseObj(value: string): Obj | null {
  const s = String(value ?? '').trim()
  if (!s.startsWith('{')) return null
  try {
    const o = JSON.parse(s)
    if (!o || typeof o !== 'object' || Array.isArray(o)) return null
    return o as Obj
  } catch {
    return null
  }
}

function asStringList(v: unknown): string[] {
  if (Array.isArray(v)) {
    return v.map((it) => String(it ?? '').trim()).filter(Boolean)
  }
  if (typeof v === 'string' && v.trim()) {
    return v.split(',').map((it) => it.trim()).filter(Boolean)
  }
  return []
}

function isStringList(v: unknown): boolean {
  return Array.isArray(v) && v.every((it) => typeof it === 'string' || typeof it === 'number')
}

interface Props {
  name: string
  value: string
  saving?: boolean
  onChange?: (name: string, value: string) => void
}

export default function ReviewRuleJsonEditor({ name, value, saving = false, onChange }: Props) {
  const obj = useMemo(() => parseObj(value) || {}, [value])

  function write(next: Obj) {
    onChange?.(name, JSON.stringify(next))
  }

  function patch(partial: Obj) {
    write({ ...obj, ...partial })
  }

  if (name === 'DDLForbidDropTable') {
    const mode = String(obj.mode || 'forbid').toLowerCase()
    const suffix = String(obj.suffix ?? '_del')
    return (
      <span className="rule-json">
        <Select
          size="small"
          style={{ width: 220 }}
          disabled={saving}
          value={DROP_OPTIONS.some((o) => o.value === mode) ? mode : 'forbid'}
          options={DROP_OPTIONS}
          onChange={(v) => {
            if (v === 'require_del_suffix') {
              patch({ mode: v, suffix: String(obj.suffix ?? '').trim() || '_del' })
              return
            }
            patch({ mode: v })
          }}
        />
        {mode === 'require_del_suffix' ? (
          <label className="rule-json-item">
            <span className="rule-json-k">后缀</span>
            <Input
              size="small"
              style={{ width: 100 }}
              disabled={saving}
              value={suffix}
              onChange={(e) => patch({ suffix: e.target.value })}
            />
          </label>
        ) : null}
      </span>
    )
  }

  if (name === 'DDLTableNameSpec' || name === 'DDLColumnNameSpec') {
    const cs = String(obj.case || 'any').toLowerCase()
    const maxLen = Number(obj.max_len)
    return (
      <span className="rule-json">
        <label className="rule-json-item">
          <span className="rule-json-k">大小写</span>
          <Select
            size="small"
            style={{ width: 100 }}
            disabled={saving}
            value={CASE_OPTIONS.some((o) => o.value === cs) ? cs : 'any'}
            options={CASE_OPTIONS}
            onChange={(v) => patch({ case: v })}
          />
        </label>
        <label className="rule-json-item">
          <span className="rule-json-k">最大长度</span>
          <InputNumber
            size="small"
            min={1}
            max={63}
            disabled={saving}
            value={Number.isFinite(maxLen) && maxLen > 0 ? maxLen : 63}
            onChange={(v) => patch({ max_len: Number(v) || 1 })}
          />
        </label>
      </span>
    )
  }

  if (name === 'DDLCharVarcharLength') {
    const unlimited = obj.unlimited === true
    const maxChar = Number(obj.max_char)
    const maxVarchar = Number(obj.max_varchar)
    return (
      <span className="rule-json">
        <label className="rule-json-item">
          <span className="rule-json-k">不限制</span>
          <Switch
            size="small"
            disabled={saving}
            checked={unlimited}
            onChange={(on) => patch({ unlimited: on })}
          />
        </label>
        <label className="rule-json-item">
          <span className="rule-json-k">CHAR 上限</span>
          <InputNumber
            size="small"
            min={1}
            disabled={saving || unlimited}
            value={Number.isFinite(maxChar) && maxChar > 0 ? maxChar : 50}
            onChange={(v) => patch({ max_char: Number(v) || 1 })}
          />
        </label>
        <label className="rule-json-item">
          <span className="rule-json-k">VARCHAR 上限</span>
          <InputNumber
            size="small"
            min={1}
            disabled={saving || unlimited}
            value={Number.isFinite(maxVarchar) && maxVarchar > 0 ? maxVarchar : 5000}
            onChange={(v) => patch({ max_varchar: Number(v) || 1 })}
          />
        </label>
      </span>
    )
  }

  if (name === 'DDLForeignKeyPolicy') {
    const mode = foreignKeyMode(obj)
    return (
      <Select
        size="small"
        style={{ width: 160 }}
        disabled={saving}
        value={mode}
        options={FK_OPTIONS}
        onChange={(v) => write({ mode: v })}
      />
    )
  }

  if (name === 'DDLRequireUpdateTime') {
    const enabled = obj.enabled === undefined ? true : obj.enabled === true
    // 字符串原样展示，避免每键 asStringList+join 吃掉末尾逗号导致无法输入多字段
    const column = Array.isArray(obj.column)
      ? asStringList(obj.column).join(',')
      : String(obj.column ?? '')
    const types = asStringList(obj.allowed_types)
    return (
      <span className="rule-json">
        <label className="rule-json-item">
          <span className="rule-json-k">启用</span>
          <Switch size="small" disabled={saving} checked={enabled} onChange={(on) => patch({ enabled: on })} />
        </label>
        <label className="rule-json-item">
          <span className="rule-json-k">字段名</span>
          <Input
            size="small"
            style={{ width: 220 }}
            disabled={saving || !enabled}
            value={column}
            placeholder="update_time,modify_time"
            onChange={(e) => patch({ column: e.target.value })}
          />
        </label>
        <label className="rule-json-item">
          <span className="rule-json-k">允许类型</span>
          <Select
            size="small"
            mode="tags"
            style={{ minWidth: 280 }}
            disabled={saving || !enabled}
            value={types}
            options={types.map((t) => ({ value: t, label: t }))}
            onChange={(v) => patch({ allowed_types: v })}
          />
        </label>
      </span>
    )
  }

  return <GenericJsonFields obj={obj} saving={saving} onPatch={patch} />
}

function foreignKeyMode(obj: Obj): string {
  if (obj.enabled === false) return 'off'
  const payload = obj.payload
  let nested = ''
  if (payload && typeof payload === 'object' && !Array.isArray(payload)) {
    nested = String((payload as Obj).mode ?? '')
  }
  const mode = String(obj.mode || nested || 'off').toLowerCase()
  if (mode === 'forbid' || mode === 'require' || mode === 'off') return mode
  return 'off'
}

function GenericJsonFields({
  obj,
  saving,
  onPatch,
}: {
  obj: Obj
  saving: boolean
  onPatch: (partial: Obj) => void
}) {
  const keys = Object.keys(obj)
  if (!keys.length) return null
  return (
    <span className="rule-json">
      {keys.map((key) => (
        <JsonField key={key} name={key} value={obj[key]} saving={saving} onPatch={onPatch} />
      ))}
    </span>
  )
}

function JsonField({
  name,
  value,
  saving,
  onPatch,
}: {
  name: string
  value: unknown
  saving: boolean
  onPatch: (partial: Obj) => void
}) {
  if (typeof value === 'boolean') {
    return (
      <label className="rule-json-item">
        <span className="rule-json-k">{name}</span>
        <Switch size="small" disabled={saving} checked={value} onChange={(on) => onPatch({ [name]: on })} />
      </label>
    )
  }
  if (typeof value === 'number') {
    return (
      <label className="rule-json-item">
        <span className="rule-json-k">{name}</span>
        <InputNumber
          size="small"
          disabled={saving}
          value={value}
          onChange={(v) => onPatch({ [name]: Number(v) || 0 })}
        />
      </label>
    )
  }
  if (typeof value === 'string') {
    return (
      <label className="rule-json-item">
        <span className="rule-json-k">{name}</span>
        <Input
          size="small"
          style={{ width: 160 }}
          disabled={saving}
          value={value}
          onChange={(e) => onPatch({ [name]: e.target.value })}
        />
      </label>
    )
  }
  if (isStringList(value)) {
    const list = asStringList(value)
    return (
      <label className="rule-json-item">
        <span className="rule-json-k">{name}</span>
        <Select
          size="small"
          mode="tags"
          style={{ minWidth: 220 }}
          disabled={saving}
          value={list}
          options={list.map((t) => ({ value: t, label: t }))}
          onChange={(v) => onPatch({ [name]: v })}
        />
      </label>
    )
  }
  return <NestedJsonField name={name} value={value} saving={saving} onPatch={onPatch} />
}

function NestedJsonField({
  name,
  value,
  saving,
  onPatch,
}: {
  name: string
  value: unknown
  saving: boolean
  onPatch: (partial: Obj) => void
}) {
  const text = value == null ? '' : JSON.stringify(value)
  const [draft, setDraft] = useState(text)
  useEffect(() => {
    setDraft(text)
  }, [text])
  return (
    <label className="rule-json-item">
      <span className="rule-json-k">{name}</span>
      <Input
        size="small"
        style={{ width: 220 }}
        disabled={saving}
        value={draft}
        onChange={(e) => {
          const raw = e.target.value
          setDraft(raw)
          const trimmed = raw.trim()
          if (!trimmed) return
          try {
            onPatch({ [name]: JSON.parse(trimmed) })
          } catch {
            /* 未完成的 JSON 先留在输入框，不写回 */
          }
        }}
      />
    </label>
  )
}
