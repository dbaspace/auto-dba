import { describe, expect, it } from 'vitest'
import { newClientRequestID } from './clientRequestId'

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

describe('newClientRequestID', () => {
  it('uses randomUUID when it returns a valid id', () => {
    const id = newClientRequestID({
      randomUUID: () => 'abc-1234',
      getRandomValues: () => {
        throw new Error('unused')
      },
    })
    expect(id).toBe('abc-1234')
  })

  it('builds a uuid from getRandomValues when randomUUID is missing', () => {
    const id = newClientRequestID({
      getRandomValues: (bytes) => {
        bytes.fill(0)
        return bytes
      },
    })
    expect(id).toBe('00000000-0000-4000-8000-000000000000')
    expect(id).toMatch(uuid)
  })

  it('still returns a valid id when randomUUID throws', () => {
    const id = newClientRequestID({
      randomUUID: () => {
        throw new TypeError('crypto.randomUUID is not a function')
      },
      getRandomValues: (bytes) => {
        bytes.fill(1)
        return bytes
      },
    })
    expect(id).toMatch(uuid)
    expect(id.length).toBeGreaterThanOrEqual(8)
    expect(id.length).toBeLessThanOrEqual(64)
  })
})
