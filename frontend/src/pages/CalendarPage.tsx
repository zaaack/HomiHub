import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Bell, ChevronLeft, ChevronRight, Plus, Users, X, Trash2, SlidersHorizontal } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../store/auth'
import AttachmentField from '../components/AttachmentField'
import { PopConfirm } from '../components/ui/pop-confirm'
import ItemTrashPanel from '../components/ItemTrashPanel'
import type { CalendarEvent, Member, Reminder, Visibility } from '../types'

const CATS = ['work', 'school', 'family'] as const
const VIS = [1, 2, 3] as const
const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6]

const REMINDER_OPTIONS = [
  { unit: 'min', value: 5 },
  { unit: 'min', value: 10 },
  { unit: 'min', value: 15 },
  { unit: 'min', value: 30 },
  { unit: 'hour', value: 1 },
  { unit: 'hour', value: 2 },
  { unit: 'hour', value: 12 },
  { unit: 'day', value: 1 },
  { unit: 'day', value: 2 },
  { unit: 'day', value: 7 },
] as const

function remKey(r: Reminder) {
  return r.unit === 'at' ? `at:${r.at}` : `${r.unit}:${r.value}`
}

function fmtReminder(r: Reminder, t: (k: string) => string): string {
  if (r.unit === 'at' && r.at) {
    return new Date(r.at).toLocaleString()
  }
  return `${r.value} ${t(`calendar.remindUnit${r.unit === 'min' ? 'Min' : r.unit === 'hour' ? 'Hour' : 'Day'}`)}`
}

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

interface FormState {
  id?: string
  title: string
  category: string
  location: string
  description: string
  startsAt: string
  endsAt: string
  allDay: boolean
  repeat: RRuleState
  visibility: Visibility
  attendees: string[] // invited team member ids
  reminders: Reminder[]
  exdates: string[]
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
    repeat: { freq: 'none', interval: 1, weekdays: [], monthDays: [] },
    visibility: 3,
    attendees: [],
    reminders: [],
    exdates: [],
  }
}

