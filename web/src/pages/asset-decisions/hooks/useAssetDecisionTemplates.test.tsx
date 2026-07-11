import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../../lib/api'
import type {
  AssetDecisionManualGroupDetail,
  AssetDecisionScenarioTemplateDetail,
  AssetDecisionScenarioTemplateSummary,
} from '../../../lib/types'
import { useAssetDecisionTemplates } from './useAssetDecisionTemplates'

function templateDetail(
  templateID = 'adt_001',
  overrides: Partial<AssetDecisionScenarioTemplateDetail> = {},
): AssetDecisionScenarioTemplateDetail {
  return {
    template_id: templateID,
    builtin: false,
    status: 'active',
    scenario: 'provider_review',
    title: '服务商评估',
    goal: '复核服务商组合',
    note: '模板备注',
    source_manual_group_id: 'admg_001',
    member_count: 0,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    archived_at: null,
    members: [],
    ...overrides,
  }
}

function templateSummary(detail: AssetDecisionScenarioTemplateDetail): AssetDecisionScenarioTemplateSummary {
  const { members, ...summary } = detail
  void members
  return summary
}

const MANUAL_DETAIL = {
  manual_group_id: 'admg_001',
  title: '德国主备组合',
  scenario: 'primary_standby',
  goal: '保留主力',
  note: '人工复核',
} as unknown as AssetDecisionManualGroupDetail

function deferred<T>() {
  let resolvePromise: (value: T) => void = () => undefined
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve
  })
  return { promise, resolve: resolvePromise }
}

function mockSuccessfulReads(detail = templateDetail()) {
  vi.spyOn(api, 'listAssetDecisionScenarioTemplates').mockResolvedValue([templateSummary(detail)])
  vi.spyOn(api, 'getAssetDecisionScenarioTemplate').mockResolvedValue(detail)
}

