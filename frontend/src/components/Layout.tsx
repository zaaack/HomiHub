import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { CalendarDays, CheckSquare, FolderOpen, Users, LogOut, Menu, X, Globe, Settings, PanelLeftClose, PanelLeft, ChevronDown, User, Shield, UserCog, NotebookPen } from 'lucide-react'
import { useAuth } from '../store/auth'
import { setLang } from '../i18n'
import { PopConfirm } from './ui/pop-confirm'

const NAV: { to: string; labelKey: string; icon: typeof CalendarDays; parentOnly?: boolean }[] = [
  { to: '/calendar', labelKey: 'nav.calendar', icon: CalendarDays },
  { to: '/todos', labelKey: 'nav.todos', icon: CheckSquare },
  { to: '/notes', labelKey: 'nav.notes', icon: NotebookPen },
  { to: '/files', labelKey: 'nav.files', icon: FolderOpen },
  { to: '/team', labelKey: 'nav.team', icon: Users },
]

export default function Layout() {
  const { t, i18n } = useTranslation()
  const { user, team, teams, switchTeam, logout } = useAuth()
  const nav = useNavigate()
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [userMenuOpen, setUserMenuOpen] = useState(false)
  const userMenuRef = useRef<HTMLDivElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)


  // Close user menu on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (userMenuRef.current && !userMenuRef.current.contains(e.target as Node)) {
        setUserMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const onLogout = async () => {
    await fetch('/api/v1/auth/logout', { method: 'POST' }).catch(() => {})
    logout()
    nav('/login')
  }

  const closeMobile = () => setMobileOpen(false)

  const sidebarWidth = sidebarOpen ? 'w-52' : 'w-16'
  const ml = sidebarOpen ? 'lg:ml-52' : 'lg:ml-16'

  const sidebar = (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className={`py-4 ${sidebarOpen ? 'px-2' : 'px-1'}`}>
        <div className="flex items-center justify-between gap-2">
          {sidebarOpen && <div className="ml-3 text-lg font-bold">{t('app.title')}</div>}
          <button
            onClick={() => setSidebarOpen((v) => !v)}
            className="nav-link hidden lg:flex shrink-0"
            title={sidebarOpen ? t('common.collapse') : t('common.expand')}
          >
            {sidebarOpen ? <PanelLeftClose size={18} /> : <PanelLeft size={18} />}
          </button>
        </div>
        {team && sidebarOpen && (
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
      </div>

      {/* Navigation */}
      <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto sidebar-scroll">
        <nav className={`space-y-0.5 ${sidebarOpen ? 'px-2' : 'px-1'}`}>
          {NAV.filter((n) => !n.parentOnly || user?.role === 'parent').map(({ to, labelKey, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              onClick={closeMobile}
              className={({ isActive }) => `nav-link ${isActive ? 'active' : ''}`}
            >
              <Icon size={18} className="shrink-0" />
              {sidebarOpen && t(labelKey)}
            </NavLink>
          ))}
          {/* System Settings - admin only, at bottom */}
          {user?.role === 'parent' && (
            <NavLink
              to="/settings"
              onClick={closeMobile}
              className={({ isActive }) => `nav-link ${isActive ? 'active' : ''}`}
            >
              <Settings size={18} className="shrink-0" />
              {sidebarOpen && t('nav.settings')}
            </NavLink>
          )}
        </nav>
      </div>

      {/* User Avatar & Logout */}
      <div className="border-t border-[var(--app-border)] p-2">
        {/* Language Toggle */}
        <button
          onClick={() => setLang(i18n.language === 'zh' ? 'en' : 'zh')}
          className="nav-link w-full"
        >
          <Globe size={18} className="shrink-0" />
          {sidebarOpen && (i18n.language === 'zh' ? 'English' : '中文')}
        </button>

        {/* User Avatar Dropdown */}
        {user && (
          <div className="relative" ref={userMenuRef}>
            <button
              onClick={() => setUserMenuOpen((v) => !v)}
              className="nav-link w-full"
            >
              <div className="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--app-accent)] text-xs font-medium text-white shrink-0">
                {user.name?.[0] ?? '?'}
              </div>
              {sidebarOpen && (
                <>
                  <span className="flex-1 text-left truncate">{user.name}</span>
                  <ChevronDown size={14} className={`shrink-0 transition ${userMenuOpen ? 'rotate-180' : ''}`} />
                </>
              )}
            </button>

            {/* Dropdown Menu */}
            {userMenuOpen && (
              <div className="absolute bottom-full left-0 mb-1 w-48 rounded-lg border border-[var(--app-border)] bg-[var(--app-card)] shadow-lg z-50">
                <NavLink
                  to="/profile"
                  onClick={() => { setUserMenuOpen(false); closeMobile() }}
                  className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-[var(--app-card-sub)]"
                >
                  <User size={16} className="text-[var(--app-muted)]" />
                  {t('nav.profile')}
                </NavLink>
                <NavLink
                  to="/user-settings"
                  onClick={() => { setUserMenuOpen(false); closeMobile() }}
                  className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-[var(--app-card-sub)]"
                >
                  <UserCog size={16} className="text-[var(--app-muted)]" />
                  {t('nav.userSettings')}
                </NavLink>
                <NavLink
                  to="/security"
                  onClick={() => { setUserMenuOpen(false); closeMobile() }}
                  className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-[var(--app-card-sub)]"
                >
                  <Shield size={16} className="text-[var(--app-muted)]" />
                  {t('nav.security')}
                </NavLink>
                <div className="border-t border-[var(--app-border)]" />
                <PopConfirm
                  onConfirm={() => void onLogout()}
                  title={t('auth.logout')}
                  description={t('auth.logoutConfirm')}
                  confirmText={t('auth.logout')}
                  cancelText={t('common.cancel')}
                >
                  <button
                    className="flex w-full items-center gap-2 px-3 py-2 text-sm text-[var(--app-danger)] hover:bg-[var(--app-card-sub)]"
                  >
                    <LogOut size={16} />
                    {t('auth.logout')}
                  </button>
                </PopConfirm>
              </div>
            )}
          </div>
        )}

        {/* Logout Button (always visible) */}
        {!user && (
          <button onClick={() => void onLogout()} className="nav-link w-full">
            <LogOut size={18} className="shrink-0" />
            {sidebarOpen && t('auth.logout')}
          </button>
        )}
      </div>
    </div>
  )

  return (
    <div className="flex h-screen bg-[var(--app-bg)]">
      {/* Desktop sidebar */}
      <aside
        className={`hidden lg:flex ${sidebarWidth} bg-[var(--app-card)] border-r border-[var(--app-border)] flex-col fixed h-full transition-all duration-200`}
      >
        {sidebar}
      </aside>

      {/* Mobile sidebar overlay */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={closeMobile} />
          <aside className="relative w-64 bg-[var(--app-card)] h-full flex flex-col">
            <div className="flex justify-end p-3">
              <button onClick={closeMobile} className="p-1 rounded-lg hover:bg-[var(--app-card-sub)]">
                <X size={20} />
              </button>
            </div>
            {sidebar}
          </aside>
        </div>
      )}

      {/* Main content */}
      <div className={`flex min-w-0 flex-1 flex-col ${ml} transition-all duration-200`}>
        <header className="flex items-center gap-2 border-b border-[var(--app-border)] bg-[var(--app-card)] px-4 py-3 lg:hidden">
          <button onClick={() => setMobileOpen(true)} className="nav-link">
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
