import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  TargetProbeForm,
  type ProbeCreateFormState,
} from './TargetProbeForm'

function form(overrides: Partial<ProbeCreateFormState> = {}): ProbeCreateFormState {
  return {
    probeKind: 'tcp',
    enabled: true,
    frequencyTier: '5s',
    timeoutSeconds: '5',
    port: '443',
    httpScheme: 'https',
    httpPath: '/',
    httpMethod: 'GET',
    expectedStatusStart: '200',
    expectedStatusEnd: '299',
    tlsExpiryWarningDays: '14',
    ...overrides,
  }
}

const noopHandlers = {
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => event.preventDefault(),
  onProbeKindChange: () => {},
  onFieldChange: () => {},
}

describe('TargetProbeForm', () => {
  it('renders TCP-specific fields and the create button when mode is create', () => {
    render(
      <TargetProbeForm
        mode={{ kind: 'create' }}
        form={form()}
        submitting={false}
        error={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('ProbeItem 创建')).toBeInTheDocument()
    expect(screen.getByLabelText('端口')).toBeInTheDocument()
    expect(screen.getByLabelText('频率档位')).toHaveValue('5s')
    expect(screen.getByRole('button', { name: '创建 ProbeItem' })).toBeInTheDocument()
  })

  it('renders HTTP-specific fields when probeKind is http', () => {
    render(
      <TargetProbeForm
        mode={{ kind: 'create' }}
        form={form({ probeKind: 'http' })}
        submitting={false}
        error={null}
        {...noopHandlers}
      />,
    )

    expect(screen.getByLabelText('HTTP 协议')).toBeInTheDocument()
    expect(screen.getByLabelText('HTTP 路径')).toBeInTheDocument()
    expect(screen.getByLabelText('期望状态码起点')).toHaveValue('200')
    expect(screen.queryByLabelText('端口')).not.toBeInTheDocument()
  })

  it('shows the edit button copy and the inline error when in edit mode with an error', () => {
    render(
      <TargetProbeForm
        mode={{ kind: 'edit', probeItemId: 'pb_001' }}
        form={form()}
        submitting={false}
        error="端口必须为正整数。"
        {...noopHandlers}
      />,
    )

    expect(screen.getByText('ProbeItem 编辑')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存 ProbeItem' })).toBeInTheDocument()
    expect(screen.getByText('端口必须为正整数。')).toBeInTheDocument()
  })

  it('invokes onProbeKindChange with the selected probe kind value', () => {
    const onProbeKindChange = vi.fn()
    render(
      <TargetProbeForm
        mode={{ kind: 'create' }}
        form={form()}
        submitting={false}
        error={null}
        {...noopHandlers}
        onProbeKindChange={onProbeKindChange}
      />,
    )

    fireEvent.change(screen.getByLabelText('Probe 类型'), { target: { value: 'tls' } })
    expect(onProbeKindChange).toHaveBeenCalledWith('tls')
  })
})
