import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { User, Save } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'

export default function ProfilePage() {
  const { t } = useTranslation()
  const { user, refresh } = useAuth()
  const [name, setName] = useState(user?.name || '')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)

  const saveName = async () => {
    if (!name.trim()) return
    setBusy(true)
    try {
      await api.patch('/api/v1/auth/profile', { name: name.trim() })
      await refresh()
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch { /* ignore */ }
    setBusy(false)
  }

  return (
    <div className="mx-auto max-w-lg space-y-4">
      <h1 className="text-xl font-semibold">{t('profile.title')}</h1>

      <div className="card p-4">
        <div className="mb-3 flex items-center gap-2 text-base font-semibold">
          <User size={18} />
          {t('profile.basicInfo')}
        </div>

        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('auth.email')}</label>
            <input className="input w-full" readOnly value={user?.email || ''} />
          </div>

          <div>
            <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('auth.name')}</label>
            <input
              className="input w-full"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div>
            <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('auth.role')}</label>
            <input className="input w-full" readOnly value={user?.role === 'parent' ? t('auth.parent') : t('auth.child')} />
          </div>
        </div>

        <div className="mt-4 flex items-center gap-3">
          <button className="btn-primary" onClick={() => void saveName()} disabled={busy || !name.trim()}>
            <Save size={16} />
            {t('common.save')}
          </button>
          {saved && <span className="text-sm text-[var(--app-accent)]">{t('settings.saved')}</span>}
        </div>
      </div>
    </div>
  )
}
