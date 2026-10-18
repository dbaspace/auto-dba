import { describe, expect, it } from 'vitest'
import { instanceHasTag, resolveInstanceTagLabels, type MonitorInstance } from './dbMonitorApi'

function inst(partial: Partial<MonitorInstance>): MonitorInstance {
  return { id: '1', name: 'beijing-bill-01', host: '10.0.0.1', port: 5432, ...partial }
}

describe('resolveInstanceTagLabels', () => {
  it('prefers db=', () => {
    expect(resolveInstanceTagLabels(inst({
      tags: ['idc=beijing', 'db=beijing-shop', 'alias=shop'],
    }))).toEqual(['beijing-shop'])
  })

  it('falls back to alias/label when no db=', () => {
    expect(resolveInstanceTagLabels(inst({
      tags: ['idc=beijing', 'alias=bill-cluster', 'label=ignored-second'],
    }))).toEqual(['bill-cluster', 'ignored-second'])
  })

  it('falls back to instance name when no db/alias', () => {
    expect(resolveInstanceTagLabels(inst({
      name: 'beijing-bill-01',
      tags: ['idc=beijing', 'domain=pg.bill01.bj.restosuite.cn'],
    }))).toEqual(['beijing-bill-01'])
  })

  it('instanceHasTag matches resolved labels', () => {
    const it = inst({ tags: ['idc=beijing', 'domain=x'] })
    expect(instanceHasTag(it, 'beijing-bill-01')).toBe(true)
    expect(instanceHasTag(it, 'beijing-shop')).toBe(false)
  })
})
