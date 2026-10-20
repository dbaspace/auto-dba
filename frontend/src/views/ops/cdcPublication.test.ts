import { describe, expect, it } from 'vitest'
import {
  analyzeMappingPipelineDrift,
  deriveShardRegexPart,
  groupPublishedTablesByMappings,
  isProtectedPublicationTable,
  normalizeTableName,
  parseConnectorTableConfig,
} from './cdcPublication'

describe('normalizeTableName', () => {
  it('strips public prefix', () => {
    expect(normalizeTableName('public.tbl_a')).toBe('tbl_a')
    expect(normalizeTableName('PUBLIC.tbl_a')).toBe('tbl_a')
  })
})

describe('isProtectedPublicationTable', () => {
  it('detects signal table', () => {
    expect(isProtectedPublicationTable('debezium_signal')).toBe(true)
    expect(isProtectedPublicationTable('public.debezium_signal')).toBe(true)
    expect(isProtectedPublicationTable('tbl_a')).toBe(false)
  })
})

describe('deriveShardRegexPart', () => {
  it('derives from source tables', () => {
    expect(deriveShardRegexPart('ods_tbl_order', '', ['tbl_order_0', 'tbl_order_1'])).toBe(
      'tbl_order(_[0-9]+|[0-9]+)',
    )
  })
  it('uses default from target', () => {
    expect(deriveShardRegexPart('tbl_orders', '', [])).toBe('tbl_orders(_[0-9]+|[0-9]+)')
  })
})

describe('groupPublishedTablesByMappings', () => {
  it('marks signal as system orphan and groups shards', () => {
    const rows = groupPublishedTablesByMappings(
      ['tbl_a', 'tbl_order_0', 'tbl_order_1', 'debezium_signal'],
      [
        { target_table: 'tbl_a', source_tables: ['tbl_a'] },
        {
          target_table: 'tbl_order',
          source_tables: ['tbl_order_0', 'tbl_order_1'],
          is_shard_group: true,
        },
      ],
    )
    expect(rows.some((r) => r.kind === 'shard' && r.tables.length === 2)).toBe(true)
    expect(rows.some((r) => r.system && r.tables[0] === 'debezium_signal')).toBe(true)
  })
})

describe('analyzeMappingPipelineDrift', () => {
  it('flags missing include and router for shard', () => {
    const drift = analyzeMappingPipelineDrift(
      ['tbl_order_0', 'tbl_order_1', 'debezium_signal'],
      [
        {
          target_table: 'tbl_order',
          source_tables: ['tbl_order_0', 'tbl_order_1'],
          is_shard_group: true,
        },
      ],
      {
        'table.include.list': 'public.other',
        transforms: 'unwrap',
      },
    )
    expect(drift.publishedSystemCount).toBe(1)
    expect(drift.shardMissingInclude.length).toBe(1)
    expect(drift.shardMissingRouter.length).toBe(1)
    expect(drift.hasDrift).toBe(true)
  })

  it('passes when include pattern and router present', () => {
    const regex = deriveShardRegexPart('tbl_order', '', ['tbl_order_0', 'tbl_order_1'])
    const drift = analyzeMappingPipelineDrift(
      ['tbl_order_0', 'tbl_order_1'],
      [
        {
          target_table: 'tbl_order',
          source_tables: ['tbl_order_0', 'tbl_order_1'],
          is_shard_group: true,
        },
      ],
      {
        'table.include.list': `public.${regex}`,
        transforms: 'tbl_order',
        'transforms.tbl_order.type': 'io.debezium.transforms.ByLogicalTableRouter',
      },
    )
    expect(drift.shardMissingInclude).toEqual([])
    expect(drift.shardMissingRouter).toEqual([])
    expect(drift.hasDrift).toBe(false)
  })
})

describe('parseConnectorTableConfig', () => {
  it('splits exact and pattern entries', () => {
    const p = parseConnectorTableConfig({
      'table.include.list': 'public.tbl_a,public.tbl_order(_[0-9]+|[0-9]+)',
      transforms: 'tbl_order,unwrap',
      'transforms.tbl_order.type': 'io.debezium.transforms.ByLogicalTableRouter',
    })
    expect(p.exactTables).toContain('tbl_a')
    expect(p.patterns.some((x) => x.includes('tbl_order'))).toBe(true)
    expect(p.routerLogicalNames).toContain('tbl_order')
  })
})
