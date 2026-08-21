import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const compareDir = dirname(fileURLToPath(import.meta.url))

describe('records compare surfaces', () => {
  it('do not add download or export chrome', () => {
    const files = readdirSync(compareDir).filter((name) => name.endsWith('.tsx') || name.endsWith('.ts'))
    const forbidden = [/下载/, /导出/, /\bdownload\b/i, /content-disposition/i, /\.csv/i]
    for (const name of files) {
      if (name === 'noDownloadChrome.test.ts') continue
      const source = readFileSync(join(compareDir, name), 'utf8')
      for (const pattern of forbidden) {
        expect(source, `${name} contains ${pattern}`).not.toMatch(pattern)
      }
    }
    const page = readFileSync(join(compareDir, '../../RecordComparisonPage.tsx'), 'utf8')
    expect(page).not.toMatch(/下载/)
    expect(page).not.toMatch(/导出/)
    expect(page).not.toMatch(/\bdownload\b/i)
  })
})
