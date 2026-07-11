import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Stepper, type StepperStep } from './Stepper'

describe('Stepper', () => {
  it('renders all step labels', () => {
    const steps: StepperStep[] = [
      { label: '未开始接入', state: 'current' },
      { label: '等待绑定', state: 'pending' },
      { label: '等待稳定观测', state: 'pending' },
      { label: '接入完成', state: 'pending' },
    ]
    render(<Stepper steps={steps} />)
    expect(screen.getByText('未开始接入')).toBeInTheDocument()
    expect(screen.getByText('等待绑定')).toBeInTheDocument()
    expect(screen.getByText('等待稳定观测')).toBeInTheDocument()
    expect(screen.getByText('接入完成')).toBeInTheDocument()
  })

  it('applies the per-state modifier class to each step', () => {
    const steps: StepperStep[] = [
      { label: 'A', state: 'done' },
      { label: 'B', state: 'current' },
      { label: 'C', state: 'pending' },
      { label: 'D', state: 'error' },
    ]
    const { container } = render(<Stepper steps={steps} />)
    const items = container.querySelectorAll('.stepper__step')
    expect(items.length).toBe(4)
    const [doneStep, currentStep, pendingStep, errorStep] = items
    if (!doneStep || !currentStep || !pendingStep || !errorStep) {
      throw new Error('stepper must render all configured steps')
    }
    expect(doneStep.classList.contains('stepper__step--done')).toBe(true)
    expect(currentStep.classList.contains('stepper__step--current')).toBe(true)
    expect(pendingStep.classList.contains('stepper__step--pending')).toBe(true)
    expect(errorStep.classList.contains('stepper__step--error')).toBe(true)
  })

  it('renders error state with critical color circle', () => {
    const steps: StepperStep[] = [{ label: '冲突', state: 'error' }]
    const { container } = render(<Stepper steps={steps} />)
    const errorStep = container.querySelector('.stepper__step--error')
    expect(errorStep).toBeTruthy()
    if (!errorStep) throw new Error('error step must be rendered')
    // The dot SVG should be present inside the step
    expect(errorStep.querySelector('svg.stepper__dot')).toBeTruthy()
  })

  it('returns null when given an empty steps array', () => {
    const { container } = render(<Stepper steps={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('uses the provided aria-label when supplied', () => {
    const steps: StepperStep[] = [{ label: 'A', state: 'current' }]
    render(<Stepper steps={steps} ariaLabel="监控实例接入进度" />)
    expect(screen.getByRole('list', { name: '监控实例接入进度' })).toBeInTheDocument()
  })

  it('renders a connector for every step except the last', () => {
    const steps: StepperStep[] = [
      { label: 'A', state: 'done' },
      { label: 'B', state: 'current' },
      { label: 'C', state: 'pending' },
    ]
    const { container } = render(<Stepper steps={steps} />)
    const connectors = container.querySelectorAll('.stepper__connector')
    expect(connectors.length).toBe(steps.length - 1)
    expect(container.querySelector('[style]')).not.toBeInTheDocument()
  })
})
