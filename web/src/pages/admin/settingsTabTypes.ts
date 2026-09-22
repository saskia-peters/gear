// Shared types for the Einstellungen surface (Story 3.1/3.2/4.1/5-2b): the
// tab components (Email/Backup/Schedule/System) and the page shell agree on
// the feedback shape, the active-tab key and the sort direction so the four
// sibling files and the parent never drift.
export type Feedback = { kind: 'success' | 'error'; message: string } | null
export type Tab = 'email' | 'backup' | 'schedules' | 'system'
export type SortDir = 'asc' | 'desc'

export interface TabProps {
  // onApiError handles an API error: a 403 clears the cached admin flag and
  // leaves the admin module; a 401 clears auth and redirects to /login.
  // Returns true when the error was handled (redirect), false otherwise.
  onApiError: (err: unknown) => boolean
}