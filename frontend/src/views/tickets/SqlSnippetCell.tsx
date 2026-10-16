import React, { useState } from 'react'
import { Button, Modal } from 'antd'

const PREVIEW_LEN = 80

export default function SqlSnippetCell(props: { sql?: string | null }) {
  const [open, setOpen] = useState(false)
  const text = String(props.sql ?? '').trim()
  if (!text) return <span>-</span>

  const needView = text.length > PREVIEW_LEN
  const preview = needView ? `${text.slice(0, PREVIEW_LEN)}…` : text

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
        <pre className="sql-pre sql-snippet-full">{text}</pre>
      </Modal>
    </>
  )
}
