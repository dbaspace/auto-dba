import { describe, expect, it } from 'vitest'
import { applySqlLineComment } from './sqlComment'

describe('applySqlLineComment', () => {
  it('comments the current line when nothing is selected', () => {
    const sql = 'SELECT 1'
    const got = applySqlLineComment(sql, 3, 3, 'comment')
    expect(got.text).toBe('-- SELECT 1')
    expect(got).toMatchObject({ start: 0, end: got.text.length })
  })

  it('comments every touched line and keeps indent', () => {
    const sql = '  SELECT a\n\n    FROM t'
    const got = applySqlLineComment(sql, 0, sql.length, 'comment')
    expect(got.text).toBe('  -- SELECT a\n\n    -- FROM t')
  })

  it('does not include the next line when the selection ends at its start', () => {
    const sql = 'SELECT a\nSELECT b\nSELECT c'
    const end = sql.indexOf('SELECT c')
    const got = applySqlLineComment(sql, 0, end, 'comment')
    expect(got.text).toBe('-- SELECT a\n-- SELECT b\nSELECT c')
  })

  it('adds another comment layer on an already commented line', () => {
    const sql = '-- SELECT 1'
    const got = applySqlLineComment(sql, 0, sql.length, 'comment')
    expect(got.text).toBe('-- -- SELECT 1')
  })

  it('removes one comment marker and a single following space', () => {
    const sql = '  -- SELECT 1\nSELECT 2\n    --  SELECT 3'
    const once = applySqlLineComment(sql, 0, sql.length, 'uncomment')
    expect(once.text).toBe('  SELECT 1\nSELECT 2\n     SELECT 3')
    const twice = applySqlLineComment('-- -- SELECT 1', 0, 14, 'uncomment')
    expect(twice.text).toBe('-- SELECT 1')
  })
})