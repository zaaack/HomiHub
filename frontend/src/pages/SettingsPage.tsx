import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Save, ShieldCheck, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'

export default function SettingsPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [origins, setOrigins] = useState('')
  const [trashDays, setTrashDays] = useState(90)
  const [trashItemsDays, setTrashItemsDays] = useState(90)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    void api
      .get<{ origins: string }>('/api/v1/settings/cors')
      .then((r) => setOrigins(r.origins ?? ''))
      .catch(() => {})
    void api
      .get<{ days: number }>('/api/v1/settings/trash')
      .then((r) => setTrashDays(r.days ?? 90))
      .catch(() => {})
    void api
      .get<{ days: number }>('/api/v1/settings/trash-items')
      .then((r) => setTrashItemsDays(r.days ?? 90))
      .catch(() => {})
  }, [])

  const save = async () => {
    setBusy(true)
    setSaved(false)
    try {
      await api.put('/api/v1/settings/cors', { origins })
      await api.put('/api/v1/settings/trash', { days: trashDays })
      await api.put('/api/v1/settings/trash-items', { days: trashItemsDays })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } finally {
      setBusy(false)
    }
  }

  // Only admins can access system settings
  if (user?.role !== 'parent') {
    return (
      <div className="mx-auto max-w-2xl space-y-4">
        <h1 className="text-xl font-semibold">{t('settings.title')}</h1>
        <div className="card p-4">
          <p className="text-sm text-[var(--app-muted)]">{t('settings.parentOnly')}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-xl font-semibold">{t('settings.title')}</h1>

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

        <div className="card p-4">
          <div className="mb-3 flex items-center gap-2 text-base font-semibold">
            <Trash2 size={18} />
            {t('settings.trashTitle')}
          </div>
          <p className="mb-3 text-xs text-[var(--app-muted)]">{t('settings.trashHint')}</p>
          <div className="flex items-center gap-3">
            <input
              type="number"
              min={0}
              className="input w-32"
              value={trashDays}
              onChange={(e) => setTrashDays(Number(e.target.value))}
            />
            <span className="text-sm text-[var(--app-muted)]">{t('settings.trashDays')}</span>
          </div>
          <p className="mt-2 text-xs text-[var(--app-faint)]">{t('settings.trashZeroHint')}</p>
        </div>

        <div className="card p-4">
          <div className="mb-3 flex items-center gap-2 text-base font-semibold">
            <Trash2 size={18} />
            {t('settings.trashItemsTitle')}
          </div>
          <p className="mb-3 text-xs text-[var(--app-muted)]">{t('settings.trashItemsHint')}</p>
          <div className="flex items-center gap-3">
            <input
              type="number"
              min={0}
              className="input w-32"
              value={trashItemsDays}
              onChange={(e) => setTrashItemsDays(Number(e.target.value))}
            />
            <span className="text-sm text-[var(--app-muted)]">{t('settings.trashDays')}</span>
          </div>
          <p className="mt-2 text-xs text-[var(--app-faint)]">{t('settings.trashZeroHint')}</p>
        </div>
    </div>
  )
}
