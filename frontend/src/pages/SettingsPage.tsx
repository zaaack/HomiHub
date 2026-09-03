import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Save, ShieldCheck, Key, Plus, Trash2, Monitor, AlertCircle } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'

interface TokenEntry {
  id: string
  kind: string
  name: string
  ip: string
  userAgent: string
  expiresAt: string | null
  lastUsedAt: string | null
  createdAt: string
}

interface AppPasswordCreated {
  id: string
  name: string
  token: string
  createdAt: string
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [origins, setOrigins] = useState('')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)

  const [appPasswords, setAppPasswords] = useState<TokenEntry[]>([])
  const [newAppName, setNewAppName] = useState('')
  const [createdPassword, setCreatedPassword] = useState<AppPasswordCreated | null>(null)

  const [sessions, setSessions] = useState<TokenEntry[]>([])

  const loadAppPasswords = useCallback(async () => {
    try {
      const list = await api.get<TokenEntry[]>('/api/v1/auth/app-passwords')
      setAppPasswords(list)
    } catch { /* ignore */ }
  }, [])

  const loadSessions = useCallback(async () => {
    try {
      const list = await api.get<TokenEntry[]>('/api/v1/auth/tokens')
      setSessions(list)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    void api
      .get<{ origins: string }>('/api/v1/settings/cors')
      .then((r) => setOrigins(r.origins ?? ''))
      .catch(() => {})
    void loadAppPasswords()
    void loadSessions()
  }, [loadAppPasswords, loadSessions])

  const save = async () => {
    setBusy(true)
    setSaved(false)
    try {
      await api.put('/api/v1/settings/cors', { origins })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } finally {
      setBusy(false)
    }
  }

  const createAppPassword = async () => {
    setBusy(true)
    try {
      const r = await api.post<AppPasswordCreated>('/api/v1/auth/app-passwords', { name: newAppName || 'App Password' })
      setCreatedPassword(r)
      setNewAppName('')
      await loadAppPasswords()
    } finally {
      setBusy(false)
    }
  }

  const revokeAppPassword = async (id: string) => {
    if (!window.confirm(t('common.confirm'))) return
    setBusy(true)
    try {
      await api.del(`/api/v1/auth/app-passwords/${id}`)
      await loadAppPasswords()
    } finally {
      setBusy(false)
    }
  }

  const revokeSession = async (id: string) => {
    if (!window.confirm(t('common.confirm'))) return
    setBusy(true)
    try {
      await api.del(`/api/v1/auth/tokens/${id}`)
      await loadSessions()
    } finally {
      setBusy(false)
    }
  }

  const isCurrentSession = (ua: string) => {
    return ua === navigator.userAgent
  }

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-xl font-semibold">{t('settings.title')}</h1>

      {user?.role === 'parent' && (
        <div className="card p-4">
          <div className="mb-3 flex items-center gap-2 text-base font-semibold">
            <ShieldCheck size={18} />
            {t('settings.corsTitle')}
          </div>
          <p className="mb-3 text-xs text-[var(--app-muted)]">{t('settings.corsHint')}</p>
          <textarea
            className="input min-h-[80px] w-full resize-y"
            value={origins}
            onChange={(e) => setOrigins(e.target.value)}
            placeholder="https://app.example.com, https://calendar.example.com"
          />
          <div className="mt-3 flex items-center gap-3">
            <button className="btn-primary" onClick={() => void save()} disabled={busy}>
              <Save size={16} />
              {t('common.save')}
            </button>
            {saved && <span className="text-sm text-[var(--app-accent)]">{t('settings.saved')}</span>}
          </div>
        </div>
      )}

      {/* App Passwords */}
      <div className="card p-4">
        <div className="mb-3 flex items-center gap-2 text-base font-semibold">
          <Key size={18} />
          {t('settings.appPasswords')}
        </div>
        <p className="mb-3 text-xs text-[var(--app-muted)]">{t('settings.appPasswordsHint')}</p>

        {createdPassword && (
          <div className="mb-4 rounded-lg border border-[var(--app-accent)] bg-[var(--app-accent-soft)] p-3">
            <div className="mb-2 flex items-center gap-2 text-sm font-medium text-[var(--app-accent)]">
              <AlertCircle size={16} />
              {t('settings.appPasswordCreated')}
            </div>
            <div className="mb-2 text-sm">
              <span className="text-[var(--app-muted)]">{t('settings.appName')}:</span>{' '}
              {createdPassword.name}
            </div>
            <input
              className="input w-full text-sm"
              readOnly
              value={createdPassword.token}
              onFocus={(e) => e.target.select()}
            />
            <button
              className="btn-ghost mt-2 text-sm"
              onClick={() => setCreatedPassword(null)}
            >
              {t('common.close')}
            </button>
          </div>
        )}

        <div className="mb-3 flex items-center gap-2">
          <input
            className="input flex-1"
            placeholder={t('settings.appNamePlaceholder')}
            value={newAppName}
            onChange={(e) => setNewAppName(e.target.value)}
          />
          <button className="btn-primary shrink-0" onClick={() => void createAppPassword()} disabled={busy}>
            <Plus size={16} />
            {t('settings.createAppPassword')}
          </button>
        </div>

        {appPasswords.length === 0 ? (
          <div className="text-sm text-[var(--app-muted)]">{t('settings.noAppPasswords')}</div>
        ) : (
          <div className="divide-y divide-[var(--app-border)]">
            {appPasswords.map((p) => (
              <div key={p.id} className="flex items-center gap-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{p.name}</div>
                  <div className="truncate text-xs text-[var(--app-muted)]">
                    {p.ip} &middot; {new Date(p.createdAt).toLocaleDateString()}
                  </div>
                </div>
                <button
                  className="btn-ghost text-[var(--app-danger)]"
                  onClick={() => void revokeAppPassword(p.id)}
                  disabled={busy}
                  title={t('settings.revoke')}
                >
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Sessions */}
      <div className="card p-4">
        <div className="mb-3 flex items-center gap-2 text-base font-semibold">
          <Monitor size={18} />
          {t('settings.sessions')}
        </div>
        <p className="mb-3 text-xs text-[var(--app-muted)]">{t('settings.sessionsHint')}</p>

        {sessions.length === 0 ? (
          <div className="text-sm text-[var(--app-muted)]">{t('settings.noSessions')}</div>
        ) : (
          <div className="divide-y divide-[var(--app-border)]">
            {sessions.map((s) => (
              <div key={s.id} className="flex items-center gap-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{s.name}</span>
                    {isCurrentSession(s.userAgent) && (
                      <span className="rounded-full bg-[var(--app-accent-soft)] px-2 py-0.5 text-xs text-[var(--app-accent)]">
                        {t('settings.currentSession')}
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-[var(--app-muted)]">
                    {s.ip} &middot; {s.userAgent} &middot; {new Date(s.createdAt).toLocaleDateString()}
                  </div>
                </div>
                {!isCurrentSession(s.userAgent) && (
                  <button
                    className="btn-ghost text-[var(--app-danger)]"
                    onClick={() => void revokeSession(s.id)}
                    disabled={busy}
                    title={t('settings.revokeSession')}
                  >
                    <Trash2 size={16} />
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
