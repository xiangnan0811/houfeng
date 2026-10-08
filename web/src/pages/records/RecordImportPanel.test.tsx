import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RecordImportPanel } from './RecordImportPanel'

const api = vi.hoisted(() => ({
  dryRunRecordImport: vi.fn(),
  applyRecordImport: vi.fn(),
}))

const inventory = vi.hoisted(() => ({
  listVPSAssets: vi.fn(),
  listMonitoringInstances: vi.fn(),
  listTargets: vi.fn(),
}))

vi.mock('../../lib/recordsApi', () => api)
vi.mock('../../lib/api', () => inventory)

function vps(id: string, name: string) {
  return { vps_id: id, display_name: name, provider_name: '甲', region: '东京' }
}

function instance(id: string, name: string) {
  return { monitoring_instance_id: id, display_name: name, provider: '乙', region: '大阪' }
}

function target(id: string, name: string) {
  return { target_id: id, name, host: '10.0.0.8' }
}

function planFor(subjectId = 'vps_a', patch: Record<string, unknown> = {}) {
  return {
    plan_id: 'rip_1',
    job_state: 'planned',
    lock_version: 2,
    destination_subject: { subject_kind: 'vps', subject_id: subjectId },
    remaps: [{ entity_kind: 'record', source_id: 'rec_source01', target_id: 'rec_local01' }],
    quarantine: [],
    object_count: 1,
    expires_at: '2026-08-21T13:00:00Z',
    ...patch,
  }
}

function renderPanel() {
  return render(
    <MemoryRouter>
      <RecordImportPanel />
    </MemoryRouter>,
  )
}

function chooseFile(name = 'archive.zip') {
  fireEvent.change(screen.getByLabelText('归档文件'), {
    target: { files: [new File(['PK'], name, { type: 'application/zip' })] },
  })
}

async function selectVps(id = 'vps_a', name = '边缘甲') {
  inventory.listVPSAssets.mockResolvedValue([vps(id, name)])
  fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'vps' } })
  const subject = await screen.findByLabelText('目标主体')
  fireEvent.change(subject, { target: { value: id } })
  return subject
}

async function prepareImport() {
  const subject = await selectVps()
  chooseFile()
  await waitFor(() => expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled())
  return subject
}

