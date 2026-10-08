import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  createAttachmentUpload: vi.fn(),
  uploadAttachmentContent: vi.fn(),
  completeAttachmentUpload: vi.fn(),
  getAttachmentMetadata: vi.fn(),
  getAttachmentContent: vi.fn(),
  captureEvidencePreview: vi.fn(),
  getEvidenceSnapshot: vi.fn(),
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

vi.mock('../../lib/auth-context', () => {
  const auth = {
    user: {
      user_id: 'usr_1',
      username: 'admin',
      role: 'admin',
      display_name: '管理员',
      runtime_capabilities: { records: true, comparison: true, portability: true },
      management_capabilities: { access: false },
    },
    loading: false,
    status: 'ready' as const,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    retry: vi.fn(),
  }
  return {
    useAuth: () => auth,
  }
})

const vpsApi = vi.hoisted(() => ({
  listVPSAssets: vi.fn(),
  listVPSMonitoringInstances: vi.fn(),
}))

// 可让发布在删除本地缓冲前停住：此时新修订已读回，待保存证据尚未放下。
const bufferGate = vi.hoisted(() => ({ hold: null as Promise<void> | null }))

vi.mock('./draftBuffer', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./draftBuffer')>()
  return {
    ...actual,
    memoryDraftBufferStore: (...args: Parameters<typeof actual.memoryDraftBufferStore>) => {
      const store = actual.memoryDraftBufferStore(...args)
      return {
        ...store,
        delete: async (key: string) => {
          await bufferGate.hold
          return store.delete(key)
        },
      }
    },
  }
})

vi.mock('../../lib/recordsApi', () => api)
vi.mock('../../lib/api', async (importOriginal) => ({ ...await importOriginal<object>(), ...vpsApi }))
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

type CapturePreviewInput = { kind: string; schema_version: number; source_type: string; source_id: string; requested_window: { start: string; end: string } }

