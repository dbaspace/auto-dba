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

  it('同一实例的多个分库分成多块', () => {
    const { groups, stats } = buildDdlExecGroups(
      [
        { instance_id: 'i1', host: 'pg.order.bj', port: 5432, db: 'db_order_0', sql: 's1', status: 'success' },
        { instance_id: 'i1', host: 'pg.order.bj', port: 5432, db: 'db_order_1', sql: 's1', status: 'success' },
      ],
      { execSuccess: true },
    )
    expect(groups).toHaveLength(2)
    expect(groups[0].label).toBe('db_order_0 - pg.order.bj:5432')
    expect(groups[1].label).toBe('db_order_1 - pg.order.bj:5432')
    expect(stats.instTotal).toBe(2)
  })

  it('全部 success 且未失败时实例 ok', () => {
    const { groups, stats } = buildDdlExecGroups(
      [{ instance_id: 'i1', host: 'h', db: 'd', sql: 'u1', status: 'success' }],
      { execSuccess: true },
    )
    expect(groups[0].ok).toBe(true)
    expect(stats.instOk).toBe(1)
  })

  it('整单失败但各库有成功有失败时，成功库仍标成功', () => {
    const { groups, stats } = buildDdlExecGroups(
      [
        { instance_id: 'a', host: 'h', db: 'okdb', sql: 's1', status: 'success' },
        { instance_id: 'b', host: 'h', db: 'baddb', sql: 's1', status: 'failed', error_message: 'boom' },
      ],
      { execFailed: true, execSuccess: false, execMessage: '部分分库执行失败，成功 1 个库，失败 1 个库' },
    )
    expect(groups[0].ok).toBe(true)
    expect(groups[1].ok).toBe(false)
    expect(groups[1].error).toBe('boom')
    expect(stats.instOk).toBe(1)
    expect(stats.instFail).toBe(1)
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

  it('不同实例使用各自 envByInstanceId，path 前缀不同', () => {
    const { groups } = buildDdlExecGroups(
      [
        { instance_id: 'bj', host: 'pg.scdata.bj', port: 5432, db: 'db_x', sql: 's1', status: 'success' },
        { instance_id: 'fra', host: 'pg.scdata.fra', port: 5432, db: 'db_x', sql: 's1', status: 'success' },
      ],
      {
        env: 'frankfurt',
        envByInstanceId: { bj: 'beijing', fra: 'frankfurt' },
        execSuccess: true,
      },
    )
    expect(groups[0].path).toContain('beijing /')
    expect(groups[1].path).toContain('frankfurt /')
    expect(groups[0].path).not.toContain('frankfurt /')
  })

  it('有 instance_id 但缺映射时不套用主实例 fallbackEnv', () => {
    const { groups } = buildDdlExecGroups(
      [{ instance_id: 'other', host: 'pg.pobeice', db: 'db_x', sql: 's1', status: 'success' }],
      { env: 'frankfurt', envByInstanceId: {}, execSuccess: true },
    )
    expect(groups[0].path.startsWith('- /')).toBe(true)
    expect(groups[0].path).not.toContain('frankfurt /')
  })

  it('缺 instance_id 时可用 envByHost，否则回退主环境', () => {
    const { groups: byHost } = buildDdlExecGroups(
      [{ host: 'pg.scdata.bj:5432', db: 'db_x', sql: 's1', status: 'success' }],
      { env: 'frankfurt', envByHost: { 'pg.scdata.bj': 'beijing' }, execSuccess: true },
    )
    expect(byHost[0].path).toContain('beijing /')

    const { groups: fallback } = buildDdlExecGroups(
      [{ host: 'unknown.host', db: 'db_x', sql: 's1', status: 'success' }],
      { env: 'frankfurt', execSuccess: true },
    )
    expect(fallback[0].path).toContain('frankfurt /')
  })
})
