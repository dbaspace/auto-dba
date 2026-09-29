/** 流式计算 File 的 SHA-256（大文件分块，避免整文件入内存）。 */

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

function rotr(n: number, x: number) {
  return (x >>> n) | (x << (32 - n))
}

class Sha256 {
  private h = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ])
  private buf = new Uint8Array(64)
  private bufLen = 0
  private bytes = 0

  update(data: Uint8Array) {
    let off = 0
    this.bytes += data.length
    while (off < data.length) {
      const n = Math.min(64 - this.bufLen, data.length - off)
      this.buf.set(data.subarray(off, off + n), this.bufLen)
      this.bufLen += n
      off += n
      if (this.bufLen === 64) {
        this.compress(this.buf)
        this.bufLen = 0
      }
    }
  }

  digestHex(): string {
    const bitHi = Math.floor(this.bytes / 0x20000000) >>> 0
    const bitLo = (this.bytes * 8) >>> 0
    // 0x80 + 零填充至 56，再写 8 字节长度
    this.buf[this.bufLen++] = 0x80
    if (this.bufLen > 56) {
      while (this.bufLen < 64) this.buf[this.bufLen++] = 0
      this.compress(this.buf)
      this.bufLen = 0
    }
    while (this.bufLen < 56) this.buf[this.bufLen++] = 0
    const view = new DataView(this.buf.buffer)
    view.setUint32(56, bitHi, false)
    view.setUint32(60, bitLo, false)
    this.compress(this.buf)
    let out = ''
    for (let i = 0; i < 8; i++) out += this.h[i].toString(16).padStart(8, '0')
    return out
  }

  private compress(block: Uint8Array) {
    const w = new Uint32Array(64)
    const view = new DataView(block.buffer, block.byteOffset, 64)
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(i * 4, false)
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(7, w[i - 15]) ^ rotr(18, w[i - 15]) ^ (w[i - 15] >>> 3)
      const s1 = rotr(17, w[i - 2]) ^ rotr(19, w[i - 2]) ^ (w[i - 2] >>> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0
    }
    let a = this.h[0]
    let b = this.h[1]
    let c = this.h[2]
    let d = this.h[3]
    let e = this.h[4]
    let f = this.h[5]
    let g = this.h[6]
    let hh = this.h[7]
    for (let i = 0; i < 64; i++) {
      const S1 = rotr(6, e) ^ rotr(11, e) ^ rotr(25, e)
      const ch = (e & f) ^ (~e & g)
      const t1 = (hh + S1 + ch + K[i] + w[i]) >>> 0
      const S0 = rotr(2, a) ^ rotr(13, a) ^ rotr(22, a)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) >>> 0
      hh = g
      g = f
      f = e
      e = (d + t1) >>> 0
      d = c
      c = b
      b = a
      a = (t1 + t2) >>> 0
    }
    this.h[0] = (this.h[0] + a) >>> 0
    this.h[1] = (this.h[1] + b) >>> 0
    this.h[2] = (this.h[2] + c) >>> 0
    this.h[3] = (this.h[3] + d) >>> 0
    this.h[4] = (this.h[4] + e) >>> 0
    this.h[5] = (this.h[5] + f) >>> 0
    this.h[6] = (this.h[6] + g) >>> 0
    this.h[7] = (this.h[7] + hh) >>> 0
  }
}

const READ_CHUNK = 4 * 1024 * 1024

/** 计算文件 SHA-256 十六进制小写；onProgress 为 0–1。 */
export async function sha256File(file: File, onProgress?: (p: number) => void): Promise<string> {
  const hasher = new Sha256()
  let offset = 0
  while (offset < file.size) {
    const end = Math.min(offset + READ_CHUNK, file.size)
    const buf = await file.slice(offset, end).arrayBuffer()
    hasher.update(new Uint8Array(buf))
    offset = end
    onProgress?.(file.size ? offset / file.size : 1)
  }
  return hasher.digestHex()
}
