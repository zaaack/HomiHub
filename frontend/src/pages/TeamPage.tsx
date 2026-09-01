import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy, Link2, RotateCw, Users, Check, Share2, KeyRound } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'
import type { Invite, Member } from '../types'

export default function TeamPage() {
  const { t } = useTranslation()
  const { user, team, refresh } = useAuth()
  const [members, setMembers] = useState<Member[]>([])
  const [invites, setInvites] = useState<Invite[]>([])
  const [newName, setNewName] = useState('')
  const [calendarToken, setCalendarToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [busy, setBusy] = useState(false)

  const isOwner = user?.role === 'parent'

  const load = useCallback(async () => {
    try {
      const [t, ms, invs] = await Promise.all([
        api.get<{ calendarToken?: string }>('/api/v1/team'),
        api.get<Member[]>('/api/v1/team/members'),
        api.get<Invite[]>('/api/v1/invites'),
      ])
      if (t.calendarToken) setCalendarToken(t.calendarToken)
      setMembers(ms)
      setInvites(invs)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const rename = async () => {
    if (!newName.trim()) return
    setBusy(true)
    try {
      await api.patch('/api/v1/team', { name: newName.trim() })
      setNewName('')
      await refresh()
    } finally {
      setBusy(false)
    }
  }

  const createInvite = async () => {
    setBusy(true)
    try {
      const inv = await api.post<Invite>('/api/v1/invites', { role: 'child' })
      setInvites((prev) => [...prev, inv])
    } finally {
      setBusy(false)
    }
  }

  const copyInvite = (link: string) => {
    const url = `${window.location.origin}/join?code=${encodeURIComponent(link)}`
    void navigator.clipboard.writeText(url)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const resetToken = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ calendarToken: string }>('/api/v1/team/reset-calendar-token')
      setCalendarToken(r.calendarToken)
      await refresh()
    } finally {
      setBusy(false)
    }
  }

  const inviteUrl = (link: string) => `${window.location.origin}/join?code=${encodeURIComponent(link)}`
  const icsUrl = (token: string) => `${window.location.origin}/api/v1/calendar/feed.ics?token=${encodeURIComponent(token)}&scope=team`

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <h1 className="text-xl font-semibold">{t('team.title')}</h1>

      <div className="card p-4">
        <div className="mb-3 flex items-center justify-between">
          <div className="text-base font-semibold">{team?.name}</div>
          {isOwner && (
            <span className="rounded-full bg-[var(--app-accent-soft)] px-2 py-0.5 text-xs text-[var(--app-accent)]">
              {t('auth.owner')}
            </span>
          )}
        </div>
        {isOwner && (
          <div className="flex items-center gap-2">
            <input
              className="input"
              placeholder={t('team.rename')}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <button className="btn-primary shrink-0" onClick={() => void rename()} disabled={busy}>
              {t('team.rename')}
            </button>
          </div>
        )}
      </div>

      <div className="card p-4">
        <div className="mb-3 flex items-center gap-2 text-base font-semibold">
          <Users size={18} />
          {t('team.members')}
        </div>
        {members.length === 0 && <div className="text-sm text-[var(--app-muted)]">{t('team.emptyMembers')}</div>}
        <div className="divide-y divide-[var(--app-border)]">
          {members.map((m) => (
            <div key={m.id} className="flex items-center gap-3 py-2">
              <div className="flex h-9 w-9 items-center justify-center rounded-full bg-[var(--app-accent)] text-sm font-medium text-white">
                {m.name?.[0] ?? '?'}
              </div>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium">
                  {m.name}
                  {m.isOwner && <span className="ml-2 text-xs text-[var(--app-muted)]">{t('auth.owner')}</span>}
                </div>
                <div className="truncate text-xs text-[var(--app-muted)]">{m.email}</div>
              </div>
              <span className="rounded-full bg-[var(--app-card-sub)] px-2 py-0.5 text-xs text-[var(--app-muted)]">
                {m.role === 'parent' ? t('auth.parent') : t('auth.child')}
              </span>
            </div>
          ))}
        </div>
      </div>

      {isOwner && (
        <div className="card p-4">
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2 text-base font-semibold">
              <Share2 size={18} />
              {t('team.invite')}
            </div>
            <button className="btn-ghost" onClick={() => void createInvite()} disabled={busy}>
              <Link2 size={16} />
              {t('team.createInvite')}
            </button>
          </div>
          <p className="mb-3 text-xs text-[var(--app-muted)]">{t('team.inviteHint')}</p>
          {invites.length === 0 && <div className="text-sm text-[var(--app-muted)]">{t('team.emptyMembers')}</div>}
          <div className="space-y-2">
            {invites.map((inv) => (
              <div key={inv.id} className="flex items-center gap-2">
                <input className="input flex-1 text-xs" readOnly value={inv.link ? inviteUrl(inv.link) : ''} onFocus={(e) => e.target.select()} />
                <button className="btn-ghost shrink-0" onClick={() => inv.link && copyInvite(inv.link)} title={t('team.copyLink')}>
                  {copied ? <Check size={16} className="text-[var(--app-accent)]" /> : <Copy size={16} />}
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="card p-4">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2 text-base font-semibold">
            <KeyRound size={18} />
            {t('team.calendarToken')}
          </div>
          {isOwner && (
            <button className="btn-ghost" onClick={() => void resetToken()} disabled={busy}>
              <RotateCw size={16} />
              {t('team.resetToken')}
            </button>
          )}
        </div>
        <p className="mb-3 text-xs text-[var(--app-muted)]">{t('team.resetTokenHint')}</p>
        <div className="space-y-2">
          {calendarToken && (
            <div>
              <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('team.calendarToken')}</label>
              <input className="input text-xs" readOnly value={calendarToken} onFocus={(e) => e.target.select()} />
            </div>
          )}
          {calendarToken && (
            <div>
              <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('team.iCalUrl')}</label>
              <input className="input text-xs" readOnly value={icsUrl(calendarToken)} onFocus={(e) => e.target.select()} />
            </div>
          )}
          {!calendarToken && <div className="text-sm text-[var(--app-muted)]">{t('team.resetTokenHint')}</div>}
        </div>
      </div>
    </div>
  )
}
