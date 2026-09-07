import { describe, it, expect } from 'vitest'
import {
  ADMIN_NAV_ENTRIES,
  filteredAdminNav,
  hasAnyAdminCode,
} from './permissions.ts'

describe('permissions admin-nav model', () => {
  it('ADMIN_NAV_ENTRIES: the nav lists exactly the 7 EXPERIENCE.md entries in IA order', () => {
    expect(ADMIN_NAV_ENTRIES.map((e) => e.label)).toEqual([
      'Übersicht',
      'Benutzer',
      'Rollen',
      'Qualifikationen',
      'Werkzeuge',
      'Einstellungen',
      'DSGVO',
    ])
    // Plain-language rule (UX-DR4/5/8): labels/descriptions never contain a
    // permission-code name or jargon.
    const codeLike = /\b(users|roles|qualifications|tools|tool_types|admin\.|dsgvo)\./
    for (const entry of ADMIN_NAV_ENTRIES) {
      expect(entry.label).not.toMatch(codeLike)
      expect(entry.description).not.toMatch(codeLike)
    }
  })

  it('filteredAdminNav: a full admin set exposes all 7 entries', () => {
    const perms = [
      'users.view',
      'users.approve',
      'users.manage',
      'roles.create',
      'roles.edit',
      'roles.assign',
      'qualifications.manage',
      'tools.manage',
      'tool_types.manage',
      'admin.settings.email',
      'admin.settings.backup',
      'dsgvo.access_report',
      'dsgvo.delete',
      'admin.recovery.approve',
    ]
    expect(filteredAdminNav(perms)).toHaveLength(7)
  })

  it('filteredAdminNav: a schirrmeister (tools.manage + tool_types.manage) sees only Übersicht + Werkzeuge', () => {
    const got = filteredAdminNav(['tools.manage', 'tool_types.manage'])
    expect(got.map((e) => e.key)).toEqual(['uebersicht', 'werkzeuge'])
  })

  it('filteredAdminNav: a helfende (no admin codes) sees nothing', () => {
    expect(filteredAdminNav(['dashboard.view', 'inspection.submit'])).toHaveLength(0)
  })

  it('filteredAdminNav: only the codes the set holds expose entries (anti-enumeration, FR-19)', () => {
    // A caller with only users.approve sees Benutzer (+ Übersicht), not Rollen.
    const got = filteredAdminNav(['users.approve'])
    expect(got.map((e) => e.key)).toEqual(['uebersicht', 'benutzer'])
  })

  it('hasAnyAdminCode: true when any admin-module code is held, false otherwise', () => {
    expect(hasAnyAdminCode(['tools.manage'])).toBe(true)
    expect(hasAnyAdminCode(['tool_types.manage'])).toBe(true)
    expect(hasAnyAdminCode(['users.approve'])).toBe(true)
    expect(hasAnyAdminCode(['dashboard.view', 'inspection.submit'])).toBe(false)
    expect(hasAnyAdminCode([])).toBe(false)
  })

  it('hasAnyAdminCode: unknown/garbage codes never open the admin module', () => {
    expect(hasAnyAdminCode(['nonsense.code', 'totally.made.up'])).toBe(false)
  })
})