import React, { useMemo, useRef, useState } from 'react'
import { Checkbox, Input } from 'antd'
import './DatabasePicker.css'

const SYSTEM_DB = new Set([
  'information_schema', 'performance_schema', 'sys', 'mysql',
  'template0', 'template1', 'postgres', '_statistics_',
])

export interface DbOption {
  value: string
  label: string
}

interface Props {
  value?: string[]
  onChange?: (next: string[]) => void
  options?: DbOption[]
  loading?: boolean
  disabled?: boolean
}

export default function DatabasePicker({
  value = [],
  onChange,
  options = [],
  loading = false,
  disabled = false,
}: Props) {
  const [searchQ, setSearchQ] = useState('')
  const [hideSystemDb, setHideSystemDb] = useState(true)
  const lastIndexRef = useRef<number | null>(null)

  const selectedSet = useMemo(() => new Set(value || []), [value])
  const optionById = useMemo(() => new Map(options.map((o) => [o.value, o])), [options])
  const selectedOptions = useMemo(
    () => (value || []).map((id) => optionById.get(id) || { value: id, label: id }),
    [value, optionById],
  )

  const leftVisible = useMemo(() => {
    const q = searchQ.trim().toLowerCase()
    return options.filter((opt) => {
      if (hideSystemDb && SYSTEM_DB.has(String(opt.label).trim().toLowerCase()) && !selectedSet.has(opt.value)) {
        return false
      }
      if (!q) return true
      return String(opt.label).toLowerCase().includes(q)
    })
  }, [options, hideSystemDb, searchQ, selectedSet])

  const leftCheckedCount = leftVisible.filter((o) => selectedSet.has(o.value)).length
  const leftAllChecked = leftVisible.length > 0 && leftCheckedCount === leftVisible.length
  const leftIndeterminate = leftCheckedCount > 0 && leftCheckedCount < leftVisible.length

  function emitChange(next: string[]) {
    onChange?.(next)
  }

  function toggleOne(id: string, checked: boolean) {
    if (disabled) return
    if (checked) {
      if (selectedSet.has(id)) return
      emitChange([...(value || []), id])
      return
    }
    emitChange((value || []).filter((x) => x !== id))
  }

  function toggleRange(from: number, to: number, checked: boolean) {
    const start = Math.min(from, to)
    const end = Math.max(from, to)
    const rangeIds = leftVisible.slice(start, end + 1).map((o) => o.value)
    if (checked) emitChange([...new Set([...(value || []), ...rangeIds])])
    else {
      const removeSet = new Set(rangeIds)
      emitChange((value || []).filter((id) => !removeSet.has(id)))
    }
  }

  function onRowClick(index: number, id: string, e: React.MouseEvent) {
    e.preventDefault()
    const checked = !selectedSet.has(id)
    if (e.shiftKey && lastIndexRef.current != null && lastIndexRef.current !== index) {
      toggleRange(lastIndexRef.current, index, checked)
    } else {
      toggleOne(id, checked)
    }
    lastIndexRef.current = index
  }

  function onCheckAll(checked: boolean) {
    const visibleIds = leftVisible.map((o) => o.value)
    if (checked) emitChange([...new Set([...(value || []), ...visibleIds])])
    else {
      const vis = new Set(visibleIds)
      emitChange((value || []).filter((id) => !vis.has(id)))
    }
  }

  function remove(id: string) {
    if (disabled) return
    emitChange((value || []).filter((x) => x !== id))
  }

  return (
    <div className={`db-picker${disabled ? ' disabled' : ''}`}>
      <div className="panel">
        <div className="panel-head">
          <span>请选择数据库</span>
          <button type="button" className="link" disabled={disabled} onClick={() => setHideSystemDb((v) => !v)}>
            {hideSystemDb ? '显示系统库' : '隐藏系统库'}
          </button>
        </div>
        <div className="panel-search">
          <Input
            allowClear
            disabled={disabled}
            placeholder="请输入数据库名称"
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
          />
        </div>
        <div className="panel-checkall">
          <Checkbox
            checked={leftAllChecked}
            indeterminate={leftIndeterminate}
            disabled={disabled || !leftVisible.length}
            onChange={(e) => onCheckAll(e.target.checked)}
          />
          <span>数据库</span>
        </div>
        <div className="panel-list">
          {!leftVisible.length ? (
            <div className="empty">{loading ? '加载中…' : '无匹配库'}</div>
          ) : (
            leftVisible.map((opt, index) => (
              <label key={opt.value} className="row" onClick={(e) => onRowClick(index, opt.value, e)}>
                <Checkbox checked={selectedSet.has(opt.value)} disabled={disabled} />
                <span className="name">{opt.label}</span>
              </label>
            ))
          )}
        </div>
        <div className="panel-foot">支持按住 shift 键进行多选</div>
      </div>

      <div className="swap">⇄</div>

      <div className="panel">
        <div className="panel-head">
          <span>已选择</span>
        </div>
        <div className="panel-checkall">
          <span>数据库</span>
        </div>
        <div className="panel-list">
          {!selectedOptions.length ? (
            <div className="empty">暂无已选数据库</div>
          ) : (
            selectedOptions.map((opt) => (
              <div key={opt.value} className="row between">
                <span className="name">{opt.label}</span>
                <button type="button" className="remove" disabled={disabled} onClick={() => remove(opt.value)}>
                  ×
                </button>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
