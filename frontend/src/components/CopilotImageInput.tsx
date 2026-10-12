import React, { useRef } from 'react'
import { Button, Space, Typography, message } from 'antd'
import { DeleteOutlined, PictureOutlined } from '@ant-design/icons'

export type CopilotImage = {
  data: string // pure base64 or data URL
  mime_type: string
  preview: string // data URL for <img>
  name?: string
}

const MAX = 5
const MAX_BYTES = 15 * 1024 * 1024
const ALLOW = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']

function readFileAsImage(file: File): Promise<CopilotImage> {
  return new Promise((resolve, reject) => {
    if (!ALLOW.includes(file.type) && !file.type.startsWith('image/')) {
      reject(new Error('仅支持 png/jpeg/gif/webp'))
      return
    }
    if (file.size > MAX_BYTES) {
      reject(new Error('单张图片不超过 15MB'))
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const preview = String(reader.result || '')
      const comma = preview.indexOf(',')
      const header = comma > 0 ? preview.slice(0, comma) : ''
      const data = comma > 0 ? preview.slice(comma + 1) : preview
      let mime = file.type || 'image/png'
      const m = /data:([^;]+);/.exec(header)
      if (m?.[1]) mime = m[1]
      if (mime === 'image/jpg') mime = 'image/jpeg'
      resolve({ data, mime_type: mime, preview, name: file.name })
    }
    reader.onerror = () => reject(new Error('读取图片失败'))
    reader.readAsDataURL(file)
  })
}

type Props = {
  value: CopilotImage[]
  onChange: (next: CopilotImage[]) => void
  disabled?: boolean
  textareaProps?: React.TextareaHTMLAttributes<HTMLTextAreaElement> & {
    value?: string
    onChange?: (e: React.ChangeEvent<HTMLTextAreaElement>) => void
  }
}

/** 支持粘贴 / 选择图片的提问输入区 */
export default function CopilotImageInput({ value, onChange, disabled, textareaProps }: Props) {
  const fileRef = useRef<HTMLInputElement>(null)

  const addFiles = async (files: FileList | File[]) => {
    const list = Array.from(files)
    if (!list.length) return
    const room = MAX - value.length
    if (room <= 0) {
      message.warning(`最多 ${MAX} 张图片`)
      return
    }
    try {
      const added: CopilotImage[] = []
      for (const f of list.slice(0, room)) {
        added.push(await readFileAsImage(f))
      }
      onChange([...value, ...added])
    } catch (e: any) {
      message.error(e?.message || '添加图片失败')
    }
  }

  const onPaste = async (e: React.ClipboardEvent) => {
    const items = e.clipboardData?.items
    if (!items) return
    const files: File[] = []
    for (const it of Array.from(items)) {
      if (it.type.startsWith('image/')) {
        const f = it.getAsFile()
        if (f) files.push(f)
      }
    }
    if (files.length) {
      e.preventDefault()
      await addFiles(files)
    }
  }

  return (
    <div style={{ width: '100%', maxWidth: '100%', minWidth: 0, overflow: 'hidden', boxSizing: 'border-box' }}>
      <textarea
        {...textareaProps}
        disabled={disabled || textareaProps?.disabled}
        onPaste={(e) => {
          void onPaste(e)
          textareaProps?.onPaste?.(e)
        }}
        rows={textareaProps?.rows ?? 3}
        style={{
          display: 'block',
          width: '100%',
          maxWidth: '100%',
          boxSizing: 'border-box',
          padding: 8,
          borderRadius: 6,
          border: '1px solid #d9d9d9',
          resize: 'vertical',
          fontFamily: 'inherit',
          fontSize: 14,
          ...((textareaProps?.style as React.CSSProperties) || {}),
        }}
        placeholder={textareaProps?.placeholder || '描述问题；可 Ctrl+V 粘贴截图'}
      />
      <Space style={{ marginTop: 8 }} wrap>
        <Button
          size="small"
          icon={<PictureOutlined />}
          disabled={disabled || value.length >= MAX}
          onClick={() => fileRef.current?.click()}
        >
          添加图片
        </Button>
        <Typography.Text type="secondary">
          支持粘贴/上传，最多 {MAX} 张，用于识图分析监控/告警截图
        </Typography.Text>
        <input
          ref={fileRef}
          type="file"
          accept="image/png,image/jpeg,image/gif,image/webp"
          multiple
          hidden
          onChange={(e) => {
            if (e.target.files) void addFiles(e.target.files)
            e.target.value = ''
          }}
        />
      </Space>
      {value.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 8 }}>
          {value.map((img, i) => (
            <div key={i} style={{ position: 'relative', width: 72, height: 72 }}>
              <img
                src={img.preview}
                alt={img.name || `img-${i}`}
                style={{ width: 72, height: 72, objectFit: 'cover', borderRadius: 4, border: '1px solid #eee' }}
              />
              <Button
                type="text"
                size="small"
                danger
                icon={<DeleteOutlined />}
                disabled={disabled}
                style={{ position: 'absolute', top: -6, right: -6, background: '#fff' }}
                onClick={() => onChange(value.filter((_, j) => j !== i))}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export function toApiImages(imgs: CopilotImage[]) {
  return imgs.map((i) => ({ data: i.data, mime_type: i.mime_type }))
}
