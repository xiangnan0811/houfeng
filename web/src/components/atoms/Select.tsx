import { forwardRef, type ReactNode, type SelectHTMLAttributes, useId } from 'react'
import { mergeAriaTokens } from './fieldA11y'

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
  {
    label,
    error,
    hint,
    required,
    options,
    children,
    id,
    className = '',
    'aria-describedby': describedBy,
    'aria-invalid': ariaInvalid,
    ...rest
  },
  ref,
) {
  const fallbackId = useId()
  const selectId = id ?? fallbackId
  const errorId = `${selectId}-error`
  const hintId = `${selectId}-hint`
  const descriptionId = error ? errorId : hint ? hintId : undefined
  const mergedDescribedBy = mergeAriaTokens(describedBy, descriptionId)
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
        <select
          ref={ref}
          id={selectId}
          className={cls}
          required={required}
          aria-describedby={mergedDescribedBy}
          aria-invalid={error ? true : ariaInvalid}
          {...rest}
        >
          {options ? options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>) : children}
        </select>
      </div>
      {error ? (
        <div id={errorId} className="input-field__error">{error}</div>
      ) : hint ? (
        <div id={hintId} className="input-field__hint">{hint}</div>
      ) : null}
    </div>
  )
})
