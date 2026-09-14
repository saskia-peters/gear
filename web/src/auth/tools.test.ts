// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { DASHBOARD_TOOLS_URL, submitInspection } from './tools.ts'

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

  it('submitInspection POSTs /api/v1/tools/{id}/inspection with the bearer token and the pass_fail body, and casts the response', async () => {
    const mock = stubOk(submitOk('green'))
    const res = await submitInspection('tool-1', {
      mode: 'pass_fail',
      result: 'pass',
      notes: 'Alles in Ordnung.',
      items: [],
    })
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
    })
  })

  it('submitInspection encodes the tool id into the path', async () => {
    const mock = stubOk(submitOk('green'))
    await submitInspection('tool/with special chars', { mode: 'pass_fail', result: 'pass', notes: '', items: [] })
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
    await expect(submitInspection('tool-1', { mode: 'pass_fail', result: 'fail', notes: '', items: [] })).rejects.toMatchObject({
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
    await expect(submitInspection('tool-1', { mode: 'pass_fail', result: 'pass', notes: '', items: [] })).rejects.toMatchObject({
      status: 500,
      message: 'Ein interner Fehler ist aufgetreten.',
    })
  })
})