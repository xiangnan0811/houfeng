import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../lib/api'
import { useAuth } from '../lib/auth-context'
import './LoginPage.css'

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
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        setError('用户名或密码不正确')
      } else {
        setError('登录服务异常，请检查服务状态或稍后重试')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-page__seal" aria-hidden="true">
        <span>候</span>
      </div>
      <form className="login-page__card" onSubmit={onSubmit}>
        <div className="login-page__brand">
          <div className="login-page__brand-zh">候风控制面板</div>
          <div className="login-page__brand-en">Fleet Control Plane</div>
          <div className="login-page__motto">观风测候 · 守界安服</div>
        </div>
        {error && <p className="login-page__error" role="alert">{error}</p>}
        <div className="login-page__field">
          <label htmlFor="login-username">用户名</label>
          <input
            id="login-username"
            type="text"
            autoComplete="username"
            placeholder="admin"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="login-page__field">
          <label htmlFor="login-password">密码</label>
          <input
            id="login-password"
            type="password"
            autoComplete="current-password"
            placeholder="••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <button type="submit" className="lc-btn" disabled={submitting}>
          {submitting ? '登录中…' : '登录'}
        </button>
      </form>
      <div className="login-page__footer">观测入口 · 仅授权人员</div>
    </div>
  )
}
