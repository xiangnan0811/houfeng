export function eventPageRange(page: number, pageSize: number, total: number) {
  if (total <= 0 || pageSize <= 0) return { start: 0, end: 0 }
  const safePage = Math.max(1, page)
  const start = (safePage - 1) * pageSize + 1
  if (start > total) return { start: 0, end: 0 }
  return { start, end: Math.min(safePage * pageSize, total) }
}

export function visiblePageItems(current: number, total: number): Array<number | 'ellipsis'> {
  if (total < 1) return []
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1)

  const wanted = new Set([1, total, current, current - 1, current + 1])
  const pages = [...wanted].filter((page) => page >= 1 && page <= total).sort((left, right) => left - right)
  const items: Array<number | 'ellipsis'> = []
  for (const page of pages) {
    const last = items.at(-1)
    if (typeof last === 'number' && page - last > 1) items.push('ellipsis')
    items.push(page)
  }
  return items
}
