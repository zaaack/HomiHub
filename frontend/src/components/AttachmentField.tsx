import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Download, Paperclip, Trash2, Upload } from 'lucide-react'
import { api, uploadFile } from '../api/client'
import { useAuth } from '../store/auth'
import type { Attachment } from '../types'

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// AttachmentField manages the files attached to an event / todo / note. It is
// only active when an existing item (itemId) is being edited; brand-new items
// attach files after their first save.
export default function AttachmentField({ kind, itemId }: { kind: 'event' | 'todo' | 'note'; itemId?: string }) {
  const { t } = useTranslation()
  const token = useAuth((s) => s.token)
  const [items, setItems] = useState<Attachment[]>([])
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    if (!itemId) {
      setItems([])
      return
    }
    try {
      const list = await api.get<Attachment[]>(`/api/v1/attachments?kind=${kind}&itemId=${itemId}`)
      setItems(list)
    } catch {
      setItems([])
    }
  }, [kind, itemId])

  useEffect(() => {
    void load()
  }, [load])

  const upload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (!f || !itemId) return
    const form = new FormData()
    form.append('kind', kind)
    form.append('itemId', itemId)
    form.append('file', f)
    setBusy(true)
    try {
      await uploadFile('/api/v1/attachments', form)
      await load()
    } catch {
      /* ignore */
    } finally {
      setBusy(false)
      e.target.value = ''
    }
  }

  const download = async (a: Attachment) => {
    const res = await fetch(`/api/v1/files/${a.fileId}/content`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (!res.ok) return
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const el = document.createElement('a')
    el.href = url
    el.download = a.name
    el.click()
    URL.revokeObjectURL(url)
  }

  const remove = async (id: string) => {
    try {
      await api.del(`/api/v1/attachments/${id}`)
      await load()
    } catch {
      /* ignore */
    }
  }

  if (!itemId) return null

  return (
    <div>
      <label className="mb-1 flex items-center gap-1 text-xs text-[var(--app-muted)]">
        <Paperclip size={12} />
        {t('attachments.title')}
      </label>
      <div className="space-y-1">
        {items.length === 0 && <div className="text-[11px] text-[var(--app-faint)]">{t('attachments.empty')}</div>}
        {items.map((a) => (
          <div key={a.id} className="flex items-center gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-card-sub)] px-2 py-1 text-xs">
            <span className="min-w-0 flex-1 truncate" title={a.name}>
              {a.name}
            </span>
            <span className="shrink-0 text-[var(--app-faint)]">{fmtSize(a.size)}</span>
            <button type="button" className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]" title={t('attachments.download')} onClick={() => void download(a)}>
              <Download size={13} />
            </button>
            <button type="button" className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-danger)]" title={t('attachments.delete')} onClick={() => void remove(a.id)}>
              <Trash2 size={13} />
            </button>
          </div>
        ))}
        <label className="flex cursor-pointer items-center gap-1 text-xs text-[var(--app-accent)]">
          <Upload size={13} />
          <span>{t('attachments.upload')}</span>
          <input type="file" className="hidden" disabled={busy} onChange={(e) => void upload(e)} />
        </label>
      </div>
    </div>
  )
}
