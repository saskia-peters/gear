// Auth state helpers (review finding 1.6-12): the login response carries the
// user's is_mfa_enabled flag (wired from LoginUser.IsMFAEnabled) and it is
// persisted so the SPA can show the "MFA aktiv" indicator. The display name is
// also cached for the header greeting. The server remains the source of truth;
// this is only a client cache.
//
// SESSION_TOKEN_KEY is the single source of truth for the session-token storage
// key (review finding 1.7-14): consumers import it instead of repeating the
// 'gear.session_token' literal so the key cannot drift.
export const SESSION_TOKEN_KEY = 'gear.session_token'

const MFA_ENABLED_KEY = 'gear.is_mfa_enabled'
const DISPLAY_NAME_KEY = 'gear.display_name'

export interface AuthUserInfo {
  displayName?: string
}

export function saveAuthState(token: string, isMfaEnabled: boolean, user?: AuthUserInfo): void {
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
}

export function isMfaEnabled(): boolean {
  return localStorage.getItem(MFA_ENABLED_KEY) === 'true'
}

export function getDisplayName(): string | null {
  return localStorage.getItem(DISPLAY_NAME_KEY)
}

export function clearAuthState(): void {
  localStorage.removeItem(SESSION_TOKEN_KEY)
  localStorage.removeItem(MFA_ENABLED_KEY)
  localStorage.removeItem(DISPLAY_NAME_KEY)
}
