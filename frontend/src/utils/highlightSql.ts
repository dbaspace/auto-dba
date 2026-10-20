/** Escape text for safe HTML highlight rendering. */
function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

const SQL_KEYWORDS = new Set(
  [
    'SELECT',
    'FROM',
    'WHERE',
    'AND',
    'OR',
    'NOT',
    'IN',
    'IS',
    'NULL',
    'LIKE',
    'ILIKE',
    'BETWEEN',
    'JOIN',
    'LEFT',
    'RIGHT',
    'INNER',
    'OUTER',
    'FULL',
    'CROSS',
    'ON',
    'AS',
    'USING',
    'GROUP',
    'BY',
    'ORDER',
    'HAVING',
    'LIMIT',
    'OFFSET',
    'UNION',
    'ALL',
    'DISTINCT',
    'INSERT',
    'INTO',
    'VALUES',
    'UPDATE',
    'SET',
    'DELETE',
    'REPLACE',
    'CREATE',
    'ALTER',
    'DROP',
    'TRUNCATE',
    'TABLE',
    'DATABASE',
    'INDEX',
    'VIEW',
    'SCHEMA',
    'COLUMN',
    'ADD',
    'RENAME',
    'TO',
    'COMMENT',
    'PRIMARY',
    'KEY',
    'FOREIGN',
    'REFERENCES',
    'CONSTRAINT',
    'DEFAULT',
    'UNIQUE',
    'CHECK',
    'CASCADE',
    'RESTRICT',
    'GRANT',
    'REVOKE',
    'WITH',
    'RECURSIVE',
    'CASE',
    'WHEN',
    'THEN',
    'ELSE',
    'END',
    'EXISTS',
    'IF',
    'RETURNING',
    'TRUE',
    'FALSE',
    'INTERVAL',
    'ASC',
    'DESC',
    'OVER',
    'PARTITION',
    'WINDOW',
    'EXPLAIN',
    'ANALYZE',
    'SHOW',
    'BEGIN',
    'COMMIT',
    'ROLLBACK',
    'TRANSACTION',
    'COPY',
    'VACUUM',
    'ANALYZE',
    'CONFLICT',
    'DO',
    'NOTHING',
    'RETURNING',
  ].map((k) => k.toUpperCase()),
)

/**
 * Tokenize SQL into highlighted HTML (keywords / strings / comments / numbers).
 * Keeps character positions 1:1 with input (except HTML escaping of special chars).
 */
export function highlightSql(sql: string): string {
  if (!sql) return ''
  const src = sql
  let i = 0
  let out = ''

  const push = (cls: string | null, text: string) => {
    const esc = escapeHtml(text)
    out += cls ? `<span class="${cls}">${esc}</span>` : esc
  }

  while (i < src.length) {
    const ch = src[i]

    // line comment --
    if (ch === '-' && src[i + 1] === '-') {
      let j = i + 2
      while (j < src.length && src[j] !== '\n') j++
      push('sql-cmt', src.slice(i, j))
      i = j
      continue
    }

    // block comment /* */
    if (ch === '/' && src[i + 1] === '*') {
      let j = i + 2
      while (j < src.length && !(src[j] === '*' && src[j + 1] === '/')) j++
      j = Math.min(j + 2, src.length)
      push('sql-cmt', src.slice(i, j))
      i = j
      continue
    }

    // single-quoted string ('' escape)
    if (ch === "'") {
      let j = i + 1
      while (j < src.length) {
        if (src[j] === "'" && src[j + 1] === "'") {
          j += 2
          continue
        }
        if (src[j] === "'") {
          j++
          break
        }
        j++
      }
      push('sql-str', src.slice(i, j))
      i = j
      continue
    }

    // double-quoted identifier
    if (ch === '"') {
      let j = i + 1
      while (j < src.length) {
        if (src[j] === '"' && src[j + 1] === '"') {
          j += 2
          continue
        }
        if (src[j] === '"') {
          j++
          break
        }
        j++
      }
      push('sql-id', src.slice(i, j))
      i = j
      continue
    }

    // number
    if (/[0-9]/.test(ch) && (i === 0 || !/[A-Za-z_]/.test(src[i - 1]))) {
      let j = i
      while (j < src.length && /[0-9.]/.test(src[j])) j++
      push('sql-num', src.slice(i, j))
      i = j
      continue
    }

    // word / keyword
    if (/[A-Za-z_]/.test(ch)) {
      let j = i + 1
      while (j < src.length && /[A-Za-z0-9_]/.test(src[j])) j++
      const word = src.slice(i, j)
      if (SQL_KEYWORDS.has(word.toUpperCase())) {
        push('sql-kw', word)
      } else {
        push(null, word)
      }
      i = j
      continue
    }

    push(null, ch)
    i++
  }

  return out
}
