import React, { forwardRef, useCallback, useMemo, useRef } from 'react'
import { highlightSql } from '../utils/highlightSql'
import './SqlEditor.css'

export type SqlEditorProps = {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  className?: string
  minRows?: number
  disabled?: boolean
  onKeyDown?: React.KeyboardEventHandler<HTMLTextAreaElement>
  spellCheck?: boolean
}

/** Overlay highlighter + textarea for SQL keyword highlighting. */
const SqlEditor = forwardRef<HTMLTextAreaElement, SqlEditorProps>(function SqlEditor(
  {
    value,
    onChange,
    placeholder,
    className,
    minRows = 16,
    disabled,
    onKeyDown,
    spellCheck = false,
  },
  ref,
) {
  const localRef = useRef<HTMLTextAreaElement | null>(null)
  const preRef = useRef<HTMLPreElement | null>(null)

  const setRefs = useCallback(
    (el: HTMLTextAreaElement | null) => {
      localRef.current = el
      if (typeof ref === 'function') ref(el)
      else if (ref) ref.current = el
    },
    [ref],
  )

  const html = useMemo(() => highlightSql(value || ''), [value])
  const fill = Boolean(className?.includes('sql-box-hl'))

  const syncScroll = () => {
    const ta = localRef.current
    const pre = preRef.current
    if (!ta || !pre) return
    pre.scrollTop = ta.scrollTop
    pre.scrollLeft = ta.scrollLeft
  }

  return (
    <div
      className={['sql-hl', fill ? 'sql-hl-fill' : 'sql-hl-box', className].filter(Boolean).join(' ')}
      style={fill ? undefined : { ['--sql-hl-min-rows' as string]: String(minRows) }}
    >
      <pre
        ref={preRef}
        className="sql-hl-pre"
        aria-hidden
        dangerouslySetInnerHTML={{ __html: html || '&nbsp;' }}
      />
      <textarea
        ref={setRefs}
        className="sql-hl-input"
        value={value}
        disabled={disabled}
        spellCheck={spellCheck}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onScroll={syncScroll}
        onKeyDown={onKeyDown}
      />
    </div>
  )
})

export default SqlEditor
