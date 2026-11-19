import { describe, expect, it } from 'vitest'
import { CHUNK_RELOAD_WINDOW_MS, claimChunkReload, isChunkLoadError } from './chunkReload'

describe('isChunkLoadError', () => {
  it('matches stale dynamic import failures', () => {
    expect(isChunkLoadError(new TypeError('Failed to fetch dynamically imported module: https://keeplus.example/assets/AgentManage-0vxH10zP.js'))).toBe(true)
    expect(isChunkLoadError(new Error('error loading dynamically imported module'))).toBe(true)
    expect(isChunkLoadError('Importing a module script failed.')).toBe(true)
    expect(isChunkLoadError(new Error('ChunkLoadError: Loading chunk 3 failed'))).toBe(true)
  })

  it('ignores other failures', () => {
    expect(isChunkLoadError(new Error('网络错误'))).toBe(false)
    expect(isChunkLoadError(null)).toBe(false)
  })
})

describe('claimChunkReload', () => {
  it('allows one reload and blocks the next one inside the window', () => {
    const now = 1_700_000_000_000
    const stamp = claimChunkReload(null, now)
    expect(stamp).toBe(String(now))
    expect(claimChunkReload(stamp, now + CHUNK_RELOAD_WINDOW_MS - 1)).toBeNull()
    expect(claimChunkReload(stamp, now + CHUNK_RELOAD_WINDOW_MS)).toBe(String(now + CHUNK_RELOAD_WINDOW_MS))
  })
})
