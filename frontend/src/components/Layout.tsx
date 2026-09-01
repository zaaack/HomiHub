import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink, Outlet } from 'react-router-dom'
import { CalendarDays, CheckSquare, FolderOpen, Users, LogOut, Menu, X, Globe, Settings } from 'lucide-react'
import { useAuth } from '../store/auth'
import { setLang } from '../i18n'

const NAV: { to: string; labelKey: string; icon: typeof CalendarDays; parentOnly?: boolean }[] = [
  { to: '/calendar', labelKey: 'nav.calendar', icon: CalendarDays },
  { to: '/todos', labelKey: 'nav.todos', icon: CheckSquare },
  { to: '/files', labelKey: 'nav.files', icon: FolderOpen },
  { to: '/team', labelKey: 'nav.team', icon: Users },
  { to: '/settings', labelKey: 'nav.settings', icon: Settings, parentOnly: true },
]

export default function Layout() {
  const { t, i18n } = useTranslation()
  const { user, team, teams, switchTeam, logout } = useAuth()
  const [sidebarOpen, setSidebarOpen] = useState(false)

  const onLogout = async () => {
    await fetch('/api/v1/auth/logout', { method: 'POST' }).catch(() => {})
    logout()
  }

  const close = () => setSidebarOpen(false)

  const sidebar = (
    <div className="flex h-full flex-col">
      <div className="p-4">
        <div className="flex items-center justify-between">
          <div className="text-lg font-bold">{t('app.title')}</div>
          <button onClick={close} className="nav-link lg:hidden">
            <X size={18} />
          </button>
        </div>
        {team && (
          <div className="mt-3">
            {teams.length > 1 ? (
              <select
                value={team.id}
                onChange={(e) => void switchTeam(e.target.value)}
                className="select w-full text-sm"
                title={t('auth.switchTeam')}
              >
                {teams.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name}
                  </option>
                ))}
              </select>
            ) : (
              <div className="text-sm text-[var(--app-muted)]">{team.name}</div>
            )}
          </div>
        )}
        {user && (
          <div className="mt-4 flex items-center gap-2">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--app-accent)] text-sm font-medium text-white">
              {user.name?.[0] ?? '?'}
            </div>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{user.name}</div>
              <div className="text-xs text-[var(--app-muted)]">
                {user.role === 'parent' ? t('auth.parent') : t('auth.child')}
              </div>
            </div>
          </div>
        )}
      </div>
      <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 sidebar-scroll">
        {NAV.filter((n) => !n.parentOnly || user?.role === 'parent').map(({ to, labelKey, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            onClick={close}
            className={({ isActive }) => `nav-link ${isActive ? 'active' : ''}`}
          >
            <Icon size={18} className="shrink-0" />
            {t(labelKey)}
          </NavLink>
        ))}
      </nav>
      <div className="space-y-0.5 border-t border-[var(--app-border)] p-2">
        <button
          onClick={() => setLang(i18n.language === 'zh' ? 'en' : 'zh')}
          className="nav-link w-full"
        >
          <Globe size={18} className="shrink-0" />
          {i18n.language === 'zh' ? 'English' : '中文'}
        </button>
        <button onClick={() => void onLogout()} className="nav-link w-full">
          <LogOut size={18} className="shrink-0" />
          {t('auth.logout')}
        </button>
      </div>
    </div>
  )

  return (
    <div className="flex h-screen">
      <aside className="hidden w-64 shrink-0 border-r border-[var(--app-border)] bg-[var(--app-card)] lg:block">
        {sidebar}
      </aside>
      {sidebarOpen && (
        <div className="fixed inset-0 z-40 flex lg:hidden">
          <div className="w-64 shrink-0 border-r border-[var(--app-border)] bg-[var(--app-card)]">
            {sidebar}
          </div>
          <div className="flex-1 bg-black/30" onClick={close} />
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-2 border-b border-[var(--app-border)] bg-[var(--app-card)] px-4 py-3 lg:hidden">
          <button onClick={() => setSidebarOpen(true)} className="nav-link">
            <Menu size={18} />
          </button>
          <div className="font-bold">{t('app.title')}</div>
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto p-4 lg:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
