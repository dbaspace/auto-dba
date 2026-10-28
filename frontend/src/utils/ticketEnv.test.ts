import { describe, expect, it } from 'vitest'
import { resolveDbNameForApprover, unionApproverNames } from './ticketEnv'

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

describe('resolveDbNameForApprover', () => {
  const records = [
    { instance_id: 'i1', db_name: 'db_marketing_064', approvers: ['a', 'b', 'c'] },
    { instance_id: 'i2', db_name: 'db_marketing_OSS', approvers: ['a', 'b', 'c'] },
    { instance_id: 'i3', db_name: 'db_marketing_000', approvers: ['a', 'b', 'c'] },
    { instance_id: 'i3', db_name: 'other_db', approvers: [] },
  ]

  it('prefers exact db name on the instance', () => {
    expect(resolveDbNameForApprover('i1', 'db_marketing_064', records)).toBe('db_marketing_064')
  })

  it('uses shard prefix / family prefix when exact name missing', () => {
    expect(
      resolveDbNameForApprover('i2', 'db_marketing_064', records, {
        shardDb: true,
        shardDbPrefix: 'db_marketing_',
      }),
    ).toBe('db_marketing_OSS')
    // 前缀 db_marketing_0 本身匹配不到 OSS，回退族前缀 db_marketing_
    expect(
      resolveDbNameForApprover('i2', 'db_marketing_064', records, {
        shardDb: true,
        shardDbPrefix: 'db_marketing_0',
      }),
    ).toBe('db_marketing_OSS')
    expect(
      resolveDbNameForApprover('i3', 'db_marketing_064', records, {
        shardDb: true,
        shardDbPrefix: 'db_marketing_0',
      }),
    ).toBe('db_marketing_000')
  })

  it('returns empty when no match so backend falls back to instance', () => {
    expect(resolveDbNameForApprover('i2', 'db_marketing_064', records)).toBe('')
    expect(
      resolveDbNameForApprover('i9', 'db_marketing_064', records, {
        shardDb: true,
        shardDbPrefix: 'db_marketing_0',
      }),
    ).toBe('')
  })
})
