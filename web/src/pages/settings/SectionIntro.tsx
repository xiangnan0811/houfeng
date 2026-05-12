import type { ReactNode } from 'react'

type SectionIntroProps = {
  children: ReactNode
}

export function SectionIntro({ children }: SectionIntroProps) {
  return <div style={{ fontSize: 'var(--type-small-size)', color: 'var(--text-secondary)', lineHeight: 1.5 }}>{children}</div>
}
