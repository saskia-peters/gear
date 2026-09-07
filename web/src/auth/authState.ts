// Auth state helpers (review finding 1.6-12): the login response carries the
// user's is_mfa_enabled flag (wired from LoginUser.IsMFAEnabled) and it is
// persisted so the SPA can show the "MFA aktiv" indicator. The display name is
// also cached for the header greeting, and is_admin (server-authoritative,
// Story 1.8) is cached to decide the ADMIN module's sidebar visibility. The
// server remains the source of truth; this is only a client cache.
//
// SESSION_TOKEN_KEY is the single source of truth for the session-token storage
// key (review finding 1.7-14): consumers import it instead of repeating the
// 'gear.session_token' literal so the key cannot drift.
export const SESSION_TOKEN_KEY = 'gear.session_token'

// authHeaders returns the JSON headers for an authenticated API call, adding the
// bearer token when one is present (Epic 1 retro finding 1). Every auth page
// imports this instead of re-deriving its own copy, so the header shape cannot
// drift across surfaces.
export function authHeaders(): HeadersInit {
  const token = localStorage.getItem(SESSION_TOKEN_KEY)
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
}

const MFA_ENABLED_KEY = 'gear.is_mfa_enabled'
const DISPLAY_NAME_KEY = 'gear.display_name'
const IS_ADMIN_KEY = 'gear.is_admin'
const PERMISSIONS_KEY = 'gear.permissions'

export interface AuthUserInfo {
  displayName?: string
}

export function saveAuthState(
  token: string,
  isMfaEnabled: boolean,
  user?: AuthUserInfo,
  isAdmin?: boolean,
): void {
  localStorage.setItem(SESSION_TOKEN_KEY, token)
  if (isMfaEnabled) {
    localStorage.setItem(MFA_ENABLED_KEY, 'true')
  } else {
    localStorage.removeItem(MFA_ENABLED_KEY)
  }
  if (user?.displayName) {
    localStorage.setItem(DISPLAY_NAME_KEY, user.displayName)
  } else {
    localStorage.removeItem(DISPLAY_NAME_KEY)
  }
  if (isAdmin !== undefined) {
    setIsAdmin(isAdmin)
  }
}

export function isMfaEnabled(): boolean {
  return localStorage.getItem(MFA_ENABLED_KEY) === 'true'
}

export function getDisplayName(): string | null {
  return localStorage.getItem(DISPLAY_NAME_KEY)
}

// setDisplayName refreshes the cached display name used by the header greeting
// (e.g. after the user saves base-data edits on the profile page, Story 2.1).
// The server remains the source of truth; this only keeps the client cache in
// sync.
export function setDisplayName(displayName: string): void {
  if (displayName) {
    localStorage.setItem(DISPLAY_NAME_KEY, displayName)
  } else {
    localStorage.removeItem(DISPLAY_NAME_KEY)
  }
}

// getIsAdmin reports the cached admin flag (Story 1.8). The value is only ever
// set from a server response (login / GET profile); the client never derives
// it. Absence of the key is treated as non-admin.
export function getIsAdmin(): boolean {
  return localStorage.getItem(IS_ADMIN_KEY) === 'true'
}

// setIsAdmin updates the cached admin flag from a server response (login /
// GET profile). The server remains the source of truth (AD-2/AD-6); this only
// keeps the sidebar visibility in sync.
export function setIsAdmin(isAdmin: boolean): void {
  if (isAdmin) {
    localStorage.setItem(IS_ADMIN_KEY, 'true')
  } else {
    localStorage.removeItem(IS_ADMIN_KEY)
  }
}

// adminForbiddenHandled inspects a response from an admin-surface API call
// (Story 2.1, review finding 2.1-6). A 403 means the caller's admin role was
// revoked or the cached is_admin flag is stale/forged — the cached flag is
// cleared immediately so no admin UI keeps rendering from a non-server value
// (FR-19). It returns true so the caller can redirect to the Dashboard.
export function adminForbiddenHandled(res: { status: number }): boolean {
  if (res.status !== 403) return false
  setIsAdmin(false)
  return true
}

// getPermissions reports the cached resolved permission set (Story 2.3). The
// value is only ever set from a server response (GET /api/v1/auth/me/permissions);
// the client never derives it. Absence of the key is treated as an empty set.
export function getPermissions(): string[] {
  try {
    const raw = localStorage.getItem(PERMISSIONS_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : []
  } catch {
    return []
  }
}

// savePermissions updates the cached resolved permission set from the
// server-authoritative GET /me/permissions response (AD-2/AD-6). The server
// remains the source of truth; this only keeps client-side filtering in sync.
export function savePermissions(permissions: string[]): void {
  if (Array.isArray(permissions) && permissions.length > 0) {
    localStorage.setItem(PERMISSIONS_KEY, JSON.stringify(permissions))
  } else {
    localStorage.removeItem(PERMISSIONS_KEY)
  }
}

// hasPermission reports whether the cached resolved permission set contains the
// given code (Story 2.3). It is a convenience for components that need a single
// code check; the nav/landing filtering uses filteredAdminNav on the full set.
export function hasPermission(code: string): boolean {
  return getPermissions().includes(code)
}

export function clearAuthState(): void {
  localStorage.removeItem(SESSION_TOKEN_KEY)
  localStorage.removeItem(MFA_ENABLED_KEY)
  localStorage.removeItem(DISPLAY_NAME_KEY)
  localStorage.removeItem(IS_ADMIN_KEY)
  localStorage.removeItem(PERMISSIONS_KEY)
}
