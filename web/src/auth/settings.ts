// SMTP + backup settings + schedule-catalog data module (Story 3.1 + 3.2 +
// 4.1, FR-28/FR-29/FR-30). It holds the API types and calls for the
// Einstellungen → E-Mail, Einstellungen → Backup and Einstellungen → Zeitpläne
// surfaces: read/save/test SMTP; list/create/update/delete/test backup
// destinations; list/create/update/archive schedules. The server is the source
// of truth for the German microcopy. Credentials are WRITE-ONLY — GET returns
// only password_configured / credential_configured, and the save bodies omit
// the credential when it was left blank (so the server keeps the existing
// encrypted one, NFR-S4). Schedule archive is SOFT (server-side archived_at) —
// the client never hard-deletes.

import { ApiError, request, authTokenHeaders } from './http.ts'

export type SmtpSecurity = 'none' | 'starttls' | 'tls'

// Permission codes gating the three Einstellungen surfaces (AD-6, server-side
// source of truth). Kept here so the per-tab gating cannot drift from the
// server codes.
export const SMTP_SETTINGS_PERMISSION = 'admin.settings.email'
export const BACKUP_SETTINGS_PERMISSION = 'admin.settings.backup'
export const SCHEDULES_PERMISSION = 'schedules.manage'

// SmtpSettings is the GET payload — never a password (only password_configured).
export interface SmtpSettings {
  host: string
  port: number
  security: SmtpSecurity
  sender_address: string
  sender_name: string
  username: string
  password_configured: boolean
}

// SmtpSettingsWriteResult is the PUT payload: the settings plus the
// server-authoritative German confirmation.
export interface SmtpSettingsWriteResult extends SmtpSettings {
  message: string
}

// SmtpSettingsInput is the PUT body. password is OPTIONAL and omitted when the
// admin leaves it blank, so an existing password is kept (write-only edit).
export interface SmtpSettingsInput {
  host: string
  port: number
  security: SmtpSecurity
  sender_address: string
  sender_name: string
  username: string
  password?: string
}

// SmtpTestResult is the 200-style inline outcome of the test-email action.
export interface SmtpTestResult {
  ok: boolean
  message: string
}

const SMTP_URL = '/api/v1/admin/settings/smtp'

// getSmtpSettings fetches the current SMTP settings (zero defaults +
// password_configured=false when nothing is configured yet).
export async function getSmtpSettings(): Promise<SmtpSettings> {
  return (await request(SMTP_URL, { headers: authTokenHeaders() })) as SmtpSettings
}

// updateSmtpSettings persists the SMTP settings. When input.password is empty
// it is OMITTED from the body so the server keeps the existing encrypted
// password (PUT_NO_PASSWORD); a non-empty password is sent and encrypted at
// rest (PUT_VALID).
export async function updateSmtpSettings(input: SmtpSettingsInput): Promise<SmtpSettingsWriteResult> {
  const body: Record<string, unknown> = {
    host: input.host,
    port: input.port,
    security: input.security,
    sender_address: input.sender_address,
    sender_name: input.sender_name,
    username: input.username,
  }
  if (typeof input.password === 'string' && input.password !== '') {
    body.password = input.password
  }
  return (await request(SMTP_URL, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(body),
  })) as SmtpSettingsWriteResult
}

// testSmtpEmail sends a test email to the acting admin through the configured
// server and returns the inline German result (ok + message).
export async function testSmtpEmail(): Promise<SmtpTestResult> {
  return (await request(`${SMTP_URL}/test`, {
    method: 'POST',
    headers: authTokenHeaders(),
  })) as SmtpTestResult
}

export type BackupMechanism = 's3' | 'ftp' | 'sftp' | 'local'

// BackupDestination is the GET payload — never a credential (only
// credential_configured).
export interface BackupDestination {
  id: string
  name: string
  mechanism: BackupMechanism
  endpoint: string
  bucket_or_path: string
  username: string
  credential_configured: boolean
  schedule?: string
  created_at: string
  updated_at: string
}

// BackupDestinationWriteResult is the POST/PUT payload: the destination plus
// the server-authoritative German confirmation.
export interface BackupDestinationWriteResult extends BackupDestination {
  message: string
}

// BackupDestinationInput is the POST/PUT body. password is OPTIONAL and
// omitted when the admin leaves it blank, so an existing credential is kept
// (write-only edit). clear_credential explicitly revokes a stored credential
// on update (a blank password alone means keep-existing); it is mutually
// exclusive with a provided password.
export interface BackupDestinationInput {
  name: string
  mechanism: BackupMechanism
  endpoint: string
  bucket_or_path: string
  username: string
  password?: string
  clear_credential?: boolean
  schedule?: string
}

// BackupTestResult is the 200-style inline outcome of the "Verbindung testen"
// action.
export interface BackupTestResult {
  ok: boolean
  message: string
}

const BACKUP_URL = '/api/v1/admin/settings/backup'

