import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useParams } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RecordWorkspace } from './RecordWorkspace'
import { emptyRecordDraftPayload, recordDetailFixture, recordRevisionFixture } from './testFixtures'

const api = vi.hoisted(() => ({
  getRecord: vi.fn(),
  getRecordRevision: vi.fn(),
  listRecordDrafts: vi.fn(),
  getRecordDraft: vi.fn(),
  createRecordDraft: vi.fn(),
  patchRecordDraft: vi.fn(),
  createRecord: vi.fn(),
  createRecordRevision: vi.fn(),
  restoreRecordRevision: vi.fn(),
}))

const collab = vi.hoisted(() => ({
  listRecordActions: vi.fn(),
  listRecordComments: vi.fn(),
  getRecordWatch: vi.fn(),
  createRecordAction: vi.fn(),
  updateRecordAction: vi.fn(),
  transitionRecordAction: vi.fn(),
  createRecordComment: vi.fn(),
  editRecordComment: vi.fn(),
  redactRecordComment: vi.fn(),
  setRecordWatch: vi.fn(),
}))

vi.mock('../../lib/auth-context', () => ({
  useAuth: () => ({
    user: { user_id: 'usr_1', username: 'admin', role: 'admin', display_name: '管理员' },
    loading: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }),
}))

vi.mock('../../lib/recordsApi', () => api)
vi.mock('../../lib/recordCollaborationApi', () => collab)

function draftFixture() {
  return {
    draft_id: 'dft_001',
    payload: emptyRecordDraftPayload('usr_1'),
    version: 1,
    etag: 'etag-1',
    warning_at: '2026-08-18T00:00:00Z',
    created_at: '2026-08-18T00:00:00Z',
    updated_at: '2026-08-18T00:00:00Z',
    expires_at: '2026-08-19T00:00:00Z',
  }
}

function WorkspaceByRecordId({ mode }: { mode: 'read' | 'edit' }) {
  const { recordId } = useParams()
  return <RecordWorkspace mode={mode} {...(recordId ? { recordId } : {})} />
}

function LocationProbe({ testId }: { testId: string }) {
  const loc = useLocation()
  return (
    <pre data-testid={testId} data-state={JSON.stringify(loc.state)}>
      {loc.pathname}{loc.search}
    </pre>
  )
}

