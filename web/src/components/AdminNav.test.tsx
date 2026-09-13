// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { AdminNav } from './AdminNav.tsx'
import { filteredAdminNav } from '../auth/permissions.ts'

describe('AdminNav', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    cleanup()
  })

  it('NAV_FILTERED: renders only the entries it is given (already permission-filtered)', () => {
    const entries = filteredAdminNav(['tools.manage'])
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    // The entry list is the same either way; the caller filters. We assert
    // that the passed entries are the only ones rendered — no hidden surfaces.
    expect(screen.getByRole('navigation', { name: 'Verwaltung' })).toBeInTheDocument()
    expect(screen.getAllByRole('link').map((l) => l.textContent)).toEqual(['Übersicht', 'Werkzeuge'])
  })

  it('ACTIVE_STATE: the current route entry carries aria-current="page"', () => {
    const entries = filteredAdminNav([
      'users.manage',
      'tools.manage',
      'dsgvo.delete',
    ])
    render(
      <MemoryRouter initialEntries={['/admin/werkzeuge']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    const werkzeuge = screen.getByRole('link', { name: 'Werkzeuge' })
    expect(werkzeuge).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'Übersicht' })).not.toHaveAttribute('aria-current')
  })

  it('MOBILE_TOGGLE: the hamburger toggles the list on narrow screens (aria-expanded)', async () => {
    const user = userEvent.setup()
    const entries = filteredAdminNav(['users.manage', 'tools.manage'])
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    const toggle = screen.getByRole('button', { name: /Verwaltung/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveAttribute('aria-controls', 'admin-nav-list')

    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('ESCAPE_CLOSE: pressing Escape closes the menu and returns focus to the toggle', async () => {
    const user = userEvent.setup()
    const entries = filteredAdminNav(['users.manage', 'tools.manage'])
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    const toggle = screen.getByRole('button', { name: /Verwaltung/ })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')

    await user.keyboard('{Escape}')

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveFocus()
  })

  it('OUTSIDE_CLICK: a mousedown outside the nav closes the menu', async () => {
    const user = userEvent.setup()
    const entries = filteredAdminNav(['users.manage', 'tools.manage'])
    const { baseElement } = render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    const toggle = screen.getByRole('button', { name: /Verwaltung/ })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')

    await user.click(baseElement)

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  it('LINK_NAV_CLOSES: clicking a nav link closes the menu', async () => {
    const user = userEvent.setup()
    const entries = filteredAdminNav(['users.manage', 'tools.manage'])
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    const toggle = screen.getByRole('button', { name: /Verwaltung/ })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')

    await user.click(screen.getByRole('link', { name: 'Werkzeuge' }))

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  it('NAV_ICONS: every nav link renders an aria-hidden svg icon before its label', () => {
    const entries = filteredAdminNav([
      'users.view',
      'users.approve',
      'users.manage',
      'user_groups.manage',
      'roles.create',
      'roles.edit',
      'roles.assign',
      'qualifications.manage',
      'tools.manage',
      'tool_types.manage',
      'admin.settings.email',
      'admin.settings.backup',
      'schedules.manage',
      'dsgvo.access_report',
      'dsgvo.delete',
    ])
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={entries} />
      </MemoryRouter>,
    )

    for (const entry of entries) {
      const link = screen.getByRole('link', { name: entry.label })
      const icon = link.querySelector('svg')
      expect(icon, `${entry.label} link should show its icon`).not.toBeNull()
      expect(icon).toHaveAttribute('aria-hidden', 'true')
      // The icon is purely decorative and never leaks into the label text.
      expect(link.textContent).toBe(entry.label)
    }
  })

  it('FUEHRENDE_AFTER_REVOKE: a revoked tools.manage removes the Werkzeuge link on the next render (live set)', () => {
    // First render: fuehrende holds tools.manage → Werkzeuge present.
    const before = filteredAdminNav(['tools.manage'])
    const { rerender } = render(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={before} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'Werkzeuge' })).toBeInTheDocument()

    // Permission revoked: the new filtered set no longer exposes Werkzeuge.
    const after = filteredAdminNav([])
    rerender(
      <MemoryRouter initialEntries={['/admin']}>
        <AdminNav entries={after} />
      </MemoryRouter>,
    )
    expect(screen.queryByRole('link', { name: 'Werkzeuge' })).not.toBeInTheDocument()
  })
})