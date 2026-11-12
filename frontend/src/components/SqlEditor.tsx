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
  onKeyUp?: React.KeyboardEventHandler<HTMLTextAreaElement>
  onBeforeInput?: React.FormEventHandler<HTMLTextAreaElement>
  onClick?: React.MouseEventHandler<HTMLTextAreaElement>
  onScroll?: React.UIEventHandler<HTMLTextAreaElement>
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
    onKeyUp,
    onBeforeInput,
    onClick,
    onScroll,
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

  const placeCursor = (pos: number) => {
    window.setTimeout(() => {
      const ta = localRef.current
      if (!ta) return
      const n = Math.max(0, Math.min(pos, ta.value.length))
      ta.setSelectionRange(n, n)
    }, 0)
  }

  const onCopy = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget
    const { selectionStart: s, selectionEnd: t } = ta
    if (t <= s) return
    e.preventDefault()
    e.clipboardData.setData('text/plain', ta.value.slice(s, t))
  }

  const onCut = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget
    const { selectionStart: s, selectionEnd: t } = ta
    if (t <= s || disabled) return
    e.preventDefault()
    e.clipboardData.setData('text/plain', ta.value.slice(s, t))
    onBeforeInput?.(e as unknown as React.FormEvent<HTMLTextAreaElement>)
    onChange(ta.value.slice(0, s) + ta.value.slice(t))
    placeCursor(s)
  }

  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    if (disabled) return
    const text = e.clipboardData.getData('text/plain')
    if (!text) return
    const ta = e.currentTarget
    const { selectionStart: s, selectionEnd: t } = ta
    e.preventDefault()
    onBeforeInput?.(e as unknown as React.FormEvent<HTMLTextAreaElement>)
    onChange(ta.value.slice(0, s) + text + ta.value.slice(t))
    placeCursor(s + text.length)
  }

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
        onScroll={(e) => {
          syncScroll()
          onScroll?.(e)
        }}
        onKeyDown={onKeyDown}
        onBeforeInput={onBeforeInput}
        onCopy={onCopy}
        onCut={onCut}
        onPaste={onPaste}
        onKeyUp={onKeyUp}
        onClick={onClick}
      />
    </div>
  )
})

export default SqlEditor
