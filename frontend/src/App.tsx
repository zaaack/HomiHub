import { createBrowserRouter, RouterProvider, Navigate, Outlet } from 'react-router-dom'
import { useAuth } from './store/auth'
import Layout from './components/Layout'
import Login from './pages/Login'
import Register from './pages/Register'
import CalendarPage from './pages/CalendarPage'
import TodosPage from './pages/TodosPage'
import NotesPage from './pages/NotesPage'
import FilesPage from './pages/FilesPage'
import TeamPage from './pages/TeamPage'
import SettingsPage from './pages/SettingsPage'
import ProfilePage from './pages/ProfilePage'
import UserSettingsPage from './pages/UserSettingsPage'
import SecurityPage from './pages/SecurityPage'
import Join from './pages/Join'

function RequireAuth() {
  const { token } = useAuth()
  if (!token) return <Navigate to="/login" replace />
  return <Outlet />
}

const router = createBrowserRouter([
  { path: '/login', element: <Login /> },
  { path: '/register', element: <Register /> },
  { path: '/join', element: <Join /> },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <Layout />,
        children: [
          { path: '/', element: <CalendarPage /> },
          { path: '/calendar', element: <CalendarPage /> },
          { path: '/todos', element: <TodosPage /> },
          { path: '/notes', element: <NotesPage /> },
          { path: '/files', element: <FilesPage /> },
          { path: '/team', element: <TeamPage /> },
          { path: '/settings', element: <SettingsPage /> },
          { path: '/profile', element: <ProfilePage /> },
          { path: '/user-settings', element: <UserSettingsPage /> },
          { path: '/security', element: <SecurityPage /> },
        ],
      },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

export default function App() {
  return <RouterProvider router={router} />
}
