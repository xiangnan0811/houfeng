import type { ReactNode } from 'react'

type SectionIntroProps = {
  children: ReactNode
}

export function SectionIntro({ children }: SectionIntroProps) {
  return <p className="empty-inline">{children}</p>
}
