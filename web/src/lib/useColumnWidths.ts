import { useEffect, useLayoutEffect, useRef, useState } from 'react'

const STORAGE_PREFIX = 'houfeng.table-col-widths.'
const MIN_COLUMN_WIDTH = 32

function readStored(tableId: string, expectedLength: number): number[] | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.localStorage.getItem(STORAGE_PREFIX + tableId)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed) || parsed.length !== expectedLength) return null
    if (!parsed.every((value) => typeof value === 'number' && Number.isFinite(value) && value >= MIN_COLUMN_WIDTH)) {
      return null
    }
    return parsed
  } catch {
    return null
  }
}

function writeStored(tableId: string, widths: number[]) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_PREFIX + tableId, JSON.stringify(widths))
  } catch {
    // quota or private mode — widths still work for the session
  }
}

export function useColumnWidths(tableId: string, defaults: readonly number[]) {
  const defaultsRef = useRef(defaults)
  useLayoutEffect(() => {
    defaultsRef.current = defaults
  }, [defaults])
  const [widths, setWidths] = useState<number[]>(() => readStored(tableId, defaults.length) ?? [...defaults])
  const dragRef = useRef<{ index: number; startX: number; startWidth: number } | null>(null)

  useEffect(() => {
    const expected = defaultsRef.current
    if (widths.length !== expected.length) {
      setWidths(readStored(tableId, expected.length) ?? [...expected])
    }
  }, [tableId, widths.length])

  useEffect(() => {
    writeStored(tableId, widths)
  }, [tableId, widths])

  useEffect(() => {
    function onMove(event: PointerEvent) {
      const drag = dragRef.current
      if (!drag) return
      const next = Math.max(MIN_COLUMN_WIDTH, Math.round(drag.startWidth + (event.clientX - drag.startX)))
      setWidths((current) => {
        if (current[drag.index] === next) return current
        const copy = current.slice()
        copy[drag.index] = next
        return copy
      })
    }
    function onUp() {
      if (!dragRef.current) return
      dragRef.current = null
      document.body.classList.remove('is-col-resizing')
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    return () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
    }
  }, [])

  function startResize(index: number, clientX: number) {
    const startWidth = widths[index] ?? defaultsRef.current[index] ?? MIN_COLUMN_WIDTH
    dragRef.current = { index, startX: clientX, startWidth }
    document.body.classList.add('is-col-resizing')
  }

  return { widths, startResize }
}
