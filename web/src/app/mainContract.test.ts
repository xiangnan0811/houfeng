import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('application root error recovery', () => {
  it('wraps the provider and router tree in AppErrorBoundary', () => {
    const mainSource = readFileSync('src/main.tsx', 'utf8')

    expect(mainSource).toContain("import { AppErrorBoundary } from './app/AppErrorBoundary'")
    expect(mainSource).toContain('<AppErrorBoundary>')
    expect(mainSource).toContain('</AppErrorBoundary>')
  })
})
