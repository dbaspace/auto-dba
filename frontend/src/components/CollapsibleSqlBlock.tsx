import React, { useEffect, useMemo, useState } from 'react'
import { Button, message } from 'antd'
import { CopyOutlined, DownOutlined, UpOutlined } from '@ant-design/icons'
import { highlightSql } from '../utils/highlightSql'
import './CollapsibleSqlBlock.css'

const DEFAULT_COLLAPSED_LINES = 8
const SHORT_CHAR_LIMIT = 400

type Props = {
  value: string
  collapsedLines?: number
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* fallback below */
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

export default function CollapsibleSqlBlock({
  value,
  collapsedLines = DEFAULT_COLLAPSED_LINES,
}: Props) {
  const text = String(value || '')
  const lineCount = text ? text.split('\n').length : 0
  const needsCollapse = lineCount > collapsedLines || text.length > SHORT_CHAR_LIMIT
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    setExpanded(!needsCollapse)
  }, [text, needsCollapse])

  const html = useMemo(() => highlightSql(text), [text])

  async function onCopy() {
    if (!text.trim()) {
      message.warning('无内容可复制')
      return
    }
    const ok = await copyText(text)
    if (ok) message.success('已复制 SQL')
    else message.error('复制失败')
  }

  return (
    <div className="collapsible-sql">
      <div className="collapsible-sql-toolbar">
        <Button size="small" icon={<CopyOutlined />} onClick={() => void onCopy()}>
          复制
        </Button>
        {needsCollapse ? (
          <Button
            size="small"
            type="link"
            icon={expanded ? <UpOutlined /> : <DownOutlined />}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? '收起' : '展开全部'}
          </Button>
        ) : null}
      </div>
      <pre
        className={`collapsible-sql-body${expanded ? ' is-expanded' : ' is-collapsed'}`}
        style={
          expanded
            ? undefined
            : { maxHeight: `calc(${collapsedLines} * 1.5em)` }
        }
        dangerouslySetInnerHTML={{ __html: html || '&nbsp;' }}
      />
      {!expanded && needsCollapse ? (
        <button type="button" className="collapsible-sql-fade" onClick={() => setExpanded(true)}>
          展开全部
        </button>
      ) : null}
    </div>
  )
}
