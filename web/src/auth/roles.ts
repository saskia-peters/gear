// Role & Permission-Group Management data module (Story 2.5). It holds the
// server-authoritative permission catalog types plus the three API calls
// (list / create / update) for the "Rollen" surface. The 21-code catalog is
// fetched from the backend so the SPA editor never hardcodes a stale list
// (Design Notes spec 2.5); PERMISSION_LABELS is only a local fallback when the
// fetch fails.

import { authHeaders } from './authState.ts'

export interface RoleGroup {
  id: string
  name: string
  description: string
  is_base_role: boolean
  permissions: string[]
}

export interface PermissionCatalogEntry {
  code: string
  label: string
}

export interface RoleList {
  groups: RoleGroup[]
  available_permissions: PermissionCatalogEntry[]
}

export interface RoleInput {
  name: string
  description: string
  permissions: string[]
}

const GROUPS_URL = '/api/v1/admin/groups'

// German display labels for the 21 base codes (UX-DR4/DR8). Local fallback; the
// server is authoritative for the code list and labels.
export const PERMISSION_LABELS: Record<string, string> = {
  'dashboard.view': 'Dashboard ansehen',
  'inspection.submit': 'Prüfung einreichen',
  'inspection.history.view': 'Prüfungshistorie ansehen',
  'report.export': 'Berichte exportieren',
  'tool.reinstate': 'Gerät wiederherstellen',
  'tools.manage': 'Geräte verwalten',
  'tool_types.manage': 'Gerätetypen verwalten',
  'users.view': 'Benutzer ansehen',
  'users.approve': 'Benutzerfreigaben erteilen',
  'users.manage': 'Benutzer verwalten',
  'users.qualifications.manage': 'Qualifikationen an Benutzer vergeben',
  'user_groups.manage': 'Benutzergruppen verwalten',
  'roles.create': 'Rollen erstellen',
  'roles.edit': 'Rollen bearbeiten',
  'roles.assign': 'Rollen zuweisen',
  'qualifications.manage': 'Qualifikationen verwalten',
  'dsgvo.access_report': 'DSGVO-Auskünfte erteilen',
  'dsgvo.delete': 'Daten löschen (DSGVO)',
  'admin.recovery.approve': 'Kontowiederherstellung freigeben',
  'admin.settings.email': 'E-Mail-Einstellungen verwalten',
  'admin.settings.backup': 'Sicherungen verwalten',
  'schedules.manage': 'Dienstpläne verwalten',
}

// The full 21-code base series (AD-12). Used to sort/validate the local fallback
// only; the server remains authoritative.
export const BASE_PERMISSION_CODES: readonly string[] = [
  'dashboard.view',
  'inspection.submit',
  'inspection.history.view',
  'report.export',
  'tool.reinstate',
  'tools.manage',
  'tool_types.manage',
  'users.view',
  'users.approve',
  'users.manage',
  'users.qualifications.manage',
  'user_groups.manage',
  'roles.create',
  'roles.edit',
  'roles.assign',
  'qualifications.manage',
  'dsgvo.access_report',
  'dsgvo.delete',
  'admin.recovery.approve',
  'admin.settings.email',
  'admin.settings.backup',
  'schedules.manage',
]

// permissionLabel returns the German label for a code from the local fallback.
export function permissionLabel(code: string): string {
  return PERMISSION_LABELS[code] ?? code
}

// fallbackCatalog builds the full 21-code catalog from the shipped local copies
// (BASE_PERMISSION_CODES + PERMISSION_LABELS). It is used when the server's
// list response omits or empties available_permissions, so the editor still
// renders all 21 checkboxes (the save is still validated server-side).
function fallbackCatalog(): PermissionCatalogEntry[] {
  return BASE_PERMISSION_CODES.map((code) => ({
    code,
    label: PERMISSION_LABELS[code] ?? code,
  }))
}

// ApiError carries the server's uniform envelope message plus the HTTP status,
// so a caller can branch on 403 (revocation downgrade) as well as show German
// feedback.
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// extractMessage reads the server's German message from the uniform envelope,
// falling back to a generic German string.
function extractMessage(status: number, body: unknown): string {
  const msg =
    (body as { error?: { message?: unknown } } | null)?.error?.message
  if (typeof msg === 'string' && msg !== '') return msg
  if (status >= 500) return 'Der Server ist gerade nicht erreichbar. Bitte versuche es später erneut.'
  return 'Die Aktion ist fehlgeschlagen. Bitte versuche es erneut.'
}

async function request(path: string, init: RequestInit): Promise<unknown> {
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

// listRoles fetches every permission group (base roles + custom) plus the
// server-authoritative 21-code catalog. If the server response omits or empties
// the catalog, the shipped BASE_PERMISSION_CODES/PERMISSION_LABELS fallback is
// used so the editor never renders an empty grid.
export async function listRoles(): Promise<RoleList> {
  const data = (await request(GROUPS_URL, { headers: authHeaders() })) as RoleList
  const groups = Array.isArray(data.groups) ? data.groups : []
  let catalog = Array.isArray(data.available_permissions) ? data.available_permissions : []
  if (catalog.length === 0) {
    catalog = fallbackCatalog()
  }
  return { groups, available_permissions: catalog }
}

// createRole creates a named permission group with its additive permission set.
export async function createRole(input: RoleInput): Promise<RoleGroup> {
  return (await request(GROUPS_URL, {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify(input),
  })) as RoleGroup
}

// updateRole replaces a permission group's name/description and permission set.
export async function updateRole(id: string, input: RoleInput): Promise<RoleGroup> {
  return (await request(`${GROUPS_URL}/${id}`, {
    method: 'PUT',
    headers: authHeaders(),
    body: JSON.stringify(input),
  })) as RoleGroup
}
