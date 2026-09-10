// SMTP settings data module (Story 3.1, FR-28). It holds the API types and
// calls for the Einstellungen → E-Mail surface: read the current SMTP settings,
// save them, and send a test email. The server is the source of truth for the
// German microcopy; the password is WRITE-ONLY — GET returns only
// password_configured and the save body omits the password when it was left
// blank (so the server keeps the existing encrypted one, NFR-S4).

import { ApiError, request, authTokenHeaders } from './http.ts'

export type SmtpSecurity = 'none' | 'starttls' | 'tls'

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

export { ApiError }