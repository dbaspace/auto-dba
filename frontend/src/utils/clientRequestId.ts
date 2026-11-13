type RequestIdSource = {
  randomUUID?: () => string
  getRandomValues?: (bytes: Uint8Array) => Uint8Array
}

function isClientRequestID(id: string): boolean {
  return id.length >= 8 && id.length <= 64 && /^[A-Za-z0-9-]+$/.test(id)
}

function uuidV4(bytes: Uint8Array): string {
  const b = bytes.slice()
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const hex = Array.from(b, (n) => n.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

/** 查询审计用的 client_request_id。HTTP 页面没有 crypto.randomUUID 时改用 getRandomValues。 */
export function newClientRequestID(source: RequestIdSource | undefined = globalThis.crypto): string {
  if (source && typeof source.randomUUID === 'function') {
    try {
      const id = String(source.randomUUID() || '').trim()
      if (isClientRequestID(id)) return id
    } catch {
      /* 落到 getRandomValues */
    }
  }
  const bytes = new Uint8Array(16)
  if (source && typeof source.getRandomValues === 'function') {
    try {
      source.getRandomValues(bytes)
      return uuidV4(bytes)
    } catch {
      /* 落到 Math.random */
    }
  }
  for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  return uuidV4(bytes)
}
