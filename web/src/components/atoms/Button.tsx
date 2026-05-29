import { type ButtonHTMLAttributes, forwardRef } from 'react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(({
  variant = 'primary',
  size = 'md',
  className = '',
  type = 'button',
  ...rest
}, ref) => {
  const classes = ['btn', size, variant, className].filter(Boolean).join(' ')
  return <button ref={ref} type={type} className={classes} {...rest} />
})
Button.displayName = 'Button'
