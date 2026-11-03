import { describe, expect, it } from 'vitest'
import { effectiveSourceEndpoint, sourceEndpointLabel } from './cdc'

const inst = { name: 'ai-sea-pg', host: 'pg.member.example', port: 5432 }

describe('effectiveSourceEndpoint', () => {
  it('prefers task source_host', () => {
    const extra = { source_host: 'pg.bill.example', source_port: 5433, debezium_config: '{"database.hostname":"x","database.port":"1"}' }
    expect(effectiveSourceEndpoint(extra, inst)).toEqual({ host: 'pg.bill.example', port: 5433 })
  })

  it('falls back to debezium config, then instance', () => {
    expect(effectiveSourceEndpoint({ debezium_config: '{"database.hostname":"pg.dbz.example","database.port":"6432"}' }, inst)).toEqual({
      host: 'pg.dbz.example',
      port: 6432,
    })
    expect(effectiveSourceEndpoint({ debezium_config: { 'database.hostname': 'pg.obj.example' } }, inst)).toEqual({ host: 'pg.obj.example', port: 5432 })
    expect(effectiveSourceEndpoint({}, inst)).toEqual({ host: 'pg.member.example', port: 5432 })
    expect(effectiveSourceEndpoint({ debezium_config: 'not json' }, inst)).toEqual({ host: 'pg.member.example', port: 5432 })
  })
})

describe('sourceEndpointLabel', () => {
  it('shows only the effective address', () => {
    const extra = { source_host: 'pg.bill.example', source_port: 5432 }
    expect(sourceEndpointLabel(inst, extra, true)).toBe('ai-sea-pg — pg.bill.example:5432')
    expect(sourceEndpointLabel(inst, extra)).toBe('pg.bill.example:5432')
    expect(sourceEndpointLabel(inst, {}, true)).toBe('ai-sea-pg — pg.member.example:5432')
    expect(sourceEndpointLabel(null, {})).toBe('—')
  })
})
