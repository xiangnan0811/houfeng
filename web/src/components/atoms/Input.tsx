import { forwardRef, type InputHTMLAttributes, type ReactNode, useId } from 'react'
import { mergeAriaTokens } from './fieldA11y'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  hint?: string
  leadingIcon?: ReactNode
  trailingIcon?: ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  {
    label,
    error,
    hint,
    leadingIcon,
    trailingIcon,
    id,
    className = '',
    required,
    'aria-describedby': describedBy,
    'aria-invalid': ariaInvalid,
    ...rest
  },
  ref,
) {
  const fallbackId = useId()
  const inputId = id ?? fallbackId
  const errorId = `${inputId}-error`
  const hintId = `${inputId}-hint`
  const descriptionId = error ? errorId : hint ? hintId : undefined
  const mergedDescribedBy = mergeAriaTokens(describedBy, descriptionId)
  const cls = ['input', error ? 'input--error' : '', className].filter(Boolean).join(' ')
  const labelCls = ['input-field__label', required ? 'input-field__label--required' : '']
    .filter(Boolean)
    .join(' ')
  return (
    <div className="input-field">
      {label && (
        <label className={labelCls} htmlFor={inputId}>
          {label}
        </label>
      )}
      <div className="input-field__shell">
        {leadingIcon && <span className="input-field__prefix">{leadingIcon}</span>}
        <input
          ref={ref}
          id={inputId}
          className={cls}
          required={required}
          aria-describedby={mergedDescribedBy}
          aria-invalid={error ? true : ariaInvalid}
          {...rest}
        />
        {trailingIcon && <span className="input-field__suffix">{trailingIcon}</span>}
      </div>
      {error ? (
        <div id={errorId} className="input-field__error">{error}</div>
      ) : hint ? (
        <div id={hintId} className="input-field__hint">{hint}</div>
      ) : null}
    </div>
  )
})
