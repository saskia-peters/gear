// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { DASHBOARD_TOOLS_URL, submitInspection, reinstateTool, importToolsCsv, TOOL_IMPORT_URL } from './tools.ts'

function stubOk(body: unknown) {
  const mock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => body })
  vi.stubGlobal('fetch', mock)
  return mock
}

// submitOk is a server-authoritative 200 pass_fail submit response (Story 5.3
// contract): the persisted record + the derived status.
function submitOk(status: 'oos' | 'green' = 'green') {
  return {
    inspection: {
      id: 'insp-1',
      tool_id: 'tool-1',
      inspector_id: 'user-1',
      mode: 'pass_fail',
      overall_result: status === 'oos' ? 'fail' : 'pass',
      notes: 'Alles in Ordnung.',
      submitted_at: '2026-09-14T10:00:00Z',
      items: [],
    },
    status: { status, next_due: status === 'oos' ? null : '2027-09-14T10:00:00Z' },
  }
}

describe('inspection submit client (Story 5.4)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('submitInspection POSTs /api/v1/tools/{id}/inspection with the bearer token, the pass_fail body AND the client idempotency_key, and casts the response', async () => {
    const mock = stubOk(submitOk('green'))
    const res = await submitInspection(
      'tool-1',
      {
        mode: 'pass_fail',
        result: 'pass',
        notes: 'Alles in Ordnung.',
        items: [],
      },
      '11111111-1111-1111-1111-111111111111',
    )
    expect(res.inspection.overall_result).toBe('pass')
    expect(res.inspection.mode).toBe('pass_fail')
    expect(res.inspection.items).toEqual([])
    expect(res.status.status).toBe('green')
    expect(res.status.next_due).toBe('2027-09-14T10:00:00Z')

    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`${DASHBOARD_TOOLS_URL}/tool-1/inspection`)
    expect(init.method).toBe('POST')
    expect(init.headers).toEqual({
      'Content-Type': 'application/json',
      Authorization: 'Bearer sesstoken123',
    })
    expect(JSON.parse(init.body as string)).toEqual({
      mode: 'pass_fail',
      result: 'pass',
      notes: 'Alles in Ordnung.',
      items: [],
      idempotency_key: '11111111-1111-1111-1111-111111111111',
    })
  })

  it('submitInspection encodes the tool id into the path', async () => {
    const mock = stubOk(submitOk('green'))
    await submitInspection('tool/with special chars', { mode: 'pass_fail', result: 'pass', notes: '', items: [] }, '11111111-1111-1111-1111-111111111111')
    const url = mock.mock.calls[0][0] as string
    expect(url).toBe(`${DASHBOARD_TOOLS_URL}/tool%2Fwith%20special%20chars/inspection`)
  })

  it('submitInspection surfaces a 400 as an ApiError with the server German reason', async () => {
    const mock = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: { code: 'invalid_request', message: 'Bitte wähle ein gültiges Prüfergebnis.' } }),
    })
    vi.stubGlobal('fetch', mock)
    await expect(submitInspection('tool-1', { mode: 'pass_fail', result: 'fail', notes: '', items: [] }, '11111111-1111-1111-1111-111111111111')).rejects.toMatchObject({
      status: 400,
      message: 'Bitte wähle ein gültiges Prüfergebnis.',
    })
  })

  it('submitInspection surfaces a 500 as an ApiError with the server German reason', async () => {
    const mock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: { code: 'internal_error', message: 'Ein interner Fehler ist aufgetreten.' } }),
    })
    vi.stubGlobal('fetch', mock)
    await expect(submitInspection('tool-1', { mode: 'pass_fail', result: 'pass', notes: '', items: [] }, '11111111-1111-1111-1111-111111111111')).rejects.toMatchObject({
      status: 500,
      message: 'Ein interner Fehler ist aufgetreten.',
    })
  })
})

