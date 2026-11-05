import { describe, expect, it } from 'vitest'
import { resolveOpenTabTitle } from './tabsSlice'

describe('resolveOpenTabTitle', () => {
  it('云资源页签用登记标题，不用菜单短名', () => {
    expect(resolveOpenTabTitle('/ops/cloud/postgres', 'PostgreSQL')).toBe('云 PostgreSQL')
    expect(resolveOpenTabTitle('/ops/cloud/mysql', 'MySQL')).toBe('云 MySQL')
    expect(resolveOpenTabTitle('/ops/cloud/redis', 'Redis')).toBe('云 Redis')
    expect(resolveOpenTabTitle('/ops/cloud/hosts', '主机')).toBe('云主机')
    expect(resolveOpenTabTitle('/ops/cloud/policies', '自愈策略')).toBe('云自愈策略')
  })

  it('实例页保留传入的类型名', () => {
    expect(resolveOpenTabTitle('/config/instances', 'PostgreSQL')).toBe('PostgreSQL')
  })
})
