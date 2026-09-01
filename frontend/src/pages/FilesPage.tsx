import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeft, Download, File as FileIcon, Folder, FolderPlus, Trash2, Upload, Plus, X, PenLine,
} from 'lucide-react'
import { api, uploadFile } from '../api/client'
import { useAuth } from '../store/auth'
import type { FileFolderItem, FileItem } from '../types'

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

interface Breadcrumb {
  id: string
  name: string
}

export default function FilesPage() {
  const { t } = useTranslation()
  const { token } = useAuth()
  const [scope, setScope] = useState<'public' | 'personal'>('public')
  const [crumb, setCrumb] = useState<Breadcrumb[]>([])
  const [folders, setFolders] = useState<FileFolderItem[]>([])
  const [files, setFiles] = useState<FileItem[]>([])
  const [busy, setBusy] = useState(false)
  const [newFolderOpen, setNewFolderOpen] = useState(false)
  const [newFolderName, setNewFolderName] = useState('')
  const [renameTarget, setRenameTarget] = useState<{ id: string; name: string; isFolder: boolean } | null>(null)
  const [renameName, setRenameName] = useState('')

  const folderId = crumb.length > 0 ? crumb[crumb.length - 1].id : ''

  const load = useCallback(async () => {
    const q = new URLSearchParams({ scope })
    if (folderId) q.set('folderId', folderId)
    const folderQ = new URLSearchParams({ scope })
    if (folderId) folderQ.set('parentId', folderId)
    try {
      const [fs, fls] = await Promise.all([
        api.get<FileFolderItem[]>(`/api/v1/files/folders?${folderQ}`),
        api.get<FileItem[]>(`/api/v1/files?${q}`),
      ])
      setFolders(fs)
      setFiles(fls)
    } catch {
      setFolders([])
      setFiles([])
    }
  }, [scope, folderId])

  useEffect(() => {
    void load()
  }, [load])

  const switchScope = (s: 'public' | 'personal') => {
    setScope(s)
    setCrumb([])
  }

  const enterFolder = (id: string, name: string) => {
    setCrumb((c) => [...c, { id, name }])
  }

  const back = () => setCrumb((c) => c.slice(0, -1))

  const upload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (!f) return
    const form = new FormData()
    form.append('file', f)
    form.append('scope', scope)
    if (folderId) form.append('folderId', folderId)
    setBusy(true)
    try {
      await uploadFile('/api/v1/files', form)
      await load()
    } catch {
      /* ignore */
    } finally {
      setBusy(false)
      e.target.value = ''
    }
  }

  const download = async (f: FileItem) => {
    const res = await fetch(`/api/v1/files/${f.id}/content`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (!res.ok) return
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = f.name
    a.click()
    URL.revokeObjectURL(url)
  }

  const createFolder = async () => {
    const name = newFolderName.trim()
    if (!name) return
    try {
      await api.post('/api/v1/files/folders', { name, scope, parentId: folderId })
      setNewFolderName('')
      setNewFolderOpen(false)
      await load()
    } catch {
      /* ignore */
    }
  }

  const deleteFolder = async (id: string) => {
    if (!window.confirm(t('files.confirmDelete'))) return
    try {
      await api.del(`/api/v1/files/folders/${id}`)
      await load()
    } catch {
      /* ignore */
    }
  }

  const deleteFile = async (id: string) => {
    if (!window.confirm(t('files.confirmDelete'))) return
    try {
      await api.del(`/api/v1/files/${id}`)
      await load()
    } catch {
      /* ignore */
    }
  }

  const doRename = async () => {
    if (!renameTarget || !renameName.trim()) return
    try {
      if (renameTarget.isFolder) {
        await api.patch(`/api/v1/files/folders/${renameTarget.id}`, { name: renameName.trim() })
      } else {
        await api.patch(`/api/v1/files/${renameTarget.id}`, { name: renameName.trim() })
      }
      setRenameTarget(null)
      await load()
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="mx-auto max-w-4xl">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">{t('files.title')}</h1>
        <div className="flex items-center gap-2">
          <div className="flex overflow-hidden rounded-lg border border-[var(--app-border)]">
            {(['public', 'personal'] as const).map((s) => (
              <button
                key={s}
                onClick={() => switchScope(s)}
                className={`px-3 py-1.5 text-sm ${scope === s ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--app-card)] text-[var(--app-muted)] hover:bg-[var(--app-card-sub)]'}`}
              >
                {s === 'public' ? t('files.public') : t('files.personal')}
              </button>
            ))}
          </div>
          <button className="btn-ghost" onClick={() => setNewFolderOpen(true)}>
            <FolderPlus size={16} />
            {t('files.newFolder')}
          </button>
          <label className="btn-primary cursor-pointer">
            <Upload size={16} />
            {t('files.upload')}
            <input type="file" className="hidden" onChange={(e) => void upload(e)} disabled={busy} />
          </label>
        </div>
      </div>

      <div className="card mb-4 flex items-center gap-2 px-3 py-2 text-sm">
        <button className="nav-link" onClick={() => setCrumb([])}>
          {t('files.title')}
        </button>
        {crumb.map((c) => (
          <span key={c.id} className="flex items-center gap-2">
            <span className="text-[var(--app-faint)]">/</span>
            <button className="nav-link" onClick={() => setCrumb((cur) => cur.slice(0, cur.indexOf(c) + 1))}>
              {c.name}
            </button>
          </span>
        ))}
        {folderId && (
          <button className="nav-link ml-auto" onClick={back} title={t('files.back')}>
            <ArrowLeft size={16} />
          </button>
        )}
      </div>

      {folders.length === 0 && files.length === 0 ? (
        <div className="card py-12 text-center text-sm text-[var(--app-muted)]">{t('files.empty')}</div>
      ) : (
        <div className="card overflow-hidden">
          <div className="divide-y divide-[var(--app-border)]">
            {folders.map((f) => (
              <div key={f.id} className="flex items-center gap-3 px-4 py-2.5 hover:bg-[var(--app-card-sub)]">
                <button className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => enterFolder(f.id, f.name)}>
                  <Folder size={18} className="shrink-0 text-[var(--app-accent)]" />
                  <span className="truncate font-medium">{f.name}</span>
                </button>
                <div className="shrink-0 text-xs text-[var(--app-muted)]">{new Date(f.updatedAt).toLocaleDateString()}</div>
                <button className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]" onClick={() => { setRenameTarget({ id: f.id, name: f.name, isFolder: true }); setRenameName(f.name) }} title={t('files.rename')}>
                  <PenLine size={16} />
                </button>
                <button className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-danger)]" onClick={() => void deleteFolder(f.id)}>
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
            {files.map((f) => (
              <div key={f.id} className="flex items-center gap-3 px-4 py-2.5 hover:bg-[var(--app-card-sub)]">
                <FileIcon size={18} className="shrink-0 text-[var(--app-muted)]" />
                <button className="min-w-0 flex-1 truncate text-left font-medium hover:text-[var(--app-accent)]" onClick={() => void download(f)}>
                  {f.name}
                </button>
                <div className="shrink-0 text-xs text-[var(--app-muted)]">{fmtSize(f.size)}</div>
                <div className="shrink-0 text-xs text-[var(--app-muted)]">{new Date(f.updatedAt).toLocaleDateString()}</div>
                <button className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]" onClick={() => void download(f)} title={t('files.download')}>
                  <Download size={16} />
                </button>
                <button className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]" onClick={() => { setRenameTarget({ id: f.id, name: f.name, isFolder: false }); setRenameName(f.name) }} title={t('files.rename')}>
                  <PenLine size={16} />
                </button>
                <button className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-danger)]" onClick={() => void deleteFile(f.id)}>
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {newFolderOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setNewFolderOpen(false)}>
          <div className="card w-full max-w-sm p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 flex items-center justify-between">
              <div className="text-lg font-semibold">{t('files.newFolder')}</div>
              <button className="nav-link" onClick={() => setNewFolderOpen(false)}>
                <X size={18} />
              </button>
            </div>
            <input
              className="input mb-4"
              placeholder={t('files.folderName')}
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void createFolder()
              }}
              autoFocus
            />
            <button className="btn-primary w-full" onClick={() => void createFolder()}>
              <Plus size={16} />
              {t('common.save')}
            </button>
          </div>
        </div>
      )}

      {renameTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setRenameTarget(null)}>
          <div className="card w-full max-w-sm p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 text-lg font-semibold">{t('files.rename')}</div>
            <input
              className="input mb-4"
              value={renameName}
              onChange={(e) => setRenameName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void doRename()
              }}
              autoFocus
            />
            <button className="btn-primary w-full" onClick={() => void doRename()}>
              {t('common.save')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
