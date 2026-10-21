import { describe, expect, it } from 'vitest'
import { conclusionFromSteps, parseRichBlocks, streamFailureText } from './richText'

describe('parseRichBlocks', () => {
  it('renders pipe rows as a table and keeps GB text', () => {
    const text = [
      '现象：容量最高的是 db_marketing，占 68.0%',
      '数据依据：',
      '| 库名 | 容量 | 占比 |',
      '| --- | --- | --- |',
      '| db_marketing | 1190.61 GB | 68.0% |',
      '| 其他 | 560.12 GB | 32.0% |',
      '预判：大库占满',
    ].join('\n')
    const blocks = parseRichBlocks(text)
    const table = blocks.find((b) => b.type === 'table')
    expect(table && table.type === 'table' && table.columns).toEqual(['库名', '容量', '占比'])
    expect(table && table.type === 'table' && table.rows[0][1]).toBe('1190.61 GB')
    expect(table && table.type === 'table' && table.rows[1][0]).toBe('其他')
    expect(text.includes('e+')).toBe(false)
  })
})

describe('streamFailureText', () => {
  it('prefers judge text over abort', () => {
    const steps = [{ action: 'judge', status: 'ok', output: '现象：容量最高' }]
    expect(streamFailureText('signal is aborted without reason', steps)).toBe('现象：容量最高')
    expect(streamFailureText('回答生成超时', steps)).toBe('现象：容量最高')
    expect(streamFailureText('已停止', [])).toBe('已停止')
    expect(conclusionFromSteps([{ action: 'analyze', status: 'ok', output: '已分析' }])).toBe('已分析')
  })
})
