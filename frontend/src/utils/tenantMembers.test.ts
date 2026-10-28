import { describe, expect, it } from 'vitest'
import { filterNamesByMembers } from './tenantMembers'

describe('filterNamesByMembers', () => {
  it('keeps members and drops outsiders', () => {
    expect(filterNamesByMembers(['admin', 'daiyong', 'Admin'], ['daiyong', 'gaoxiang'])).toEqual({
      kept: ['daiyong'],
      dropped: ['admin'],
    })
  })

  it('handles empty', () => {
    expect(filterNamesByMembers(undefined, ['a'])).toEqual({ kept: [], dropped: [] })
    expect(filterNamesByMembers(['a'], [])).toEqual({ kept: [], dropped: ['a'] })
  })
})
