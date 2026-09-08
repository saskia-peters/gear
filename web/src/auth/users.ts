// User & Group Administration data module (Story 2.6). It holds the API types
// and calls for the "Benutzer" surface: user list / detail / create / update /
// deactivate, plus organisational user-group (team) list / create / member
// assignment. The server is the source of truth for status (active /
// pending_approval / deactivated) and the qualification expiry status — the
// SPA only maps them to German badges.

import { ApiError } from './roles.ts'
import type { PermissionCatalogEntry } from './roles.ts'

export type UserStatus = 'active' | 'pending_approval' | 'deactivated'

// UserStatusFilter drives the status filter chips and the ?status= query on
// ListUsers (Effort 2): 'all' sends no status so every user is returned.
export type UserStatusFilter = UserStatus | 'all'

// USER_STATUS_FILTERS is the ordered chip list (label + value) of the user list
// spreadsheet. DEFAULT_USER_STATUS_FILTER is the default (aktiv), per the spec.
export const USER_STATUS_FILTERS: Array<{ value: UserStatusFilter; label: string }> = [
  { value: 'active', label: 'Aktiv' },
  { value: 'pending_approval', label: 'Pending' },
  { value: 'deactivated', label: 'Deaktiviert' },
  { value: 'all', label: 'Alle' },
]

export const DEFAULT_USER_STATUS_FILTER: UserStatusFilter = 'active'

// AdminUserSummary is one row of the user list.
export interface AdminUserSummary {
  id: string
  vorname: string
  nachname: string
  email: string
  status: UserStatus
  // user_groups holds the organisational team names the user belongs to
  // (Effort 2): rendered as inline tags under/beside the name. Membership
  // grants no permission (AD-12) — display data only.
  user_groups?: string[]
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

// OtpIssueResult is the one-time-password issuance response (Spec 2.8): the
// server-authoritative German confirmation, the target identity and the
// PLAINTEXT one-time password — returned exactly once, never stored, never
// recoverable. The SPA shows it once in a dismissible panel and discards it on
// close (no re-display/copy persistence).
export interface OtpIssueResult {
  message: string
  user_id: string
  email: string
  one_time_password: string
  expires_at: string
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
// An optional status filter (active | pending_approval | deactivated) is passed
// as ?status= and narrows the set server-side (Spec 2.9) — the SPA's default
// "aktiv" chip drives it.
export async function listUsers(status?: UserStatus): Promise<AdminUserSummary[]> {
  const qs = status ? `?status=${encodeURIComponent(status)}` : ''
  const data = (await request(`${USERS_URL}${qs}`, { headers: authTokenHeaders() })) as { users?: AdminUserSummary[] }
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

// issueOneTimePassword generates a single-use one-time password for an ACTIVE
// user (Spec 2.8): the plaintext OTP is returned exactly once. Confirmation is
// required server-side. Gated by users.manage. A 409 answers a non-active
// target (deactivated/pending — OTPs are active-only); the server message is
// displayed verbatim.
export async function issueOneTimePassword(userId: string): Promise<OtpIssueResult> {
  return (await request(`${USERS_URL}/${userId}/otp`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ confirmed: true }),
  })) as OtpIssueResult
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

// assignUserGroups replaces a USER's organisational user-group set from the
// user detail (Effort 2). Returns the refreshed user detail + server message.
export async function assignUserGroups(userId: string, groupIds: string[]): Promise<AdminUserWriteResult> {
  return (await request(`${USERS_URL}/${userId}/groups`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify({ user_group_ids: groupIds }),
  })) as AdminUserWriteResult
}

// listUserGroupMembers fetches the current member user ids of a group (finding
// 6: drives the member editor's pre-checked set).
export async function listUserGroupMembers(groupId: string): Promise<string[]> {
  const data = (await request(`${USER_GROUPS_URL}/${groupId}/members`, { headers: authTokenHeaders() })) as { user_ids?: string[] }
  return Array.isArray(data.user_ids) ? data.user_ids : []
}

// listUserGroupRoles fetches the permission groups (roles) an organisational
// user group grants its members (Spec 2.9). Gated by user_groups.manage.
export async function listUserGroupRoles(groupId: string): Promise<RoleGroupRef[]> {
  const data = (await request(`${USER_GROUPS_URL}/${groupId}/roles`, { headers: authTokenHeaders() })) as { roles?: RoleGroupRef[] }
  return Array.isArray(data.roles) ? data.roles : []
}

// assignUserGroupRoles REPLACES an organisational user group's role set
// atomically (Spec 2.9). Members inherit the new roles on the next request.
export async function assignUserGroupRoles(groupId: string, roleIds: string[]): Promise<RoleGroupRef[]> {
  const data = (await request(`${USER_GROUPS_URL}/${groupId}/roles`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ role_ids: roleIds }),
  })) as { roles?: RoleGroupRef[] }
  return Array.isArray(data.roles) ? data.roles : []
}

// UserQualificationAssignResult is the per-user qualification assignment
// response (Spec 2.9): the server-authoritative German message plus the
// target ids so the client can scope feedback.
export interface UserQualificationAssignResult {
  message: string
  user_id: string
  qualification_id: string
}

// assignUserQualification assigns a qualification to a user with an optional
// per-user valid-until (Spec 2.9). A `fixed` qualification REQUIRES expires_at;
// an `unlimited` one must not carry it (send null). Gated by
// users.qualifications.manage.
export async function assignUserQualification(
  userId: string,
  qualificationId: string,
  expiresAt?: string | null,
): Promise<UserQualificationAssignResult> {
  return (await request(`${USERS_URL}/${userId}/qualifications/${qualificationId}`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ expires_at: expiresAt ?? null }),
  })) as UserQualificationAssignResult
}

// revokeUserQualification revokes a qualification from a user immediately
// (Spec 2.9). Gated by users.qualifications.manage.
export async function revokeUserQualification(userId: string, qualificationId: string): Promise<UserQualificationAssignResult> {
  return (await request(`${USERS_URL}/${userId}/qualifications/${qualificationId}`, {
    method: 'DELETE',
    headers: authTokenHeaders(),
  })) as UserQualificationAssignResult
}

// updateUserQualificationExpiry edits a user's per-assignment valid-until
// (Spec 2.9): a null clears the override (reverting to the vocabulary expiry).
// Gated by users.qualifications.manage.
export async function updateUserQualificationExpiry(
  userId: string,
  qualificationId: string,
  expiresAt?: string | null,
): Promise<UserQualificationAssignResult> {
  return (await request(`${USERS_URL}/${userId}/qualifications/${qualificationId}/expiry`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify({ expires_at: expiresAt ?? null }),
  })) as UserQualificationAssignResult
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
