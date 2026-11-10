import { describe, expect, it } from 'vitest'
import { applySuggestion, buildSuggestions, extendsPrefix, quoteIdent, sqlContext, suggestAnchor } from './sqlComplete'

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

  it('lists columns after WHERE and a space, with comment in the label', () => {
    const sql = 'select * from ods_tbl_l18n_text where '
    const ctx = sqlContext(sql, sql.length)
    expect(ctx.kind).toBe('column')
    expect(ctx.tables.map((t) => t.name)).toEqual(['ods_tbl_l18n_text'])
    const items = buildSuggestions({
      ctx,
      tableNames: ['ods_tbl_l18n_text'],
      columns: {
        ods_tbl_l18n_text: [
          { name: 'id', data_type: 'bigint', comment: ' 主键 ' },
          { name: 'text', data_type: 'text', nullable: true, comment: '   ' },
          { name: 'lang', data_type: 'varchar' },
        ],
      },
      dialect: 'mysql',
    })
    const id = items.find((item) => item.insert === 'id')
    const text = items.find((item) => item.insert === 'text')
    const lang = items.find((item) => item.insert === 'lang')
    expect(id?.label).toBe('id-主键')
    expect(text?.label).toBe('text')
    expect(lang?.label).toBe('lang')
    expect(items.filter((item) => item.insert === 'id').every((item) => !item.label.includes('bigint'))).toBe(true)
    expect(items.findIndex((item) => item.group === 'column')).toBeLessThan(items.findIndex((item) => item.group === 'keyword'))
    const byComment = buildSuggestions({
      ctx: { ...ctx, prefix: '主键' },
      tableNames: ['ods_tbl_l18n_text'],
      columns: { ods_tbl_l18n_text: [{ name: 'id', comment: '主键' }] },
      dialect: 'mysql',
    })
    expect(byComment.some((item) => item.insert === 'id')).toBe(false)
  })

  it('suggests tables after a database dot in FROM', () => {
    const sql = 'select * from db_ods.'
    const ctx = sqlContext(sql, sql.length)
    expect(ctx.kind).toBe('table')
    expect(ctx.prefix).toBe('')
    const got = labels(sql)
    expect(got).toContain('debezium_signal')
    expect(got).not.toContain('id')
    const typed = 'select * from db_ods.de'
    expect(sqlContext(typed, typed.length).kind).toBe('table')
    expect(sqlContext(typed, typed.length).prefix).toBe('de')
    expect(labels(typed)[0]).toBe('debezium_signal')
    const next = applySuggestion(typed, sqlContext(typed, typed.length), 'debezium_signal')
    expect(next.sql).toBe('select * from db_ods.debezium_signal')
  })

  it('keeps columns after an alias dot', () => {
    const sql = 'select id from t s where s.'
    expect(sqlContext(sql, sql.length).kind).toBe('qual')
    expect(labels(sql)).toContain('name')
  })

  it('stops when the column name is already complete', () => {
    const sql = 'select id from t where name'
    const ctx = sqlContext(sql, sql.length)
    const items = buildSuggestions({ ctx, tableNames: ['t'], columns: cols, dialect: 'mysql' })
    expect(extendsPrefix(items, ctx.prefix)).toBe(false)
    expect(extendsPrefix(items, 'n')).toBe(true)
  })

  it('opens only beside the current statement', () => {
    const done = 'select id from t where name ;'
    const semi = done.indexOf(';')
    expect(suggestAnchor(done, semi, sqlContext(done, semi))).toBe('semi')
    const one = 'select id from t ;'
    const oneSpace = one.indexOf(';') - 1
    expect(suggestAnchor(one, oneSpace, sqlContext(one, oneSpace))).toBe('semi')
    const spaced = 'select id from t  ;'
    const atSpace = spaced.indexOf(';') - 1
    expect(suggestAnchor(spaced, atSpace, sqlContext(spaced, atSpace))).toBe('semi')
    const blank = 'select id from t\n\n'
    expect(suggestAnchor(blank, blank.length, sqlContext(blank, blank.length))).toBe('none')
    const after = 'select id from t;\n'
    expect(suggestAnchor(after, after.length, sqlContext(after, after.length))).toBe('none')
    const mid = 'select * from t where  id = 1'
    const gap = mid.indexOf('  ') + 1
    expect(suggestAnchor(mid, gap + 1, sqlContext(mid, gap + 1))).toBe('none')
  })

  it('replaces the typed prefix', () => {
    const sql = 'select * from de'
    const ctx = sqlContext(sql, sql.length)
    const next = applySuggestion(sql, ctx, 'debezium_signal')
    expect(next.sql).toBe('select * from debezium_signal')
  })
})
