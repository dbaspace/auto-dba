import { describe, expect, it } from 'vitest'
import {
  buildBatchAddTablePlan,
  nextAddTableParentIds,
  validateAddTableParents,
} from './cdc'

describe('nextAddTableParentIds', () => {
  it('accumulates across different source dbs', () => {
    expect(nextAddTableParentIds(['a'], 'b', true)).toEqual(['a', 'b'])
    expect(nextAddTableParentIds(['a', 'b'], 'c', true)).toEqual(['a', 'b', 'c'])
  })

  it('does not duplicate', () => {
    expect(nextAddTableParentIds(['a', 'b'], 'a', true)).toEqual(['a', 'b'])
  })

  it('removes on uncheck', () => {
    expect(nextAddTableParentIds(['a', 'b', 'c'], 'b', false)).toEqual(['a', 'c'])
  })
})

describe('validateAddTableParents', () => {
  it('requires at least one', () => {
    expect(validateAddTableParents([])).toContain('至少选择')
  })

  it('allows cross-instance rows', () => {
    const rows = [
      { id: '1', source_instance_id: 'inst-a' },
      { id: '2', source_instance_id: 'inst-b' },
    ]
    expect(validateAddTableParents(rows, () => ({ host: 'h', port: 1 }))).toBe('')
  })
})

describe('buildBatchAddTablePlan', () => {
  it('skips already synced tables per parent', () => {
    const plan = buildBatchAddTablePlan({
      parents: [
        { id: 'p1', name: 't1', source_db: 'db_0', synced_tables: ['orders'] },
        { id: 'p2', name: 't2', source_db: 'db_1', synced_tables: [] },
      ],
      tables: ['orders', 'items'],
      baseName: 'batch_add',
    })
    expect(plan).toHaveLength(2)
    expect(plan[0].tables_to_add).toEqual(['items'])
    expect(plan[0].skipped_tables).toEqual(['orders'])
    expect(plan[0].child_name).toBe('batch_add_t1')
    expect(plan[1].tables_to_add).toEqual(['orders', 'items'])
    expect(plan[1].skipped).toBe(false)
  })

  it('marks fully skipped parents', () => {
    const plan = buildBatchAddTablePlan({
      parents: [{ id: 'p1', name: 't1', synced_tables: ['a'] }],
      tables: ['a'],
    })
    expect(plan[0].skipped).toBe(true)
    expect(plan[0].tables_to_add).toEqual([])
  })

  it('exposes each parent pipeline name', () => {
    const plan = buildBatchAddTablePlan({
      parents: [
        {
          id: 'p7',
          name: 'ai_beijing_db_bill_7',
          source_db: 'db_bill_7',
          description: '-- auto-dba-cdc:{"publication_name":"ai_beijing_db_bill_7","connector_name":"ai_beijing_db_bill_7","slot_name":"slot_ai_beijing_db_bill_7"}',
        },
        {
          id: 'p29',
          name: 'ai_beijing_db_bill_29',
          source_db: 'db_bill_29',
          description: '-- auto-dba-cdc:{"publication_name":"ai_beijing_db_bill_29","connector_name":"ai_beijing_db_bill_29","slot_name":"slot_ai_beijing_db_bill_29"}',
        },
      ],
      tables: ['orders'],
    })
    expect(plan[0].publication_name).toBe('ai_beijing_db_bill_7')
    expect(plan[0].connector_name).toBe('ai_beijing_db_bill_7')
    expect(plan[1].publication_name).toBe('ai_beijing_db_bill_29')
    expect(plan[1].connector_name).toBe('ai_beijing_db_bill_29')
    expect(plan[1].slot_name).toBe('slot_ai_beijing_db_bill_29')
  })
})
