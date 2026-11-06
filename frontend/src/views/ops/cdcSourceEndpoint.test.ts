import { describe, expect, it } from 'vitest'
import { cdcDomainOptionLabel, cdcInstLabel, cdcSourceDomainRows, effectiveSourceEndpoint, instVisibleInEnv, pickGrantedDomain, sourceEndpointLabel } from './cdc'
import { sourcesFromAccessDomains } from '../../utils/instanceSource'

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

describe('granted source instance', () => {
  const foreign = {
    id: 'inst-b',
    name: 'b-pg',
    host: '10.0.0.8',
    port: 5432,
    idc: 'other',
    db_type: 'postgres',
    granted_domains: [{ domain: 'pg.test.restosuite.ai', port: 5432, env_code: 'test' }],
  }
  const own = { id: 'inst-c', name: 'c-pg', host: '10.0.0.9', port: 5432, idc: 'test', db_type: 'postgres' }

  it('keeps an instance when its idc or granted domain matches the env', () => {
    expect(instVisibleInEnv(own, 'test')).toBe(true)
    expect(instVisibleInEnv(foreign, 'test')).toBe(true)
    expect(instVisibleInEnv(foreign, 'prod')).toBe(false)
    expect(instVisibleInEnv({ idc: '' }, 'test')).toBe(true)
  })

  it('labels and dials the granted domain for the selected env', () => {
    expect(cdcInstLabel(foreign, 'test')).toBe('b-pg — pg.test.restosuite.ai:5432')
    expect(cdcInstLabel(own, 'test')).toBe('c-pg — 10.0.0.9:5432')
    expect(pickGrantedDomain(foreign, 'test')?.domain).toBe('pg.test.restosuite.ai')
    expect(pickGrantedDomain(foreign, 'prod')).toBeUndefined()
    expect(pickGrantedDomain(own, 'test')).toBeUndefined()
  })
})

describe('cdc source domains', () => {
  it('keeps the selected env and engine, and labels domain before instance name', () => {
    const rows = [
      { instance_id: 'a', domain: 'pg.test.shop', env_code: 'test', db_type: 'postgres', instance_name: 'shop' },
      { instance_id: 'b', domain: '10.0.0.1', env_code: 'test', db_type: 'mysql', instance_name: 'my' },
      { instance_id: 'c', domain: 'pg.prod.shop', env_code: 'prod', db_type: 'postgres', instance_name: 'prod' },
      { instance_id: 'd', domain: '', env_code: 'test', db_type: 'postgres' },
    ]
    const kept = sourcesFromAccessDomains(cdcSourceDomainRows(rows, 'test', 'postgres'))
    expect(kept.map((r) => r.domain)).toEqual(['pg.test.shop'])
    expect(cdcDomainOptionLabel('pg.test.shop', 5432, 'shop')).toBe('pg.test.shop:5432 — shop')
    expect(cdcDomainOptionLabel('pg.test.shop', 0, '')).toBe('pg.test.shop')
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
