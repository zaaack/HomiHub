import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Key, Plus, Trash2, Monitor, AlertCircle, Copy, Check, HardDrive, Calendar, ChevronDown } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'
import { PopConfirm } from '../components/ui/pop-confirm'

interface TokenEntry {
  id: string
  kind: string
  name: string
  token?: string // full value — only present for app passwords
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

function maskToken(token: string | undefined) {
  if (!token) return ''
  if (token.length <= 8) return '•'.repeat(token.length)
  return token.slice(0, 4) + '••••' + token.slice(-4)
}

// RevealPassword shows a masked password that expands to the full value (and
// auto-selects it) while focused, so it can be copied with Ctrl/Cmd+C directly.
function RevealPassword({ value, className }: { value: string; className?: string }) {
  const [focused, setFocused] = useState(false)
  const shown = focused ? value : maskToken(value)
  return (
    <input
      className={className}
      readOnly
      value={shown}
      onFocus={(e) => {
        const el = e.currentTarget
        setFocused(true)
        // Select after the full value has been rendered.
        requestAnimationFrame(() => el.select())
      }}
      onBlur={() => setFocused(false)}
      onMouseUp={(e) => {
        // Keep the whole value selected when re-clicking while focused.
        if (focused) e.currentTarget.select()
      }}
    />
  )
}

export default function UserSettingsPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState<string | null>(null)

  // App Passwords
  const [appPasswords, setAppPasswords] = useState<TokenEntry[]>([])
  const [newAppName, setNewAppName] = useState('')
  const [createdPassword, setCreatedPassword] = useState<AppPasswordCreated | null>(null)
  const [selectedTokenId, setSelectedTokenId] = useState('')

  // Sessions
  const [sessions, setSessions] = useState<TokenEntry[]>([])

  // Collapsible sections
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({
    appPasswords: true,
    sessions: true,
    webdav: true,
    ical: true,
  })