describe('useAssetDecisionTemplates', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads the template list and keyed detail independently and seeds the manual-group draft', async () => {
    const detail = templateDetail()
    const listTemplates = vi.spyOn(api, 'listAssetDecisionScenarioTemplates').mockResolvedValue([templateSummary(detail)])
    const getTemplate = vi.spyOn(api, 'getAssetDecisionScenarioTemplate').mockResolvedValue(detail)

    const { result } = renderHook(() => useAssetDecisionTemplates({
      selectedTemplateID: 'adt_001',
      renewalWindow: 60,
      revision: 0,
      onNotice: vi.fn(),
    }))

    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    expect(listTemplates).toHaveBeenCalledWith()
    expect(getTemplate).toHaveBeenCalledWith('adt_001')

    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))
    expect(result.current.state.list.templates).toEqual([templateSummary(detail)])
    expect(result.current.state.manualDraft).toEqual({
      title: '服务商评估',
      goal: '复核服务商组合',
      note: '',
      renewWithinDays: 60,
    })
  })

  it('keeps list and detail errors independent', async () => {
    const detail = templateDetail()
    vi.spyOn(api, 'listAssetDecisionScenarioTemplates').mockRejectedValue('list offline')
    vi.spyOn(api, 'getAssetDecisionScenarioTemplate').mockResolvedValue(detail)

    const { result } = renderHook(() => useAssetDecisionTemplates({
      selectedTemplateID: 'adt_001',
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
    }))

    await waitFor(() => expect(result.current.state.list.error).toBe('加载场景模板失败'))
    expect(result.current.state.detail.detail).toBe(detail)
    expect(result.current.state.detail.error).toBeNull()
  })

  it('ignores a stale detail response after the selected template changes', async () => {
    const firstRequest = deferred<AssetDecisionScenarioTemplateDetail>()
    const secondRequest = deferred<AssetDecisionScenarioTemplateDetail>()
    const nextDetail = templateDetail('adt_002', { title: '第二个模板' })
    vi.spyOn(api, 'listAssetDecisionScenarioTemplates').mockResolvedValue([])
    const getTemplate = vi.spyOn(api, 'getAssetDecisionScenarioTemplate')
      .mockReturnValueOnce(firstRequest.promise)
      .mockReturnValueOnce(secondRequest.promise)

    const { result, rerender } = renderHook(
      ({ selectedTemplateID }) => useAssetDecisionTemplates({
        selectedTemplateID,
        renewalWindow: 30,
        revision: 0,
        onNotice: vi.fn(),
      }),
      { initialProps: { selectedTemplateID: 'adt_001' as string | null } },
    )

    rerender({ selectedTemplateID: 'adt_002' })
    expect(result.current.state.detail).toEqual({ loading: true, error: null, detail: null })

    act(() => secondRequest.resolve(nextDetail))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(nextDetail))
    await act(async () => {
      firstRequest.resolve(templateDetail())
      await firstRequest.promise
    })
    expect(result.current.state.detail.detail).toBe(nextDetail)
    expect(getTemplate).toHaveBeenCalledTimes(2)
  })

  it('reloads list and open detail on an external revision', async () => {
    mockSuccessfulReads()
    const listTemplates = vi.mocked(api.listAssetDecisionScenarioTemplates)
    const getTemplate = vi.mocked(api.getAssetDecisionScenarioTemplate)

    const { result, rerender } = renderHook(
      ({ revision }) => useAssetDecisionTemplates({
        selectedTemplateID: 'adt_001',
        renewalWindow: 30,
        revision,
        onNotice: vi.fn(),
      }),
      { initialProps: { revision: 0 } },
    )
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    rerender({ revision: 1 })
    expect(result.current.state.list.loading).toBe(true)
    expect(result.current.state.detail.loading).toBe(true)
    await waitFor(() => expect(getTemplate).toHaveBeenCalledTimes(2))
    expect(listTemplates).toHaveBeenCalledTimes(2)
  })

  it('creates a template from a manual group and locally merges the returned representation', async () => {
    const detail = templateDetail()
    mockSuccessfulReads(detail)
    const created = templateDetail('adt_created', {
      title: '德国主备组合 模板',
      scenario: 'primary_standby',
    })
    const createTemplate = vi.spyOn(api, 'createAssetDecisionScenarioTemplate').mockResolvedValue(created)
    const notice = vi.fn()

    const { result } = renderHook(() => useAssetDecisionTemplates({
      selectedTemplateID: 'adt_001',
      renewalWindow: 30,
      revision: 0,
      onNotice: notice,
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    let returned: AssetDecisionScenarioTemplateDetail | null = null
    await act(async () => {
      returned = await result.current.commands.createFromManualGroup(MANUAL_DETAIL)
    })

    expect(returned).toBe(created)
    expect(createTemplate).toHaveBeenCalledWith({
      source_manual_group_id: 'admg_001',
      title: '德国主备组合 模板',
      scenario: 'primary_standby',
      goal: '保留主力',
      note: '人工复核',
    })
    expect(result.current.state.list.templates[0]?.template_id).toBe('adt_created')
    expect(notice).toHaveBeenCalledWith('已另存为场景模板：德国主备组合 模板')
  })

  it('guards built-in templates and requires pending confirmation before status PATCH', async () => {
    const builtin = templateDetail('adt_builtin', { builtin: true })
    mockSuccessfulReads(builtin)
    const patchTemplate = vi.spyOn(api, 'patchAssetDecisionScenarioTemplate')

    const { result } = renderHook(() => useAssetDecisionTemplates({
      selectedTemplateID: 'adt_builtin',
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(builtin))

    act(() => result.current.commands.requestStatusUpdate('archived'))
    expect(result.current.state.pendingStatus).toBeNull()
    await act(async () => result.current.commands.updateStatus('archived'))
    expect(patchTemplate).not.toHaveBeenCalled()
  })

  it('patches a custom template only after confirmation and updates list/detail locally', async () => {
    const detail = templateDetail()
    mockSuccessfulReads(detail)
    const updated = templateDetail('adt_001', { status: 'archived' })
    const patchTemplate = vi.spyOn(api, 'patchAssetDecisionScenarioTemplate').mockResolvedValue(updated)

    const { result } = renderHook(() => useAssetDecisionTemplates({
      selectedTemplateID: 'adt_001',
      renewalWindow: 30,
      revision: 0,
      onNotice: vi.fn(),
    }))
    await waitFor(() => expect(result.current.state.detail.detail).toBe(detail))

    await act(async () => result.current.commands.updateStatus('archived'))
    expect(patchTemplate).not.toHaveBeenCalled()

    act(() => result.current.commands.requestStatusUpdate('archived'))
    expect(result.current.state.pendingStatus).toBe('archived')
    await act(async () => result.current.commands.updateStatus('archived'))

    expect(patchTemplate).toHaveBeenCalledWith('adt_001', { status: 'archived' })
    expect(result.current.state.pendingStatus).toBeNull()
    expect(result.current.state.detail.detail).toBe(updated)
    expect(result.current.state.list.templates[0]?.status).toBe('archived')
  })

  it('owns keyed panel and manual draft patches', async () => {
    mockSuccessfulReads()

    const { result, rerender } = renderHook(
      ({ selectedTemplateID }) => useAssetDecisionTemplates({
        selectedTemplateID,
        renewalWindow: 30,
        revision: 0,
        onNotice: vi.fn(),
      }),
      { initialProps: { selectedTemplateID: 'adt_001' as string | null } },
    )
    await waitFor(() => expect(result.current.state.detail.loading).toBe(false))

    act(() => {
      result.current.commands.selectPanel('create')
      result.current.commands.updateManualDraft({ title: '自定义标题', renewWithinDays: 90 })
    })
    expect(result.current.state.detailPanel).toBe('create')
    expect(result.current.state.manualDraft).toMatchObject({
      title: '自定义标题',
      renewWithinDays: 90,
    })

    rerender({ selectedTemplateID: 'adt_002' })
    expect(result.current.state.detailPanel).toBe('overview')
    act(() => result.current.commands.resetDetailUI())
    expect(result.current.state.error).toBeNull()
  })
})
