// Qualification Management data module (Story 2.7). It holds the API types and
// calls for the "Qualifikationen" surface: vocabulary list / create / update,
// plus the per-qualification assignee list / replacement. The server is the
// source of truth for the status indicator (Unbegrenzt / Befristet for the
// vocabulary; Gültig / Bald ablaufend / Abgelaufen / Unbegrenzt per assignment,
// FR-22/AD-7) and the user roster — the SPA only maps the status codes to
// German badges.

import { ApiError, request, authTokenHeaders } from './http.ts'
import type { PermissionCatalogEntry } from './roles.ts'

export type QualificationExpiryKind = 'unlimited' | 'fixed'
export type QualificationStatus = 'unlimited' | 'valid' | 'expiring_soon' | 'expired' | 'fixed'

// Qualification is one vocabulary entry with its server-derived status
// indicator (Story 2.7). A qualification itself has NO valid-until date
// (2026-09-08 rework) — only its expiry kind; a per-user valid-until lives on
// assignments (Spec 2.9).
export interface Qualification {
  id: string
  name: string
  description: string
  expiry_kind: QualificationExpiryKind
  status: QualificationStatus
}

// QualificationRosterUser is one entry of the user roster returned alongside
// the qualification list, so the assignee editor can pick volunteers in one
// round-trip.
export interface QualificationRosterUser {
  id: string
  name: string
}

// QualificationList is the GET /qualifications payload.
export interface QualificationList {
  qualifications: Qualification[]
  users: QualificationRosterUser[]
}

// QualificationInput is the create/edit body (name, description, expiry kind).
// There is no valid-until date on a qualification itself (2026-09-08 rework).
export interface QualificationInput {
  name: string
  description: string
  expiry_kind: QualificationExpiryKind
}

// QualificationWriteResult is the create/update response (finding: the
// server-authoritative German confirmation plus the resulting qualification —
// the SPA must not hardcode its own success text).
export interface QualificationWriteResult {
  message: string
  qualification: Qualification
}

// QualificationAssignee is one user currently assigned a qualification.
export interface QualificationAssignee {
  id: string
  name: string
}

// QualificationAssignResult is the assignee-replacement response: the
// server-authoritative German confirmation plus the resulting set.
export interface QualificationAssignResult {
  message: string
  assignees: QualificationAssignee[]
}

// German display labels for the qualification statuses (FR-22/UX-DR8). The
// server sends the raw status code; the client renders the badge.
export const QUALIFICATION_STATUS_LABELS: Record<QualificationStatus, string> = {
  unlimited: 'Unbegrenzt',
  fixed: 'Befristet',
  valid: 'Gültig',
  expiring_soon: 'Bald ablaufend',
  expired: 'Abgelaufen',
}

// qualificationStatusLabel returns the German badge for a status code, falling
// back to the raw value so an unrecognized status never renders "undefined".
export function qualificationStatusLabel(status: string): string {
  return QUALIFICATION_STATUS_LABELS[status as QualificationStatus] ?? status
}

const QUALIFICATIONS_URL = '/api/v1/admin/qualifications'

// listQualifications fetches the full qualification vocabulary (with status
// indicators) plus the user roster for the assignment editor.
export async function listQualifications(): Promise<QualificationList> {
  const data = (await request(QUALIFICATIONS_URL, { headers: authTokenHeaders() })) as QualificationList
  return {
    qualifications: Array.isArray(data.qualifications) ? data.qualifications : [],
    users: Array.isArray(data.users) ? data.users : [],
  }
}

// createQualification creates a qualification with the given name and expiry
// model. The response carries the server message plus the created entry with
// its derived status.
export async function createQualification(input: QualificationInput): Promise<QualificationWriteResult> {
  return (await request(QUALIFICATIONS_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(input),
  })) as QualificationWriteResult
}

// updateQualification replaces a qualification's name/description/expiry model.
export async function updateQualification(id: string, input: QualificationInput): Promise<QualificationWriteResult> {
  return (await request(`${QUALIFICATIONS_URL}/${id}`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(input),
  })) as QualificationWriteResult
}

// listQualificationAssignees fetches the current assignees (id + display name)
// of a qualification for the editor's pre-checked set.
export async function listQualificationAssignees(id: string): Promise<QualificationAssignee[]> {
  const data = (await request(`${QUALIFICATIONS_URL}/${id}/assignees`, {
    headers: authTokenHeaders(),
  })) as { assignees?: QualificationAssignee[] }
  return Array.isArray(data.assignees) ? data.assignees : []
}

// assignQualificationUsers REPLACES the assignee set of a qualification. The
// response carries the server message + the resulting set.
export async function assignQualificationUsers(id: string, userIds: string[]): Promise<QualificationAssignResult> {
  return (await request(`${QUALIFICATIONS_URL}/${id}/assignees`, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify({ user_ids: userIds }),
  })) as QualificationAssignResult
}

// Re-export the shared uniform-envelope error and the catalog type used by the
// qualification editor's direct-grant grid.
export type { PermissionCatalogEntry }
export { ApiError }