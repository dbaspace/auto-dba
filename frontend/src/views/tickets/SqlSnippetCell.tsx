import React, { useState } from 'react'
import { Alert, Button, Modal } from 'antd'

const PREVIEW_LEN = 80

/** 后端历史 displaySQL 截断或以 …/... 结尾的不完整片段 */
function looksTruncated(sql: string): boolean {
  const s = sql.trimEnd()
  if (s.endsWith('...') || s.endsWith('…')) return true
  // 截断常落在字符串字面量中间，如 '2026...
  if (/['"][A-Za-z0-9_-]*\.\.\.$/.test(s)) return true
  return false
}

export default function SqlSnippetCell(props: { sql?: string | null }) {
  const [open, setOpen] = useState(false)
  const text = String(props.sql ?? '').trim()
  if (!text) return <span>-</span>

  const needView = text.length > PREVIEW_LEN
  const preview = needView ? `${text.slice(0, PREVIEW_LEN)}…` : text
  const truncated = looksTruncated(text)

  return (
    <>
      <div className="sql-snippet-cell">
        <span className="sql-snippet-preview">{preview}</span>
        {needView ? (
          <Button type="link" size="small" className="sql-snippet-view" onClick={() => setOpen(true)}>
            查看
          </Button>
        ) : null}
      </div>
      <Modal
        open={open}
        title="SQL语句"
        width={720}
        destroyOnClose
        onCancel={() => setOpen(false)}
        footer={[
          <Button key="close" type="primary" onClick={() => setOpen(false)}>
            关闭
          </Button>,
        ]}
      >
        {truncated ? (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="展示数据不全"
            description="该条执行明细中的 SQL 在落库时被截断。完整提交内容请查看上方「申请信息 → 执行SQL」。"
          />
        ) : null}
        <pre className="sql-pre sql-snippet-full">{text}</pre>
      </Modal>
    </>
  )
}
