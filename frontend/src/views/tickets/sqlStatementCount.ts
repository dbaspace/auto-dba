/** 非空文本行数（用于与 SplitSQL 条数对比提示，非执行口径） */
export function countNonEmptyLines(sql: string): number {
  return String(sql || '')
    .split(/\r?\n/)
    .filter((l) => l.trim().length > 0).length
}

/** 行数远大于语句数时提示用户口径差异 */
export function shouldHintLineVsStatement(lineApprox: number, statementCount: number): boolean {
  const n = Number(statementCount) || 0
  const lines = Number(lineApprox) || 0
  return n > 0 && lines >= 2 * n && lines - n >= 20
}

export function lineVsStatementHint(lineApprox: number, statementCount: number): string {
  return `文本约 ${lineApprox} 行，系统识别为 ${statementCount} 条语句（按分号拆分）。多行 VALUES / 缺分号会被合并为一条。`
}
