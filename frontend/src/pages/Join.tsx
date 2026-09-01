import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Users, Check } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { useAuth } from '../store/auth'

export default function Join() {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const code = params.get('code') ?? ''
  const { token, login, register } = useAuth()
  const navigate = useNavigate()

  const [info, setInfo] = useState<{ teamName: string; role: string } | null>(null)
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [joined, setJoined] = useState(false)

  useEffect(() => {
    void (async () => {
      try {
        const r = await api.get<{ teamName: string; role: string }>(`/api/v1/invites/info?token=${encodeURIComponent(code)}`)
        setInfo(r)
      } catch {
        setError(t('error.invite_invalid'))
      }
    })()
  }, [code, t])

  // If already logged in, join directly.
  useEffect(() => {
    if (token && info) {
      void (async () => {
        try {
          await api.post('/api/v1/invites/join', { code })
          setJoined(true)
          await new Promise((r) => setTimeout(r, 800))
          navigate('/calendar')
        } catch (err) {
          setError(err instanceof ApiError ? err.message : t('common.error'))
        }
      })()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, info])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      if (mode === 'register') {
        await register({ name, email, password, teamName: '' })
      } else {
        await login(email, password)
      }
      const r = await api.post('/api/v1/invites/join', { code })
      if (r) setJoined(true)
      await new Promise((res) => setTimeout(res, 800))
      navigate('/calendar')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('common.error'))
    } finally {
      setBusy(false)
    }
  }

  if (error && !info) {
    return (
      <div className="flex min-h-screen items-center justify-center p-4">
        <div className="card w-full max-w-sm p-6 text-center">
          <div className="mb-3 text-2xl">🤝</div>
          <div className="mb-2 font-semibold">{t('error.invite_invalid')}</div>
          <Link to="/login" className="font-medium text-[var(--app-accent)]">
            {t('auth.login')}
          </Link>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="card w-full max-w-sm p-6">
        {joined ? (
          <div className="py-8 text-center">
            <Check size={40} className="mx-auto mb-3 text-[var(--app-accent)]" />
            <div className="font-semibold">{t('team.joined')}</div>
          </div>
        ) : (
          <>
            <div className="mb-6 text-center">
              <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-full bg-[var(--app-accent-soft)]">
                <Users size={24} className="text-[var(--app-accent)]" />
              </div>
              <div className="text-lg font-semibold">{info?.teamName}</div>
              <div className="mt-1 text-sm text-[var(--app-muted)]">{t('team.joinTeam')}</div>
            </div>
            <div className="mb-4 flex overflow-hidden rounded-lg border border-[var(--app-border)]">
              {(['login', 'register'] as const).map((m) => (
                <button
                  key={m}
                  onClick={() => { setMode(m); setError('') }}
                  className={`flex-1 py-2 text-sm ${mode === m ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--app-card)] text-[var(--app-muted)]'}`}
                >
                  {m === 'login' ? t('auth.login') : t('auth.register')}
                </button>
              ))}
            </div>
            <form onSubmit={submit} className="space-y-3">
              {mode === 'register' && (
                <input
                  className="input"
                  placeholder={t('auth.name')}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                />
              )}
              <input
                type="email"
                className="input"
                placeholder={t('auth.email')}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
              <input
                type="password"
                className="input"
                placeholder={t('auth.password')}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
              {error && <div className="text-sm text-[var(--app-danger)]">{error}</div>}
              <button type="submit" className="btn-primary w-full" disabled={busy}>
                {mode === 'register' ? t('auth.register') : t('auth.login')}
              </button>
            </form>
          </>
        )}
      </div>
    </div>
  )
}
