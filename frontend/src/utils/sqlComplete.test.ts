import { describe, expect, it } from 'vitest'
import { applySuggestion, buildSuggestions, quoteIdent, sqlContext } from './sqlComplete'

const cols = {
  debezium_signal: [
    { name: 'id', data_type: 'varchar', nullable: false, comment: '主键' },
    { name: 'data', data_type: 'text', nullable: true },
  ],
  t: [
    { name: 'id', data_type: 'int' },
    { name: 'name', data_type: 'text' },
  ],
  u: [
    { name: 'id', data_type: 'int' },
    { name: 'title', data_type: 'text' },
  ],
}

function labels(sql: string, cursor = sql.length) {
  const ctx = sqlContext(sql, cursor)
  return buildSuggestions({ ctx, tableNames: ['debezium_signal', 'orders', 't', 'u'], columns: cols, dialect: 'mysql' }).map((item) => item.insert)
}

describe('sqlContext', () => {
  it('suggests tables after FROM', () => {
    const sql = 'select * from de'
    const got = labels(sql)
    expect(got[0]).toBe('debezium_signal')
    expect(got.some((item) => item === 'WHERE')).toBe(false)
  })

  it('suggests columns in SELECT before FROM', () => {
    const sql = 'SELECT i FROM debezium_signal LIMIT 50'
    const cursor = sql.indexOf('i ')
    const got = labels(sql, cursor + 1)
    expect(got).toContain('id')
    expect(got).not.toContain('debezium_signal')
  })

  it('suggests columns after WHERE and keeps tables after the cursor', () => {
    const sql = 'select id from debezium_signal where d'
    expect(labels(sql)).toContain('data')
  })

  it('suggests qualified columns', () => {
    const sql = 'select id from t s where s.n'
    expect(labels(sql)).toContain('name')
    expect(labels(sql).some((item) => item.endsWith('('))).toBe(false)
  })

  it('offers an equality snippet on JOIN ON', () => {
    const sql = 'select * from t join u on '
    const got = labels(sql)
    expect(got).toContain('t.id = u.id')
  })

  it('ignores text inside strings', () => {
    const sql = "select id from t where name = 'abc"
    expect(sqlContext(sql, sql.length).kind).toBe('none')
  })

  it('uses the current statement only', () => {
    const sql = 'select id from t; select * from u where '
    const ctx = sqlContext(sql, sql.length)
    expect(ctx.tables.map((t) => t.name)).toEqual(['u'])
  })

  it('uses the inner select', () => {
    const sql = 'select * from t where id in (select ti from u)'
    const cursor = sql.indexOf('ti')
    const ctx = sqlContext(sql, cursor + 2)
    expect(ctx.tables.map((t) => t.name)).toEqual(['u'])
    expect(labels(sql, cursor + 2)).toContain('title')
  })

  it('quotes keywords and special characters', () => {
    expect(quoteIdent('order', 'mysql')).toBe('`order`')
    expect(quoteIdent('a-b', 'pg')).toBe('"a-b"')
    expect(quoteIdent('id', 'mysql')).toBe('id')
  })

  it('ignores the outer query while a subquery is still open', () => {
    const sql = 'select * from t where id in (select ti from u'
    const cursor = sql.indexOf('ti') + 2
    expect(sqlContext(sql, cursor).tables.map((t) => t.name)).toEqual(['u'])
    expect(labels(sql, cursor)).toContain('title')
  })

  it('suggests the next table after a comma', () => {
    const sql = 'select * from t, or'
    expect(labels(sql)).toContain('orders')
  })

  it('suggests columns after ORDER BY', () => {
    const sql = 'select * from t order by n'
    expect(labels(sql)).toContain('name')
  })

  it('replaces the typed prefix', () => {
    const sql = 'select * from de'
    const ctx = sqlContext(sql, sql.length)
    const next = applySuggestion(sql, ctx, 'debezium_signal')
    expect(next.sql).toBe('select * from debezium_signal')
  })
})
