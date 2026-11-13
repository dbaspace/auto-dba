import { describe, expect, it } from 'vitest'
import { dbBoundDomains, dbHasDomain, filterDbsByDomain, pickRegisteredExportDomain, registeredDbDomains } from './instanceSource'

describe('dbHasDomain / filterDbsByDomain', () => {
  it('empty domains matches any selected domain', () => {
    const db = { db_name: 'a', domains: [] }
    expect(dbHasDomain(db, 'pg.pay.testsuite.cn')).toBe(true)
    expect(filterDbsByDomain([db], 'pg.pay.testsuite.cn')).toHaveLength(1)
  })

  it('missing domains field matches', () => {
    expect(dbHasDomain({ db_name: 'a' }, 'x.example.com')).toBe(true)
  })

  it('bound domain must hit', () => {
    const db = { db_name: 'a', domains: ['pg.pay.testsuite.cn'] }
    expect(dbHasDomain(db, 'pg.pay.testsuite.cn')).toBe(true)
    expect(dbHasDomain(db, 'other.example.com')).toBe(false)
    expect(filterDbsByDomain([db], 'other.example.com')).toHaveLength(0)
  })

  it('domain= tag counts as binding', () => {
    const db = { db_name: 'a', domains: [], tags: ['domain=alias.example.com'] }
    expect(dbBoundDomains(db)).toEqual(['alias.example.com'])
    expect(dbHasDomain(db, 'alias.example.com')).toBe(true)
    expect(dbHasDomain(db, 'other.example.com')).toBe(false)
  })

  it('registered db with one domain is selected', () => {
    const db = { domains: ['pg.pay.testsuite.cn'], tags: [] }
    expect(registeredDbDomains(db, { domain_name: 'other.example.com' })).toEqual(['pg.pay.testsuite.cn'])
    expect(pickRegisteredExportDomain(registeredDbDomains(db), [])).toBe('pg.pay.testsuite.cn')
  })

  it('multiple registered domains prefer the access-domain option', () => {
    const domains = ['a.example.com', 'b.example.com']
    expect(pickRegisteredExportDomain(domains, ['b.example.com', 'c.example.com'])).toBe('b.example.com')
    expect(pickRegisteredExportDomain(domains, ['c.example.com'])).toBe('a.example.com')
  })

  it('registered db without a domain is not selected', () => {
    expect(registeredDbDomains({ domains: [], tags: [] }, { domain_name: '' })).toEqual([])
    expect(registeredDbDomains({ domains: [], tags: ['env=prod'] }, {})).toEqual([])
    expect(pickRegisteredExportDomain([], ['a.example.com'])).toBe('')
  })

  it('falls back to instance primary domain only when the db has none', () => {
    expect(registeredDbDomains({ domains: [], tags: [] }, { domain_name: 'primary.example.com' })).toEqual([
      'primary.example.com',
    ])
    expect(registeredDbDomains({ tags: ['domain=tag.example.com'] }, { domain_name: 'primary.example.com' })).toEqual([
      'tag.example.com',
    ])
  })

  it('keeps a domain the user already chose', () => {
    expect(pickRegisteredExportDomain(['a.example.com'], ['b.example.com'], 'kept.example.com')).toBe('kept.example.com')
  })

  it('empty want domain never matches', () => {
    expect(dbHasDomain({ domains: [] }, '')).toBe(false)
    expect(filterDbsByDomain([{ domains: [] }], '')).toHaveLength(0)
  })
})
