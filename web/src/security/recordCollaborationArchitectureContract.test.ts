import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('record collaboration chunk architecture', () => {
  it('keeps collaboration surfaces behind the lazy route module', () => {
    const router = readFileSync('src/app/router.tsx', 'utf8')
    expect(router).toContain("import('../pages/RecordInboxPage')")
    expect(router).not.toMatch(/import\s+\{[^}]*RecordInboxPage[^}]*\}\s+from/)
  })

  it('keeps the eager shell on the bounded unread-count transport only', () => {
    const topBar = readFileSync('src/app/layout/TopBar.tsx', 'utf8')
    expect(topBar).toContain("from '../../lib/recordInboxUnreadApi'")
    for (const forbidden of ['recordCollaborationApi', 'recordsApi', 'RecordCommentMarkdown', 'RecordActionPanel']) {
      expect(topBar).not.toContain(forbidden)
    }

    const unreadTransport = readFileSync('src/lib/recordInboxUnreadApi.ts', 'utf8')
    expect(unreadTransport).toContain("from './apiRequest'")
    expect(unreadTransport).not.toContain('recordCollaborationApi')
    expect(unreadTransport).not.toContain('recordsApi')
  })
})
