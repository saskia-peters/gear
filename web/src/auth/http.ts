// Shared HTTP helper for the G.E.A.R. SPA (Epic 2 retro item 9): the uniform
// envelope error, the authenticated JSON request wrapper, and the bearer-token
// header builder. Previously duplicated across roles.ts / qualifications.ts /
// users.ts — this is the single source of truth so the header shape, the error
// parsing and the German fallback strings cannot drift between surfaces.

import { SESSION_TOKEN_KEY } from './authState.ts'

// ApiError is the uniform-envelope HTTP error thrown by request(): it carries
// the status code and the server's German message so callers can branch on
// 401/403 and render the message inline.
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// authTokenHeaders returns the JSON headers for an authenticated API call,
// adding the bearer token when one is present (Epic 1 retro finding 1). This is
// the same shape as authState.authHeaders but kept here so the auth data
// modules (roles/qualifications/users) do not re-derive their own copies.
export function authTokenHeaders(): HeadersInit {
  const token = localStorage.getItem(SESSION_TOKEN_KEY)
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
}

// extractMessage reads the server's German message from the uniform envelope,
// falling back to a generic German string.
function extractMessage(status: number, body: unknown): string {
  const msg = (body as { error?: { message?: unknown } } | null)?.error?.message
  if (typeof msg === 'string' && msg !== '') return msg
  if (status >= 500) return 'Der Server ist gerade nicht erreichbar. Bitte versuche es später erneut.'
  return 'Die Aktion ist fehlgeschlagen. Bitte versuche es erneut.'
}

// request performs an authenticated JSON API call and returns the parsed body.
// A network failure throws ApiError(0); a non-2xx response throws ApiError with
// the server's German message from the uniform envelope.
export async function request(path: string, init: RequestInit): Promise<unknown> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new ApiError(0, 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.')
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    throw new ApiError(res.status, extractMessage(res.status, body))
  }
  return body
}