describe('RecordWorkspace', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.listRecordDrafts.mockResolvedValue({ items: [] })
    collab.listRecordActions.mockResolvedValue({ items: [] })
    collab.listRecordComments.mockResolvedValue({ comments: [] })
    collab.getRecordWatch.mockResolvedValue({
      record_id: 'rec_001',
      preference: 'default',
      version: 1,
      sources: { author: true, owner: false, participant: false, comment: false, mention: false, action: false },
    })
  })

  it('does not offer checklist promotion on a record that does not exist yet', () => {
    render(<MemoryRouter><RecordWorkspace mode="new" /></MemoryRouter>)
    expect(screen.queryByRole('button', { name: '提升勾选为行动' })).toBeNull()
    expect(screen.getByRole('toolbar', { name: '编辑布局' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '插入模板' })).toBeInTheDocument()
  })

  it('defaults a valid business status when switching to a workflow type', () => {
    render(<MemoryRouter><RecordWorkspace mode="new" /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('记录类型'), { target: { value: 'troubleshooting' } })
    expect(screen.getByLabelText('业务状态')).toHaveValue('pending_investigation')
    fireEvent.click(screen.getByRole('button', { name: '插入模板' }))
    expect(screen.getByLabelText('Markdown 源文')).toHaveValue('## 现象\n\n## 排查\n\n## 结论')
  })

  it('refreshes actions after an explicit checklist promotion', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current: {
        ...recordDetailFixture().current,
        body_markdown: '- [ ] inspect logs',
      },
    }))
    collab.createRecordAction.mockResolvedValue({ action_id: 'act_1' })
    collab.listRecordActions
      .mockResolvedValueOnce({ items: [] })
      .mockResolvedValueOnce({ items: [{
        action_id: 'act_1',
        record_id: 'rec_001',
        title: 'inspect logs',
        status: 'open',
        assignee_id: 'usr_1',
        version: 1,
      }] })
    render(<MemoryRouter><RecordWorkspace mode="edit" recordId="rec_001" /></MemoryRouter>)
    expect(await screen.findByLabelText('标题')).toHaveValue('Database outage')
    fireEvent.click(screen.getByRole('button', { name: '提升勾选为行动' }))
    fireEvent.click(screen.getByRole('button', { name: '预览行动项' }))
    fireEvent.click(screen.getByRole('button', { name: '确认创建行动项' }))
    await waitFor(() => expect(collab.createRecordAction).toHaveBeenCalled())
    await waitFor(() => expect(collab.listRecordActions).toHaveBeenCalledTimes(2))
  })

  it('lists only historical evidence and keeps materials read-only on a revision page', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({
        revision_id: 'rrv_002',
        title: 'current',
        evidence_snapshot_ids: ['ev_current'],
      }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture({
      revision_id: 'rrv_001',
      evidence_snapshot_ids: ['ev_hist'],
    }))
    render(
      <MemoryRouter>
        <RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '导出' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '材料与引用' }))
    expect(screen.getByText('ev_hist')).toBeInTheDocument()
    expect(screen.queryByText('ev_current')).toBeNull()
    expect(screen.getByRole('button', { name: '插入证据 ev_hist' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '移除证据 ev_hist' })).toBeDisabled()
  })

  it('navigates to the record after restoring a historical revision', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'current' }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture())
    api.restoreRecordRevision.mockResolvedValue(recordDetailFixture())
    render(
      <MemoryRouter initialEntries={['/records/rec_001/revisions/rrv_001']}>
        <Routes>
          <Route path="/records/:recordId/revisions/:revisionId" element={<RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />} />
          <Route path="/records/:recordId" element={<p>record home</p>} />
        </Routes>
      </MemoryRouter>,
    )
    expect(await screen.findByRole('button', { name: '恢复为新修订' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复为新修订' }))
    expect(await screen.findByText('record home')).toBeInTheDocument()
  })

  it('keeps non-default inventory state after restoring a revision', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'current' }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture())
    api.restoreRecordRevision.mockResolvedValue(recordDetailFixture())
    function Probe() {
      const { state } = useLocation()
      return <pre>{JSON.stringify(state)}</pre>
    }
    render(
      <MemoryRouter initialEntries={[{
        pathname: '/records/rec_001/revisions/rrv_001',
        state: { vpsInventoryHref: '/vps?workspace=ledger&q=Tokyo&selected=vps_001' },
      }]}>
        <Routes>
          <Route path="/records/:recordId/revisions/:revisionId" element={<RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />} />
          <Route path="/records/:recordId" element={<Probe />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '恢复为新修订' }))
    expect(await screen.findByText(/vpsInventoryHref/)).toHaveTextContent(
      '/vps?workspace=ledger&q=Tokyo&selected=vps_001',
    )
  })

  it('restores validated return_vps onto monitoring subject return and keeps list state', () => {
    function Probe() {
      const loc = useLocation()
      return (
        <pre data-testid="subject-probe" data-state={JSON.stringify(loc.state)}>
          {loc.pathname}{loc.search}
        </pre>
      )
    }
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_001',
      return_vps: 'vps_tokyo_origin',
    }
    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/records/new',
          search: '?subject=monitoring_instance%3Ami_001%3Aaffected%3Aprimary&return_to=%2Fmonitoring%2Fmi_001%2Frecords',
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/records/new" element={<RecordWorkspace mode="new" />} />
          <Route path="/monitoring/:monitoringInstanceId/records" element={<Probe />} />
        </Routes>
      </MemoryRouter>,
    )
    const back = screen.getByRole('link', { name: '返回主体' })
    expect(back).toHaveAttribute('href', '/monitoring/mi_001/records?return_vps=vps_tokyo_origin')
    fireEvent.click(back)
    expect(screen.getByTestId('subject-probe')).toHaveTextContent('/monitoring/mi_001/records?return_vps=vps_tokyo_origin')
    expect(JSON.parse(screen.getByTestId('subject-probe').getAttribute('data-state')!)).toEqual(navState)
  })

  it('refuses invalid return_vps provenance from history state', () => {
    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/records/new',
          search: '?subject=monitoring_instance%3Ami_001%3Aaffected%3Aprimary&return_to=%2Fmonitoring%2Fmi_001%2Frecords',
          state: {
            monitoringListHref: '/monitoring?selected=mi_001',
            return_vps: 'javascript:alert(1)',
          },
        }]}
      >
        <Routes>
          <Route path="/records/new" element={<RecordWorkspace mode="new" />} />
        </Routes>
      </MemoryRouter>,
    )
    const back = screen.getByRole('link', { name: '返回主体' })
    expect(back).toHaveAttribute('href', '/monitoring/mi_001/records')
    expect(back.getAttribute('href')).not.toContain('return_vps=')
    expect(back.getAttribute('href')).not.toContain('javascript')
  })

  it('preserves canonical subject return across publish and related read/edit hops', async () => {
    api.createRecordDraft.mockResolvedValue(draftFixture())
    api.createRecord.mockResolvedValue({ record_id: 'rec_001' })
    api.getRecord.mockResolvedValue(recordDetailFixture())
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_001',
      return_vps: 'vps_tokyo_origin',
    }
    const subjectReturnQuery = 'return_to=%2Fmonitoring%2Fmi_001%2Frecords'
    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/records/new',
          search: `?subject=monitoring_instance%3Ami_001%3Aaffected%3Aprimary&${subjectReturnQuery}&foo=1`,
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/records/new" element={<RecordWorkspace mode="new" />} />
          <Route path="/records/:recordId" element={<><LocationProbe testId="record-probe" /><WorkspaceByRecordId mode="read" /></>} />
          <Route path="/records/:recordId/edit" element={<><LocationProbe testId="record-probe" /><WorkspaceByRecordId mode="edit" /></>} />
          <Route path="/monitoring/:monitoringInstanceId/records" element={<LocationProbe testId="subject-probe" />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '发布修订' }))
    await waitFor(() => expect(api.createRecord).toHaveBeenCalledWith({
      draft_id: 'dft_001',
      draft_etag: 'etag-1',
    }, expect.any(String)))

    const published = await screen.findByTestId('record-probe')
    expect(published).toHaveTextContent(`/records/rec_001?${subjectReturnQuery}`)
    expect(published).not.toHaveTextContent('subject=')
    expect(published).not.toHaveTextContent('foo=')
    expect(JSON.parse(published.getAttribute('data-state')!)).toEqual(navState)
    expect(await screen.findByRole('link', { name: '返回主体' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001/records?return_vps=vps_tokyo_origin',
    )
    expect(screen.getByRole('link', { name: '编辑' })).toHaveAttribute(
      'href',
      `/records/rec_001/edit?${subjectReturnQuery}`,
    )

    fireEvent.click(screen.getByRole('link', { name: '编辑' }))
    expect(await screen.findByLabelText('标题')).toHaveValue('Database outage')
    expect(screen.getByTestId('record-probe')).toHaveTextContent(`/records/rec_001/edit?${subjectReturnQuery}`)
    expect(screen.getByRole('link', { name: '返回主体' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001/records?return_vps=vps_tokyo_origin',
    )
    expect(screen.getByRole('link', { name: '阅读' })).toHaveAttribute(
      'href',
      `/records/rec_001?${subjectReturnQuery}`,
    )

    fireEvent.click(screen.getByRole('link', { name: '阅读' }))
    expect(await screen.findByRole('link', { name: '编辑' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('link', { name: '返回主体' }))
    expect(screen.getByTestId('subject-probe')).toHaveTextContent('/monitoring/mi_001/records?return_vps=vps_tokyo_origin')
    expect(JSON.parse(screen.getByTestId('subject-probe').getAttribute('data-state')!)).toEqual(navState)
  })

  it.each([
    'https://evil.example/phish',
    '/records/new',
    '/monitoring/mi_001/records?leak=1',
  ])('does not forward invalid return_to %s across publish', async (raw) => {
    api.createRecordDraft.mockResolvedValue(draftFixture())
    api.createRecord.mockResolvedValue({ record_id: 'rec_001' })
    api.getRecord.mockResolvedValue(recordDetailFixture())
    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/records/new',
          search: `?subject=monitoring_instance%3Ami_001%3Aaffected%3Aprimary&return_to=${encodeURIComponent(raw)}&foo=1`,
          state: {
            monitoringListHref: '/monitoring?view=abnormal&selected=mi_001',
            return_vps: 'vps_tokyo_origin',
          },
        }]}
      >
        <Routes>
          <Route path="/records/new" element={<RecordWorkspace mode="new" />} />
          <Route path="/records/:recordId" element={<><LocationProbe testId="record-probe" /><WorkspaceByRecordId mode="read" /></>} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.queryByRole('link', { name: '返回主体' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '发布修订' }))
    await waitFor(() => expect(api.createRecord).toHaveBeenCalled())

    const published = await screen.findByTestId('record-probe')
    expect(published).toHaveTextContent('/records/rec_001')
    expect(published).not.toHaveTextContent('return_to=')
    expect(published).not.toHaveTextContent('foo=')
    expect(published).not.toHaveTextContent('subject=')
    expect(published.textContent).not.toContain(raw)
    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '返回主体' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '编辑' })).toHaveAttribute('href', '/records/rec_001/edit')
  })

  it('keeps canonical subject return after restoring a historical revision', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'current' }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture())
    api.restoreRecordRevision.mockResolvedValue(recordDetailFixture())
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_001',
      return_vps: 'vps_tokyo_origin',
    }
    const subjectReturnQuery = 'return_to=%2Fmonitoring%2Fmi_001%2Frecords'
    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/records/rec_001/revisions/rrv_001',
          search: `?${subjectReturnQuery}`,
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/records/:recordId/revisions/:revisionId" element={<RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />} />
          <Route path="/records/:recordId" element={<><LocationProbe testId="record-probe" /><WorkspaceByRecordId mode="read" /></>} />
          <Route path="/monitoring/:monitoringInstanceId/records" element={<LocationProbe testId="subject-probe" />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('link', { name: '阅读' })).toHaveAttribute(
      'href',
      `/records/rec_001?${subjectReturnQuery}`,
    )
    fireEvent.click(await screen.findByRole('button', { name: '恢复为新修订' }))
    await waitFor(() => expect(api.restoreRecordRevision).toHaveBeenCalled())

    const published = await screen.findByTestId('record-probe')
    expect(published).toHaveTextContent(`/records/rec_001?${subjectReturnQuery}`)
    expect(JSON.parse(published.getAttribute('data-state')!)).toEqual(navState)
    expect(await screen.findByRole('link', { name: '返回主体' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001/records?return_vps=vps_tokyo_origin',
    )
    fireEvent.click(screen.getByRole('link', { name: '返回主体' }))
    expect(screen.getByTestId('subject-probe')).toHaveTextContent('/monitoring/mi_001/records?return_vps=vps_tokyo_origin')
    expect(JSON.parse(screen.getByTestId('subject-probe').getAttribute('data-state')!)).toEqual(navState)
  })
})
