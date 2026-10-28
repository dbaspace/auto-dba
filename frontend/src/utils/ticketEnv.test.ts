import { describe, expect, it } from 'vitest'
import { unionApproverNames } from './ticketEnv'

describe('unionApproverNames', () => {
  it('merges lists and keeps first spelling', () => {
    expect(
      unionApproverNames([
        ['songxincong', 'zongjianhui'],
        ['admin', 'SongXinCong'],
        [],
      ]),
    ).toEqual(['songxincong', 'zongjianhui', 'admin'])
  })

  it('returns empty for no lists', () => {
    expect(unionApproverNames([])).toEqual([])
    expect(unionApproverNames([[], []])).toEqual([])
  })
})
