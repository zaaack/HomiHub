import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../store/auth'
import { ApiError } from '../api/client'

export default function Register() {
  const { t } = useTranslation()
  const { register } = useAuth()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [teamName, setTeamName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await register({ name, email, password, teamName })
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
          <div className="mt-1 text-sm text-[var(--app-muted)]">{t('auth.createTeam')}</div>
        </div>
        <form onSubmit={submit} className="space-y-4">
          <div>
            <label className="mb-1 block text-sm text-[var(--app-muted)]">{t('auth.name')}</label>
            <input value={name} onChange={(e) => setName(e.target.value)} className="input" required />
          </div>
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
              autoComplete="new-password"
              required
            />
          </div>
          <div>
            <label className="mb-1 block text-sm text-[var(--app-muted)]">{t('auth.teamName')}</label>
            <input
              value={teamName}
              onChange={(e) => setTeamName(e.target.value)}
              className="input"
              placeholder={t('auth.teamNamePlaceholder')}
            />
          </div>
          {error && <div className="text-sm text-[var(--app-danger)]">{error}</div>}
          <button type="submit" className="btn-primary w-full" disabled={busy}>
            {t('auth.register')}
          </button>
        </form>
        <div className="mt-4 text-center text-sm">
          {t('auth.hasAccount')}{' '}
          <Link to="/login" className="font-medium text-[var(--app-accent)]">
            {t('auth.login')}
          </Link>
        </div>
      </div>
    </div>
  )
}
