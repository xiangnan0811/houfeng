export type StepState = 'pending' | 'current' | 'done' | 'error'

export type StepperStep = {
  label: string
  state: StepState
}

export interface StepperProps {
  steps: StepperStep[]
  ariaLabel?: string
  className?: string
}

const STATE_COLOR: Record<StepState, string> = {
  pending: 'var(--color-state-offline)',
  current: 'var(--accent)',
  done: 'var(--color-state-normal)',
  error: 'var(--color-state-critical)',
}

const STATE_LABEL: Record<StepState, string> = {
  pending: '待进行',
  current: '进行中',
  done: '已完成',
  error: '出错',
}

function StepDot({ state }: { state: StepState }) {
  const color = STATE_COLOR[state]
  // current state: extra outer ring + thicker stroke; others: filled circle
  if (state === 'current') {
    return (
      <svg
        className="stepper__dot"
        width={14}
        height={14}
        viewBox="0 0 14 14"
        role="img"
        aria-hidden="true"
      >
        <circle cx={7} cy={7} r={5.5} fill="none" stroke={color} strokeWidth={1.5} />
        <circle cx={7} cy={7} r={3} fill={color} />
      </svg>
    )
  }
  if (state === 'done') {
    return (
      <svg
        className="stepper__dot"
        width={14}
        height={14}
        viewBox="0 0 14 14"
        role="img"
        aria-hidden="true"
      >
        <circle cx={7} cy={7} r={5} fill={color} />
      </svg>
    )
  }
  if (state === 'error') {
    return (
      <svg
        className="stepper__dot"
        width={14}
        height={14}
        viewBox="0 0 14 14"
        role="img"
        aria-hidden="true"
      >
        <circle cx={7} cy={7} r={5} fill={color} />
        <path d="M4 4 L10 10 M10 4 L4 10" stroke="var(--bg)" strokeWidth={1.4} />
      </svg>
    )
  }
  // pending: hollow circle
  return (
    <svg
      className="stepper__dot"
      width={14}
      height={14}
      viewBox="0 0 14 14"
      role="img"
      aria-hidden="true"
    >
      <circle cx={7} cy={7} r={5} fill="none" stroke={color} strokeWidth={1.4} />
    </svg>
  )
}

export function Stepper({ steps, ariaLabel, className }: StepperProps) {
  if (steps.length === 0) {
    return null
  }

  const cls = ['stepper', className].filter(Boolean).join(' ')

  return (
    <ol
      className={cls}
      role="list"
      aria-label={ariaLabel ?? `流程进度 (${steps.length} 步)`}
    >
      {steps.map((step, i) => {
        const stepCls = [
          'stepper__step',
          `stepper__step--${step.state}`,
        ].join(' ')
        // Connector reflects this step's state (line trailing each step except last)
        const isLast = i === steps.length - 1
        return (
          <li
            key={`${i}-${step.label}`}
            className={stepCls}
            aria-label={`${step.label} · ${STATE_LABEL[step.state]}`}
          >
            {!isLast ? (
              <span
                className="stepper__connector"
                aria-hidden="true"
              />
            ) : null}
            <StepDot state={step.state} />
            <span className="stepper__label">{step.label}</span>
          </li>
        )
      })}
    </ol>
  )
}
