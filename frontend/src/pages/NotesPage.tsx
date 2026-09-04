import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { List, NotebookPen, Pencil, Plus, Trash2, Users, X } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'
import { PopConfirm } from '../components/ui/pop-confirm'
import AttachmentField from '../components/AttachmentField'
import ItemTrashPanel from '../components/ItemTrashPanel'
import type { Member, Note, NoteList } from '../types'

const LIST_COLORS = ['#22c55e', '#4f8cff', '#ef4444', '#f59e0b', '#a855f7', '#06b6d4', '#ec4899', '#64748b', '#84cc16', '#f97316']
const LIST_ICONS = ['📝', '💡', '📌', '🗒️', '📕', '⭐', '🎯', '🎓', '✈️', '🔬', '❤️', '🧠', '🍳', '🏔️', '📖', '🎬', '💰', '👶', '🔧', '🛒']
const PERSONAL_COLOR = '#22c55e'
const TEAM_COLOR = '#f59e0b'

interface ListDraft {
  name: string
  color: string
  icon: string
  memberIds: string[]
}

interface NoteDraft {
  id?: string
  title: string
  body: string
  tags: string
  calendar: string
  attendeeIds: string[]
}

export default function NotesPage() {
  const { t } = useTranslation()
  const me = useAuth((s) => s.user)
  const [lists, setLists] = useState<NoteList[]>([])
  const [notes, setNotes] = useState<Note[]>([])
  const [teamMembers, setTeamMembers] = useState<Member[]>([])
  const [activeListId, setActiveListId] = useState('')
  const [editing, setEditing] = useState<NoteDraft | null>(null)
  const [listDraft, setListDraft] = useState<ListDraft | null>(null)
  const [editListId, setEditListId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [trashOpen, setTrashOpen] = useState(false)

  const load = async () => {
    try {
      const [ls, ns] = await Promise.all([
        api.get<NoteList[]>('/api/v1/note-lists'),
        api.get<Note[]>('/api/v1/notes'),
      ])
      setLists(ls)
      setNotes(ns)
      if (activeListId && !ls.some((l) => l.id === activeListId)) setActiveListId('')
    } catch {
      setLists([])
      setNotes([])
    }
  }

  useEffect(() => {
    void load()
    void api
      .get<Member[]>('/api/v1/team/members')
      .then(setTeamMembers)
      .catch(() => setTeamMembers([]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const listsById = useMemo(() => new Map(lists.map((l) => [l.id, l])), [lists])
  const activeList = activeListId ? listsById.get(activeListId) : undefined

  const listName = (l: NoteList | undefined): string => {
    if (!l) return t('notes.allNotes')
    if (l.kind === 'personal') return t('notes.personalList')
    if (l.kind === 'team') return t('notes.teamList')
    return l.name || l.id
  }

  const filtered = useMemo(() => {
    let out = notes
    if (activeListId) out = out.filter((n) => n.calendar === activeListId)
    return out
  }, [notes, activeListId])

  const allTags = useMemo(() => {
    const s = new Set<string>()
    for (const n of notes) {
      for (const tag of n.tags.split(',').map((x) => x.trim())) if (tag) s.add(tag)
    }
    return [...s].sort((a, b) => a.localeCompare(b, 'zh'))
  }, [notes])
  const [selTags, setSelTags] = useState<string[]>([])

  const tagged = useMemo(() => {
    let out = filtered
    if (selTags.length > 0) {
      out = out.filter((n) => {
        const tags = n.tags.split(',').map((s) => s.trim()).filter(Boolean)
        return selTags.every((st) => tags.includes(st))
      })
    }
    return out
  }, [filtered, selTags])

  const writableLists = lists.filter((l) => l.writable)

  const openCreate = (calendar?: string) => {
    setEditing({ title: '', body: '', tags: '', calendar: calendar ?? (activeListId || 'self'), attendeeIds: [] })
  }

  const openEdit = (n: Note) => {
    setEditing({
      id: n.id,
      title: n.title,
      body: n.body,
      tags: n.tags,
      calendar: n.calendar,
      attendeeIds: (n.attendees ?? []).map((a) => a.id).filter(Boolean),
    })
  }

  const submit = async () => {
    if (!editing) return
    setBusy(true)
    try {
      const payload = {
        title: editing.title.trim(),
        body: editing.body.trim(),
        tags: editing.tags.trim(),
        calendar: editing.calendar || 'self',
        attendees: editing.attendeeIds,
      }
      if (editing.id) await api.put(`/api/v1/notes/${editing.id}`, payload)
      else await api.post('/api/v1/notes', payload)
      setEditing(null)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const remove = async (id: string) => {
    try {
      await api.del(`/api/v1/notes/${id}`)
      if (editing?.id === id) setEditing(null)
      await load()
    } catch {
      /* ignore */
    }
  }

  const openCreateList = () => {
    setEditListId(null)
    setListDraft({ name: '', color: LIST_COLORS[0], icon: LIST_ICONS[0], memberIds: [] })
  }

  const openEditList = (l: NoteList) => {
    setEditListId(l.id)
    setListDraft({
      name: l.name,
      color: l.color || PERSONAL_COLOR,
      icon: l.icon || LIST_ICONS[0],
      memberIds: (l.members ?? []).map((m) => m.id),
    })
  }

  const submitList = async () => {
    if (!listDraft || !listDraft.name.trim()) return
    setBusy(true)
    try {
      const payload = {
        name: listDraft.name.trim(),
        color: listDraft.color,
        icon: listDraft.icon,
        memberIds: listDraft.memberIds,
      }
      if (editListId) await api.put(`/api/v1/note-lists/${editListId}`, payload)
      else await api.post('/api/v1/note-lists', payload)
      setListDraft(null)
      setEditListId(null)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const deleteList = async (id: string) => {
    try {
      await api.del(`/api/v1/note-lists/${id}`)
      if (activeListId === id) setActiveListId('')
      setListDraft(null)
      setEditListId(null)
      await load()
    } catch {
      /* ignore */
    }
  }

  const toggleTag = (tag: string) => {
    setSelTags((prev) => (prev.includes(tag) ? prev.filter((x) => x !== tag) : [...prev, tag]))
  }

  const countIn = (id: string) => notes.filter((n) => n.calendar === id).length

  const chip = (l: NoteList, size: 'sm' | 'lg' = 'sm') => {
    const color = l.kind === 'personal' ? PERSONAL_COLOR : l.kind === 'team' ? TEAM_COLOR : l.color || PERSONAL_COLOR
    const shared = l.kind === 'team' || (l.members?.length ?? 0) > 0
    const emoji = l.kind === 'custom' ? l.icon : ''
    const cls = size === 'lg' ? 'h-9 w-9 rounded-lg text-lg' : 'h-6 w-6 rounded-md text-sm'
    return (
      <span className={`relative flex shrink-0 items-center justify-center ${cls}`} style={{ backgroundColor: `${color}22` }}>
        {emoji ? (
          <span>{emoji}</span>
        ) : (
          <NotebookPen size={size === 'lg' ? 18 : 14} style={{ color }} />
        )}
        {shared && (
          <span className="absolute -bottom-0.5 -right-0.5 flex items-center justify-center rounded-full border border-white bg-white shadow-sm">
            <Users size={size === 'lg' ? 10 : 8} style={{ color }} />
          </span>
        )}
      </span>
    )
  }

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-4 md:flex-row">
      {/* List sidebar */}
      <div className="card w-full shrink-0 p-3 md:w-56">
        <button
          onClick={() => setActiveListId('')}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${!activeListId ? 'bg-[var(--app-accent-soft)] font-medium' : ''}`}
        >
          <List size={14} className="shrink-0" />
          {t('notes.allNotes')}
          <span className="ml-auto text-xs text-[var(--app-muted)]">{notes.length}</span>
        </button>
        <div className="mb-1 mt-3 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('notes.lists')}</div>
        <div className="flex flex-col gap-0.5">
          {lists.map((l) => {
            const selected = activeListId === l.id
            return (
              <div key={l.id} className={`group relative flex items-center rounded-md ${selected ? 'bg-[var(--app-accent-soft)]' : ''}`}>
                <button
                  onClick={() => setActiveListId(l.id)}
                  className={`flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ${selected ? 'font-medium' : ''}`}
                >
                  {chip(l)}
                  <span className="truncate">{listName(l)}</span>
                  <span className="ml-auto pl-1 text-xs text-[var(--app-muted)]">{countIn(l.id)}</span>
                </button>
                {l.canEdit && (
                  <button
                    onClick={() => openEditList(l)}
                    className="nav-link shrink-0 rounded px-1 opacity-0 transition-opacity group-hover:opacity-100"
                  >
                    <Pencil size={13} />
                  </button>
                )}
              </div>
            )
          })}
        </div>
        <button onClick={openCreateList} className="mt-2 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--app-accent)]">
          <Plus size={14} />
          {t('notes.newList')}
        </button>
        <div className="mt-4 border-t border-[var(--app-border)] pt-2">
          <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('notes.tags')}</div>
          {allTags.length === 0 ? (
            <div className="text-[11px] text-[var(--app-muted)]">-</div>
          ) : (
            <div className="flex flex-wrap gap-1">
              {allTags.map((tag) => {
                const on = selTags.includes(tag)
                return (
                  <button
                    key={tag}
                    onClick={() => toggleTag(tag)}
                    className={`rounded-full px-2 py-0.5 text-[11px] ${on ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'}`}
                  >
                    #{tag}
                  </button>
                )
              })}
            </div>
          )}
        </div>
      </div>

      {/* Note list */}
      <div className="min-w-0 flex-1">
        <div className="mb-4 flex items-center justify-between gap-2">
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            {activeList && chip(activeList, 'lg')}
            {activeList ? listName(activeList) : t('notes.title')}
          </h1>
          <div className="flex items-center gap-2">
            <button className="btn-ghost shrink-0" onClick={() => setTrashOpen(true)} title={t('trash.itemsTitle')}>
              <Trash2 size={16} />
            </button>
            <button className="btn-primary shrink-0" onClick={() => openCreate()}>
              <Plus size={16} />
              {t('notes.newNote')}
            </button>
          </div>
        </div>
        <div className="space-y-2">
          {tagged.length === 0 && <div className="card py-10 text-center text-sm text-[var(--app-muted)]">{t('notes.empty')}</div>}
          {tagged.map((n) => (
            <button key={n.id} onClick={() => openEdit(n)} className="card block w-full p-3 text-left transition-colors hover:bg-[var(--app-card-sub)]">
              <div className="flex items-center justify-between gap-2">
                <div className="truncate font-medium">{n.title || t('notes.untitled')}</div>
                <div className="flex shrink-0 items-center gap-1 text-xs text-[var(--app-muted)]">
                  {!activeListId && (
                    <span className="flex items-center gap-1">
                      <List size={12} />
                      {listName(listsById.get(n.calendar))}
                    </span>
                  )}
                  {n.attendees && n.attendees.length > 0 && (
                    <span className="flex items-center gap-1" title={t('notes.invitees')}>
                      <Users size={12} />
                      {n.attendees.slice(0, 2).map((a) => a.name || a.email).join(', ')}
                      {n.attendees.length > 2 ? `+${n.attendees.length - 2}` : ''}
                    </span>
                  )}
                  {n.calendar !== 'self' && n.calendar !== 'team' && (
                    <span className="flex items-center gap-1">
                      <Users size={12} />
                      {t('notes.shared')}
                    </span>
                  )}
                </div>
              </div>
              {n.body && <div className="mt-1 line-clamp-2 whitespace-pre-wrap text-sm text-[var(--app-muted)]">{n.body}</div>}
              {n.tags && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {n.tags
                    .split(',')
                    .map((x) => x.trim())
                    .filter(Boolean)
                    .map((tag) => (
                      <span key={tag} className="rounded-full bg-[var(--app-card-sub)] px-1.5 py-0.5 text-[10px]">
                        #{tag}
                      </span>
                    ))}
                </div>
              )}
            </button>
          ))}
        </div>
      </div>

      {/* List create/edit dialog */}
      {listDraft && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setListDraft(null)}>
          <div className="card w-full max-w-sm p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 flex items-center justify-between">
              <div className="text-lg font-semibold">{editListId ? t('notes.editList') : t('notes.newList')}</div>
              <button className="nav-link" onClick={() => setListDraft(null)}>
                <X size={18} />
              </button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.listName')}</label>
                <input
                  className="input"
                  placeholder={t('notes.listNamePlaceholder')}
                  value={listDraft.name}
                  autoFocus
                  onChange={(e) => setListDraft({ ...listDraft, name: e.target.value })}
                />
              </div>
              <div className="flex items-center gap-4">
                <div className="flex-1">
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.color')}</label>
                  <div className="flex flex-wrap gap-1.5">
                    {LIST_COLORS.map((c) => (
                      <button
                        key={c}
                        type="button"
                        onClick={() => setListDraft({ ...listDraft, color: c })}
                        className="h-6 w-6 rounded-full"
                        style={{ backgroundColor: c, outline: listDraft.color === c ? '2px solid var(--app-accent)' : 'none', outlineOffset: 1 }}
                      />
                    ))}
                  </div>
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.icon')}</label>
                  <div className="flex max-h-24 w-28 flex-wrap gap-1 overflow-y-auto">
                    {LIST_ICONS.map((ic) => (
                      <button
                        key={ic}
                        type="button"
                        onClick={() => setListDraft({ ...listDraft, icon: ic })}
                        className={`flex h-7 w-7 items-center justify-center rounded text-sm ${listDraft.icon === ic ? 'bg-[var(--app-accent-soft)] ring-1 ring-[var(--app-accent)]' : ''}`}
                      >
                        {ic}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.shareTitle')}</label>
                <div className="space-y-1">
                  {teamMembers
                    .filter((m) => m.id !== me?.id)
                    .map((m) => {
                      const on = listDraft.memberIds.includes(m.id)
                      return (
                        <label key={m.id} className="flex cursor-pointer items-center gap-2 text-sm">
                          <input
                            type="checkbox"
                            className="h-4 w-4"
                            checked={on}
                            onChange={(e) =>
                              setListDraft({
                                ...listDraft,
                                memberIds: e.target.checked
                                  ? [...listDraft.memberIds, m.id]
                                  : listDraft.memberIds.filter((x) => x !== m.id),
                              })
                            }
                          />
                          {m.name}
                        </label>
                      )
                    })}
                  {teamMembers.length <= 1 && <div className="text-xs text-[var(--app-muted)]">{t('notes.shareHint')}</div>}
                </div>
                <p className="mt-1.5 text-[11px] text-[var(--app-muted)]">{t('notes.shareHint')}</p>
              </div>
              <div className="flex items-center gap-2 pt-1">
                <button className="btn-primary flex-1" onClick={() => void submitList()} disabled={busy || !listDraft.name.trim()}>
                  {t('notes.save')}
                </button>
                {editListId && (
                  <PopConfirm
                    title={t('notes.deleteList')}
                    description={t('notes.deleteListConfirm')}
                    confirmText={t('common.delete')}
                    cancelText={t('common.cancel')}
                    onConfirm={() => void deleteList(editListId)}
                  >
                    <button type="button" className="btn-ghost shrink-0 text-[var(--app-danger)]" disabled={busy}>
                      <Trash2 size={16} />
                    </button>
                  </PopConfirm>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Note editor dialog */}
      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card max-h-[90vh] w-full max-w-2xl overflow-y-auto p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 flex items-center justify-between">
              <div className="text-lg font-semibold">{editing.id ? t('notes.editNote') : t('notes.newNote')}</div>
              <button className="nav-link" onClick={() => setEditing(null)}>
                <X size={18} />
              </button>
            </div>
            <div className="space-y-3">
              <input
                className="input text-lg font-medium"
                placeholder={t('notes.titlePlaceholder')}
                value={editing.title}
                autoFocus
                onChange={(e) => setEditing({ ...editing, title: e.target.value })}
              />
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.lists')}</label>
                  <select className="select" value={editing.calendar} onChange={(e) => setEditing({ ...editing, calendar: e.target.value })}>
                    {writableLists.map((l) => (
                      <option key={l.id} value={l.id}>
                        {listName(l)}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.tags')}</label>
                  <input
                    className="input"
                    placeholder={t('notes.tagsPlaceholder')}
                    value={editing.tags}
                    onChange={(e) => setEditing({ ...editing, tags: e.target.value })}
                  />
                </div>
              </div>
              <textarea
                className="input min-h-[320px] resize-y font-mono text-sm"
                placeholder={t('notes.bodyPlaceholder')}
                value={editing.body}
                onChange={(e) => setEditing({ ...editing, body: e.target.value })}
              />
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('notes.inviteTitle')}</label>
                <div className="space-y-1 rounded-lg border border-[var(--app-border)] p-2">
                  {teamMembers
                    .filter((m) => m.id !== me?.id)
                    .map((m) => {
                      const on = editing.attendeeIds.includes(m.id)
                      return (
                        <label key={m.id} className="flex cursor-pointer items-center gap-2 text-sm">
                          <input
                            type="checkbox"
                            className="h-4 w-4"
                            checked={on}
                            onChange={(e) =>
                              setEditing({
                                ...editing,
                                attendeeIds: e.target.checked
                                  ? [...editing.attendeeIds, m.id]
                                  : editing.attendeeIds.filter((x) => x !== m.id),
                              })
                            }
                          />
                          <span className="truncate">{m.name}</span>
                        </label>
                      )
                    })}
                  {teamMembers.length <= 1 && <div className="text-xs text-[var(--app-muted)]">{t('notes.shareHint')}</div>}
                </div>
                <p className="mt-1.5 text-[11px] text-[var(--app-muted)]">{t('notes.inviteHint')}</p>
              </div>
              <AttachmentField kind="note" itemId={editing.id} />
              <div className="flex gap-2 pt-2">
                <button className="btn-primary flex-1" onClick={() => void submit()} disabled={busy || (!editing.title.trim() && !editing.body.trim())}>
                  {t('notes.save')}
                </button>
                {editing.id && (
                  <PopConfirm
                    title={t('notes.deleteTitle')}
                    description={t('notes.deleteConfirm')}
                    confirmText={t('common.delete')}
                    cancelText={t('common.cancel')}
                    onConfirm={() => {
                      void remove(editing.id!)
                      setEditing(null)
                    }}
                  >
                    <button className="btn-ghost text-[var(--app-danger)]" disabled={busy}>
                      <Trash2 size={16} />
                    </button>
                  </PopConfirm>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
      {trashOpen && <ItemTrashPanel kind="note" onClose={() => setTrashOpen(false)} onChanged={() => void load()} />}
    </div>
  )
}
