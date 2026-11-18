import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  clearQueryWorkspaces,
  queryFavoriteKey,
  queryHistoryKey,
  queryWorkspaceKey,
  readQueryWorkspace,
  writeQueryWorkspace,
} from './querySession'

function memStorage(): Storage {
  const data = new Map<string, string>()
  return {
    get length() {
      return data.size
    },
    clear() {
      data.clear()
    },
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null
    },
    key(index: number) {
      return [...data.keys()][index] ?? null
    },
    removeItem(key: string) {
      data.delete(key)
    },
    setItem(key: string, value: string) {
      data.set(key, String(value))
    },
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('query workspace', () => {
  it('keeps the same user sql in localStorage and leaves favorites alone', () => {
    const local = memStorage()
    const session = memStorage()
    vi.stubGlobal('localStorage', local)
    vi.stubGlobal('sessionStorage', session)

    const favKey = queryFavoriteKey('tenant-default')
    const histKey = queryHistoryKey('tenant-default')
    local.setItem(favKey, '[{"id":"f1","sql":"select 1"}]')
    local.setItem(histKey, '[{"sql":"select 2"}]')
    session.setItem('auto-dba-query-workspace.tenant-default', '{"tabs":[{"sql":"select host"}]}')

    const migrated = readQueryWorkspace('user-1', 'tenant-default')
    expect(migrated).toContain('select host')
    expect(local.getItem(queryWorkspaceKey('user-1', 'tenant-default'))).toContain('select host')
    expect(local.getItem(favKey)).toContain('select 1')
    expect(local.getItem(histKey)).toContain('select 2')

    writeQueryWorkspace('user-1', 'tenant-default', '{"tabs":[{"sql":"select host,port"}]}')
    clearQueryWorkspaces()
    expect(session.getItem('auto-dba-query-workspace.tenant-default')).toBeNull()
    expect(readQueryWorkspace('user-1', 'tenant-default')).toContain('select host,port')
    expect(readQueryWorkspace('user-2', 'tenant-default')).toBeNull()
    expect(local.getItem(favKey)).toContain('select 1')
    expect(local.getItem(histKey)).toContain('select 2')
    expect(queryFavoriteKey('tenant-default')).toBe('auto-dba-query-favorites.tenant-default')
    expect(queryHistoryKey('tenant-default')).toBe('auto-dba-query-history.tenant-default')
  })

  it('does not read or write before the user and tenant are ready', () => {
    const local = memStorage()
    vi.stubGlobal('localStorage', local)
    vi.stubGlobal('sessionStorage', memStorage())
    local.setItem(queryWorkspaceKey('user-1', 'tenant-default'), '{"tabs":[{"sql":"keep"}]}')
    expect(readQueryWorkspace('', 'tenant-default')).toBeNull()
    writeQueryWorkspace('', 'tenant-default', '{"tabs":[]}')
    expect(local.getItem(queryWorkspaceKey('user-1', 'tenant-default'))).toContain('keep')
  })
})