export default function CalendarPage() {
  const { t } = useTranslation()
  const me = useAuth((s) => s.user)
  const [cursor, setCursor] = useState(() => startOfDay(new Date()))
  const [events, setEvents] = useState<CalendarEvent[]>([])
  const [teamMembers, setTeamMembers] = useState<Member[]>([])
  const [selected, setSelected] = useState<Date>(() => startOfDay(new Date()))
  const [editing, setEditing] = useState<FormState | null>(null)
  const [busy, setBusy] = useState(false)
  const [trashOpen, setTrashOpen] = useState(false)
  const [calFilter, setCalFilter] = useState<string[]>(['self', 'team'])
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [])

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
    void api
      .get<Member[]>('/api/v1/team/members')
      .then(setTeamMembers)
      .catch(() => setTeamMembers([]))
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
      if (calFilter.length > 0 && !calFilter.includes(ev.calendar)) continue
      const d = startOfDay(new Date(ev.start))
      const k = d.toDateString()
      if (!m.has(k)) m.set(k, [])
      m.get(k)!.push(ev)
    }
    for (const list of m.values()) {
      list.sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime())
    }
    return m
  }, [events, calFilter])

  const calOptions = useMemo(() => {
    const s = new Set(events.map((ev) => ev.calendar || ''))
    s.delete('')
    return [...s].sort()
  }, [events])

  const toggleCal = (name: string) => {
    setCalFilter((prev) => (prev.includes(name) ? prev.filter((x) => x !== name) : [...prev, name]))
  }

  const calLabel = (name: string): string => {
    if (name === 'self') return t('calendar.self')
    if (name === 'team') return t('calendar.teamCal')
    return name
  }

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
      repeat: parseRRule(ev.rrule),
      visibility: ev.visibility,
      attendees: (ev.attendees ?? []).map((a) => a.id).filter(Boolean),
      reminders: ev.reminders ?? [],
      exdates: ev.exdates ?? [],
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
        rrule: buildRRule(editing.repeat),
        visibility: editing.visibility,
        attendees: editing.attendees,
        reminders: editing.reminders,
        exdates: editing.exdates,
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

  // evAttendeeStatus shows a stored PARTSTAT next to an invited member's name.
  const evAttendeeStatus = (f: FormState, memberId: string): string => {
    const ev = events.find((x) => x.id === f.id)
    const a = (ev?.attendees ?? []).find((x) => x.id === memberId)
    if (!a || a.status === 'NEEDS-ACTION') return ''
    return a.status === 'ACCEPTED' ? '✓' : '✕'
  }

  const toggleReminder = (r: Reminder) => {
    if (!editing) return
    const has = editing.reminders.some((x) => remKey(x) === remKey(r))
    setEditing({
      ...editing,
      reminders: has ? editing.reminders.filter((x) => remKey(x) !== remKey(r)) : [...editing.reminders, r],
    })
  }

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

  const toggleWeekday = (d: number) => {
    if (!editing) return
    const s = editing.repeat
    setEditing({
      ...editing,
      repeat: {
        ...s,
        weekdays: s.weekdays.includes(d) ? s.weekdays.filter((x) => x !== d) : [...s.weekdays, d].sort(),
      },
    })
  }

  const toggleMonthDay = (d: number) => {
    if (!editing) return
    const s = editing.repeat
    setEditing({
      ...editing,
      repeat: {
        ...s,
        monthDays: s.monthDays.includes(d) ? s.monthDays.filter((x) => x !== d) : [...s.monthDays, d].sort((a, b) => a - b),
      },
    })
  }

  const repeatLabel = (r: RRuleState): string => {
    if (r.freq === 'none') return t('calendar.noRepeat')
    if (r.freq === 'daily') return r.interval > 1 ? `${t('calendar.every')}${r.interval}${t('calendar.daysUnit')}` : t('calendar.daily')
    if (r.freq === 'weekly') return r.interval > 1 ? `${t('calendar.every')}${r.interval}${t('calendar.weeksUnit')}` : t('calendar.weekly')
    return r.interval > 1 ? `${t('calendar.every')}${r.interval}${t('calendar.monthsUnit')}` : t('calendar.monthly')
  }

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
          <div className="relative" ref={menuRef}>
            <button className="btn-ghost" onClick={() => setMenuOpen((v) => !v)} title={t('calendar.filterCal')}>
              <SlidersHorizontal size={16} />
            </button>
            {menuOpen && (
              <div className="absolute right-0 top-full z-50 mt-1 w-56 rounded-lg border border-[var(--app-border)] bg-[var(--app-card)] p-3 shadow-lg">
                <div className="mb-2 text-xs font-semibold text-[var(--app-muted)]">{t('calendar.filterCal')}</div>
                {calOptions.length === 0 ? (
                  <div className="text-xs text-[var(--app-faint)]">{t('calendar.noCals')}</div>
                ) : (
                  <div className="flex flex-wrap gap-1">
                    {calOptions.map((c) => (
                      <button
                        key={c}
                        onClick={() => toggleCal(c)}
                        className={`rounded-full px-2 py-1 text-xs ${calFilter.includes(c) ? 'bg-[var(--app-accent)] text-white' : 'bg-[var(--app-card-sub)] text-[var(--app-muted)] hover:bg-[var(--app-accent-soft)]'}`}
                      >
                        {calLabel(c)}
                      </button>
                    ))}
                  </div>
                )}
                <button
                  className="mt-3 flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-[var(--app-muted)] hover:bg-[var(--app-card-sub)]"
                  onClick={() => {
                    setMenuOpen(false)
                    setTrashOpen(true)
                  }}
                >
                  <Trash2 size={15} />
                  {t('trash.itemsTitle')}
                </button>
              </div>
            )}
          </div>
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
                {ev.rrule ? ` · ${repeatLabel(parseRRule(ev.rrule))}` : ''}
                {ev.reminders && ev.reminders.length > 0 && (
                  <span className="ml-1 inline-flex items-center gap-0.5">
                    <Bell size={11} />
                    {ev.reminders.length}
                  </span>
                )}
              </div>
              {ev.attendees && ev.attendees.length > 0 && (
                <div className="mt-1 flex flex-wrap items-center gap-1 text-[11px] text-[var(--app-muted)]">
                  <Users size={11} className="shrink-0" />
                  {ev.attendees.slice(0, 2).map((a) => (
                    <span key={a.id || a.email} className="truncate">
                      {a.name || a.email}
                    </span>
                  ))}
                  {ev.attendees.length > 2 && <span>+{ev.attendees.length - 2}</span>}
                </div>
              )}
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
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.inviteTitle')}</label>
                <div className="space-y-1 rounded-lg border border-[var(--app-border)] p-2">
                  {teamMembers
                    .filter((m) => m.id !== me?.id)
                    .map((m) => {
                      const on = editing.attendees.includes(m.id)
                      return (
                        <label key={m.id} className="flex cursor-pointer items-center gap-2 text-sm">
                          <input
                            type="checkbox"
                            className="h-4 w-4"
                            checked={on}
                            onChange={(e) =>
                              setEditing({
                                ...editing,
                                attendees: e.target.checked
                                  ? [...editing.attendees, m.id]
                                  : editing.attendees.filter((x) => x !== m.id),
                              })
                            }
                          />
                          <span className="truncate">{m.name}</span>
                          {evAttendeeStatus(editing, m.id) && (
                            <span className="ml-auto text-[10px] text-[var(--app-muted)]">{evAttendeeStatus(editing, m.id)}</span>
                          )}
                        </label>
                      )
                    })}
                  {teamMembers.length <= 1 && <div className="text-xs text-[var(--app-muted)]">{t('todos.shareHint')}</div>}
                </div>
                <p className="mt-1.5 text-[11px] text-[var(--app-muted)]">{t('calendar.inviteHint')}</p>
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
                      <span>{t('calendar.every')}</span>
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
                          ? t('calendar.daysUnit')
                          : editing.repeat.freq === 'weekly'
                            ? t('calendar.weeksUnit')
                            : t('calendar.monthsUnit')}
                      </span>
                    </div>
                  )}
                </div>
              </div>
              {editing.repeat.freq === 'weekly' && (
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.repeatWeeklyDays')}</label>
                  <div className="flex flex-wrap gap-1.5">
                    {WEEKDAYS.map((d) => (
                      <button
                        key={d}
                        type="button"
                        onClick={() => toggleWeekday(d)}
                        className={`rounded-full px-2.5 py-1 text-xs transition-colors ${
                          editing.repeat.weekdays.includes(d)
                            ? 'bg-[var(--app-accent)] text-white'
                            : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                        }`}
                      >
                        {t(`calendar.week.${weekdays[d]}`)}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {editing.repeat.freq === 'monthly' && (
                <div>
                  <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.repeatMonthlyDays')}</label>
                  <div className="flex flex-wrap gap-1.5">
                    {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
                      <button
                        key={d}
                        type="button"
                        onClick={() => toggleMonthDay(d)}
                        className={`h-7 w-7 rounded text-xs transition-colors ${
                          editing.repeat.monthDays.includes(d)
                            ? 'bg-[var(--app-accent)] text-white'
                            : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                        }`}
                      >
                        {d}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              <div>
                <label className="mb-1 block text-xs text-[var(--app-muted)]">{t('calendar.reminders')}</label>
                <div className="flex flex-wrap gap-1.5">
                  {REMINDER_OPTIONS.map((r) => (
                    <button
                      key={remKey(r)}
                      type="button"
                      onClick={() => toggleReminder(r as unknown as Reminder)}
                      className={`rounded-full px-2.5 py-1 text-xs transition-colors ${
                        editing.reminders.some((x) => remKey(x) === remKey(r as unknown as Reminder))
                          ? 'bg-[var(--app-accent)] text-white'
                          : 'bg-[var(--app-card-sub)] text-[var(--app-muted)]'
                      }`}
                    >
                      {fmtReminder(r as unknown as Reminder, t)}
                    </button>
                  ))}
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <input
                    type="datetime-local"
                    className="input flex-1"
                    value={atSel}
                    onChange={(e) => setAtSel(e.target.value)}
                  />
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
                        <span key={d} className="flex items-center gap-1 rounded-full bg-[var(--app-card-sub)] px-2.5 py-1 text-xs text-[var(--app-muted)]">
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
              <AttachmentField kind="event" itemId={editing.id} />
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
                  <PopConfirm
                    title={t('calendar.deleteTitle')}
                    description={t('calendar.deleteConfirm')}
                    confirmText={t('common.delete')}
                    cancelText={t('common.cancel')}
                    onConfirm={remove}
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
      {trashOpen && <ItemTrashPanel kind="event" onClose={() => setTrashOpen(false)} onChanged={() => void load()} />}
    </div>
  )
}
