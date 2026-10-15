/** 类型 Tab：固定露出条数，其余进「…」；有数据的优先排前。 */
export const TYPE_TAB_VISIBLE = 6

export type TypeTabItem = { value: string; label: string }

export type TypeTabWithCount = TypeTabItem & { count: number }

/**
 * 按数量排序：count>0 在前（数量降序），其余保持原相对顺序；
 * 当前选中若不在前 maxVisible，则钉到可见区末位，避免选中项掉进「…」。
 */
export function splitTypeTabsByDataCount(
  tabs: TypeTabItem[],
  getCount: (value: string) => number,
  activeValue?: string,
  maxVisible = TYPE_TAB_VISIBLE,
): { visible: TypeTabWithCount[]; more: TypeTabWithCount[] } {
  const indexed = tabs.map((t, i) => ({
    ...t,
    count: Number(getCount(t.value)) || 0,
    _i: i,
  }))
  indexed.sort((a, b) => {
    const az = a.count > 0 ? 0 : 1
    const bz = b.count > 0 ? 0 : 1
    if (az !== bz) return az - bz
    if (b.count !== a.count) return b.count - a.count
    return a._i - b._i
  })

  let ordered = indexed
  const active = String(activeValue || '').trim()
  if (active) {
    const idx = ordered.findIndex((t) => t.value === active)
    if (idx >= maxVisible) {
      const copy = [...ordered]
      const [act] = copy.splice(idx, 1)
      copy.splice(Math.max(0, maxVisible - 1), 0, act)
      ordered = copy
    }
  }

  const strip = ({ _i, ...rest }: (typeof ordered)[number]): TypeTabWithCount => rest
  return {
    visible: ordered.slice(0, maxVisible).map(strip),
    more: ordered.slice(maxVisible).map(strip),
  }
}
