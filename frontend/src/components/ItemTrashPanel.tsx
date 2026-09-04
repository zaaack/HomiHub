import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { RotateCcw, Trash2, X } from 'lucide-react'
import { api } from '../api/client'
import type { TrashItem } from '../types'

interface ItemTrashPanelProps {
  kind: 'event' | 'todo' | 'note'
  onClose: () => void
  onChanged?: () => void
}

// ItemTrashPanel is the items recycle bin popup shown from the calendar /
// todos / notes page headers. It lists soft-deleted items with restore and
// permanent-delete actions, mirroring the files trash tab.
export default function ItemTrashPanel({ kind, onClose, onChanged }: ItemTrashPanelProps) {
  const { t } = useTranslation()
  const [items, setItems] = useState<TrashItem[]>([])
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    setBusy(true)
    try {
      setItems(await api.get<TrashItem[]>(`/api/v1/trash/items?kind=${kind}`))
    } catch {
      setItems([])
    } finally {
      setBusy(false)
    }
  }, [kind])

  useEffect(() => {
    void load()
  }, [load])

  const restore = async (id: string) => {
    try {
      await api.post(`/api/v1/trash/items/${kind}/${id}/restore`, {})
      await load()
      onChanged?.()
    } catch {
      /* ignore */
    }
  }

  const purge = async (it: TrashItem) => {
    if (!window.confirm(t('trash.itemsPurgeConfirm'))) return
    try {
      await api.del(`/api/v1/trash/items/${kind}/${it.id}`)
      await load()
      onChanged?.()
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div className="card flex max-h-[80vh] w-full max-w-md flex-col p-5" onClick={(e) => e.stopPropagation()}>
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center gap-2 text-lg font-semibold">
            <Trash2 size={18} />
            {t('trash.itemsTitle')}
          </div>
          <button className="nav-link" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        {items.length === 0 ? (
          <div className="py-10 text-center text-sm text-[var(--app-muted)]">
            {busy ? '…' : t('trash.itemsEmpty')}
          </div>
        ) : (
          <div className="min-h-0 flex-1 divide-y divide-[var(--app-border)] overflow-y-auto">
            {items.map((it) => (
              <div key={it.id} className="flex items-center gap-3 py-2.5">
                <span className="min-w-0 flex-1 truncate font-medium">{it.title || t('notes.untitled')}</span>
                <div className="shrink-0 text-xs text-[var(--app-muted)]">
                  {new Date(it.deletedAt).toLocaleDateString()}
                </div>
                <button
                  className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]"
                  onClick={() => void restore(it.id)}
                  title={t('trash.restore')}
                >
                  <RotateCcw size={16} />
                </button>
                <button
                  className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-danger)]"
                  onClick={() => void purge(it)}
                  title={t('trash.purge')}
                >
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
