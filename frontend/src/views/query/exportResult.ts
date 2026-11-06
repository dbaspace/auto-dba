const JS_MAX_SAFE = '9007199254740991'

/** 把超出 JavaScript 安全整数的整数字面量改成字符串，避免 JSON.parse 改掉位数。 */
export function quoteUnsafeIntegers(input: string): string {
  let out = ''
  let i = 0
  let inStr = false
  let esc = false
  while (i < input.length) {
    const c = input[i]
    if (inStr) {
      out += c
      if (esc) esc = false
      else if (c === '\\') esc = true
      else if (c === '"') inStr = false
      i++
      continue
    }
    if (c === '"') {
      inStr = true
      out += c
      i++
      continue
    }
    if (c === '-' || (c >= '0' && c <= '9')) {
      const start = i
      if (c === '-') i++
      const numStart = i
      while (i < input.length && input[i] >= '0' && input[i] <= '9') i++
      const next = input[i]
      if (next === '.' || next === 'e' || next === 'E') {
        out += input.slice(start, i)
        continue
      }
      const digits = input.slice(numStart, i).replace(/^0+(?=\d)/, '')
      if (digits && integerExceeds(digits, JS_MAX_SAFE)) {
        out += `"${input.slice(start, i)}"`
      } else {
        out += input.slice(start, i)
      }
      continue
    }
    out += c
    i++
  }
  return out
}

function integerExceeds(digits: string, limit: string): boolean {
  if (digits.length !== limit.length) return digits.length > limit.length
  return digits > limit
}

/** 内容是 JSON 对象或数组时返回格式化文本，大整数保持原位数。 */
export function formatJsonKeepBigint(text: string): string | null {
  const trimmed = String(text ?? '').trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return null
  try {
    const parsed = JSON.parse(quoteUnsafeIntegers(trimmed))
    if (!parsed || typeof parsed !== 'object') return null
    return JSON.stringify(parsed, null, 2)
  } catch {
    return null
  }
}

export function tableNameFromSQL(sql: string): string {
  const m = String(sql || '').match(/\bfrom\s+([`"'[\]]?[\w.]+)/i)
  const name = (m?.[1] || 'export_result').replace(/[`"'[\]]/g, '')
  return name || 'export_result'
}

function csvCell(v: unknown): string {
  if (v === null || v === undefined) return ''
  const str = String(v)
  if (/[",\n\r]/.test(str)) return `"${str.replace(/"/g, '""')}"`
  return str
}

function sqlIdent(name: string): string {
  return `"${String(name).replace(/"/g, '""')}"`
}

function sqlLiteral(v: unknown): string {
  if (v === null || v === undefined) return 'NULL'
  if (typeof v === 'number' && Number.isFinite(v)) return String(v)
  if (typeof v === 'boolean') return v ? 'TRUE' : 'FALSE'
  return `'${String(v).replace(/'/g, "''")}'`
}

export function buildExportContent(
  format: 'csv' | 'json' | 'sql',
  columns: string[],
  rows: unknown[][],
  table: string,
): string {
  if (format === 'json') {
    const list = rows.map((row) => {
      const rec: Record<string, unknown> = {}
      columns.forEach((col, i) => { rec[col] = i < row.length ? row[i] : null })
      return rec
    })
    return JSON.stringify(list, null, 2)
  }
  if (format === 'sql') {
    const cols = columns.map(sqlIdent).join(',')
    let sql = 'BEGIN;\n'
    for (const row of rows) {
      const vals = columns.map((_, i) => sqlLiteral(i < row.length ? row[i] : null)).join(',')
      sql += `INSERT INTO ${sqlIdent(table)} (${cols}) VALUES (${vals});\n`
    }
    sql += 'COMMIT;\n'
    return sql
  }
  const header = columns.map(csvCell).join(',')
  const body = rows.map((row) => columns.map((_, i) => csvCell(i < row.length ? row[i] : null)).join(',')).join('\n')
  return body ? `${header}\n${body}` : header
}

export function downloadTextFile(filename: string, content: string) {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = filename
  link.click()
  URL.revokeObjectURL(link.href)
}

export async function downloadPasswordZip(filename: string, entryName: string, content: string, password: string) {
  const zip = await import('@zip.js/zip.js')
  const writer = new zip.BlobWriter()
  const zw = new zip.ZipWriter(writer, { password, level: 9 })
  await zw.add(entryName, new zip.TextReader(content))
  const blob = await zw.close()
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = filename
  link.click()
  URL.revokeObjectURL(link.href)
}
