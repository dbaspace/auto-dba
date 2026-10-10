import React, { useEffect, useRef, useState } from 'react'
import { Image, Space, Spin, Upload, message } from 'antd'
import { CloseCircleFilled, PictureOutlined, PlusOutlined } from '@ant-design/icons'
import api from '../../api/request'

export const TODO_IMAGE_MAX = 9
const MAX_BYTES = 5 * 1024 * 1024
const ACCEPT = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']

// 图片接口需要 Authorization 头，<img src> 带不上，统一取 blob 转 object URL 并缓存。
const cache = new Map<string, Promise<string>>()

function loadImage(id: string) {
  let p = cache.get(id)
  if (!p) {
    p = api
      .get(`/todos/images/${id}`, { responseType: 'blob', silent: true })
      .then((res) => URL.createObjectURL(res.data as Blob))
      .catch((e) => {
        cache.delete(id)
        throw e
      })
    cache.set(id, p)
  }
  return p
}

/** 过滤类型 / 大小并按剩余名额截断，不合格的给出提示。 */
export function pickValidImages(files: File[], room: number, max = TODO_IMAGE_MAX): File[] {
  if (room <= 0) {
    if (files.length) message.warning(`最多上传 ${max} 张图片`)
    return []
  }
  const valid = files.filter((f) => {
    if (!ACCEPT.includes(f.type)) {
      message.warning(`${f.name || '图片'}：仅支持 PNG / JPG / GIF / WebP`)
      return false
    }
    if (f.size > MAX_BYTES) {
      message.warning(`${f.name || '图片'}：单张不能超过 5MB`)
      return false
    }
    return true
  })
  if (valid.length > room) message.warning(`最多上传 ${max} 张图片，已忽略多余的 ${valid.length - room} 张`)
  return valid.slice(0, room)
}

/** 上传一张图片，返回图片 ID；本地文件直接作为预览，免去再下载。 */
export async function uploadTodoImageFile(file: File): Promise<string | undefined> {
  const fd = new FormData()
  fd.append('file', file, file.name || 'paste.png')
  const res = await api.post('/todos/images', fd, { timeout: 60000 })
  const id = res.data?.data?.id as string | undefined
  if (id) cache.set(id, Promise.resolve(URL.createObjectURL(file)))
  return id
}

/** 从粘贴事件中取图片文件。 */
export function clipboardImages(e: React.ClipboardEvent): File[] {
  return Array.from(e.clipboardData?.files || []).filter((f) => f.type.startsWith('image/'))
}

const boxStyle = (size: number): React.CSSProperties => ({
  width: size,
  height: size,
  borderRadius: 6,
  border: '1px solid #f0f0f0',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: '#fafafa',
  color: '#bbb',
})

export function AuthImage({ id, size = 64 }: { id: string; size?: number }) {
  const [src, setSrc] = useState<string>()
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let alive = true
    setSrc(undefined)
    setFailed(false)
    loadImage(id)
      .then((u) => alive && setSrc(u))
      .catch(() => alive && setFailed(true))
    return () => {
      alive = false
    }
  }, [id])
  if (!src) {
    return <div style={boxStyle(size)}>{failed ? <PictureOutlined title="图片加载失败" /> : <Spin size="small" />}</div>
  }
  return (
    <Image
      src={src}
      width={size}
      height={size}
      style={{ objectFit: 'cover', borderRadius: 6, border: '1px solid #f0f0f0' }}
    />
  )
}

/** 只读缩略图，点击可放大并在同组内切换。 */
export function TodoImageThumbs({ ids, size = 56 }: { ids?: string[]; size?: number }) {
  if (!ids?.length) return null
  return (
    <Image.PreviewGroup>
      <Space wrap size={6}>
        {ids.map((id) => <AuthImage key={id} id={id} size={size} />)}
      </Space>
    </Image.PreviewGroup>
  )
}

type Pending = { key: string; name: string }

/**
 * 图片上传（Form 受控：value 为图片 ID 数组）。
 * 选择文件或聚焦后 Ctrl+V 粘贴截图；上传即落库为未绑定图片，保存表单时由后端绑定。
 */
export function TodoImageUpload({
  value,
  onChange,
  max = TODO_IMAGE_MAX,
  size = 72,
}: {
  value?: string[]
  onChange?: (ids: string[]) => void
  max?: number
  size?: number
}) {
  const ids = value || []
  const idsRef = useRef(ids)
  idsRef.current = ids
  const [pending, setPending] = useState<Pending[]>([])
  const pendingRef = useRef(0)

  const emit = (next: string[]) => {
    idsRef.current = next
    onChange?.(next)
  }

  const uploadOne = async (file: File) => {
    const key = `${Date.now()}-${Math.random()}`
    pendingRef.current += 1
    setPending((p) => [...p, { key, name: file.name }])
    try {
      const id = await uploadTodoImageFile(file)
      if (id) emit([...idsRef.current, id])
    } finally {
      pendingRef.current -= 1
      setPending((p) => p.filter((x) => x.key !== key))
    }
  }

  const accept = (files: File[]) => {
    pickValidImages(files, max - idsRef.current.length - pendingRef.current, max).forEach((f) => void uploadOne(f))
  }

  const onPaste = (e: React.ClipboardEvent) => {
    const files = clipboardImages(e)
    if (!files.length) return
    e.preventDefault()
    e.stopPropagation()
    accept(files)
  }

  const full = ids.length + pending.length >= max
  return (
    <div
      tabIndex={0}
      onPaste={onPaste}
      style={{ outline: 'none' }}
      onFocus={(e) => (e.currentTarget.style.boxShadow = '0 0 0 2px rgba(22,119,255,0.15)')}
      onBlur={(e) => (e.currentTarget.style.boxShadow = 'none')}
    >
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, padding: 2 }}>
        <Image.PreviewGroup>
          {ids.map((id) => (
            <div key={id} style={{ position: 'relative' }}>
              <AuthImage id={id} size={size} />
              <CloseCircleFilled
                title="移除"
                onClick={() => emit(idsRef.current.filter((x) => x !== id))}
                style={{
                  position: 'absolute', top: -6, right: -6, fontSize: 16, color: '#ff4d4f',
                  background: '#fff', borderRadius: '50%', cursor: 'pointer',
                }}
              />
            </div>
          ))}
        </Image.PreviewGroup>
        {pending.map((p) => (
          <div key={p.key} style={boxStyle(size)} title={p.name}><Spin size="small" /></div>
        ))}
        {!full && (
          <Upload
            accept={ACCEPT.join(',')}
            multiple
            showUploadList={false}
            fileList={[]}
            beforeUpload={(file, list) => {
              if (file === list[0]) accept(list as File[])
              return false
            }}
          >
            <div style={{ ...boxStyle(size), border: '1px dashed #d9d9d9', cursor: 'pointer', flexDirection: 'column', color: '#999' }}>
              <PlusOutlined />
              <div style={{ fontSize: 12, marginTop: 4 }}>上传</div>
            </div>
          </Upload>
        )}
      </div>
      <div style={{ fontSize: 12, color: '#999', marginTop: 4 }}>
        PNG / JPG / GIF / WebP，单张不超过 5MB，最多 {max} 张；点击此区域后可 Ctrl+V 粘贴截图
      </div>
    </div>
  )
}
