import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '../lib/auth-context'

export function LoginPage() {
  const { login } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()
  const [params] = useSearchParams()

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(username, password)
      const next = params.get('next') ?? '/'
      navigate(next, { replace: true })
    } catch {
      setError('用户名或密码不正确')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <div className="lc-logo">
          <div className="logo-mark logo-mark--lg">
            <svg viewBox="0 0 24 24">
              <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5" />
            </svg>
          </div>
        </div>
        <div className="lc-title">候风控制面板</div>
        <div className="lc-sub">Fleet Control Plane</div>
        {error && <p className="login-page__error" role="alert">{error}</p>}
        <div className="lc-field">
          <label htmlFor="login-username">用户名</label>
          <input id="login-username" type="text" autoComplete="username" placeholder="admin" value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className="lc-field">
          <label htmlFor="login-password">密码</label>
          <input id="login-password" type="password" autoComplete="current-password" placeholder="••••••••" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        <button type="submit" className="lc-btn" disabled={submitting}>
          {submitting ? '登录中…' : '登录'}
        </button>
      </form>
    </div>
  )
}
