import { format } from 'sql-formatter'

export type FormatSqlResult = {
  ok: boolean
  text: string
  unchanged?: boolean
  error?: string
}

/** Format SQL with PostgreSQL dialect; preserves comments and multi-statements. */
export function formatSqlContent(sql: string): FormatSqlResult {
  const input = String(sql || '').trim()
  if (!input) {
    return { ok: false, text: '', error: '请填写 SQL' }
  }
  try {
    const text = format(input, {
      language: 'postgresql',
      tabWidth: 2,
      keywordCase: 'upper',
    }).trim()
    if (text === input) {
      return { ok: true, text, unchanged: true }
    }
    return { ok: true, text }
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    return { ok: false, text: input, error: msg || 'SQL 美化失败' }
  }
}
