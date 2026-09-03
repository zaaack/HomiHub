import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { KeyRound, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import { PopConfirm } from '../components/ui/pop-confirm'

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

export default function SecurityPage() {
  const { t } = useTranslation()
  const [sessions, setSessions] = useState<TokenEntry[]>([])
  const [busy, setBusy] = useState(false)
  const [currentPwd, setCurrentPwd] = useState('')
  const [newPwd, setNewPwd] = useState('')
  const [msg, setMsg] = useState<{ ok?: boolean; text: string } | null>(null)
  const [passwordSaved, setPasswordSaved] = useState(false)

  const loadSessions = useCallback(async () => {
    try {
      const list = await api.get<TokenEntry[]>('/api/v1/auth/tokens')
      setSessions(list)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    void loadSessions()
  }, [loadSessions])

  const revokeSession = async (id: string) => {
    setBusy(true)
    try {
      await api.del(`/api/v1/auth/tokens/${id}`)
      await loadSessions()
    } finally {
      setBusy(false)
    }
  }

  const isCurrentSession = (ua: string) => ua === navigator.userAgent

  const changePassword = async () => {
    setBusy(true)
    setMsg(null)
    setPasswordSaved(false)
    try {
      await api.post('/api/v1/auth/password', { currentPassword: currentPwd, newPassword: newPwd })
      setCurrentPwd('')
      setNewPwd('')
      setMsg({ ok: true, text: t('security.passwordSaved') })
      setPasswordSaved(true)
      setTimeout(() => setPasswordSaved(false), 2000)
    } catch (ex: any) {
      setMsg({ ok: false, text: ex.message })
    }
    setBusy(false)
  }

  return (
    <div className="mx-auto max-w-lg space-y-4">
      <h1 className="text-xl font-semibold">{t('security.title')}</h1>

      {/* Change Password */}
      <div className="card p-4">
        <div className="mb-3 flex items-center gap-2 text-base font-semibold">
          <KeyRound size={18} />
          {t('security.changePassword')}
        </div>

        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('security.currentPassword')}</label>
            <input
              className="input w-full"
              type="password"
              value={currentPwd}
              onChange={(e) => setCurrentPwd(e.target.value)}
            />
          </div>

          <div>
            <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('security.newPassword')}</label>
            <input
              className="input w-full"
              type="password"
              value={newPwd}
              onChange={(e) => setNewPwd(e.target.value)}
            />
          </div>
        </div>

        <div className="mt-3 flex items-center gap-3">
          <button
            className="btn-primary relative z-10"
            onClick={() => void changePassword()}
            disabled={busy || !currentPwd || !newPwd}
          >
            {t('common.save')}
          </button>
          {passwordSaved && (
            <span className="text-sm text-[var(--app-accent)]">{t('security.passwordSaved')}</span>
          )}
        </div>

        {msg && !passwordSaved && (
          <p className={`mt-2 text-sm ${msg.ok ? 'text-[var(--app-accent)]' : 'text-[var(--app-danger)]'}`}>
            {msg.text}
          </p>
        )}
      </div>

      {/* Sessions */}
      <div className="card p-4">
        <div className="mb-3 flex items-center gap-2 text-base font-semibold">
          <Trash2 size={18} />
          {t('security.sessions')}
        </div>
        <p className="mb-3 text-xs text-[var(--app-muted)]">{t('security.sessionsHint')}</p>

        {sessions.length === 0 ? (
          <div className="text-sm text-[var(--app-muted)]">{t('security.noSessions')}</div>
        ) : (
          <div className="divide-y divide-[var(--app-border)]">
            {sessions.map((s) => (
              <div key={s.id} className="flex items-center gap-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{s.name}</span>
                    {isCurrentSession(s.userAgent) && (
                      <span className="rounded-full bg-[var(--app-accent-soft)] px-2 py-0.5 text-xs text-[var(--app-accent)]">
                        {t('security.currentSession')}
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-[var(--app-muted)]">
                    {s.ip} · {s.userAgent} · {new Date(s.createdAt).toLocaleDateString()}
                  </div>
                </div>
                {!isCurrentSession(s.userAgent) && (
                  <PopConfirm
                    onConfirm={() => void revokeSession(s.id)}
                    title={t('security.revokeSession')}
                    description={t('security.revokeSessionConfirm')}
                    confirmText={t('common.confirm')}
                    cancelText={t('common.cancel')}
                    disabled={busy}
                  >
                    <button
                      className="btn-ghost relative z-10 text-[var(--app-danger)]"
                      disabled={busy}
                      title={t('security.revokeSession')}
                    >
                      <Trash2 size={16} />
                    </button>
                  </PopConfirm>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
