import { forwardRef, type ReactNode, type SelectHTMLAttributes, useId } from 'react'

export interface SelectOption {
  value: string
  label: string
}

export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  error?: string
  hint?: ReactNode
  required?: boolean
  options?: SelectOption[]
  children?: ReactNode
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { label, error, hint, required, options, children, id, className = '', ...rest },
  ref,
) {
  const fallbackId = useId()
  const selectId = id ?? fallbackId
  const cls = ['input', error ? 'input--error' : '', className].filter(Boolean).join(' ')
  const labelCls = ['input-field__label', required ? 'input-field__label--required' : '']
    .filter(Boolean)
    .join(' ')
  return (
    <div className="input-field">
      {label && (
        <label className={labelCls} htmlFor={selectId}>
          {label}
        </label>
      )}
      <div className="input-field__shell">
        <select ref={ref} id={selectId} className={cls} {...rest}>
          {options ? options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>) : children}
        </select>
      </div>
      {error ? (
        <div className="input-field__error">{error}</div>
      ) : hint ? (
        <div className="input-field__hint">{hint}</div>
      ) : null}
    </div>
  )
})