describe('RecordImportPanel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    inventory.listVPSAssets.mockResolvedValue([])
    inventory.listMonitoringInstances.mockResolvedValue([])
    inventory.listTargets.mockResolvedValue([])
  })

  it('starts empty and loads only the catalog for the chosen kind', async () => {
    inventory.listMonitoringInstances.mockResolvedValue([instance('mi_1', '实例甲')])
    inventory.listTargets.mockResolvedValue([target('tgt_1', '探测甲')])
    renderPanel()

    expect(screen.getByText('归档内所有记录将关联此主体。')).toBeInTheDocument()
    expect(screen.getByText('选择主体类型后列出可关联的对象。')).toBeInTheDocument()
    expect(screen.getByLabelText('主体类型')).toHaveValue('')
    expect(screen.queryByLabelText('目标主体')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '预检导入' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重新预检' })).toBeDisabled()
    expect(inventory.listVPSAssets).not.toHaveBeenCalled()
    expect(inventory.listMonitoringInstances).not.toHaveBeenCalled()
    expect(inventory.listTargets).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(api.dryRunRecordImport).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'monitoring_instance' } })
    expect(await screen.findByRole('option', { name: '实例甲' })).toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('')
    expect(screen.getByRole('button', { name: '预检导入' })).toBeDisabled()
    expect(inventory.listVPSAssets).not.toHaveBeenCalled()
    expect(inventory.listTargets).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'target' } })
    expect(await screen.findByRole('option', { name: '探测甲' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '实例甲' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('')
    expect(inventory.listVPSAssets).not.toHaveBeenCalled()
  })

  it('keeps a late catalog response from replacing the kind now on screen', async () => {
    let resolveVps: (rows: ReturnType<typeof vps>[]) => void = () => {}
    inventory.listVPSAssets.mockImplementation(() => new Promise((resolve) => {
      resolveVps = resolve
    }))
    inventory.listTargets.mockResolvedValue([target('tgt_1', '探测甲')])
    renderPanel()

    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'vps' } })
    expect(await screen.findByRole('heading', { name: '正在读取资产' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'target' } })
    expect(await screen.findByRole('option', { name: '探测甲' })).toBeInTheDocument()

    await act(async () => {
      resolveVps([vps('vps_late', '迟到边缘')])
      await Promise.resolve()
    })

    expect(screen.queryByRole('option', { name: '迟到边缘' })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: '探测甲' })).toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('')
  })

  it('shows an empty catalog separately from a retryable read failure', async () => {
    inventory.listVPSAssets.mockResolvedValueOnce([])
    renderPanel()
    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'vps' } })
    expect(await screen.findByRole('heading', { name: '暂无 VPS。' })).toBeInTheDocument()
    expect(screen.queryByLabelText('目标主体')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '预检导入' })).toBeDisabled()

    inventory.listMonitoringInstances.mockRejectedValueOnce(new Error('postgres.internal:5432 connection refused'))
    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'monitoring_instance' } })
    expect(await screen.findByRole('heading', { name: '无法读取资产' })).toBeInTheDocument()
    expect(screen.getByText('无法读取资产，请重试')).toBeInTheDocument()
    expect(screen.queryByText(/postgres/)).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '暂无监控实例。' })).not.toBeInTheDocument()

    inventory.listMonitoringInstances.mockResolvedValueOnce([instance('mi_1', '实例甲')])
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByRole('option', { name: '实例甲' })).toBeInTheDocument()
  })

  it('does not present a permission failure as an empty catalog', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    inventory.listVPSAssets.mockRejectedValueOnce(new ApiError(404, 'hidden', { code: 'resource_not_found' }))
    renderPanel()
    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'vps' } })
    expect(await screen.findByRole('heading', { name: '资产无法读取' })).toBeInTheDocument()
    expect(screen.getByText('这些资产无法读取。')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '暂无 VPS。' })).not.toBeInTheDocument()
    expect(screen.queryByText(/不存在/)).not.toBeInTheDocument()
  })

  it('previews the server subject then applies only the plan id and lock version', async () => {
    api.dryRunRecordImport.mockResolvedValue(planFor('vps_a', {
      quarantine: [{ kind: 'vendor.unknown', schema: 'vendor.unknown/v1', digest: 'aa', byte_size: 8, reason: 'cannot interpret' }],
      object_count: 2,
    }))
    api.applyRecordImport.mockResolvedValue({
      plan_id: 'rip_1',
      job_state: 'applied',
      record_ids: ['rec_local01', 'rec_local02'],
    })
    renderPanel()
    await prepareImport()
    expect(screen.getByText(/技术标识/)).toHaveTextContent('vps_a')
    expect(screen.getByRole('button', { name: '重新预检' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('服务器确认：边缘甲（VPS vps_a）')).toBeInTheDocument()
    expect(screen.getByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    expect(screen.getByText('vendor.unknown vendor.unknown/v1：cannot interpret')).toBeInTheDocument()
    expect(screen.getByText('有 1 项证据已隔离，仅展示信封，不会当作可信证据。')).toBeInTheDocument()
    expect(api.dryRunRecordImport).toHaveBeenCalledWith(
      expect.any(File),
      { subject_kind: 'vps', subject_id: 'vps_a' },
      expect.any(String),
      expect.any(AbortSignal),
    )
    expect(screen.getByRole('button', { name: '预检导入' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重新预检' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByText('已导入 2 条记录')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看记录 rec_local01' })).toHaveAttribute('href', '/records/rec_local01')
    expect(screen.getByRole('link', { name: '查看记录 rec_local02' })).toHaveAttribute('href', '/records/rec_local02')
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重新预检' })).toBeDisabled()
    expect(api.applyRecordImport).toHaveBeenCalledTimes(1)
    expect(api.applyRecordImport).toHaveBeenCalledWith('rip_1', 2, expect.any(AbortSignal))
    expect(api.applyRecordImport.mock.calls[0]).toHaveLength(3)
  })

  it('keeps the idempotency key when the same file and subject are retried', async () => {
    api.dryRunRecordImport
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(planFor())
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('导入失败，请重试。')).toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')
    expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('服务器确认：边缘甲（VPS vps_a）')).toBeInTheDocument()
    expect(api.dryRunRecordImport.mock.calls[0]?.[2]).toBe(api.dryRunRecordImport.mock.calls[1]?.[2])
    expect(api.dryRunRecordImport.mock.calls[1]?.[1]).toEqual({ subject_kind: 'vps', subject_id: 'vps_a' })
  })

  it('rotates the key for an explicit re-preview and for a conflicting plan', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport
      .mockResolvedValueOnce(planFor())
      .mockResolvedValueOnce(planFor())
      .mockRejectedValueOnce(new ApiError(409, 'conflict', { code: 'import_cas_conflict' }))
      .mockResolvedValueOnce(planFor())
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('服务器确认：边缘甲（VPS vps_a）')).toBeInTheDocument()

    await waitFor(() => expect(screen.getByRole('button', { name: '重新预检' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '重新预检' }))
    await waitFor(() => expect(api.dryRunRecordImport).toHaveBeenCalledTimes(2))
    expect(api.dryRunRecordImport.mock.calls[0]?.[2]).not.toBe(api.dryRunRecordImport.mock.calls[1]?.[2])
    expect(api.dryRunRecordImport.mock.calls[1]?.[1]).toEqual({ subject_kind: 'vps', subject_id: 'vps_a' })

    await waitFor(() => expect(screen.getByRole('button', { name: '重新预检' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '重新预检' }))
    expect(await screen.findByText('导入计划已变化，请重新预检。')).toBeInTheDocument()
    expect(screen.queryByText('服务器确认：边缘甲（VPS vps_a）')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')

    await waitFor(() => expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    await waitFor(() => expect(api.dryRunRecordImport).toHaveBeenCalledTimes(4))
    expect(api.dryRunRecordImport.mock.calls[2]?.[2]).not.toBe(api.dryRunRecordImport.mock.calls[3]?.[2])
  })

  it('drops a dry-run that resolves after the file changes and disables the controls while it is pending', async () => {
    let resolveDry: (value: ReturnType<typeof planFor>) => void = () => {}
    api.dryRunRecordImport.mockImplementation(() => new Promise((resolve) => {
      resolveDry = resolve
    }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    await waitFor(() => expect(api.dryRunRecordImport).toHaveBeenCalledTimes(1))
    expect(screen.getByLabelText('主体类型')).toBeDisabled()
    expect(screen.getByLabelText('目标主体')).toBeDisabled()
    expect(screen.getByLabelText('归档文件')).toBeDisabled()

    fireEvent.change(screen.getByLabelText('归档文件'), {
      target: { files: [new File(['Q'], 'other.zip', { type: 'application/zip' })] },
    })
    await act(async () => {
      resolveDry(planFor())
      await Promise.resolve()
    })

    expect(screen.queryByText(/服务器确认/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
  })

  it('aborts the dry-run request when the panel unmounts', async () => {
    let signal: AbortSignal | undefined
    api.dryRunRecordImport.mockImplementation((_file: File, _destination: unknown, _key: string, next?: AbortSignal) => {
      signal = next
      return new Promise(() => {})
    })
    const view = renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    await waitFor(() => expect(signal).toBeInstanceOf(AbortSignal))
    view.unmount()
    expect(signal?.aborted).toBe(true)
  })

  it('clears the plan when the subject changes so the previous plan cannot be applied', async () => {
    inventory.listVPSAssets.mockResolvedValue([vps('vps_a', '边缘甲'), vps('vps_b', '边缘乙')])
    api.dryRunRecordImport.mockResolvedValue(planFor('vps_a'))
    renderPanel()
    fireEvent.change(screen.getByLabelText('主体类型'), { target: { value: 'vps' } })
    const subject = await screen.findByLabelText('目标主体')
    fireEvent.change(subject, { target: { value: 'vps_a' } })
    chooseFile()
    await waitFor(() => expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('目标主体'), { target: { value: 'vps_b' } })
    expect(screen.queryByText('record rec_source01 → rec_local01')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    await waitFor(() => expect(api.dryRunRecordImport).toHaveBeenCalledTimes(2))
    expect(api.dryRunRecordImport.mock.calls[1]?.[1]).toEqual({ subject_kind: 'vps', subject_id: 'vps_b' })
    expect(api.dryRunRecordImport.mock.calls[0]?.[2]).not.toBe(api.dryRunRecordImport.mock.calls[1]?.[2])
  })

  it('does not attach the local name when the echoed subject differs', async () => {
    api.dryRunRecordImport.mockResolvedValue(planFor('vps_a', {
      destination_subject: { subject_kind: 'vps', subject_id: 'vps_other' },
    }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('服务器确认：VPS（VPS vps_other）')).toBeInTheDocument()
    expect(screen.queryByText('服务器确认：边缘甲（VPS vps_other）')).not.toBeInTheDocument()
  })

  it('names tombstoned origins instead of pretending restore succeeded', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockRejectedValue(new ApiError(409, 'tombstoned', { code: 'origin_tombstoned' }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('该来源已墓碑化，不能官方恢复或再导入。')).toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')
    expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled()
  })

  it('names an already-imported origin on dry-run instead of offering apply', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockRejectedValue(new ApiError(409, 'already imported', { code: 'import_origin_conflict' }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('该归档已导入过，不能再次官方导入。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
    expect(screen.getByLabelText('主体类型')).toHaveValue('vps')
  })

  it('names an already-imported origin when apply is rejected and keeps the selection', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockResolvedValue(planFor())
    api.applyRecordImport.mockRejectedValue(new ApiError(409, 'already imported', { code: 'import_origin_conflict' }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByText('该归档已导入过，不能再次官方导入。')).toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')
    expect(screen.getByText('record rec_source01 → rec_local01')).toBeInTheDocument()
  })

  it('keeps the file and subject when an opaque 404 rejects the preview', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockRejectedValue(new ApiError(404, 'missing', { code: 'resource_not_found' }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('无权访问或主体不可用。')).toBeInTheDocument()
    expect(screen.queryByText(/不存在|已删除/)).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '导入计划已删除' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')
    await waitFor(() => expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled())
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
  })

  it('requires a new key after an applied plan disappears and still keeps the selection', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockResolvedValue(planFor())
    api.applyRecordImport.mockRejectedValue(new ApiError(404, 'gone', { code: 'resource_not_found' }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByText('无权访问或主体不可用。')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '导入计划已删除' })).not.toBeInTheDocument()
    expect(screen.queryByText('record rec_source01 → rec_local01')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')

    await waitFor(() => expect(screen.getByRole('button', { name: '预检导入' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    await waitFor(() => expect(api.dryRunRecordImport).toHaveBeenCalledTimes(2))
    expect(api.dryRunRecordImport.mock.calls[0]?.[2]).not.toBe(api.dryRunRecordImport.mock.calls[1]?.[2])
    expect(api.dryRunRecordImport.mock.calls[1]?.[1]).toEqual({ subject_kind: 'vps', subject_id: 'vps_a' })
  })

  it('retries the same plan when the apply response is lost', async () => {
    api.dryRunRecordImport.mockResolvedValue(planFor())
    api.applyRecordImport
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce({ plan_id: 'rip_1', job_state: 'applied', record_ids: ['rec_local01'] })
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByText('导入失败，请重试。')).toBeInTheDocument()
    expect(screen.getByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByRole('link', { name: '查看记录 rec_local01' })).toHaveAttribute('href', '/records/rec_local01')
    expect(api.applyRecordImport).toHaveBeenNthCalledWith(1, 'rip_1', 2, expect.any(AbortSignal))
    expect(api.applyRecordImport).toHaveBeenNthCalledWith(2, 'rip_1', 2, expect.any(AbortSignal))
    expect(api.applyRecordImport.mock.calls[0]).toHaveLength(3)
  })

  it('says the import service is unavailable without discarding the plan', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockResolvedValue(planFor())
    api.applyRecordImport.mockRejectedValue(new ApiError(503, 'unavailable', { code: 'export_unavailable' }))
    renderPanel()
    await prepareImport()
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByText('导入服务暂不可用，请重试。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeEnabled()
    expect(screen.getByLabelText('目标主体')).toHaveValue('vps_a')
  })
})
