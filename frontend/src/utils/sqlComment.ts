export type SqlCommentMode = 'comment' | 'uncomment'

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n))
}

function lineStart(sql: string, index: number): number {
  const i = sql.lastIndexOf('\n', Math.max(0, index - 1))
  return i < 0 ? 0 : i + 1
}

function commentLine(line: string): string {
  const m = /^(\s*)(.*)$/.exec(line)
  if (!m || !m[2]) return line
  return `${m[1]}-- ${m[2]}`
}

function uncommentLine(line: string): string {
  const m = /^(\s*)-- ?(.*)$/.exec(line)
  if (!m) return line
  return m[1] + m[2]
}

/** 对选区碰到的每一行加或去掉 `-- `。选区结束在下一行行首时不包含那一行。 */
export function applySqlLineComment(
  sql: string,
  selStart: number,
  selEnd: number,
  mode: SqlCommentMode,
): { text: string; start: number; end: number } {
  const len = sql.length
  const a = clamp(Math.min(selStart, selEnd), 0, len)
  const b = clamp(Math.max(selStart, selEnd), 0, len)
  const from = lineStart(sql, a)
  let probe = b
  if (b > a && (b === 0 || sql[b - 1] === '\n')) probe = b - 1
  const to = lineStart(sql, clamp(Math.max(a, probe), 0, len))

  const lines: string[] = []
  let i = from
  let consumed = 0
  while (i <= to && i <= len) {
    const nl = sql.indexOf('\n', i)
    const lineEnd = nl < 0 ? len : nl
    const line = sql.slice(i, lineEnd)
    lines.push(mode === 'comment' ? commentLine(line) : uncommentLine(line))
    consumed = lineEnd - from
    if (nl < 0 || i === to) break
    i = nl + 1
  }
  const middle = lines.join('\n')
  const text = sql.slice(0, from) + middle + sql.slice(from + consumed)
  return { text, start: from, end: from + middle.length }
}
