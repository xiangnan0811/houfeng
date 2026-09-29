import { useEffect, useState } from 'react'

// 元素内容是否真的横向溢出（scrollWidth > clientWidth）。ResizeObserver 在开始观察时会先回调一次，
// 之后容器或其直接子节点尺寸变化时重新测量；直接子节点被替换（如空表与有行表切换）时，
// MutationObserver 同步观察集合，取消观察已移除的节点。不支持 ResizeObserver 的环境视为不溢出。
export function useHorizontalOverflow(element: HTMLElement | null): boolean {
  const [overflowing, setOverflowing] = useState(false)
  useEffect(() => {
    if (!element || typeof ResizeObserver === 'undefined') return undefined
    const resize = new ResizeObserver(() => {
      setOverflowing(element.scrollWidth > element.clientWidth)
    })
    let observed = new Set<Element>()
    const syncChildren = () => {
      const current = new Set(Array.from(element.children))
      for (const child of observed) if (!current.has(child)) resize.unobserve(child)
      for (const child of current) if (!observed.has(child)) resize.observe(child)
      observed = current
    }
    resize.observe(element)
    syncChildren()
    const mutations = typeof MutationObserver === 'undefined' ? null : new MutationObserver(syncChildren)
    mutations?.observe(element, { childList: true })
    return () => {
      mutations?.disconnect()
      resize.disconnect()
    }
  }, [element])
  return overflowing
}
