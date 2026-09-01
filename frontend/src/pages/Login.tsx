import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../store/auth'
import { ApiError } from '../api/client'

export default function Login() {
  const { t } = useTranslation()
  const { login } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await login(email, password)
      navigate('/calendar')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="card w-full max-w-sm p-6">
        <div className="mb-6 text-center">
          <div className="text-2xl font-bold">{t('app.title')}</div>
          <div className="mt-1 text-sm text-[var(--app-muted)]">{t('auth.welcome')}</div>
        </div>
        <form onSubmit={submit} className="space-y-4">
          <div>
            <label className="mb-1 block text-sm text-[var(--app-muted)]">{t('auth.email')}</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="input"
              autoComplete="email"
              required
            />
          </div>
          <div>
            <label className="mb-1 block text-sm text-[var(--app-muted)]">{t('auth.password')}</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="input"
              autoComplete="current-password"
              required
            />
          </div>
          {error && <div className="text-sm text-[var(--app-danger)]">{error}</div>}
          <button type="submit" className="btn-primary w-full" disabled={busy}>
            {t('auth.login')}
          </button>
        </form>
        <div className="mt-4 text-center text-sm">
          {t('auth.noAccount')}{' '}
          <Link to="/register" className="font-medium text-[var(--app-accent)]">
            {t('auth.register')}
          </Link>
        </div>
      </div>
    </div>
  )
}