describe('reinstate client (Story 5.6 + Story 7.5)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reinstateTool POSTs /api/v1/tools/{id}/reinstatement with the reason AND the client idempotency_key', async () => {
    const mock = stubOk({
      status: { status: 'green', next_due: '2027-01-01T00:00:00Z' },
      message: 'Das Gerät wurde wiederhergestellt.',
    })
    const res = await reinstateTool('tool-1', 'Ersatzteil eingetroffen', '22222222-2222-2222-2222-222222222222')
    expect(res.message).toBe('Das Gerät wurde wiederhergestellt.')
    expect(res.status.status).toBe('green')

    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`${DASHBOARD_TOOLS_URL}/tool-1/reinstatement`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      reason: 'Ersatzteil eingetroffen',
      idempotency_key: '22222222-2222-2222-2222-222222222222',
    })
  })

  it('reinstateTool surfaces a 400 as an ApiError with the server German reason', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        json: async () => ({ error: { code: 'invalid_request', message: 'Das Gerät ist nicht außer Betrieb.' } }),
      }),
    )
    await expect(reinstateTool('tool-1', 'x', '22222222-2222-2222-2222-222222222222')).rejects.toMatchObject({
      status: 400,
      message: 'Das Gerät ist nicht außer Betrieb.',
    })
  })
})

describe('importToolsCsv multipart contract (Story 4.5)', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('gear.session_token', 'sesstoken123')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('POSTs /api/v1/admin/tools/import as multipart with ONLY the Authorization header and a FormData file body', async () => {
    const mock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ imported: 2, errors: [] }) })
    vi.stubGlobal('fetch', mock)

    const file = new File(['name,tool_type\nA,B\n'], 'tools.csv', { type: 'text/csv' })
    const res = await importToolsCsv(file)

    expect(res.imported).toBe(2)
    expect(res.errors).toEqual([])

    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(TOOL_IMPORT_URL)
    expect(init.method).toBe('POST')
    // The multipart boundary is browser-generated — the headers carry ONLY the
    // bearer token, NEVER a JSON Content-Type.
    const headers = new Headers(init.headers)
    expect(headers.get('Authorization')).toBe('Bearer sesstoken123')
    expect(headers.has('Content-Type')).toBe(false)
    expect(headers.get('Content-Type')).toBeNull()
    // The body is a FormData carrying the file under the `file` field.
    expect(init.body).toBeInstanceOf(FormData)
    const form = init.body as FormData
    expect(form.get('file')).toBe(file)
  })

  it('round-trips a 200 result payload', async () => {
    const mock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ imported: 1, errors: [{ row: 3, reason: "Tool Type 'X' nicht gefunden" }] }),
    })
    vi.stubGlobal('fetch', mock)

    const res = await importToolsCsv(new File(['name,tool_type\n'], 'tools.csv', { type: 'text/csv' }))
    expect(res.imported).toBe(1)
    expect(res.errors).toEqual([{ row: 3, reason: "Tool Type 'X' nicht gefunden" }])
  })

  it('surfaces a 403 as an ApiError with the German message (tools.manage-only, AD-6)', async () => {
    const mock = vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      json: async () => ({ error: { code: 'forbidden', message: 'Keine Berechtigung.' } }),
    })
    vi.stubGlobal('fetch', mock)

    await expect(importToolsCsv(new File(['x'], 'tools.csv', { type: 'text/csv' }))).rejects.toMatchObject({
      status: 403,
      message: 'Keine Berechtigung.',
    })
  })

  it('surfaces a 400 German reason (e.g. missing header)', async () => {
    const mock = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({
        error: { code: 'invalid_request', message: "Die CSV-Datei muss die Spalten 'name' und 'tool_type' enthalten." },
      }),
    })
    vi.stubGlobal('fetch', mock)

    await expect(importToolsCsv(new File(['foo,bar\n'], 'tools.csv', { type: 'text/csv' }))).rejects.toMatchObject({
      status: 400,
      message: "Die CSV-Datei muss die Spalten 'name' und 'tool_type' enthalten.",
    })
  })
})