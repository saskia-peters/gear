// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest'
import {
  SESSION_TOKEN_KEY,
  clearAuthState,
  getPermissions,
  hasPermission,
  saveAuthState,
  savePermissions,
  setIsAdmin,
} from './authState.ts'

describe('authState permissions cache', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('savePermissions/getPermissions round-trips the resolved set', () => {
    expect(getPermissions()).toEqual([])
    savePermissions(['tools.manage', 'tool_types.manage'])
    expect(getPermissions()).toEqual(['tools.manage', 'tool_types.manage'])
  })

  it('savePermissions([]) and clearAuthState clear the cache', () => {
    savePermissions(['users.manage'])
    savePermissions([])
    expect(getPermissions()).toEqual([])

    savePermissions(['users.manage'])
    clearAuthState()
    expect(getPermissions()).toEqual([])
  })

  it('savePermissions rejects a non-array (defensive against a bad server response)', () => {
    // @ts-expect-error — a malformed server payload must not crash the cache.
    savePermissions('not-an-array')
    expect(getPermissions()).toEqual([])
  })

  it('getPermissions tolerates corrupted JSON in storage', () => {
    localStorage.setItem('gear.permissions', '{not json')
    expect(getPermissions()).toEqual([])
  })

  it('hasPermission reports membership of the cached set', () => {
    savePermissions(['tools.manage'])
    expect(hasPermission('tools.manage')).toBe(true)
    expect(hasPermission('users.manage')).toBe(false)
    expect(hasPermission('admin.settings.email')).toBe(false)
  })

  it('clearAuthState removes the permissions key alongside the other auth keys', () => {
    saveAuthState('tok', false, { displayName: 'Max' }, true)
    savePermissions(['users.manage'])
    expect(localStorage.getItem(SESSION_TOKEN_KEY)).toBe('tok')

    clearAuthState()
    expect(localStorage.getItem('gear.permissions')).toBeNull()
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })

  it('savePermissions is independent of setIsAdmin (server-authoritative cache)', () => {
    setIsAdmin(false)
    savePermissions(['tools.manage'])
    expect(getPermissions()).toEqual(['tools.manage'])
    expect(localStorage.getItem('gear.is_admin')).toBeNull()
  })
})