/** 与后端 rediscmd.SplitArgs / redis-cli 一致的参数拆分 */
export function splitRedisArgs(line: string): { parts: string[]; error?: string } {
  let p = 0
  const n = line.length
  const out: string[] = []

  const isSpace = (c: string) => c === ' ' || c === '\n' || c === '\r' || c === '\t'
  const isHex = (c: string) => /[0-9a-fA-F]/.test(c)
  const hexVal = (c: string) => {
    const code = c.charCodeAt(0)
    if (code >= 48 && code <= 57) return code - 48
    if (code >= 97 && code <= 102) return code - 97 + 10
    if (code >= 65 && code <= 70) return code - 65 + 10
    return 0
  }

  while (true) {
    while (p < n && isSpace(line[p])) p++
    if (p >= n) return { parts: out }

    let buf = ''
    let inq = false
    let insq = false
    let done = false

    while (!done) {
      if (p >= n) {
        if (inq || insq) return { parts: [], error: '引号未闭合' }
        done = true
        break
      }
      const c = line[p]

      if (inq) {
        if (c === '\\' && p + 1 < n) {
          if (line[p + 1] === 'x' && p + 3 < n && isHex(line[p + 2]) && isHex(line[p + 3])) {
            buf += String.fromCharCode((hexVal(line[p + 2]) << 4) | hexVal(line[p + 3]))
            p += 4
            continue
          }
          p++
          const esc = line[p]
          switch (esc) {
            case 'n':
              buf += '\n'
              break
            case 'r':
              buf += '\r'
              break
            case 't':
              buf += '\t'
              break
            case 'b':
              buf += '\b'
              break
            case 'a':
              buf += '\a'
              break
            default:
              buf += esc
          }
          p++
          continue
        }
        if (c === '"') {
          if (p + 1 < n && !isSpace(line[p + 1])) {
            return { parts: [], error: '闭合引号后不能紧跟非空白字符' }
          }
          done = true
          p++
          continue
        }
        buf += c
        p++
        continue
      }

      if (insq) {
        if (c === '\\' && p + 1 < n && (line[p + 1] === "'" || line[p + 1] === '\\')) {
          p++
          buf += line[p]
          p++
          continue
        }
        if (c === "'") {
          if (p + 1 < n && !isSpace(line[p + 1])) {
            return { parts: [], error: '闭合引号后不能紧跟非空白字符' }
          }
          done = true
          p++
          continue
        }
        buf += c
        p++
        continue
      }

      switch (c) {
        case ' ':
        case '\n':
        case '\r':
        case '\t':
          done = true
          break
        case '"':
          inq = true
          p++
          break
        case "'":
          insq = true
          p++
          break
        default:
          buf += c
          p++
          break
      }
    }

    out.push(buf)
  }
}
