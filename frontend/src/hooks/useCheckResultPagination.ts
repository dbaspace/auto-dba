import { useCallback, useEffect, useMemo, useState } from 'react'
import type { TablePaginationConfig } from 'antd'

/** Estimate rows that fit below the SQL editor on order pages. */
function calcPageSize(reservePx = 400, rowPx = 40): number {
  const h = typeof window === 'undefined' ? 900 : window.innerHeight
  return Math.max(5, Math.min(50, Math.floor((h - reservePx) / rowPx)))
}

/**
 * SQL 检测结果分页：按视口高度估算每页条数；结果超出一页时显示分页。
 * `rowNo(index)` 返回跨页的全局序号（antd 的 index 仅为当前页内下标）。
 */
export function useCheckResultPagination(total: number, reservePx = 400) {
  const [pageSize, setPageSize] = useState(() => calcPageSize(reservePx))
  const [current, setCurrent] = useState(1)

  useEffect(() => {
    const onResize = () => setPageSize(calcPageSize(reservePx))
    onResize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [reservePx])

  useEffect(() => {
    setCurrent(1)
  }, [total])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil((total || 1) / pageSize))
    if (current > maxPage) setCurrent(maxPage)
  }, [pageSize, total, current])

  const pagination = useMemo<TablePaginationConfig | false>(() => {
    if (total <= 0 || total <= pageSize) return false
    return {
      current,
      pageSize,
      total,
      size: 'small',
      showSizeChanger: true,
      pageSizeOptions: ['5', '10', '20', '50'],
      showTotal: (t) => `共 ${t} 条`,
      onChange: (page, size) => {
        setCurrent(page)
        if (typeof size === 'number' && size > 0 && size !== pageSize) {
          setPageSize(size)
        }
      },
    }
  }, [total, pageSize, current])

  const rowNo = useCallback(
    (pageIndex: number) => (current - 1) * pageSize + pageIndex + 1,
    [current, pageSize],
  )

  return { pagination, rowNo, pageSize, current }
}