  const toggleSection = (key: string) => {
    setExpandedSections((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  const loadAppPasswords = useCallback(async () => {
    try {
      const list = await api.get<TokenEntry[]>('/api/v1/auth/app-passwords')
      setAppPasswords(list)
      if (list.length === 0) {
        // Auto-create default app password if none exists
        const r = await api.post<AppPasswordCreated>('/api/v1/auth/app-passwords', { name: 'Default' })
        setCreatedPassword(r)
        setAppPasswords(await api.get<TokenEntry[]>('/api/v1/auth/app-passwords'))
        setSelectedTokenId(r.id)
      } else {
        const usable = list.filter((p) => p.token)
        setSelectedTokenId((prev) => (usable.some((p) => p.id === prev) ? prev : usable[0]?.id ?? ''))
      }
    } catch { /* ignore */ }
  }, [])

  const loadSessions = useCallback(async () => {
    try {
      const list = await api.get<TokenEntry[]>('/api/v1/auth/tokens')
      setSessions(list)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    void loadAppPasswords()
    void loadSessions()
  }, [loadAppPasswords, loadSessions])

  const createAppPassword = async () => {
    setBusy(true)
    try {
      const r = await api.post<AppPasswordCreated>('/api/v1/auth/app-passwords', { name: newAppName || 'App Password' })
      setCreatedPassword(r)
      setNewAppName('')
      await loadAppPasswords()
      setSelectedTokenId(r.id)
    } finally {
      setBusy(false)
    }
  }

  const revokeAppPassword = async (id: string) => {
    setBusy(true)
    try {
      await api.del(`/api/v1/auth/app-passwords/${id}`)
      await loadAppPasswords()
    } finally {
      setBusy(false)
    }
  }

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

  const copyToClipboard = async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(label)
      setTimeout(() => setCopied(null), 1500)
    } catch { /* ignore */ }
  }

  const CopyButton = ({ text, label }: { text: string; label: string }) => (
    <button
      onClick={() => void copyToClipboard(text, label)}
      className="text-xs flex items-center gap-1 text-[var(--app-accent)] hover:text-[var(--app-accent-hover)] shrink-0"
      title={t('common.copy')}
    >
      {copied === label ? (
        <><Check size={12} /> {t('common.copied')}</>
      ) : (
        <><Copy size={12} /> {t('common.copy')}</>
      )}
    </button>
  )

  const base = window.location.origin
  const email = user?.email || ''
  const webdavMountUrl = `${base}/dav/files/`
  const selfFeedPath = '/api/v1/calendar/feed.ics'

  const selectedAppPassword = appPasswords.find((p) => p.id === selectedTokenId && p.token) || null
  const selfIcalUrl =
    selectedAppPassword && selectedAppPassword.token
      ? `${base}${selfFeedPath}?token=${encodeURIComponent(selectedAppPassword.token)}`
      : ''

  const SectionHeader = ({ title, sectionKey, icon }: { title: string; sectionKey: string; icon: React.ReactNode }) => (
    <button
      onClick={() => toggleSection(sectionKey)}
      className="mb-3 flex w-full items-center gap-2 text-base font-semibold hover:opacity-80"
    >
      {icon}
      {title}
      <span className="ml-auto text-[var(--app-muted)]">
        {expandedSections[sectionKey] ? '▼' : '▶'}
      </span>
    </button>
  )

  const passwordSelect = (
    <div className="relative">
      <select
        className="input w-full appearance-none pr-8 text-xs"
        value={selectedTokenId}
        onChange={(e) => setSelectedTokenId(e.target.value)}
      >
        <option value="">{t('userSettings.selectAppPassword')}</option>
        {appPasswords.filter((p) => p.token).map((p) => (
          <option key={p.id} value={p.id}>{p.name}</option>
        ))}
      </select>
      <ChevronDown size={14} className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-[var(--app-muted)]" />
    </div>
  )

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-xl font-semibold">{t('userSettings.title')}</h1>

      {/* App Passwords */}
      <div className="card p-4">
        <SectionHeader
          title={t('settings.appPasswords')}
          sectionKey="appPasswords"
          icon={<Key size={18} />}
        />
        {expandedSections.appPasswords && (
          <>
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
                <div className="mb-2">
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('settings.appPasswordValue')}</label>
                  <div className="flex items-center gap-2">
                    <input
                      className="input flex-1 font-mono text-xs"
                      readOnly
                      value={createdPassword.token}
                      onFocus={(e) => e.target.select()}
                    />
                    <CopyButton text={createdPassword.token} label="created-password" />
                  </div>
                </div>
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
                      <div className="flex items-center gap-2 text-xs text-[var(--app-muted)]">
                        {p.token
                          ? <code className="font-mono">{maskToken(p.token)}</code>
                          : <span>{t('userSettings.passwordUnavailable')}</span>}
                        <span>·</span>
                        <span className="shrink-0">{new Date(p.createdAt).toLocaleDateString()}</span>
                      </div>
                    </div>
                    {p.token && <CopyButton text={p.token} label={`ap-${p.id}`} />}
                    <PopConfirm
                      onConfirm={() => void revokeAppPassword(p.id)}
                      title={t('settings.revoke')}
                      description={t('settings.revokeAppPasswordConfirm')}
                      confirmText={t('common.confirm')}
                      cancelText={t('common.cancel')}
                      disabled={busy}
                    >
                      <button
                        className="btn-ghost relative z-10 text-[var(--app-danger)]"
                        disabled={busy}
                        title={t('settings.revoke')}
                      >
                        <Trash2 size={16} />
                      </button>
                    </PopConfirm>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>

      {/* Sessions */}
      <div className="card p-4">
        <SectionHeader
          title={t('settings.sessions')}
          sectionKey="sessions"
          icon={<Monitor size={18} />}
        />
        {expandedSections.sessions && (
          <>
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
                        {s.ip} · {s.userAgent} · {new Date(s.createdAt).toLocaleDateString()}
                      </div>
                    </div>
                    {!isCurrentSession(s.userAgent) && (
                      <PopConfirm
                        onConfirm={() => void revokeSession(s.id)}
                        title={t('settings.revokeSession')}
                        description={t('settings.revokeSessionConfirm')}
                        confirmText={t('common.confirm')}
                        cancelText={t('common.cancel')}
                        disabled={busy}
                      >
                        <button
                          className="btn-ghost relative z-10 text-[var(--app-danger)]"
                          disabled={busy}
                          title={t('settings.revokeSession')}
                        >
                          <Trash2 size={16} />
                        </button>
                      </PopConfirm>
                    )}
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>

      {/* WebDAV */}
      <div className="card p-4">
        <SectionHeader
          title={t('userSettings.webdavTitle')}
          sectionKey="webdav"
          icon={<HardDrive size={18} />}
        />
        {expandedSections.webdav && (
          <div className="space-y-3">
            <p className="text-xs text-[var(--app-muted)]">{t('userSettings.webdavHint')}</p>

            <div>
              <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('userSettings.webdavMountUrl')}</label>
              <div className="flex items-center gap-2">
                <input className="input flex-1 font-mono text-xs" readOnly value={webdavMountUrl} onFocus={(e) => e.target.select()} />
                <CopyButton text={webdavMountUrl} label="webdav-mount" />
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('userSettings.webdavAccount')}</label>
              <div className="flex items-center gap-2">
                <input className="input flex-1 text-xs" readOnly value={email} onFocus={(e) => e.target.select()} />
                <CopyButton text={email} label="webdav-email" />
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('userSettings.webdavPassword')}</label>
              <div className="space-y-2">
                {passwordSelect}
                {selectedAppPassword ? (
                  <>
                    <div className="flex items-center gap-2">
                      <RevealPassword value={selectedAppPassword.token!} className="input min-w-0 flex-1 font-mono text-xs" />
                      <CopyButton text={selectedAppPassword.token!} label="webdav-password" />
                    </div>
                    <p className="text-xs text-[var(--app-muted)]">
                      {t('userSettings.webdavPasswordHint')}
                    </p>
                  </>
                ) : appPasswords.length === 0 ? (
                  <p className="text-xs text-[var(--app-muted)]">{t('userSettings.noAppPasswords')}</p>
                ) : null}
              </div>
            </div>

            <div className="rounded-lg bg-[var(--app-card-sub)] p-3 text-xs text-[var(--app-muted)]">
              <p>{t('userSettings.caldavHint')}</p>
            </div>
          </div>
        )}
      </div>

      {/* iCal Subscription */}
      <div className="card p-4">
        <SectionHeader
          title={t('userSettings.icalTitle')}
          sectionKey="ical"
          icon={<Calendar size={18} />}
        />
        {expandedSections.ical && (
          <div className="space-y-3">
            <p className="text-xs text-[var(--app-muted)]">{t('userSettings.icalHint')}</p>

            {appPasswords.length === 0 ? (
              <div className="rounded-lg bg-[var(--app-card-sub)] p-3 text-xs text-[var(--app-muted)]">
                {t('userSettings.noAppPasswords')}
              </div>
            ) : (
              <>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('userSettings.icalPickPassword')}</label>
                  {passwordSelect}
                </div>

                {!selectedAppPassword ? (
                  <p className="text-xs text-[var(--app-muted)]">{t('userSettings.icalNoPassword')}</p>
                ) : (
                  <div className="space-y-3">
                    {/* Single subscribe URL — the App Password is the token */}
                    <div>
                      <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('userSettings.icalUrl')}</label>
                      <div className="flex items-start gap-2">
                        <input
                          className="input min-w-0 flex-1 font-mono text-[11px] leading-4"
                          readOnly
                          value={selfIcalUrl}
                          onFocus={(e) => e.target.select()}
                        />
                        <CopyButton text={selfIcalUrl} label="ical-self" />
                      </div>
                      <p className="mt-1 text-xs text-[var(--app-muted)]">{t('userSettings.icalHintUrl')}</p>
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
