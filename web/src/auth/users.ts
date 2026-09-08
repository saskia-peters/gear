// User & Group Administration data module (Story 2.6). It holds the API types
// and calls for the "Benutzer" surface: user list / detail / create / update /
// deactivate, plus organisational user-group (team) list / create / member
// assignment. The server is the source of truth for status (active /
// pending_approval / deactivated) and the qualification expiry status — the
// SPA only maps them to German badges.

import { ApiError } from './roles.ts'
import type { PermissionCatalogEntry } from './roles.ts'

export type UserStatus = 'active' | 'pending_approval' | 'deactivated'

// AdminUserSummary is one row of the user list.
export interface AdminUserSummary {
  id: string
  vorname: string
  nachname: string
  email: string
  status: UserStatus
}

export interface RoleGroupRef {
  id: string
  name: string
  is_base_role: boolean
}

export interface UserGroupRef {
  id: string
  name: string
}

export interface DirectGrantRef {
  permission_id: string
  code: string
  granted_at: string
}

export type QualificationStatus = 'unlimited' | 'valid' | 'expiring_soon' | 'expired'

// QualificationAssignment is one qualification a user holds, with the
// server-computed status (Story 2.6 displays only; editing is Story 2.7).
export interface QualificationAssignment {
  id: string
  name: string
  description: string
  expiry_kind: 'unlimited' | 'fixed'
  expires_at: string | null
  assigned_at: string
  status: QualificationStatus
}

// AdminUserDetail is the full user detail: profile + roles + user groups +
// direct grants + qualification assignments.
export interface AdminUserDetail {
  id: string
  vorname: string
  nachname: string
  email: string
  status: UserStatus
  roles: RoleGroupRef[]
  user_groups: UserGroupRef[]
  direct_grants: DirectGrantRef[]
  qualifications: QualificationAssignment[]
  resolved_permissions?: PermissionSource[]
}

// PermissionSource annotates one resolved permission of a user with its source
// (Spec 2.9 provenance): source_kind is 'role' (individual permission-group
// membership), 'group' (inherited via an organisational user-group), or
// 'direct'; source_name is the role name, the user-group name, or 'direct'.
export interface PermissionSource {
  code: string
  source_kind: 'role' | 'group' | 'direct'
  source_name: string
}

// UserGroup is an organisational team (AD-12: membership grants no permission).
export interface UserGroup {
  id: string
  name: string
  description: string
  created_at: string
}

export interface AdminUserInput {
  vorname: string
  nachname: string
  email: string
  status: 'active' | 'pending_approval'
  role_ids: string[]
  user_group_ids: string[]
  direct_grant_codes: string[]
}

export interface UserGroupInput {
  name: string
  description: string
}

export interface DeactivateResult {
  message: string
  user_id: string
  email: string
}

// AdminUserWriteResult is the create/update response: the server-authoritative
// German confirmation plus the resulting detail (finding 8 — the SPA must not
// hardcode its own success text).
export interface AdminUserWriteResult {
  message: string
  user: AdminUserDetail
}

// German display labels for the account states (UX-DR8). The server sends the
// raw state; the client renders the badge.
export const USER_STATUS_LABELS: Record<UserStatus, string> = {
  active: 'aktiv',
  pending_approval: 'pending',
  deactivated: 'deaktiviert',
}

// German display labels for the qualification assignment statuses (FR-22/UX-DR8).
export const QUALIFICATION_STATUS_LABELS: Record<QualificationStatus, string> = {
  unlimited: 'Unbegrenzt',
  valid: 'Gültig',
  expiring_soon: 'Bald ablaufend',
  expired: 'Abgelaufen',
}

// userStatusLabel returns the German badge for a raw account state, falling
// back to the raw value so an unrecognized state never renders "undefined"
// (finding 12).
export function userStatusLabel(status: string): string {
  return USER_STATUS_LABELS[status as UserStatus] ?? status
}

