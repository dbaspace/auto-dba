import React, { forwardRef, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { highlightSql } from '../utils/highlightSql'
import './SqlEditor.css'

const COMMIT_MS = 300

export type SqlEditorProps = {
  value: string
  onChange: (value: string) => void
  /** 每个按键立刻通知，不要求父组件改状态。补全走这里。 */
  onDraft?: (value: string) => void
  /** 为真时约 300ms 后再交给 onChange。运行、失焦、卸载会立刻刷出。 */
  deferCommit?: boolean
  /** 页签或外部替换 SQL 时变化，用来采用父组件的文本。 */
  syncKey?: string
  /** 父组件在运行、切页签前调用，把未提交的文本立刻交出去。 */
  flushRef?: React.MutableRefObject<(() => void) | null>
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
    onDraft,
    deferCommit = false,
    syncKey = '',
    flushRef,
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
  const [text, setText] = useState(value)
  const [seen, setSeen] = useState({ value, syncKey })
  const textRef = useRef(value)
  const lastEmitted = useRef(value)
  const commitTimer = useRef<number | null>(null)
  const onChangeRef = useRef(onChange)
  const onDraftRef = useRef(onDraft)
  onChangeRef.current = onChange
  onDraftRef.current = onDraft

  const clearCommit = () => {
    if (commitTimer.current != null) {
      window.clearTimeout(commitTimer.current)
      commitTimer.current = null
    }
  }

  const emit = (next: string) => {
    lastEmitted.current = next
    onChangeRef.current(next)
  }

  const flush = () => {
    if (commitTimer.current == null) return
    clearCommit()
    emit(textRef.current)
  }

  if (flushRef) flushRef.current = flush

  if (syncKey !== seen.syncKey || value !== seen.value) {
    const keyChanged = syncKey !== seen.syncKey
    setSeen({ value, syncKey })
    const external = keyChanged || value !== lastEmitted.current
    if (external && value !== text) {
      clearCommit()
      textRef.current = value
      setText(value)
    } else if (keyChanged) {
      clearCommit()
      textRef.current = value
    }
  }

  useEffect(() => () => {
    if (flushRef) flushRef.current = null
    if (commitTimer.current == null) return
    window.clearTimeout(commitTimer.current)
    commitTimer.current = null
    onChangeRef.current(textRef.current)
  }, [flushRef])

  const publish = (next: string) => {
    textRef.current = next
    setText(next)
    onDraftRef.current?.(next)
    clearCommit()
    if (!deferCommit) {
      emit(next)
      return
    }
    commitTimer.current = window.setTimeout(() => {
      commitTimer.current = null
      emit(textRef.current)
    }, COMMIT_MS)
  }

  const setRefs = useCallback(
    (el: HTMLTextAreaElement | null) => {
      localRef.current = el
      if (typeof ref === 'function') ref(el)
      else if (ref) ref.current = el
    },
    [ref],
  )

  const html = useMemo(() => highlightSql(text || ''), [text])
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
    publish(ta.value.slice(0, s) + ta.value.slice(t))
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
    publish(ta.value.slice(0, s) + text + ta.value.slice(t))
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
        value={text}
        disabled={disabled}
        spellCheck={spellCheck}
        placeholder={placeholder}
        onChange={(e) => publish(e.target.value)}
        onBlur={flush}
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
