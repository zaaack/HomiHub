import i18n from '../i18n'

export class ApiError extends Error {
  status: number
  code?: string
  constructor(status: number, message: string, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

let token: string | null = null
export function setToken(t: string | null) {
  token = t
}

function renderError(raw: unknown): string {
  if (typeof raw !== 'string' || !raw) return 'API error'
  if (/^[a-z][a-z0-9_]*$/.test(raw) && i18n.exists(`error.${raw}`)) {
    return (i18n.t(`error.${raw}`) as string) || raw
  }
  return raw
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401 && token) {
      setToken(null)
      window.location.href = '/login'
    }
    throw new ApiError(res.status, renderError(data?.error), data?.error)
  }
  return data.data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

// Upload a file via multipart form.
export function uploadFile(
  path: string,
  form: FormData,
  onProgress?: (pct: number) => void,
): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', path)
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
    }
    xhr.onload = () => {
      const data = JSON.parse(xhr.responseText || '{}')
      if (xhr.status >= 200 && xhr.status < 300) resolve(data.data)
      else reject(new ApiError(xhr.status, renderError(data?.error), data?.error))
    }
    xhr.onerror = () => reject(new Error('network error'))
    xhr.send(form)
  })
}
