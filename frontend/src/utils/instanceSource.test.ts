import { describe, expect, it } from 'vitest'
import { dbBoundDomains, dbHasDomain, filterDbsByDomain } from './instanceSource'

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

  it('empty want domain never matches', () => {
    expect(dbHasDomain({ domains: [] }, '')).toBe(false)
    expect(filterDbsByDomain([{ domains: [] }], '')).toHaveLength(0)
  })
})
