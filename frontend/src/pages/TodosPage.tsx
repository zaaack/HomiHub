import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckSquare, Plus, Square, Trash2, X, Clock, Users } from 'lucide-react'
import { api } from '../api/client'
import type { Todo } from '../types'

function fmtLocal(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const REPEAT = [
  { value: '', labelKey: 'noRepeat' },
  { value: 'FREQ=DAILY', labelKey: 'daily' },
  { value: 'FREQ=WEEKLY', labelKey: 'weekly' },
  { value: 'FREQ=MONTHLY', labelKey: 'monthly' },
] as const

interface EditForm {
  id?: string
  title: string
  note: string
  dueAt: string
  shared: boolean
  rrule: string
}

export default function TodosPage() {
  const { t } = useTranslation()
  const [todos, setTodos] = useState<Todo[]>([])
  const [quick, setQuick] = useState('')
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

  const sorted = useMemo(() => {
    const list = [...todos]
    list.sort((a, b) => {
      if (a.completed !== b.completed) return a.completed ? 1 : -1
      const ad = a.dueAt ? new Date(a.dueAt).getTime() : Infinity
      const bd = b.dueAt ? new Date(b.dueAt).getTime() : Infinity
      if (ad !== bd) return ad - bd
      return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
    })
    return list
  }, [todos])

  const isOverdue = (todo: Todo) => !!todo.dueAt && !todo.completed && new Date(todo.dueAt).getTime() < Date.now()

  const quickAdd = async () => {
    const title = quick.trim()
    if (!title) return
    setBusy(true)
    try {
      await api.post('/api/v1/todos', { title })
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
        shared: editing.shared,
        rrule: editing.rrule,
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
      shared: todo.shared,
      rrule: todo.rrule,
    })
  }

  const pending = sorted.filter((x) => !x.completed)
  const done = sorted.filter((x) => x.completed)

  return (
    <div className="mx-auto max-w-2xl">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t('todos.title')}</h1>
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
        {pending.length === 0 && done.length === 0 && (
          <div className="card py-10 text-center text-sm text-[var(--app-muted)]">{t('todos.empty')}</div>
        )}
        {pending.map((todo) => (
          <TodoRow key={todo.id} todo={todo} overdue={isOverdue(todo)} onToggle={() => void toggle(todo)} onEdit={() => openEdit(todo)} onDelete={() => void remove(todo.id)} />
        ))}
        {done.length > 0 && (
          <>
            <div className="pt-3 text-xs font-medium text-[var(--app-muted)]">{t('todos.completed')}</div>
            {done.map((todo) => (
              <TodoRow key={todo.id} todo={todo} overdue={false} onToggle={() => void toggle(todo)} onEdit={() => openEdit(todo)} onDelete={() => void remove(todo.id)} />
            ))}
          </>
        )}
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={() => setEditing(null)}>
          <div className="card w-full max-w-md p-5" onClick={(e) => e.stopPropagation()}>
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
              <textarea
                className="input min-h-[64px]"
                placeholder={t('todos.note')}
                value={editing.note}
                onChange={(e) => setEditing({ ...editing, note: e.target.value })}
              />
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
  overdue,
  onToggle,
  onEdit,
  onDelete,
}: {
  todo: Todo
  overdue: boolean
  onToggle: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="card flex items-center gap-3 p-3">
      <button onClick={onToggle} className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-accent)]" title={t('todos.toggle')}>
        {todo.completed ? <CheckSquare size={20} className="text-[var(--app-accent)]" /> : <Square size={20} />}
      </button>
      <button onClick={onEdit} className="min-w-0 flex-1 text-left">
        <div className={`truncate ${todo.completed ? 'text-[var(--app-faint)] line-through' : ''}`}>{todo.title}</div>
        {(todo.dueAt || todo.shared || todo.rrule) && (
          <div className="mt-0.5 flex items-center gap-2 text-xs text-[var(--app-muted)]">
            {todo.dueAt && (
              <span className={`flex items-center gap-1 ${overdue ? 'font-medium text-[var(--app-danger)]' : ''}`}>
                <Clock size={12} />
                {new Date(todo.dueAt).toLocaleDateString()}
                {overdue && ` · ${t('todos.overdue')}`}
              </span>
            )}
            {todo.rrule && <span>{t(`calendar.${todo.rrule === 'FREQ=DAILY' ? 'daily' : todo.rrule === 'FREQ=WEEKLY' ? 'weekly' : 'monthly'}`)}</span>}
            {todo.shared && (
              <span className="flex items-center gap-1">
                <Users size={12} />
                {t('todos.shared')}
              </span>
            )}
          </div>
        )}
      </button>
      <button onClick={onDelete} className="shrink-0 text-[var(--app-muted)] hover:text-[var(--app-danger)]">
        <Trash2 size={16} />
      </button>
    </div>
  )
}
