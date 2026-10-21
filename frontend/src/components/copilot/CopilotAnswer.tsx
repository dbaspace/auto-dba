import React from 'react'
import { Table, Typography } from 'antd'
import { parseRichBlocks } from './richText'

function TextBlock({ text }: { text: string }) {
  const lines = text.split('\n')
  return (
    <div>
      {lines.map((line, i) => {
        const heading = line.trim().match(/^\*\*([^*]+)\*\*$/)
        if (heading) {
          return (
            <Typography.Text key={i} strong style={{ display: 'block', marginTop: i ? 8 : 0 }}>
              {heading[1]}
            </Typography.Text>
          )
        }
        const parts = line.split(/(\*\*[^*]+\*\*)/g)
        return (
          <div key={i} style={{ whiteSpace: 'pre-wrap' }}>
            {parts.map((part, j) => {
              const inline = part.match(/^\*\*([^*]+)\*\*$/)
              if (inline) return <Typography.Text key={j} strong>{inline[1]}</Typography.Text>
              return <span key={j}>{part}</span>
            })}
          </div>
        )
      })}
    </div>
  )
}

export default function CopilotAnswer({ text }: { text: string }) {
  if (!text) return null
  const blocks = parseRichBlocks(text)
  return (
    <div>
      {blocks.map((block, i) => {
        if (block.type === 'table') {
          return (
            <Table
              key={i}
              size="small"
              pagination={false}
              style={{ marginTop: 8 }}
              columns={block.columns.map((title, ci) => ({
                title,
                dataIndex: String(ci),
                key: String(ci),
              }))}
              dataSource={block.rows.map((row, ri) => {
                const record: Record<string, string> = { key: String(ri) }
                block.columns.forEach((_, ci) => { record[String(ci)] = row[ci] ?? '' })
                return record
              })}
            />
          )
        }
        return <TextBlock key={i} text={block.text} />
      })}
    </div>
  )
}
