import { createBrowserRouter, RouterProvider, Navigate, Outlet } from 'react-router-dom'
import { useAuth } from './store/auth'
import Layout from './components/Layout'
import Login from './pages/Login'
import Register from './pages/Register'
import CalendarPage from './pages/CalendarPage'
import TodosPage from './pages/TodosPage'
import FilesPage from './pages/FilesPage'
import TeamPage from './pages/TeamPage'
import SettingsPage from './pages/SettingsPage'
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
          { path: '/files', element: <FilesPage /> },
          { path: '/team', element: <TeamPage /> },
          { path: '/settings', element: <SettingsPage /> },
        ],
      },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

export default function App() {
  return <RouterProvider router={router} />
}
