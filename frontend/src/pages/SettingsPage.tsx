import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Save, ShieldCheck } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'

export default function SettingsPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [origins, setOrigins] = useState('')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    void api
      .get<{ origins: string }>('/api/v1/settings/cors')
      .then((r) => setOrigins(r.origins ?? ''))
      .catch(() => {})
  }, [])

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
    </div>
  )
}
