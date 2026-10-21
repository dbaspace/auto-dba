export type RichBlock =
  | { type: 'text'; text: string }
  | { type: 'table'; columns: string[]; rows: string[][] }

export type StepLike = { action?: string; status?: string; output?: string }

function splitRow(line: string): string[] {
  return line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map((s) => s.trim())
}

function isSep(cells: string[]): boolean {
  return cells.length > 0 && cells.every((c) => /^:?-{3,}:?$/.test(c))
}

/** 连续以 | 开头的行收成表格，其余保持原文。 */
export function parseRichBlocks(text: string): RichBlock[] {
  const lines = (text || '').split('\n')
  const blocks: RichBlock[] = []
  let buf: string[] = []
  const flush = () => {
    if (!buf.length) return
    const textBlock = buf.join('\n')
    if (textBlock.trim()) blocks.push({ type: 'text', text: textBlock })
    buf = []
  }
  let i = 0
  while (i < lines.length) {
    if (lines[i].trim().startsWith('|')) {
      const tableLines: string[] = []
      while (i < lines.length && lines[i].trim().startsWith('|')) {
        tableLines.push(lines[i])
        i++
      }
      flush()
      const parsed = tableLines.map(splitRow).filter((cells) => cells.some((c) => c !== ''))
      const body = parsed.filter((cells) => !isSep(cells))
      if (body.length) {
        const columns = body[0]
        const rows = body.slice(1).map((cells) => columns.map((_, ci) => cells[ci] ?? ''))
        blocks.push({ type: 'table', columns, rows })
      }
      continue
    }
    buf.push(lines[i])
    i++
  }
  flush()
  return blocks
}

/** judge 或 analyze 已有正文时，用它代替中止原文。 */
export function conclusionFromSteps(steps: StepLike[] | undefined): string {
  if (!steps) return ''
  for (let i = steps.length - 1; i >= 0; i--) {
    const s = steps[i]
    if ((s.action === 'judge' || s.action === 'analyze') && s.status === 'ok' && (s.output || '').trim()) {
      return (s.output || '').trim()
    }
  }
  return ''
}

export function streamFailureText(message: string, steps: StepLike[] | undefined): string {
  const msg = message || '助手调用失败'
  const aborted = msg === '已停止' || msg === '回答生成超时' || /abort/i.test(msg)
  if (!aborted) return msg
  const conclusion = conclusionFromSteps(steps)
  if (conclusion) return conclusion
  if (msg === '已停止') return '已停止'
  return '回答生成超时'
}