// 预览按请求回显来源与窗口，每次一个新的采集意图。
function mockCapturePreviews() {
  let intent = 0
  api.captureEvidencePreview.mockImplementation((input: CapturePreviewInput) => {
    intent += 1
    return Promise.resolve({
      record_id: 'rec_001', snapshot_id: `evs_new_${intent}`, capture_intent_id: `eci_${intent}`,
      kind: input.kind, schema_version: input.schema_version,
      subject: { type: 'vps', id: 'vps_0123456789abcdef', display_name: 'VPS Alpha' },
      source: { type: input.source_type, id: input.source_id, display_name: '' },
      requested_window: input.requested_window, actual_window: input.requested_window,
      quality: { status: 'complete' }, quota: { status: 'allowed' }, estimated_canonical_bytes: 2048,
      valid_until: new Date(Date.now() + 15 * 60_000).toISOString(),
    })
  })
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
    api.getEvidenceSnapshot.mockResolvedValue({ title: '第三晚 TCP 观测', kind: 'monitoring.host' })
    render(
      <MemoryRouter>
        <RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '导出' })).toBeInTheDocument()
    const materials = screen.getByRole('list', { name: '材料清单' })
    // 证据按类型与标题显示，不露出快照 ID；只读取历史修订引用的那一份。
    expect(await within(materials).findByText('主机监控 · 第三晚 TCP 观测')).toBeInTheDocument()
    expect(within(materials).queryByText('ev_hist')).toBeNull()
    expect(api.getEvidenceSnapshot).toHaveBeenCalledTimes(1)
    expect(api.getEvidenceSnapshot).toHaveBeenCalledWith('ev_hist', expect.any(AbortSignal))
    expect(within(materials).getByRole('link', { name: '查看证据' })).toHaveAttribute('href', '/evidence/ev_hist')
    expect(screen.queryByText('ev_current')).toBeNull()
    expect(screen.queryByRole('button', { name: '管理材料' })).toBeNull()
    expect(screen.queryByRole('button', { name: '插入证据 ev_hist' })).toBeNull()
    expect(screen.queryByRole('button', { name: '移除证据 ev_hist' })).toBeNull()
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

  it('drops failed uploads of a draft once the draft has been published', async () => {
    const existingDraft = { ...draftFixture(), record_id: 'rec_001', base_revision_id: 'rrv_001' }
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.createRecordDraft.mockResolvedValue(existingDraft)
    api.patchRecordDraft.mockResolvedValue(existingDraft)
    api.createRecordRevision.mockResolvedValue({ record_id: 'rec_001' })
    api.getAttachmentMetadata.mockRejectedValue(new Error('unavailable'))
    api.createAttachmentUpload.mockRejectedValue(new Error('网络中断'))
    render(
      <MemoryRouter initialEntries={['/records/rec_001/edit']}>
        <Routes>
          <Route path="/records/:recordId/edit" element={<WorkspaceByRecordId mode="edit" />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '管理材料' }))
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [new File(['x'], 'trace.log')] } })
    expect(await screen.findByRole('button', { name: '重试上传trace.log' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '发布修订' }))
    await waitFor(() => expect(api.createRecordRevision).toHaveBeenCalled())
    await waitFor(() => expect(api.getRecord).toHaveBeenCalledTimes(2))

    // 草稿已随发布消费：它名下的失败项不能再对它重试，队列随之清空。
    fireEvent.click(screen.getByRole('button', { name: '管理材料' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: '重试上传trace.log' })).not.toBeInTheDocument())
  })

  it('keeps captured evidence pending on the page and publishes it with the revision', async () => {
    const existingDraft = { ...draftFixture(), record_id: 'rec_001', base_revision_id: 'rrv_001' }
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current: recordRevisionFixture({ evidence_snapshot_ids: ['evs_old'] }),
    }))
    api.getEvidenceSnapshot.mockResolvedValue({ title: '上周观测', kind: 'monitoring.host' })
    api.getAttachmentMetadata.mockRejectedValue(new Error('unavailable'))
    api.createRecordDraft.mockResolvedValue(existingDraft)
    api.patchRecordDraft.mockResolvedValue(existingDraft)
    api.createRecordRevision.mockResolvedValue({ record_id: 'rec_001' })
    vpsApi.listVPSMonitoringInstances.mockResolvedValue([{ monitoring_instance_id: 'mi_alpha', display_name: 'Alpha 监控' }])
    mockCapturePreviews()
    render(
      <MemoryRouter initialEntries={['/records/rec_001/edit']}>
        <Routes>
          <Route path="/records/:recordId/edit" element={<WorkspaceByRecordId mode="edit" />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '管理材料' }))
    fireEvent.click(screen.getByRole('button', { name: '采集证据' }))
    // 主体 VPS 名下的监控实例是默认来源，带记录 ID 预览。
    await waitFor(() => expect(screen.getByLabelText('来源')).toHaveDisplayValue('VPS Alpha · Alpha 监控'))
    const addPreview = async () => {
      fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
      fireEvent.click(within(await screen.findByRole('region', { name: '证据预览' })).getByRole('button', { name: '加入记录' }))
    }
    await addPreview()
    await addPreview()
    expect(api.captureEvidencePreview).toHaveBeenCalledWith(
      expect.objectContaining({ record_id: 'rec_001', source_type: 'monitoring_instance', source_id: 'mi_alpha' }),
      expect.any(AbortSignal),
    )

    const materials = screen.getByRole('dialog', { name: '材料与引用' })
    expect(within(materials).getAllByText('待保存')).toHaveLength(2)
    // 已保存的证据不能移除，本次新采集的可以。
    expect(within(materials).queryByRole('button', { name: '移除主机监控 · 上周观测' })).not.toBeInTheDocument()
    fireEvent.click(within(materials).getAllByRole('button', { name: '移除主机监控 · VPS Alpha · Alpha 监控' })[0]!)
    expect(within(materials).getAllByText('待保存')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '发布修订' }))
    await waitFor(() => expect(api.createRecordRevision).toHaveBeenCalled())
    expect(api.createRecordRevision).toHaveBeenCalledWith('rec_001', expect.objectContaining({
      evidence_items: [{ existing_snapshot_id: 'evs_old' }, { capture_intent_id: 'eci_2' }],
    }), expect.any(String))
    await waitFor(() => expect(api.getRecord).toHaveBeenCalledTimes(2))
    fireEvent.click(screen.getByRole('button', { name: '管理材料' }))
    expect(screen.queryByText('待保存')).not.toBeInTheDocument()
  })

  it('shows a published capture once, as saved, even before the publish finishes', async () => {
    const existingDraft = { ...draftFixture(), record_id: 'rec_001', base_revision_id: 'rrv_001' }
    api.getRecord
      .mockResolvedValueOnce(recordDetailFixture({ current: recordRevisionFixture({ evidence_snapshot_ids: ['evs_old'] }) }))
      .mockResolvedValue(recordDetailFixture({
        current_revision_id: 'rrv_002',
        current: recordRevisionFixture({ revision_id: 'rrv_002', evidence_snapshot_ids: ['evs_old', 'evs_new_1'] }),
      }))
    api.getEvidenceSnapshot.mockImplementation((id: string) => Promise.resolve({ title: id === 'evs_old' ? '上周观测' : '本次观测', kind: 'monitoring.host' }))
    api.getAttachmentMetadata.mockRejectedValue(new Error('unavailable'))
    api.createRecordDraft.mockResolvedValue(existingDraft)
    api.patchRecordDraft.mockResolvedValue(existingDraft)
    let releaseBuffer: () => void = () => undefined
    // 修订写入后才拦住缓冲删除：保存草稿时的删除照常放行。
    api.createRecordRevision.mockImplementation(() => {
      bufferGate.hold = new Promise((resolve) => { releaseBuffer = resolve })
      return Promise.resolve({ record_id: 'rec_001' })
    })
    vpsApi.listVPSMonitoringInstances.mockResolvedValue([{ monitoring_instance_id: 'mi_alpha', display_name: 'Alpha 监控' }])
    mockCapturePreviews()
    render(
      <MemoryRouter initialEntries={['/records/rec_001/edit']}>
        <Routes>
          <Route path="/records/:recordId/edit" element={<WorkspaceByRecordId mode="edit" />} />
        </Routes>
      </MemoryRouter>,
    )
    try {
      fireEvent.click(await screen.findByRole('button', { name: '管理材料' }))
      fireEvent.click(screen.getByRole('button', { name: '采集证据' }))
      await waitFor(() => expect(screen.getByLabelText('来源')).toHaveDisplayValue('VPS Alpha · Alpha 监控'))
      fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
      fireEvent.click(within(await screen.findByRole('region', { name: '证据预览' })).getByRole('button', { name: '加入记录' }))

      fireEvent.click(screen.getByRole('button', { name: '关闭' }))
      fireEvent.click(screen.getByRole('button', { name: '发布修订' }))
      await waitFor(() => expect(api.getRecord).toHaveBeenCalledTimes(2))
      fireEvent.click(screen.getByRole('button', { name: '管理材料' }))
      const materials = screen.getByRole('dialog', { name: '材料与引用' })
      // 新修订已带回这份快照：它只作为已保存证据出现一次，不再是“待保存”。
      expect(await within(materials).findByText('主机监控 · 本次观测')).toBeInTheDocument()
      expect(within(materials).queryByText('待保存')).not.toBeInTheDocument()
      expect(within(materials).queryByRole('button', { name: '移除主机监控 · VPS Alpha · Alpha 监控' })).not.toBeInTheDocument()
    } finally {
      releaseBuffer()
      bufferGate.hold = null
    }
  })

  it('locks pending evidence while a publish is in flight', async () => {
    const existingDraft = { ...draftFixture(), record_id: 'rec_001', base_revision_id: 'rrv_001' }
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.getAttachmentMetadata.mockRejectedValue(new Error('unavailable'))
    api.createRecordDraft.mockResolvedValue(existingDraft)
    api.patchRecordDraft.mockResolvedValue(existingDraft)
    let releasePublish: () => void = () => undefined
    api.createRecordRevision.mockImplementation(() => new Promise((resolve) => {
      releasePublish = () => resolve({ record_id: 'rec_001' })
    }))
    vpsApi.listVPSMonitoringInstances.mockResolvedValue([{ monitoring_instance_id: 'mi_alpha', display_name: 'Alpha 监控' }])
    mockCapturePreviews()
    render(
      <MemoryRouter initialEntries={['/records/rec_001/edit']}>
        <Routes>
          <Route path="/records/:recordId/edit" element={<WorkspaceByRecordId mode="edit" />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '管理材料' }))
    fireEvent.click(screen.getByRole('button', { name: '采集证据' }))
    await waitFor(() => expect(screen.getByLabelText('来源')).toHaveDisplayValue('VPS Alpha · Alpha 监控'))
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    fireEvent.click(within(await screen.findByRole('region', { name: '证据预览' })).getByRole('button', { name: '加入记录' }))
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '发布修订' }))
    await waitFor(() => expect(api.createRecordRevision).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '管理材料' }))
    const materials = screen.getByRole('dialog', { name: '材料与引用' })
    // 已随这次发布提交的证据不能移除，新预览也不能加入。
    expect(within(materials).getByRole('button', { name: '移除主机监控 · VPS Alpha · Alpha 监控' })).toBeDisabled()
    fireEvent.click(within(materials).getByRole('button', { name: '采集证据' }))
    await waitFor(() => expect(screen.getByLabelText('来源')).toHaveDisplayValue('VPS Alpha · Alpha 监控'))
    fireEvent.click(screen.getByRole('button', { name: '生成预览' }))
    expect(within(await screen.findByRole('region', { name: '证据预览' })).getByRole('button', { name: '加入记录' })).toBeDisabled()

    releasePublish()
    await waitFor(() => expect(within(materials).queryByText('待保存')).not.toBeInTheDocument())
    expect(api.createRecordRevision).toHaveBeenCalledTimes(1)
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

    expect(await screen.findByRole('link', { name: '当前版本' })).toHaveAttribute(
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
