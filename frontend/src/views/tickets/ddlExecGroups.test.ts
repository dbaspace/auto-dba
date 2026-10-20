import { describe, expect, it } from 'vitest'
import { buildDdlExecGroups } from './ddlExecGroups'

describe('buildDdlExecGroups', () => {
  it('failed + rolled_back 时实例不得 ok', () => {
    const { groups, stats } = buildDdlExecGroups(
      [
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'rolled_back', affected_row: 1 },
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u2', status: 'rolled_back', affected_row: 2 },
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u3', status: 'failed', error_message: 'boom' },
      ],
      { execMessage: '第 3 条失败' },
    )
    expect(groups).toHaveLength(1)
    expect(groups[0].ok).toBe(false)
    expect(groups[0].rolledBack).toBe(2)
    expect(groups[0].error).toBe('boom')
    expect(stats.instOk).toBe(0)
    expect(stats.instFail).toBe(1)
    expect(stats.rolledBackSQL).toBe(2)
  })

  it('仅有 rolled_back 无 failed 时实例也不得 ok', () => {
    const { groups } = buildDdlExecGroups(
      [
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'rolled_back' },
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u2', status: 'rolled_back' },
      ],
      { execMessage: '提交事务失败', execFailed: true },
    )
    expect(groups[0].ok).toBe(false)
    expect(groups[0].error).toBe('提交事务失败')
  })

  it('records 全 success 但整单 execFailed 时不得 ok', () => {
    const { groups, stats } = buildDdlExecGroups(
      [
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'success' },
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u2', status: 'success' },
      ],
      { execFailed: true, execMessage: 'commit failed' },
    )
    expect(groups[0].ok).toBe(false)
    expect(groups[0].error).toBe('commit failed')
    expect(stats.instOk).toBe(0)
    expect(stats.instFail).toBe(1)
  })

  it('execSuccess=false 时即使明细全 success 也不得 ok', () => {
    const { groups } = buildDdlExecGroups(
      [{ instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'success' }],
      { execSuccess: false, execMessage: '整单失败' },
    )
    expect(groups[0].ok).toBe(false)
  })

  it('全部 success 且未失败时实例 ok', () => {
    const { groups, stats } = buildDdlExecGroups(
      [{ instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'success' }],
      { execSuccess: true },
    )
    expect(groups[0].ok).toBe(true)
    expect(stats.instOk).toBe(1)
  })

  it('优先使用后端 success_count / statement_count', () => {
    const { stats } = buildDdlExecGroups(
      [
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'success' },
        { instance_id: 'i1', host: 'h', db: 'd', sql: 'u2', status: 'success' },
      ],
      { execSuccess: true, successCount: 50, statementCount: 50, compacted: true },
    )
    expect(stats.successSQL).toBe(50)
    expect(stats.totalSQL).toBe(50)
    expect(stats.countsFromBackend).toBe(true)
    expect(stats.compacted).toBe(true)
  })
})
