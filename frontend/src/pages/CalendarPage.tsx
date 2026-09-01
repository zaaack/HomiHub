import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight, Plus, X, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import type { CalendarEvent, Visibility } from '../types'

const CATS = ['work', 'school', 'family'] as const
const VIS = [1, 2, 3] as const
const REPEAT = [
  { value: '', labelKey: 'noRepeat' },
  { value: 'FREQ=DAILY', labelKey: 'daily' },
  { value: 'FREQ=WEEKLY', labelKey: 'weekly' },
  { value: 'FREQ=MONTHLY', labelKey: 'monthly' },
] as const

const catColor: Record<string, string> = {
  work: 'bg-sky-100 text-sky-700',
  school: 'bg-emerald-100 text-emerald-700',
  family: 'bg-amber-100 text-amber-700',
}

function fmtLocal(iso: string): string {
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function startOfDay(d: Date): Date {
  const c = new Date(d)
  c.setHours(0, 0, 0, 0)
  return c
}

function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

interface FormState {
  id?: string
  title: string
  category: string
  location: string
  description: string
  startsAt: string
  endsAt: string
  allDay: boolean
  rrule: string
  visibility: Visibility
}

const emptyForm = (date: Date): FormState => {
  const start = new Date(date)
  start.setHours(9, 0, 0, 0)
  const end = new Date(start)
  end.setHours(10, 0, 0, 0)
  return {
    title: '',
    category: 'family',
    location: '',
    description: '',
    startsAt: fmtLocal(start.toISOString()),
    endsAt: fmtLocal(end.toISOString()),
    allDay: false,
    rrule: '',
    visibility: 3,
  }
}

export default function CalendarPage() {
  const { t } = useTranslation()
  const [cursor, setCursor] = useState(() => startOfDay(new Date()))
  const [events, setEvents] = useState<CalendarEvent[]>([])
  const [selected, setSelected] = useState<Date>(() => startOfDay(new Date()))
  const [editing, setEditing] = useState<FormState | null>(null)
  const [busy, setBusy] = useState(false)

  const range = useMemo(() => {
    const from = new Date(cursor.getFullYear(), cursor.getMonth(), 1)
    const to = new Date(cursor.getFullYear(), cursor.getMonth() + 1, 1)
    return { from, to }
  }, [cursor])

  const load = async () => {
    const from = range.from.toISOString()
    const to = range.to.toISOString()
    try {
      const data = await api.get<CalendarEvent[]>(`/api/v1/events?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`)
      setEvents(data)
    } catch {
      setEvents([])
    }
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [range])

  const monthLabel = useMemo(() => {
    const k = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'][cursor.getMonth()]
    return `${t(`calendar.month.${k}`)} ${cursor.getFullYear()}`
  }, [cursor, t])

  const cells = useMemo(() => {
    const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1)
    const start = startOfDay(new Date(first))
    start.setDate(start.getDate() - start.getDay())
    const out: Date[] = []
    for (let i = 0; i < 42; i++) {
      out.push(startOfDay(new Date(start)))
      start.setDate(start.getDate() + 1)
    }
    return out
  }, [cursor])

  const eventsByDay = useMemo(() => {
    const m = new Map<string, CalendarEvent[]>()
    for (const ev of events) {
      const d = startOfDay(new Date(ev.start))
      const k = d.toDateString()
      if (!m.has(k)) m.set(k, [])
      m.get(k)!.push(ev)
    }
    for (const list of m.values()) {
      list.sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime())
    }
    return m
  }, [events])

  const selectedEvents = eventsByDay.get(selected.toDateString()) ?? []

  const openCreate = (d: Date) => {
    setSelected(startOfDay(d))
    setEditing(emptyForm(d))
  }

  const openEdit = (ev: CalendarEvent) => {
    if (ev.source === 'todo') return
    setEditing({
      id: ev.id,
      title: ev.title,
      category: ev.category,
      location: ev.location,
      description: ev.description,
      startsAt: fmtLocal(ev.start),
      endsAt: fmtLocal(ev.end),
      allDay: ev.allDay,
      rrule: ev.rrule,
      visibility: ev.visibility,
    })
  }

  const submit = async () => {
    if (!editing) return
    setBusy(true)
    try {
      const payload = {
        title: editing.title,
        category: editing.category,
        location: editing.location,
        description: editing.description,
        startsAt: new Date(editing.startsAt).toISOString(),
        endsAt: new Date(editing.endsAt).toISOString(),
        allDay: editing.allDay,
        rrule: editing.rrule,
        visibility: editing.visibility,
      }
      if (editing.id) {
        await api.put(`/api/v1/events/${editing.id}`, payload)
      } else {
        await api.post('/api/v1/events', payload)
      }
      setEditing(null)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!editing?.id) return
    setBusy(true)
    try {
      await api.del(`/api/v1/events/${editing.id}`)
      setEditing(null)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const weekdays = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']

  return (
    <div className="grid gap-4 xl:grid-cols-[1fr_320px]">
      <div className="card p-4">
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <button className="btn-ghost" onClick={() => setCursor(new Date(cursor.getFullYear(), cursor.getMonth() - 1, 1))}>
              <ChevronLeft size={16} />
            </button>
            <div className="min-w-[160px] text-center text-lg font-semibold">{monthLabel}</div>
            <button className="btn-ghost" onClick={() => setCursor(new Date(cursor.getFullYear(), cursor.getMonth() + 1, 1))}>
              <ChevronRight size={16} />
            </button>
          </div>
          <button className="btn-ghost" onClick={() => setCursor(startOfDay(new Date()))}>
            {t('calendar.today')}
          </button>
        </div>
        <div className="grid grid-cols-7 gap-px overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-border)]">
          {weekdays.map((d) => (
            <div key={d} className="bg-[var(--app-card-sub)] py-1 text-center text-xs font-medium text-[var(--app-muted)]">
              {t(`calendar.week.${d}`)}
            </div>
          ))}
          {cells.map((d, i) => {
            const inMonth = d.getMonth() === cursor.getMonth()
            const isToday = sameDay(d, new Date())
            const isSelected = sameDay(d, selected)
            const dayEvents = eventsByDay.get(d.toDateString()) ?? []
            return (
              <button
                key={i}
                onClick={() => setSelected(d)}
                onDoubleClick={() => openCreate(d)}
                className={`flex min-h-[88px] flex-col items-stretch bg-[var(--app-card)] p-1 text-left transition-colors hover:bg-[var(--app-card-sub)] ${
                  inMonth ? '' : 'opacity-40'
                }`}
              >
                <div className="flex items-center justify-between px-1">
                  <span
                    className={`flex h-6 w-6 items-center justify-center rounded-full text-xs ${
                      isToday ? 'bg-[var(--app-accent)] font-bold text-white' : ''
                    } ${isSelected && !isToday ? 'bg-[var(--app-accent-soft)] font-semibold' : ''}`}
                  >
                    {d.getDate()}
                  </span>
                </div>
                <div className="mt-0.5 space-y-0.5 overflow-hidden">
                  {dayEvents.slice(0, 3).map((ev) => (
                    <div
                      key={ev.id}
                      onClick={(e) => {
                        e.stopPropagation()
                        openEdit(ev)
                      }}
                      className={`truncate rounded px-1 py-0.5 text-[11px] leading-tight ${catColor[ev.category] ?? catColor.family}`}
                    >
                      {ev.source === 'todo' && <span className="opacity-70">[{t('calendar.sourceTodo')}] </span>}
                      {ev.title}
                    </div>
                  ))}
                  {dayEvents.length > 3 && (
                    <div className="px-1 text-[11px] text-[var(--app-muted)]">+{dayEvents.length - 3} more</div>
                  )}
                </div>
              </button>
            )
          })}
        </div>
      </div>

      <div className="card flex flex-col p-4">
        <div className="mb-3 flex items-center justify-between">
          <div className="font-semibold">
            {selected.getMonth() + 1}/{selected.getDate()}
          </div>
          <button className="btn-primary" onClick={() => openCreate(selected)}>
            <Plus size={16} />
            {t('calendar.newEvent')}
          </button>
        </div>
        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto">
          {selectedEvents.length === 0 && (
            <div className="py-8 text-center text-sm text-[var(--app-muted)]">{t('calendar.empty')}</div>
          )}
          {selectedEvents.map((ev) => (
            <div
              key={ev.id}
              onClick={() => openEdit(ev)}
              className="cursor-pointer rounded-lg border border-[var(--app-border)] p-3 transition-colors hover:bg-[var(--app-card-sub)]"
            >
              <div className="flex items-center justify-between gap-2">
                <div className={`rounded px-1.5 py-0.5 text-[11px] ${catColor[ev.category] ?? catColor.family}`}>
                  {t(`calendar.cat${ev.category[0].toUpperCase()}${ev.category.slice(1)}`)}
                </div>
                {ev.source === 'todo' && (
                  <div className="text-[11px] text-[var(--app-muted)]">[{t('calendar.sourceTodo')}]</div>
                )}
              </div>
              <div className="mt-1 font-medium">{ev.title}</div>
              <div className="mt-0.5 text-xs text-[var(--app-muted)]">
                {new Date(ev.start).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} -{' '}
                {new Date(ev.end).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                {ev.rrule ? ` · ${t('calendar.weekly')}` : ''}
              </div>
            </div>
          ))}
        </div>
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card max-h-[90vh] w-full max-w-md overflow-y-auto p-5" onClick={(e) => e.stopPropagation()}>
            <div className="mb-4 flex items-center justify-between">
              <div className="text-lg font-semibold">
                {editing.id ? t('calendar.editEvent') : t('calendar.newEvent')}
              </div>
              <button className="nav-link" onClick={() => setEditing(null)}>
                <X size={18} />
              </button>
            </div>
            <div className="space-y-3">
              <input
                className="input"
                placeholder={t('calendar.titlePlaceholder')}
                value={editing.title}
                onChange={(e) => setEditing({ ...editing, title: e.target.value })}
              />
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.category')}</label>
                  <select
                    className="select"
                    value={editing.category}
                    onChange={(e) => setEditing({ ...editing, category: e.target.value })}
                  >
                    {CATS.map((c) => (
                      <option key={c} value={c}>
                        {t(`calendar.cat${c[0].toUpperCase()}${c.slice(1)}`)}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.visibility')}</label>
                  <select
                    className="select"
                    value={editing.visibility}
                    onChange={(e) => setEditing({ ...editing, visibility: Number(e.target.value) as Visibility })}
                  >
                    {VIS.map((v) => (
                      <option key={v} value={v}>
                        {v === 1 ? t('calendar.private') : v === 2 ? t('calendar.busy') : t('calendar.team')}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.start')}</label>
                  <input
                    type="datetime-local"
                    className="input"
                    value={editing.startsAt}
                    onChange={(e) => setEditing({ ...editing, startsAt: e.target.value })}
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.to')}</label>
                  <input
                    type="datetime-local"
                    className="input"
                    value={editing.endsAt}
                    onChange={(e) => setEditing({ ...editing, endsAt: e.target.value })}
                  />
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.location')}</label>
                <input
                  className="input"
                  value={editing.location}
                  onChange={(e) => setEditing({ ...editing, location: e.target.value })}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.note')}</label>
                <textarea
                  className="input min-h-[64px]"
                  value={editing.description}
                  onChange={(e) => setEditing({ ...editing, description: e.target.value })}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.repeat')}</label>
                <select
                  className="select"
                  value={editing.rrule}
                  onChange={(e) => setEditing({ ...editing, rrule: e.target.value })}
                >
                  {REPEAT.map((r) => (
                    <option key={r.value} value={r.value}>
                      {t(`calendar.${r.labelKey}`)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex items-center gap-2 pt-1">
                <input
                  type="checkbox"
                  id="allDay"
                  className="h-4 w-4"
                  checked={editing.allDay}
                  onChange={(e) => setEditing({ ...editing, allDay: e.target.checked })}
                />
                <label htmlFor="allDay" className="text-sm">
                  {t('calendar.allDay')}
                </label>
              </div>
              <div className="flex items-center gap-2 pt-2">
                <button className="btn-primary flex-1" onClick={() => void submit()} disabled={busy || !editing.title.trim()}>
                  {t('calendar.save')}
                </button>
                {editing.id && (
                  <button className="btn-ghost text-[var(--app-danger)]" onClick={() => void remove()} disabled={busy}>
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
