import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { Team, TeamEntry, User } from '../types'
import { api, setToken } from '../api/client'

interface AuthState {
  token: string | null
  user: User | null
  team: Team | null
  teams: TeamEntry[]
  login: (email: string, password: string) => Promise<void>
  register: (data: {
    name: string
    email: string
    password: string
    teamName: string
  }) => Promise<void>
  loadTeams: () => Promise<void>
  switchTeam: (teamId: string) => Promise<void>
  refresh: () => Promise<void>
  logout: () => void
}

function applyAuth(t: string, user: User, team: Team | null) {
  setToken(t)
  useAuth.setState({ token: t, user, team })
}

export const useAuth = create<AuthState>()(
  persist(
    (set, get) => ({
      token: null,
      user: null,
      team: null,
      teams: [],
      login: async (email, password) => {
        const r = await api.post<{ token: string; user: User; team: Team }>(
          '/api/v1/auth/login',
          { email, password },
        )
        applyAuth(r.token, r.user, r.team)
        void get().loadTeams()
      },
      register: async (data) => {
        const r = await api.post<{ token: string; user: User; team: Team }>(
          '/api/v1/auth/register',
          data,
        )
        applyAuth(r.token, r.user, r.team)
        void get().loadTeams()
      },
      loadTeams: async () => {
        const s = get()
        if (!s.token) return
        try {
          const teams = await api.get<TeamEntry[]>('/api/v1/auth/families')
          set({ teams })
        } catch {
          /* non-fatal */
        }
      },
      switchTeam: async (teamId) => {
        const r = await api.post<{ token: string; user: User; team: Team }>(
          '/api/v1/auth/switch-team',
          { teamId },
        )
        applyAuth(r.token, r.user, r.team)
        void get().loadTeams()
      },
      refresh: async () => {
        const s = get()
        if (!s.token) return
        try {
          const r = await api.get<{ user: User; team: Team }>('/api/v1/auth/me')
          set({ user: r.user, team: r.team })
          void get().loadTeams()
        } catch {
          s.logout()
        }
      },
      logout: () => {
        setToken(null)
        set({ token: null, user: null, team: null, teams: [] })
      },
    }),
    {
      name: 'homihub-auth',
      version: 1,
      onRehydrateStorage: () => (state) => {
        if (state?.token) setToken(state.token)
      },
    },
  ),
)
