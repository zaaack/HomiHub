import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Bell,
  CheckSquare,
  Flag,
  List,
  Plus,
  Square,
  Trash2,
  X,
  Clock,
  Users,
  Tag,
  ChevronRight,
  Pencil,
  Inbox,
  FilterX,
} from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'
import { PopConfirm } from '../components/ui/pop-confirm'
import AttachmentField from '../components/AttachmentField'
import type { Member, Reminder, Todo, TodoList } from '../types'

function fmtLocal(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const LIST_COLORS = ['#4f8cff', '#22c55e', '#ef4444', '#f59e0b', '#a855f7', '#06b6d4', '#ec4899', '#64748b', '#84cc16', '#f97316']
const LIST_ICONS = ['📋', '🛒', '💼', '🏠', '🎓', '❤️', '⭐', '🎯', '✈️', '📚', '🧹', '🏋️', '🎮', '🍔', '💊', '🎁', '🧾', '👶', '🔧', '💰']
const PERSONAL_COLOR = '#4f8cff'
const TEAM_COLOR = '#f59e0b'

const REMINDER_OPTIONS = [
  { unit: 'min', value: 5 },
  { unit: 'min', value: 10 },
  { unit: 'min', value: 15 },
  { unit: 'min', value: 30 },
  { unit: 'hour', value: 1 },
  { unit: 'hour', value: 2 },
  { unit: 'day', value: 1 },
  { unit: 'day', value: 2 },
  { unit: 'day', value: 7 },
] as const

function remKey(r: Reminder) {
  return `${r.unit}:${r.value}`
}

function priorityLabel(p: number): 'priorityHigh' | 'priorityMedium' | 'priorityLow' | null {
  if (p >= 7) return 'priorityHigh'
  if (p >= 4) return 'priorityMedium'
  if (p >= 1) return 'priorityLow'
  return null
}

function priorityColor(p: number): string {
  if (p >= 7) return 'text-red-500'
  if (p >= 4) return 'text-amber-500'
  if (p >= 1) return 'text-sky-500'
  return 'text-[var(--app-faint)]'
}

type RepeatFreq = 'none' | 'daily' | 'weekly' | 'monthly'
interface RRuleState {
  freq: RepeatFreq
  interval: number
  weekdays: number[]
  monthDays: number[]
}
function parseRRule(rrule: string): RRuleState {
  const s: RRuleState = { freq: 'none', interval: 1, weekdays: [], monthDays: [] }
  if (!rrule) return s
  const parts: Record<string, string> = {}
  for (const seg of rrule.split(';')) {
    const i = seg.indexOf('=')
    if (i > 0) parts[seg.slice(0, i).toUpperCase()] = seg.slice(i + 1)
  }
  const f = (parts['FREQ'] || '').toLowerCase()
  if (f === 'daily') s.freq = 'daily'
  else if (f === 'weekly') s.freq = 'weekly'
  else if (f === 'monthly') s.freq = 'monthly'
  s.interval = Number(parts['INTERVAL']) || 1
  if (parts['BYDAY']) {
    s.weekdays = parts['BYDAY'].split(',').map((d) => 'MOTUWETHFRSA'.indexOf(d.trim().slice(-2)) / 2).filter((n) => n >= 0)
  }
  if (parts['BYMONTHDAY']) {
    s.monthDays = parts['BYMONTHDAY'].split(',').map((d) => Number(d.trim())).filter((n) => n >= 1 && n <= 31)
  }
  return s
}

function buildRRule(s: RRuleState): string {
  if (s.freq === 'none') return ''
  const freq = s.freq === 'daily' ? 'DAILY' : s.freq === 'weekly' ? 'WEEKLY' : 'MONTHLY'
  let out = `FREQ=${freq}`
  if (s.interval > 1) out += `;INTERVAL=${s.interval}`
  if (s.freq === 'weekly' && s.weekdays.length > 0) {
    out += ';BYDAY=' + s.weekdays.map((d) => 'SU,MO,TU,WE,TH,FR,SA'.split(',')[d]).join(',')
  }
  if (s.freq === 'monthly' && s.monthDays.length > 0) {
    out += ';BYMONTHDAY=' + s.monthDays.join(',')
  }
  return out
}

interface EditForm {
  id?: string
  title: string
  note: string
  dueAt: string
  startAt: string
  repeat: RRuleState
  calendar: string
  tags: string
  priority: number
  location: string
  url: string
  percent: number
  parentId: string
  attendeeIds: string[]
  reminders: Reminder[]
  exdates: string[]
}

interface ListDraft {
  name: string
  color: string
  icon: string
  memberIds: string[]
}

const emptyListDraft = (): ListDraft => ({
  name: '',
  color: LIST_COLORS[0],
  icon: LIST_ICONS[0],
  memberIds: [],
})

export default function TodosPage() {
  const { t } = useTranslation()
  const me = useAuth((s) => s.user)
  const [lists, setLists] = useState<TodoList[]>([])
  const [todos, setTodos] = useState<Todo[]>([])
  const [teamMembers, setTeamMembers] = useState<Member[]>([])
  const [quick, setQuick] = useState('')
  const [activeListId, setActiveListId] = useState('')
  const [showDone, setShowDone] = useState(false)
  const [selTags, setSelTags] = useState<string[]>([])
  const [prioFilter, setPrioFilter] = useState('')
  const [editing, setEditing] = useState<EditForm | null>(null)
  const [listDraft, setListDraft] = useState<ListDraft | null>(null)
  const [editListId, setEditListId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [atSel, setAtSel] = useState('')
  const [exSel, setExSel] = useState('')

  const addAtReminder = () => {
    if (!editing || !atSel) return
    const at = new Date(atSel).toISOString()
    if (editing.reminders.some((x) => remKey(x) === `at:${at}`)) return
    setEditing({ ...editing, reminders: [...editing.reminders, { unit: 'at', value: 0, at }] })
    setAtSel('')
  }

  const addExDate = () => {
    if (!editing || !exSel) return
    const iso = new Date(exSel).toISOString()
    if (editing.exdates.includes(iso)) return
    setEditing({ ...editing, exdates: [...editing.exdates, iso] })
    setExSel('')
  }

  const removeExDate = (iso: string) => {
    if (!editing) return
    setEditing({ ...editing, exdates: editing.exdates.filter((x) => x !== iso) })
  }

  const removeAtReminder = (at: string) => {
    if (!editing) return
    setEditing({ ...editing, reminders: editing.reminders.filter((x) => remKey(x) !== `at:${at}`) })
  }

  const load = async () => {
    try {
      const [ls, td] = await Promise.all([
        api.get<TodoList[]>('/api/v1/todo-lists'),
        api.get<Todo[]>('/api/v1/todos'),
      ])
      setLists(ls)
      setTodos(td)
      if (activeListId && !ls.some((l) => l.id === activeListId)) setActiveListId('')
    } catch {
      setLists([])
      setTodos([])
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

  // Keep the list set fresh whenever a shared member could have changed it.
  const listsById = useMemo(() => new Map(lists.map((l) => [l.id, l])), [lists])

  const listDisplayName = (l: TodoList | undefined): string => {
    if (!l) return t('todos.allTodos')
    if (l.kind === 'personal') return t('todos.personalList')
    if (l.kind === 'team') return t('todos.teamList')
    return l.name || l.id
  }

  const allTags = useMemo(() => {
    const s = new Set<string>()
    for (const td of todos) {
      for (const tag of td.tags.split(',').map((x) => x.trim())) {
        if (tag) s.add(tag)
      }
    }
    return [...s].sort((a, b) => a.localeCompare(b, 'zh'))
  }, [todos])

  const hasFilters = selTags.length > 0 || prioFilter !== ''

  const filtered = useMemo(() => {
    let list = todos
    if (activeListId) {
      list = list.filter((x) => x.calendar === activeListId)
    }
    if (selTags.length > 0) {
      list = list.filter((x) => {
        const tags = x.tags.split(',').map((s) => s.trim()).filter(Boolean)
        return selTags.every((st) => tags.includes(st))
      })
    }
    if (prioFilter === 'high') list = list.filter((x) => x.priority >= 7)
    else if (prioFilter === 'medium') list = list.filter((x) => x.priority >= 4 && x.priority <= 6)
    else if (prioFilter === 'low') list = list.filter((x) => x.priority >= 1 && x.priority <= 3)
    if (!showDone) {
      list = list.filter((x) => !x.completed)
    }
    return list
  }, [todos, activeListId, selTags, prioFilter, showDone])

  const tree = useMemo(() => {
    const byParent = new Map<string, Todo[]>()
    const roots: Todo[] = []
    for (const td of filtered) {
      if (td.parentId && filtered.some((x) => x.id === td.parentId)) {
        if (!byParent.has(td.parentId)) byParent.set(td.parentId, [])
        byParent.get(td.parentId)!.push(td)
      } else {
        roots.push(td)
      }
    }
    for (const list of byParent.values()) {
      list.sort((a, b) => (a.completed === b.completed ? 0 : a.completed ? 1 : -1))
    }
    const sortRoots = (a: Todo, b: Todo) => {
      if (a.completed !== b.completed) return a.completed ? 1 : -1
      const ad = a.dueAt ? new Date(a.dueAt).getTime() : Infinity
      const bd = b.dueAt ? new Date(b.dueAt).getTime() : Infinity
      if (ad !== bd) return ad - bd
      return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
    }
    roots.sort(sortRoots)
    const out: { todo: Todo; depth: number }[] = []
    const walk = (td: Todo, depth: number) => {
      out.push({ todo: td, depth })
      const kids = byParent.get(td.id) || []
      kids.sort(sortRoots)
      for (const k of kids) walk(k, depth + 1)
    }
    for (const r of roots) walk(r, 0)
    return out
  }, [filtered])

  const isOverdue = (todo: Todo) => !!todo.dueAt && !todo.completed && new Date(todo.dueAt).getTime() < Date.now()

  const activeList = activeListId ? listsById.get(activeListId) : undefined
  // Where quick adds land: the active list, or the personal list when viewing All.
  const quickListId = activeListId || 'self'

  const quickAdd = async () => {
    const title = quick.trim()
    if (!title) return
    setBusy(true)
    try {
      await api.post('/api/v1/todos', { title, calendar: quickListId })
      setQuick('')
      await load()
    } finally {
      setBusy(false)
    }
  }

  const toggle = async (todo: Todo) => {
    try {
      await api.patch(`/api/v1/todos/${todo.id}/toggle`)
      await load()
    } catch {
      /* ignore */
    }
  }

  const remove = async (id: string) => {
    try {
      await api.del(`/api/v1/todos/${id}`)
      await load()
    } catch {
      /* ignore */
    }
  }

  const submit = async () => {
    if (!editing) return
    setBusy(true)
    try {
      const payload = {
        title: editing.title.trim(),
        note: editing.note,
        dueAt: editing.dueAt ? new Date(editing.dueAt).toISOString() : null,
        startAt: editing.startAt ? new Date(editing.startAt).toISOString() : null,
        rrule: buildRRule(editing.repeat),
        calendar: editing.calendar || 'self',
        tags: editing.tags,
        priority: editing.priority,
        location: editing.location,
        url: editing.url,
        percent: editing.percent,
        parentId: editing.parentId,
        attendees: editing.attendeeIds,
        reminders: editing.reminders,
        exdates: editing.exdates,
      }
      if (editing.id) {
        await api.put(`/api/v1/todos/${editing.id}`, payload)
      } else {
        await api.post('/api/v1/todos', payload)
      }
      setEditing(null)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const openEdit = (todo: Todo) => {
    setEditing({
      id: todo.id,
      title: todo.title,
      note: todo.note,
      dueAt: fmtLocal(todo.dueAt),
      startAt: fmtLocal(todo.startAt),
      repeat: parseRRule(todo.rrule),
      calendar: todo.calendar,
      tags: todo.tags,
      priority: todo.priority,
      location: todo.location,
      url: todo.url,
      percent: todo.percent,
      parentId: todo.parentId,
      attendeeIds: (todo.attendees ?? []).map((a) => a.id).filter(Boolean),
      reminders: todo.reminders ?? [],
      exdates: todo.exdates ?? [],
    })
  }

  const toggleTag = (tag: string) => {
    setSelTags((prev) => (prev.includes(tag) ? prev.filter((x) => x !== tag) : [...prev, tag]))
  }

  const clearFilters = () => {
    setSelTags([])
    setPrioFilter('')
  }

  const writableLists = lists.filter((l) => l.writable)

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
      if (editListId) {
        await api.put(`/api/v1/todo-lists/${editListId}`, payload)
      } else {
        await api.post('/api/v1/todo-lists', payload)
      }
      setListDraft(null)
      setEditListId(null)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const openCreateList = () => {
    setEditListId(null)
    setListDraft(emptyListDraft())
  }

  const openEditList = (l: TodoList) => {
    setEditListId(l.id)
    setListDraft({
      name: l.name,
      color: l.color || PERSONAL_COLOR,
      icon: l.icon || LIST_ICONS[0],
      memberIds: l.members?.map((m) => m.id) ?? [],
    })
  }

  const deleteList = async (id: string) => {
    try {
      await api.del(`/api/v1/todo-lists/${id}`)
      if (activeListId === id) setActiveListId('')
      setListDraft(null)
      setEditListId(null)
      await load()
    } catch {
      /* ignore */
    }
  }

  const allTop = filtered.filter((x) => !x.parentId || !filtered.some((y) => y.id === x.parentId))
  const doneCount = todos.filter((x) => x.completed && (!activeListId || x.calendar === activeListId)).length
  const pendingIn = (id: string) => todos.filter((x) => x.calendar === id && !x.completed).length
  const weekdays = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']

  const renderListChip = (l: TodoList, size: 'sm' | 'lg' = 'sm') => {
    const color = l.kind === 'personal' ? PERSONAL_COLOR : l.kind === 'team' ? TEAM_COLOR : l.color || PERSONAL_COLOR
    const shared = l.kind === 'team' || (l.members?.length ?? 0) > 0
    const emoji = l.kind === 'custom' ? l.icon : ''
    const cls = size === 'lg' ? 'h-9 w-9 rounded-lg text-lg' : 'h-6 w-6 rounded-md text-sm'
    return (
      <span className={`relative flex shrink-0 items-center justify-center ${cls}`} style={{ backgroundColor: `${color}22` }}>
        {emoji ? (
          <span>{emoji}</span>
        ) : l.kind === 'personal' ? (
          <Inbox size={size === 'lg' ? 18 : 14} style={{ color }} />
        ) : (
          <Users size={size === 'lg' ? 18 : 14} style={{ color }} />
        )}
        {shared && (
          <span
            className="absolute -bottom-0.5 -right-0.5 flex items-center justify-center rounded-full border border-white bg-white shadow-sm"
            title={t('todos.shareTitle')}
          >
            <Users size={size === 'lg' ? 10 : 8} style={{ color }} />
          </span>
        )}
      </span>
    )
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4 md:flex-row">
      <div className="card w-full shrink-0 p-3 md:w-52">
        <button
          onClick={() => setActiveListId('')}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${!activeListId ? 'bg-[var(--app-accent-soft)] font-medium' : ''}`}
        >
          <List size={14} className="shrink-0" />
          {t('todos.allTodos')}
          <span className="ml-auto text-xs text-[var(--app-muted)]">{todos.length}</span>
        </button>
        <div className="mb-1 mt-3 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('todos.lists')}</div>
        <div className="flex flex-col gap-0.5">
          {lists.map((l) => {
            const selected = activeListId === l.id
            return (
              <div key={l.id} className={`group relative flex items-center rounded-md ${selected ? 'bg-[var(--app-accent-soft)]' : ''}`}>
                <button
                  onClick={() => setActiveListId(l.id)}
                  className={`flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ${selected ? 'font-medium' : ''}`}
                >
                  {renderListChip(l)}
                  <span className="truncate">{listDisplayName(l)}</span>
                  <span className="ml-auto pl-1 text-xs text-[var(--app-muted)]">{pendingIn(l.id)}</span>
                </button>
                {l.canEdit && (
                  <button
                    onClick={() => openEditList(l)}
                    className="nav-link shrink-0 rounded px-1 opacity-0 transition-opacity group-hover:opacity-100"
                    title={t('todos.editList')}
                  >
                    <Pencil size={13} />
                  </button>
                )}
              </div>
            )
          })}
        </div>
        <button
          onClick={openCreateList}
          className="mt-2 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--app-accent)]"
        >
          <Plus size={14} />
          {t('todos.newList')}
        </button>

        <div className="mt-4 border-t border-[var(--app-border)] pt-2">
          <button
            onClick={() => setShowDone((v) => !v)}
            className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${showDone ? 'font-medium' : ''}`}
          >
            <CheckSquare size={14} />
            {t('todos.completed')}
            <span className="ml-auto text-xs text-[var(--app-muted)]">{doneCount}</span>
          </button>
        </div>

        <div className="mt-4 border-t border-[var(--app-border)] pt-2">
          <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('todos.tags')}</div>
          {allTags.length === 0 ? (
            <div className="text-[11px] text-[var(--app-muted)]">{t('todos.noGroup')}</div>
          ) : (
            <div className="flex flex-wrap gap-1">
              {allTags.map((tag) => {
                const on = selTags.includes(tag)
                return (
                  <button
                    key={tag}
                    onClick={() => toggleTag(tag)}
                    className={`rounded-full px-2 py-0.5 text-[11px] ${
                      on ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                    }`}
                  >
                    #{tag}
                  </button>
                )
              })}
            </div>
          )}
          <div className="mb-1 mt-3 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('todos.priority')}</div>
          <select className="select w-full text-sm" value={prioFilter} onChange={(e) => setPrioFilter(e.target.value)}>
            <option value="">{t('todos.anyPriority')}</option>
            <option value="high">{t('todos.priorityHigh')}</option>
            <option value="medium">{t('todos.priorityMedium')}</option>
            <option value="low">{t('todos.priorityLow')}</option>
          </select>
          {hasFilters && (
            <button
              onClick={clearFilters}
              className="mt-2 flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-xs text-[var(--app-accent)]"
            >
              <FilterX size={13} />
              {t('todos.clearFilters')}
            </button>
          )}
        </div>
      </div>

      <div className="min-w-0 flex-1">
        <div className="mb-4 flex items-center justify-between gap-2">
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            {activeList && renderListChip(activeList, 'lg')}
            {activeList ? listDisplayName(activeList) : t('todos.title')}
          </h1>
          {activeList && (
            <span className="flex items-center gap-1 text-xs text-[var(--app-muted)]">
              {activeList.kind !== 'personal' && (activeList.members?.length ?? 0) > 0 && (
                <>
                  <Users size={13} />
                  {activeList.members.length}
                </>
              )}
            </span>
          )}
        </div>

        <div className="card mb-4 flex items-center gap-2 p-3">
          <input
            className="input"
            placeholder={`${t('todos.addPlaceholder')} → ${listDisplayName(listsById.get(quickListId))}`}
            value={quick}
            onChange={(e) => setQuick(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void quickAdd()
            }}
          />
          <button className="btn-primary shrink-0" onClick={() => void quickAdd()} disabled={busy}>
            <Plus size={16} />
          </button>
        </div>

        <div className="space-y-2">
          {tree.length === 0 && (
            <div className="card py-10 text-center text-sm text-[var(--app-muted)]">{t('todos.empty')}</div>
          )}
          {tree.map(({ todo, depth }) => (
            <TodoRow
              key={todo.id}
              todo={todo}
              depth={depth}
              overdue={isOverdue(todo)}
              allTop={allTop}
              onToggle={() => void toggle(todo)}
              onEdit={() => openEdit(todo)}
              onDelete={() => void remove(todo.id)}
            />
          ))}
        </div>
      </div>

      {/* 新建 / 编辑清单弹窗 */}
      {listDraft && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setListDraft(null)}>
          <div className="card w-full max-w-sm p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 flex items-center justify-between">
              <div className="text-lg font-semibold">{editListId ? t('todos.editList') : t('todos.newList')}</div>
              <button className="nav-link" onClick={() => setListDraft(null)}>
                <X size={18} />
              </button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.listName')}</label>
                <input
                  className="input"
                  placeholder={t('todos.listNamePlaceholder')}
                  value={listDraft.name}
                  autoFocus
                  onChange={(e) => setListDraft({ ...listDraft, name: e.target.value })}
                />
              </div>
              <div className="flex items-center gap-4">
                <div className="flex-1">
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.color')}</label>
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
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.icon')}</label>
                  <div className="flex max-h-24 w-28 flex-wrap gap-1 overflow-y-auto">
                    {LIST_ICONS.map((ic) => (
                      <button
                        key={ic}
                        type="button"
                        onClick={() => setListDraft({ ...listDraft, icon: ic })}
                        className={`flex h-7 w-7 items-center justify-center rounded text-sm ${
                          listDraft.icon === ic ? 'bg-[var(--app-accent-soft)] ring-1 ring-[var(--app-accent)]' : ''
                        }`}
                      >
                        {ic}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.shareTitle')}</label>
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
                  {teamMembers.length <= 1 && <div className="text-xs text-[var(--app-muted)]">{t('todos.shareHint')}</div>}
                </div>
                <p className="mt-1.5 text-[11px] text-[var(--app-muted)]">{t('todos.shareHint')}</p>
              </div>
              <div className="flex items-center gap-2 pt-1">
                <button
                  className="btn-primary flex-1"
                  onClick={() => void submitList()}
                  disabled={busy || !listDraft.name.trim()}
                >
                  {t('common.save')}
                </button>
                {editListId && (
                  <PopConfirm
                    title={t('todos.deleteList')}
                    description={t('todos.deleteListConfirm')}
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

      {/* 新建 / 编辑待办弹窗 */}
      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card max-h-[90vh] w-full max-w-md overflow-y-auto p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 flex items-center justify-between">
              <div className="text-lg font-semibold">{editing.id ? t('todos.edit') : t('todos.add')}</div>
              <button className="nav-link" onClick={() => setEditing(null)}>
                <X size={18} />
              </button>
            </div>
            <div className="space-y-3">
              <input
                className="input"
                placeholder={t('todos.addPlaceholder')}
                value={editing.title}
                onChange={(e) => setEditing({ ...editing, title: e.target.value })}
              />
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.list')}</label>
                  <select
                    className="select"
                    value={editing.calendar}
                    onChange={(e) => setEditing({ ...editing, calendar: e.target.value })}
                  >
                    {writableLists.map((l) => (
                      <option key={l.id} value={l.id}>
                        {listDisplayName(l)}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.priority')}</label>
                  <select
                    className="select"
                    value={editing.priority}
                    onChange={(e) => setEditing({ ...editing, priority: Number(e.target.value) })}
                  >
                    <option value={0}>-</option>
                    <option value={9}>{t('todos.priorityHigh')}</option>
                    <option value={5}>{t('todos.priorityMedium')}</option>
                    <option value={1}>{t('todos.priorityLow')}</option>
                  </select>
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.dueDate')}</label>
                  <input
                    type="datetime-local"
                    className="input"
                    value={editing.dueAt}
                    onChange={(e) => setEditing({ ...editing, dueAt: e.target.value })}
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.startDate')}</label>
                  <input
                    type="datetime-local"
                    className="input"
                    value={editing.startAt}
                    onChange={(e) => setEditing({ ...editing, startAt: e.target.value })}
                  />
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.repeat')}</label>
                <div className="flex items-center gap-2">
                  <select
                    className="select flex-1"
                    value={editing.repeat.freq}
                    onChange={(e) => setEditing({ ...editing, repeat: { ...editing.repeat, freq: e.target.value as RepeatFreq } })}
                  >
                    <option value="none">{t('calendar.noRepeat')}</option>
                    <option value="daily">{t('calendar.daily')}</option>
                    <option value="weekly">{t('calendar.weekly')}</option>
                    <option value="monthly">{t('calendar.monthly')}</option>
                  </select>
                  {editing.repeat.freq !== 'none' && (
                    <div className="flex items-center gap-1 text-xs text-[var(--app-muted)]">
                      <span>{t('todos.every')}</span>
                      <input
                        type="number"
                        min={1}
                        max={99}
                        className="input w-16 px-2 py-1 text-center"
                        value={editing.repeat.interval}
                        onChange={(e) =>
                          setEditing({ ...editing, repeat: { ...editing.repeat, interval: Math.max(1, Number(e.target.value) || 1) } })
                        }
                      />
                      <span>
                        {editing.repeat.freq === 'daily'
                          ? t('todos.daysUnit')
                          : editing.repeat.freq === 'weekly'
                            ? t('todos.weeksUnit')
                            : t('todos.monthsUnit')}
                      </span>
                    </div>
                  )}
                </div>
                {editing.repeat.freq === 'weekly' && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {weekdays.map((w, i) => (
                      <button
                        key={w}
                        type="button"
                        onClick={() =>
                          setEditing({
                            ...editing,
                            repeat: {
                              ...editing.repeat,
                              weekdays: editing.repeat.weekdays.includes(i)
                                ? editing.repeat.weekdays.filter((x) => x !== i)
                                : [...editing.repeat.weekdays, i].sort(),
                            },
                          })
                        }
                        className={`rounded-full px-2.5 py-1 text-xs ${
                          editing.repeat.weekdays.includes(i)
                            ? 'bg-[var(--app-accent)] text-white'
                            : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                        }`}
                      >
                        {t(`calendar.week.${w}`)}
                      </button>
                    ))}
                  </div>
                )}
                {editing.repeat.freq === 'monthly' && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
                      <button
                        key={d}
                        type="button"
                        onClick={() =>
                          setEditing({
                            ...editing,
                            repeat: {
                              ...editing.repeat,
                              monthDays: editing.repeat.monthDays.includes(d)
                                ? editing.repeat.monthDays.filter((x) => x !== d)
                                : [...editing.repeat.monthDays, d].sort((a, b) => a - b),
                            },
                          })
                        }
                        className={`h-7 w-7 rounded text-xs ${
                          editing.repeat.monthDays.includes(d)
                            ? 'bg-[var(--app-accent)] text-white'
                            : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                        }`}
                      >
                        {d}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.tags')}</label>
                <input
                  className="input"
                  placeholder={t('todos.tagsPlaceholder')}
                  value={editing.tags}
                  onChange={(e) => setEditing({ ...editing, tags: e.target.value })}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.inviteTitle')}</label>
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
                  {teamMembers.length <= 1 && <div className="text-xs text-[var(--app-muted)]">{t('todos.shareHint')}</div>}
                </div>
                <p className="mt-1.5 text-[11px] text-[var(--app-muted)]">{t('todos.inviteHint')}</p>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.reminders')}</label>
                <div className="flex flex-wrap gap-1.5">
                  {REMINDER_OPTIONS.map((r) => (
                    <button
                      key={remKey(r as unknown as Reminder)}
                      type="button"
                      onClick={() => {
                        const key = remKey(r as unknown as Reminder)
                        const has = editing.reminders.some((x) => remKey(x) === key)
                        setEditing({
                          ...editing,
                          reminders: has ? editing.reminders.filter((x) => remKey(x) !== key) : [...editing.reminders, r as unknown as Reminder],
                        })
                      }}
                      className={`rounded-full px-2.5 py-1 text-xs ${
                        editing.reminders.some((x) => remKey(x) === remKey(r as unknown as Reminder))
                          ? 'bg-[var(--app-accent)] text-white'
                          : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                      }`}
                    >
                      {r.value} {t(`todos.remindUnit${r.unit === 'min' ? 'Min' : r.unit === 'hour' ? 'Hour' : 'Day'}`)}
                    </button>
                  ))}
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <input type="datetime-local" className="input flex-1" value={atSel} onChange={(e) => setAtSel(e.target.value)} />
                  <button type="button" className="btn-ghost shrink-0" onClick={addAtReminder}>
                    <Plus size={16} />
                  </button>
                </div>
                {editing.reminders.filter((x) => x.unit === 'at').length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {editing.reminders
                      .filter((x) => x.unit === 'at' && x.at)
                      .map((x) => (
                        <span
                          key={remKey(x)}
                          className="flex items-center gap-1 rounded-full bg-[var(--app-accent)] px-2.5 py-1 text-xs text-white"
                        >
                          {new Date(x.at!).toLocaleString()}
                          <button type="button" onClick={() => removeAtReminder(x.at!)}>
                            <X size={12} />
                          </button>
                        </span>
                      ))}
                  </div>
                )}
              </div>
              {editing.repeat.freq !== 'none' && (
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.exceptions')}</label>
                  <div className="flex items-center gap-2">
                    <input type="datetime-local" className="input flex-1" value={exSel} onChange={(e) => setExSel(e.target.value)} />
                    <button type="button" className="btn-ghost shrink-0" onClick={addExDate}>
                      <Plus size={16} />
                    </button>
                  </div>
                  {editing.exdates.length > 0 && (
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      {editing.exdates.map((d) => (
                        <span
                          key={d}
                          className="flex items-center gap-1 rounded-full bg-[var(--app-card-sub)] px-2.5 py-1 text-xs text-[var(--app-muted)]"
                        >
                          {new Date(d).toLocaleString()}
                          <button type="button" onClick={() => removeExDate(d)}>
                            <X size={12} />
                          </button>
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              )}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.location')}</label>
                  <input
                    className="input"
                    value={editing.location}
                    onChange={(e) => setEditing({ ...editing, location: e.target.value })}
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.url')}</label>
                  <input
                    className="input"
                    value={editing.url}
                    onChange={(e) => setEditing({ ...editing, url: e.target.value })}
                  />
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.subtaskOf')}</label>
                <select
                  className="select"
                  value={editing.parentId}
                  onChange={(e) => setEditing({ ...editing, parentId: e.target.value })}
                >
                  <option value="">{t('todos.noParent')}</option>
                  {todos
                    .filter((x) => x.id !== editing.id)
                    .map((x) => (
                      <option key={x.id} value={x.id}>
                        {x.title}
                      </option>
                    ))}
                </select>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.progress')}</label>
                <input
                  type="range"
                  min={0}
                  max={100}
                  value={editing.percent}
                  onChange={(e) => setEditing({ ...editing, percent: Number(e.target.value) })}
                  className="w-full"
                />
                <div className="text-right text-xs text-[var(--app-muted)]">{editing.percent}%</div>
              </div>
              <textarea
                className="input min-h-[64px]"
                placeholder={t('todos.note')}
                value={editing.note}
                onChange={(e) => setEditing({ ...editing, note: e.target.value })}
              />
              <AttachmentField kind="todo" itemId={editing.id} />
              {editing.calendar !== 'self' && (
                <p className="flex items-center gap-1.5 text-xs text-[var(--app-muted)]">
                  <Users size={12} />
                  {listDisplayName(listsById.get(editing.calendar))}
                </p>
              )}
              <div className="flex gap-2 pt-2">
                <button className="btn-primary flex-1" onClick={() => void submit()} disabled={busy || !editing.title.trim()}>
                  {t('common.save')}
                </button>
                {editing.id && (
                  <button
                    className="btn-ghost text-[var(--app-danger)]"
                    onClick={() => {
                      void remove(editing.id!)
                      setEditing(null)
                    }}
                    disabled={busy}
                  >
                    <Trash2 size={16} />
                  </button>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function TodoRow({
  todo,
  depth,
  overdue,
  allTop,
  onToggle,
  onEdit,
  onDelete,
}: {
  todo: Todo
  depth: number
  overdue: boolean
  allTop: Todo[]
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const prio = priorityLabel(todo.priority)
  const isParent = allTop.some((x) => x.id === todo.id)
  return (
    <div
      className="card flex items-center gap-3 p-3"
      style={{ marginLeft: depth > 0 ? `${depth * 20}px` : undefined }}
    >
      <button onClick={onToggle} className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]" title={t('todos.toggle')}>
        {todo.completed ? <CheckSquare size={20} className="text-[var(--app-accent)]" /> : <Square size={20} />}
      </button>
      <button onClick={onEdit} className="min-w-0 flex-1 text-left">
        <div className="flex items-center gap-1.5">
          {isParent && <ChevronRight size={13} className="shrink-0 text-[var(--app-faint)]" />}
          {prio && <Flag size={13} className={`shrink-0 ${priorityColor(todo.priority)}`} />}
          <span className={`truncate ${todo.completed ? 'text-[var(--app-faint)] line-through' : ''}`}>{todo.title}</span>
        </div>
        {(todo.dueAt || todo.calendar === 'team' || todo.rrule || todo.group || todo.reminders?.length) && (
          <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-[var(--app-muted)]">
            {todo.dueAt && (
              <span className={`flex items-center gap-1 ${overdue ? 'font-medium text-[var(--app-danger)]' : ''}`}>
                <Clock size={12} />
                {new Date(todo.dueAt).toLocaleDateString()}
                {overdue && ` · ${t('todos.overdue')}`}
              </span>
            )}
            {todo.group && (
              <span className="flex items-center gap-1">
                <Tag size={12} />
                {todo.group}
              </span>
            )}
            {todo.rrule && <span>{todo.rrule.replace(/FREQ=/, '')}</span>}
            {todo.reminders && todo.reminders.length > 0 && (
              <span className="flex items-center gap-1">
                <Bell size={12} />
                {todo.reminders.length}
              </span>
            )}
            {todo.calendar === 'team' && (
              <span className="flex items-center gap-1">
                <Users size={12} />
                {t('todos.shared')}
              </span>
            )}
            {todo.attendees && todo.attendees.length > 0 && (
              <span className="flex items-center gap-1">
                <Users size={12} />
                {todo.attendees.slice(0, 2).map((a) => a.name || a.email).join(', ')}
                {todo.attendees.length > 2 ? `+${todo.attendees.length - 2}` : ''}
              </span>
            )}
            {todo.tags &&
              todo.tags
                .split(',')
                .map((x) => x.trim())
                .filter(Boolean)
                .slice(0, 3)
                .map((tag) => (
                  <span key={tag} className="rounded-full bg-[var(--app-card-sub)] px-1.5 py-0.5 text-[10px]">
                    #{tag}
                  </span>
                ))}
          </div>
        )}
      </button>
      <button onClick={onDelete} className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-danger)]">
        <Trash2 size={16} />
      </button>
    </div>
  )
}
