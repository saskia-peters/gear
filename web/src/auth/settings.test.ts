// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { getSmtpSettings, updateSmtpSettings, testSmtpEmail } from './settings.ts'

const SMTP_URL = '/api/v1/admin/settings/smtp'

function stubOk(body: unknown) {
  const mock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => body })
  vi.stubGlobal('fetch', mock)
  return mock
}

describe('settings client', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('getSmtpSettings GETs with the bearer token', async () => {
    const mock = stubOk({ host: 'smtp.example.com', password_configured: false })
    const settings = await getSmtpSettings()
    expect(settings.host).toBe('smtp.example.com')
    expect(mock).toHaveBeenCalledWith(SMTP_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('updateSmtpSettings omits an empty password so the server keeps the existing one', async () => {
    const mock = stubOk({ host: 'mail.example.com', password_configured: true, message: 'SMTP-Einstellungen gespeichert.' })
    await updateSmtpSettings({
      host: 'mail.example.com',
      port: 587,
      security: 'starttls',
      sender_address: 'noreply@example.com',
      sender_name: '',
      username: '',
      password: '',
    })
    const body = JSON.parse((mock.mock.calls[0][1] as RequestInit).body as string)
    expect(body.host).toBe('mail.example.com')
    expect('password' in body).toBe(false)
  })

  it('updateSmtpSettings includes a non-empty password for encrypt-on-write', async () => {
    const mock = stubOk({ host: 'mail.example.com', password_configured: true, message: 'SMTP-Einstellungen gespeichert.' })
    await updateSmtpSettings({
      host: 'mail.example.com',
      port: 587,
      security: 'starttls',
      sender_address: 'noreply@example.com',
      sender_name: '',
      username: '',
      password: 'geheim123',
    })
    const body = JSON.parse((mock.mock.calls[0][1] as RequestInit).body as string)
    expect(body.password).toBe('geheim123')
  })

  it('testSmtpEmail POSTs /smtp/test and returns the inline result', async () => {
    const mock = stubOk({ ok: true, message: 'Test-E-Mail erfolgreich gesendet.' })
    const result = await testSmtpEmail()
    expect(result.ok).toBe(true)
    expect(mock).toHaveBeenCalledWith(`${SMTP_URL}/test`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })
})