import { forwardRef, type InputHTMLAttributes, type ReactNode, useId } from 'react'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  hint?: string
  prefix?: ReactNode
  suffix?: ReactNode
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, hint, prefix, suffix, id, className = '', ...rest },
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
        {prefix && <span className="input-field__prefix">{prefix}</span>}
        <input ref={ref} id={inputId} className={cls} {...rest} />
        {suffix && <span className="input-field__suffix">{suffix}</span>}
      </div>
      {error ? (
        <div className="input-field__error">{error}</div>
      ) : hint ? (
        <div className="input-field__hint">{hint}</div>
      ) : null}
    </div>
  )
})
