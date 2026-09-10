// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  getSmtpSettings,
  updateSmtpSettings,
  testSmtpEmail,
  listBackupDestinations,
  createBackupDestination,
  updateBackupDestination,
  deleteBackupDestination,
  testBackupDestination,
  listSchedules,
  createSchedule,
  updateSchedule,
  archiveSchedule,
} from './settings.ts'

const SMTP_URL = '/api/v1/admin/settings/smtp'
const BACKUP_URL = '/api/v1/admin/settings/backup'
const SCHEDULES_URL = '/api/v1/admin/settings/schedules'

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

  it('listBackupDestinations GETs /backup with the bearer token', async () => {
    const mock = stubOk([{ id: 'id-a', name: 'S3', credential_configured: true }])
    const dests = await listBackupDestinations()
    expect(dests).toHaveLength(1)
    expect(dests[0].credential_configured).toBe(true)
    expect(mock).toHaveBeenCalledWith(BACKUP_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('createBackupDestination POSTs the body and omits an empty credential', async () => {
    const mock = stubOk({ id: 'id-a', name: 'S3', credential_configured: false, message: 'Backup-Ziel gespeichert.' })
    await createBackupDestination({
      name: 'S3',
      mechanism: 's3',
      endpoint: 's3.example.com',
      bucket_or_path: 'bucket',
      username: 'svc',
      password: '',
      schedule: '0 2 * * *',
    })
    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(BACKUP_URL)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body as string)
    expect(body.mechanism).toBe('s3')
    expect(body.schedule).toBe('0 2 * * *')
    expect('password' in body).toBe(false)
  })

  it('createBackupDestination includes a non-empty credential for encrypt-on-write', async () => {
    const mock = stubOk({ id: 'id-a', name: 'S3', credential_configured: true, message: 'Backup-Ziel gespeichert.' })
    await createBackupDestination({
      name: 'S3',
      mechanism: 's3',
      endpoint: 's3.example.com',
      bucket_or_path: 'bucket',
      username: 'svc',
      password: 'geheim123',
    })
    const body = JSON.parse((mock.mock.calls[0][1] as RequestInit).body as string)
    expect(body.password).toBe('geheim123')
  })

  it('updateBackupDestination PUTs /backup/{id} and omits an empty credential', async () => {
    const mock = stubOk({ id: 'id-a', name: 'S3 v2', credential_configured: true, message: 'Backup-Ziel gespeichert.' })
    await updateBackupDestination('id-a', {
      name: 'S3 v2',
      mechanism: 's3',
      endpoint: 's3.example.com',
      bucket_or_path: 'bucket',
      username: 'svc',
    })
    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`${BACKUP_URL}/id-a`)
    expect(init.method).toBe('PUT')
    const body = JSON.parse(init.body as string)
    expect(body.name).toBe('S3 v2')
    expect('password' in body).toBe(false)
    expect('clear_credential' in body).toBe(false)
  })

  it('updateBackupDestination sends clear_credential:true when revoking the stored credential', async () => {
    const mock = stubOk({ id: 'id-a', name: 'S3 v2', credential_configured: false, message: 'Backup-Ziel gespeichert.' })
    await updateBackupDestination('id-a', {
      name: 'S3 v2',
      mechanism: 's3',
      endpoint: 's3.example.com',
      bucket_or_path: 'bucket',
      username: 'svc',
      clear_credential: true,
    })
    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`${BACKUP_URL}/id-a`)
    expect(init.method).toBe('PUT')
    const body = JSON.parse(init.body as string)
    expect(body.clear_credential).toBe(true)
    expect('password' in body).toBe(false)
  })

  it('deleteBackupDestination DELETEs /backup/{id} and returns the message', async () => {
    const mock = stubOk({ message: 'Backup-Ziel gelöscht.' })
    const res = await deleteBackupDestination('id-a')
    expect(res.message).toBe('Backup-Ziel gelöscht.')
    expect(mock).toHaveBeenCalledWith(`${BACKUP_URL}/id-a`, {
      method: 'DELETE',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('testBackupDestination POSTs /backup/{id}/test and returns the inline result', async () => {
    const mock = stubOk({ ok: false, message: 'Die Verbindung zum Backup-Ziel konnte nicht hergestellt werden.' })
    const result = await testBackupDestination('id-a')
    expect(result.ok).toBe(false)
    expect(mock).toHaveBeenCalledWith(`${BACKUP_URL}/id-a/test`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })
})

describe('schedule catalog client', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('listSchedules GETs /schedules with the bearer token', async () => {
    const mock = stubOk([{ id: 'id-a', name: '1 year', interval_unit: 'year', interval_magnitude: 1 }])
    const schedules = await listSchedules()
    expect(schedules).toHaveLength(1)
    expect(schedules[0].interval_unit).toBe('year')
    expect(schedules[0].interval_magnitude).toBe(1)
    expect(mock).toHaveBeenCalledWith(SCHEDULES_URL, {
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })

  it('createSchedule POSTs the name + interval body', async () => {
    const mock = stubOk({ id: 'id-a', name: '1 year', interval_unit: 'year', interval_magnitude: 1, message: 'Zeitplan gespeichert.' })
    const created = await createSchedule({ name: '1 year', interval_unit: 'year', interval_magnitude: 1 })
    expect(created.message).toBe('Zeitplan gespeichert.')
    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(SCHEDULES_URL)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body as string)
    expect(body.name).toBe('1 year')
    expect(body.interval_unit).toBe('year')
    expect(body.interval_magnitude).toBe(1)
  })

  it('updateSchedule PUTs /schedules/{id} with the interval body', async () => {
    const mock = stubOk({ id: 'id-a', name: '2 years', interval_unit: 'year', interval_magnitude: 2, message: 'Zeitplan gespeichert.' })
    await updateSchedule('id-a', { name: '2 years', interval_unit: 'year', interval_magnitude: 2 })
    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`${SCHEDULES_URL}/id-a`)
    expect(init.method).toBe('PUT')
    const body = JSON.parse(init.body as string)
    expect(body.name).toBe('2 years')
    expect(body.interval_magnitude).toBe(2)
  })

  it('archiveSchedule POSTs /schedules/{id}/archive and returns the message', async () => {
    const mock = stubOk({ id: 'id-a', name: '1 year', interval_unit: 'year', interval_magnitude: 1, message: 'Zeitplan archiviert.' })
    const result = await archiveSchedule('id-a')
    expect(result.message).toBe('Zeitplan archiviert.')
    expect(mock).toHaveBeenCalledWith(`${SCHEDULES_URL}/id-a/archive`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer sesstoken123',
      },
    })
  })
})