// qualificationStatusLabel returns the German badge for a qualification status
// code, falling back to the raw value so an unrecognized status never renders
// "undefined" (finding 12).
export function qualificationStatusLabel(status: string): string {
  return QUALIFICATION_STATUS_LABELS[status as QualificationStatus] ?? status
}

const USERS_URL = '/api/v1/admin/users'
const USER_GROUPS_URL = '/api/v1/admin/user-groups'

// extractMessage reads the server's German message from the uniform envelope,
// falling back to a generic German string.
function extractMessage(status: number, body: unknown): string {
  const msg = (body as { error?: { message?: unknown } } | null)?.error?.message
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

function authTokenHeaders(): HeadersInit {
  const token = localStorage.getItem('gear.session_token')
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
}

// listUsers fetches every user (id, names, email, status), ordered by name.
export async function listUsers(): Promise<AdminUserSummary[]> {
  const data = (await request(USERS_URL, { headers: authTokenHeaders() })) as { users?: AdminUserSummary[] }
  return Array.isArray(data.users) ? data.users : []
}

// getUserDetail fetches the full user detail.
export async function getUserDetail(id: string): Promise<AdminUserDetail> {
  return (await request(`${USERS_URL}/${id}`, { headers: authTokenHeaders() })) as AdminUserDetail
}

// createUser creates a user with the optional assignment sets. The response
// carries the server message (finding 8).
export async function createUser(input: AdminUserInput): Promise<AdminUserWriteResult> {
  return (await request(USERS_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(input),
  })) as AdminUserWriteResult
}

// updateUser replaces a user's profile and assignment sets atomically. The
// response carries the server message (finding 8).
export async function updateUser(id: string, input: AdminUserInput): Promise<AdminUserWriteResult> {
  return (await request(`${USERS_URL}/${id}`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(input),
  })) as AdminUserWriteResult
}

// deactivateUser deactivates an active user ("→ Sofort kein Login"), confirmed.
export async function deactivateUser(id: string): Promise<DeactivateResult> {
  return (await request(`${USERS_URL}/${id}/deactivate`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ confirmed: true }),
  })) as DeactivateResult
}

// listUserGroups fetches every organisational user group, ordered by name.
export async function listUserGroups(): Promise<UserGroup[]> {
  const data = (await request(USER_GROUPS_URL, { headers: authTokenHeaders() })) as { user_groups?: UserGroup[] }
  return Array.isArray(data.user_groups) ? data.user_groups : []
}

// createUserGroup creates an organisational user group.
export async function createUserGroup(input: UserGroupInput): Promise<UserGroup> {
  return (await request(USER_GROUPS_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(input),
  })) as UserGroup
}

// assignUserGroupMembers replaces an organisational user group's member set.
export async function assignUserGroupMembers(groupId: string, userIds: string[]): Promise<UserGroup> {
  return (await request(`${USER_GROUPS_URL}/${groupId}/members`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ user_ids: userIds }),
  })) as UserGroup
}

// listUserGroupMembers fetches the current member user ids of a group (finding
// 6: drives the member editor's pre-checked set).
export async function listUserGroupMembers(groupId: string): Promise<string[]> {
  const data = (await request(`${USER_GROUPS_URL}/${groupId}/members`, { headers: authTokenHeaders() })) as { user_ids?: string[] }
  return Array.isArray(data.user_ids) ? data.user_ids : []
}

// deleteUserGroup removes an organisational user group (finding 5).
export async function deleteUserGroup(groupId: string): Promise<{ message: string }> {
  return (await request(`${USER_GROUPS_URL}/${groupId}`, {
    method: 'DELETE',
    headers: authTokenHeaders(),
  })) as { message: string }
}

// Re-export the shared uniform-envelope error and the catalog type used by the
// editor's direct-grant grid.
export type { PermissionCatalogEntry }
export { ApiError }
