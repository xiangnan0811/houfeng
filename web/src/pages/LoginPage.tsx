import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Button, Input } from '../components/atoms'
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
    } catch {
      setError('用户名或密码不正确')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-page__seal" aria-hidden>
        <span>候</span>
      </div>
      <form className="login-page__card" onSubmit={onSubmit}>
        <header className="login-page__brand">
          <div className="login-page__brand-zh">候风</div>
          <div className="login-page__brand-en">FLEET CONTROL PLANE</div>
          <div className="login-page__motto">察 变 · 守 望</div>
        </header>
        {error && (
          <div role="alert" className="login-page__error">
            {error}
          </div>
        )}
        <Input
          label="用户名"
          autoComplete="username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
        <Input
          label="密码"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Button type="submit" disabled={submitting} variant="primary">
          登 录
        </Button>
        <footer className="login-page__footer">v1.0</footer>
      </form>
    </div>
  )
}
