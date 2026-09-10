// Admin-module navigation model (Story 2.3, UX-DR6/AD-6/FR-19). This is the
// single source of truth for the eight EXPERIENCE.md admin entries: their
// gating permission codes, their routes, and their plain-language labels.
//
// User-facing microcopy must stay jargon-free (no permission-code names in the
// landing/nav — those stay server-side, UX-DR4/5/8). The gating codes are only
// used here to filter which entries a caller's resolved permission set exposes.
// Kept in its own file so the app-shell and nav code do not grow into a
// god-class (standing convention).

export interface AdminNavEntry {
  /** Stable key, also used for the route segment. */
  key: string
  /** Route under /admin (without a leading slash). Übersicht is the landing. */
  route: string
  /** Plain-language label shown in the nav and on the landing card. */
  label: string
  /** Plain-language purpose shown on the landing card (no codes, no jargon). */
  description: string
  /** Permission codes that gate this entry (holding any of them exposes it). */
  codes: string[]
}

export const ADMIN_NAV_ENTRIES: readonly AdminNavEntry[] = [
  {
    key: 'uebersicht',
    route: '/admin',
    label: 'Übersicht',
    description: 'Start der Verwaltung mit allen anstehenden Freigaben.',
    codes: [
      'users.view',
      'users.approve',
      'users.manage',
      'roles.create',
      'roles.edit',
      'roles.assign',
      'qualifications.manage',
      'tools.manage',
      'tool_types.manage',
      'admin.settings.email',
      'admin.settings.backup',
      'schedules.manage',
      'dsgvo.access_report',
      'dsgvo.delete',
      'admin.recovery.approve',
    ],
  },
  {
    key: 'benutzer',
    route: '/admin/benutzer',
    label: 'Benutzer',
    description: 'Mitglieder verwalten und neue Anträge freigeben.',
    codes: ['users.view', 'users.approve', 'users.manage'],
  },
  {
    key: 'benutzergruppen',
    route: '/admin/benutzergruppen',
    label: 'Benutzergruppen',
    description: 'Teams anlegen, Mitglieder zuordnen und Rollen vergeben.',
    codes: ['user_groups.manage'],
  },
  {
    key: 'rollen',
    route: '/admin/rollen',
    label: 'Rollen',
    description: 'Rollen ansehen und anpassen.',
    codes: ['roles.create', 'roles.edit', 'roles.assign'],
  },
  {
    key: 'qualifikationen',
    route: '/admin/qualifikationen',
    label: 'Qualifikationen',
    description: 'Qualifikationen pflegen, z. B. Zertifikate und Lizenzen.',
    codes: ['qualifications.manage'],
  },
  {
    key: 'werkzeuge',
    route: '/admin/werkzeuge',
    label: 'Werkzeuge',
    description: 'Geräte und Gerätetypen verwalten.',
    codes: ['tools.manage', 'tool_types.manage'],
  },
  {
    key: 'einstellungen',
    route: '/admin/einstellungen',
    label: 'Einstellungen',
    description: 'E-Mail-, Sicherungs- und Zeitplan-Einstellungen.',
    codes: ['admin.settings.email', 'admin.settings.backup', 'schedules.manage'],
  },
  {
    key: 'dsgvo',
    route: '/admin/dsgvo',
    label: 'DSGVO',
    description: 'Datenauskünfte und Löschungen nach Datenschutz.',
    codes: ['dsgvo.access_report', 'dsgvo.delete'],
  },
] as const

// adminNavCodes returns the gating codes for a single nav entry by its key.
// Route guards reference this instead of re-declaring the arrays, so the codes
// live in exactly one place and cannot drift from the nav model.
export function adminNavCodes(key: string): readonly string[] {
  const entry = ADMIN_NAV_ENTRIES.find((e) => e.key === key)
  if (!entry) {
    throw new Error(`unknown admin nav entry: ${key}`)
  }
  return entry.codes
}

// hasAnyAdminCode reports whether the resolved permission set carries any code
// that opens the admin module (Story 2.3, FR-19). This replaces the binary
// is_admin flag for module visibility.
export function hasAnyAdminCode(perms: readonly string[]): boolean {
  const set = new Set(perms)
  return ADMIN_NAV_ENTRIES.some((entry) => entry.codes.some((code) => set.has(code)))
}

// filteredAdminNav returns only the nav entries the caller's resolved
// permission set exposes (anti-enumeration, FR-19). Übersicht is always first
// when any entry is visible.
export function filteredAdminNav(perms: readonly string[]): AdminNavEntry[] {
  const set = new Set(perms)
  return ADMIN_NAV_ENTRIES.filter((entry) => entry.codes.some((code) => set.has(code)))
}
