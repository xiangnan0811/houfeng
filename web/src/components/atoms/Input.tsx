import { forwardRef, type InputHTMLAttributes, type ReactNode, useId } from 'react'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  hint?: string
  leadingIcon?: ReactNode
  trailingIcon?: ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, hint, leadingIcon, trailingIcon, id, className = '', ...rest },
  ref,
) {
  const fallbackId = useId()
  const inputId = id ?? fallbackId
  const cls = ['input', error ? 'input--error' : '', className].filter(Boolean).join(' ')
  return (
    <div className="input-field">
      {label && (
        <label className="input-field__label" htmlFor={inputId}>
          {label}
        </label>
      )}
      <div className="input-field__shell">
        {leadingIcon && <span className="input-field__prefix">{leadingIcon}</span>}
        <input ref={ref} id={inputId} className={cls} {...rest} />
        {trailingIcon && <span className="input-field__suffix">{trailingIcon}</span>}
      </div>
      {error ? (
        <div className="input-field__error">{error}</div>
      ) : hint ? (
        <div className="input-field__hint">{hint}</div>
      ) : null}
    </div>
  )
})
