import { describe, expect, it } from 'vitest'
import { buildExportContent, formatJsonKeepBigint, quoteUnsafeIntegers } from './exportResult'

describe('quoteUnsafeIntegers', () => {
  it('keeps a bigint order id inside json text', () => {
    const raw = '{"order_id":211725322743368812,"msg":"id 211725322743368812"}'
    const parsed = JSON.parse(quoteUnsafeIntegers(raw))
    expect(parsed.order_id).toBe('211725322743368812')
    expect(parsed.msg).toBe('id 211725322743368812')
    const pretty = formatJsonKeepBigint(raw)
    expect(pretty).toContain('211725322743368812')
    expect(pretty).not.toContain('211725322743368800')
  })

  it('builds csv and sql from the current rows', () => {
    const csv = buildExportContent('csv', ['order_id'], [['211725322743368812']], 't')
    expect(csv).toContain('211725322743368812')
    const sql = buildExportContent('sql', ['order_id'], [['211725322743368812']], 't_order')
    expect(sql).toContain("'211725322743368812'")
    expect(sql).toContain('INSERT INTO')
  })
})
