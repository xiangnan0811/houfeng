import { useState, type ReactNode } from 'react'

import { useHorizontalOverflow } from '../../lib/useHorizontalOverflow'

type ScrollRegionProps = {
  labelledBy: string
  hintId: string
  hint: string
  className?: string
  hintClassName?: string
  children: ReactNode
}

// 宽表格的命名横向滚动区域：始终可聚焦以便键盘滚动；
// 只有内容真的溢出时才显示滚动提示并作为区域描述，桌面宽度放得下时不常驻说明。
export function ScrollRegion({ labelledBy, hintId, hint, className, hintClassName, children }: ScrollRegionProps) {
  const [element, setElement] = useState<HTMLDivElement | null>(null)
  const overflowing = useHorizontalOverflow(element)
  return (
    <>
      {overflowing ? <p id={hintId} className={hintClassName}>{hint}</p> : null}
      <div
        ref={setElement}
        className={className}
        role="region"
        aria-labelledby={labelledBy}
        aria-describedby={overflowing ? hintId : undefined}
        tabIndex={0}
      >
        {children}
      </div>
    </>
  )
}
