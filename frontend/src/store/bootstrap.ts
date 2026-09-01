import { useAuth } from './auth'

export function refreshAuth() {
  void useAuth.getState().refresh().catch(() => {})
}
