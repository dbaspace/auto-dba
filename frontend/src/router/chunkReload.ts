/** 发版后旧入口仍在，懒加载会去要已经不存在的分包。只刷新一次。 */
export const CHUNK_RELOAD_KEY = 'auto-dba-chunk-reload'
export const CHUNK_RELOAD_WINDOW_MS = 10_000

export function isChunkLoadError(err: unknown): boolean {
  const msg = err instanceof Error ? `${err.name} ${err.message}` : String(err ?? '')
  return /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|ChunkLoadError/i.test(msg)
}

/** 窗口内已刷新过则返回 null，否则返回要写入的时间戳。 */
export function claimChunkReload(previous: string | null, now: number): string | null {
  if (previous && now - Number(previous) < CHUNK_RELOAD_WINDOW_MS) return null
  return String(now)
}

export function reloadOnceForStaleChunk(): boolean {
  if (import.meta.env.DEV) return false
  let stamp: string | null
  try {
    stamp = claimChunkReload(sessionStorage.getItem(CHUNK_RELOAD_KEY), Date.now())
    if (!stamp) return false
    sessionStorage.setItem(CHUNK_RELOAD_KEY, stamp)
  } catch {
    return false
  }
  window.location.reload()
  return true
}
