import React, { useCallback, useLayoutEffect, useRef, useState } from 'react'

const OVERSCAN = 6

function MeasuredRow({
  top,
  onHeight,
  children,
}: {
  top: number
  onHeight: (h: number) => void
  children: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const report = () => {
      const h = el.offsetHeight
      if (h > 0) onHeight(h)
    }
    report()
    const ro = new ResizeObserver(report)
    ro.observe(el)
    return () => ro.disconnect()
  }, [onHeight])
  return (
    <div ref={ref} style={{ position: 'absolute', top, left: 0, right: 0 }}>
      {children}
    </div>
  )
}

export function VirtualList<T>({
  items,
  itemKey,
  estimate = 40,
  height = 420,
  className,
  renderItem,
}: {
  items: T[]
  itemKey: (item: T, index: number) => string
  estimate?: number
  height?: number
  className?: string
  renderItem: (item: T, index: number) => React.ReactNode
}) {
  const heights = useRef(new Map<string, number>())
  const [scrollTop, setScrollTop] = useState(0)
  const [tick, setTick] = useState(0)
  const report = useCallback((key: string, h: number) => {
    const prev = heights.current.get(key)
    if (prev != null && Math.abs(prev - h) < 2) return
    heights.current.set(key, h)
    setTick((n) => n + 1)
  }, [])

  const offsets: number[] = new Array(items.length)
  let total = 0
  for (let i = 0; i < items.length; i++) {
    offsets[i] = total
    total += heights.current.get(itemKey(items[i], i)) || estimate
  }
  void tick

  const viewBottom = scrollTop + height
  let start = 0
  while (start < items.length) {
    const h = heights.current.get(itemKey(items[start], start)) || estimate
    if (offsets[start] + h > scrollTop) break
    start++
  }
  start = Math.max(0, start - OVERSCAN)
  let end = start
  while (end < items.length && offsets[end] < viewBottom) end++
  end = Math.min(items.length, end + OVERSCAN)

  return (
    <div
      className={className}
      style={{ height, maxHeight: height, overflow: 'auto', position: 'relative' }}
      onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
    >
      <div style={{ height: total, position: 'relative' }}>
        {items.slice(start, end).map((item, i) => {
          const index = start + i
          const key = itemKey(item, index)
          return (
            <MeasuredRow key={key} top={offsets[index]} onHeight={(h) => report(key, h)}>
              {renderItem(item, index)}
            </MeasuredRow>
          )
        })}
      </div>
    </div>
  )
}