// listBackupDestinations fetches every destination, oldest first
// (GET_LIST_EMPTY when none exists — the server answers an empty array).
export async function listBackupDestinations(): Promise<BackupDestination[]> {
  return (await request(BACKUP_URL, { headers: authTokenHeaders() })) as BackupDestination[]
}

// createBackupDestination persists a new destination. When input.password is
// empty it is OMITTED from the body (only local destinations may omit it).
export async function createBackupDestination(input: BackupDestinationInput): Promise<BackupDestinationWriteResult> {
  return (await request(BACKUP_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildBackupBody(input)),
  })) as BackupDestinationWriteResult
}

// updateBackupDestination persists a destination. An empty password is OMITTED
// so the server keeps the existing encrypted credential (UPDATE_KEEP_CREDENTIAL).
export async function updateBackupDestination(id: string, input: BackupDestinationInput): Promise<BackupDestinationWriteResult> {
  return (await request(`${BACKUP_URL}/${id}`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildBackupBody(input)),
  })) as BackupDestinationWriteResult
}

// deleteBackupDestination removes one destination.
export async function deleteBackupDestination(id: string): Promise<{ message: string }> {
  return (await request(`${BACKUP_URL}/${id}`, {
    method: 'DELETE',
    headers: authTokenHeaders(),
  })) as { message: string }
}

// testBackupDestination exercises the destination's mechanism/endpoint and
// returns the inline German result (ok + message).
export async function testBackupDestination(id: string): Promise<BackupTestResult> {
  return (await request(`${BACKUP_URL}/${id}/test`, {
    method: 'POST',
    headers: authTokenHeaders(),
  })) as BackupTestResult
}

// buildBackupBody assembles the POST/PUT body, omitting a blank credential so
// the server keeps the existing one (write-only edit, NFR-S4). clear_credential
// is included only when the admin explicitly chose to revoke the stored
// credential.
function buildBackupBody(input: BackupDestinationInput): Record<string, unknown> {
  const body: Record<string, unknown> = {
    name: input.name,
    mechanism: input.mechanism,
    endpoint: input.endpoint,
    bucket_or_path: input.bucket_or_path,
    username: input.username,
    schedule: input.schedule ?? '',
  }
  if (input.clear_credential) {
    body.clear_credential = true
  } else if (typeof input.password === 'string' && input.password !== '') {
    body.password = input.password
  }
  return body
}

export type ScheduleIntervalUnit = 'year' | 'quarter' | 'month' | 'week' | 'day'

// Schedule is the GET payload for one active schedule-catalog row (FR-30,
// AD-16). The reserved weekday/time composite fields never reach the client in
// V1; archived schedules are filtered server-side.
export interface Schedule {
  id: string
  name: string
  interval_unit: ScheduleIntervalUnit
  interval_magnitude: number
  created_at: string
  updated_at: string
}

// ScheduleWriteResult is the POST/PUT/archive payload: the schedule plus the
// server-authoritative German confirmation.
export interface ScheduleWriteResult extends Schedule {
  message: string
}

// ScheduleInput is the POST/PUT body (name + interval unit/magnitude). The
// reserved weekday/time fields are NOT part of the input (stored-but-ignored
// in V1, AD-16).
export interface ScheduleInput {
  name: string
  interval_unit: ScheduleIntervalUnit
  interval_magnitude: number
}

const SCHEDULES_URL = '/api/v1/admin/settings/schedules'

// listSchedules fetches the ACTIVE catalog, oldest first (GET_LIST_EMPTY when
// none exist — the server answers an empty array; archived rows never appear).
export async function listSchedules(): Promise<Schedule[]> {
  return (await request(SCHEDULES_URL, { headers: authTokenHeaders() })) as Schedule[]
}

// createSchedule persists a new schedule (name + interval unit/magnitude).
export async function createSchedule(input: ScheduleInput): Promise<ScheduleWriteResult> {
  return (await request(SCHEDULES_URL, {
    method: 'POST',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildScheduleBody(input)),
  })) as ScheduleWriteResult
}

// updateSchedule persists a schedule. Updating an archived row answers the 404
// sentinel server-side.
export async function updateSchedule(id: string, input: ScheduleInput): Promise<ScheduleWriteResult> {
  return (await request(`${SCHEDULES_URL}/${id}`, {
    method: 'PUT',
    headers: authTokenHeaders(),
    body: JSON.stringify(buildScheduleBody(input)),
  })) as ScheduleWriteResult
}

// archiveSchedule SOFT-archives one schedule (the row leaves the active list;
// history is preserved server-side).
export async function archiveSchedule(id: string): Promise<ScheduleWriteResult> {
  return (await request(`${SCHEDULES_URL}/${id}/archive`, {
    method: 'POST',
    headers: authTokenHeaders(),
  })) as ScheduleWriteResult
}

function buildScheduleBody(input: ScheduleInput): Record<string, unknown> {
  return {
    name: input.name,
    interval_unit: input.interval_unit,
    interval_magnitude: input.interval_magnitude,
  }
}

export { ApiError }