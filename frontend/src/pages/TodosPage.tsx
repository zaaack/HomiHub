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
} from 'lucide-react'
import { api } from '../api/client'
import type { Reminder, Todo } from '../types'

function fmtLocal(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

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
const emptyRRule: RRuleState = { freq: 'none', interval: 1, weekdays: [], monthDays: [] }

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
  shared: boolean
  repeat: RRuleState
  group: string
  tags: string
  priority: number
  location: string
  url: string
  percent: number
  parentId: string
  reminders: Reminder[]
}

const emptyEdit = (group: string): EditForm => ({
  title: '',
  note: '',
  dueAt: '',
  startAt: '',
  shared: false,
  repeat: emptyRRule,
  group,
  tags: '',
  priority: 0,
  location: '',
  url: '',
  percent: 0,
  parentId: '',
  reminders: [],
})

export default function TodosPage() {
  const { t } = useTranslation()
  const [todos, setTodos] = useState<Todo[]>([])
  const [quick, setQuick] = useState('')
  const [activeGroup, setActiveGroup] = useState('')
  const [showDone, setShowDone] = useState(false)
  const [editing, setEditing] = useState<EditForm | null>(null)
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      setTodos(await api.get<Todo[]>('/api/v1/todos'))
    } catch {
      setTodos([])
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const groups = useMemo(() => {
    const s = new Set<string>()
    for (const td of todos) {
      if (td.group) s.add(td.group)
    }
    return [...s].sort((a, b) => a.localeCompare(b, 'zh'))
  }, [todos])

  const allTags = useMemo(() => {
    const s = new Set<string>()
    for (const td of todos) {
      for (const tag of td.tags.split(',').map((x) => x.trim())) {
        if (tag) s.add(tag)
      }
    }
    return [...s]
  }, [todos])

  const filtered = useMemo(() => {
    let list = todos
    if (activeGroup) {
      list = list.filter((x) => x.group === activeGroup)
    }
    if (!showDone) {
      list = list.filter((x) => !x.completed)
    }
    return list
  }, [todos, activeGroup, showDone])

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

  const quickAdd = async () => {
    const title = quick.trim()
    if (!title) return
    setBusy(true)
    try {
      await api.post('/api/v1/todos', { title, group: activeGroup })
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
        group: editing.group,
        tags: editing.tags,
        priority: editing.priority,
        location: editing.location,
        url: editing.url,
        percent: editing.percent,
        parentId: editing.parentId,
        shared: editing.shared,
        reminders: editing.reminders,
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
      shared: todo.shared,
      repeat: parseRRule(todo.rrule),
      group: todo.group,
      tags: todo.tags,
      priority: todo.priority,
      location: todo.location,
      url: todo.url,
      percent: todo.percent,
      parentId: todo.parentId,
      reminders: todo.reminders ?? [],
    })
  }

  const openCreate = (group: string) => {
    setActiveGroup(group)
    setEditing(emptyEdit(group))
  }

  const allTop = filtered.filter((x) => !x.parentId || !filtered.some((y) => y.id === x.parentId))
  const doneCount = todos.filter((x) => x.completed && (!activeGroup || x.group === activeGroup)).length
  const weekdays = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4 md:flex-row">
      <div className="card w-full shrink-0 p-3 md:w-48">
        <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('todos.groups')}</div>
        <button
          onClick={() => setActiveGroup('')}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${!activeGroup ? 'bg-[var(--app-accent-soft)] font-medium' : ''}`}
        >
          <List size={14} />
          {t('todos.allTodos')}
          <span className="ml-auto text-xs text-[var(--app-muted)]">{todos.length}</span>
        </button>
        <div className="mt-1 flex flex-col gap-0.5">
          {groups.map((g) => {
            const cnt = todos.filter((x) => x.group === g && !x.completed).length
            return (
              <button
                key={g}
                onClick={() => setActiveGroup(g)}
                className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ${
                  activeGroup === g ? 'bg-[var(--app-accent-soft)] font-medium' : ''
                }`}
              >
                <Tag size={14} />
                <span className="truncate">{g}</span>
                <span className="ml-auto text-xs text-[var(--app-muted)]">{cnt}</span>
              </button>
            )
          })}
        </div>
        <button
          onClick={() => openCreate(activeGroup || '')}
          className="mt-2 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--app-accent)]"
        >
          <Plus size={14} />
          {t('todos.newGroup')}
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
        {allTags.length > 0 && (
          <div className="mt-4 border-t border-[var(--app-border)] pt-2">
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--app-muted)]">{t('todos.tags')}</div>
            <div className="flex flex-wrap gap-1">
              {allTags.map((tag) => (
                <span key={tag} className="rounded-full bg-[var(--app-card-sub)] px-2 py-0.5 text-[11px] text-[var(--app-muted)]">
                  #{tag}
                </span>
              ))}
            </div>
          </div>
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="mb-4 flex items-center justify-between">
          <h1 className="text-xl font-semibold">{activeGroup || t('todos.title')}</h1>
        </div>

        <div className="card mb-4 flex items-center gap-2 p-3">
          <input
            className="input"
            placeholder={t('todos.addPlaceholder')}
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
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('todos.group')}</label>
                  <select className="select" value={editing.group} onChange={(e) => setEditing({ ...editing, group: e.target.value })}>
                    <option value="">{t('todos.noGroup')}</option>
                    {groups.map((g) => (
                      <option key={g} value={g}>
                        {g}
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
              </div>
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
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="shared"
                  className="h-4 w-4"
                  checked={editing.shared}
                  onChange={(e) => setEditing({ ...editing, shared: e.target.checked })}
                />
                <label htmlFor="shared" className="text-sm">
                  {t('todos.shared')}
                </label>
              </div>
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
        {(todo.dueAt || todo.shared || todo.rrule || todo.group || todo.reminders?.length) && (
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
            {todo.shared && (
              <span className="flex items-center gap-1">
                <Users size={12} />
                {t('todos.shared')}
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
