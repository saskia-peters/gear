// @vitest-environment jsdom
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, afterEach, vi } from 'vitest'
import { UserTable } from './UserTable.tsx'
import type { AdminUserSummary } from '../auth/users.ts'

function usersFixture(): AdminUserSummary[] {
  return [
    { id: 'u-tim', vorname: 'Tim', nachname: 'Müller', email: 'tim@gear.local', status: 'active', user_groups: ['Gruppe Ost'] },
    { id: 'u-lena', vorname: 'Lena', nachname: 'Schmidt', email: 'lena@gear.local', status: 'pending_approval', user_groups: [] },
    { id: 'u-gone', vorname: 'Max', nachname: 'Gone', email: 'max@gear.local', status: 'deactivated', user_groups: [] },
  ]
}

function renderTable(opts?: {
  users?: AdminUserSummary[]
  selectedFilter?: 'active' | 'pending_approval' | 'deactivated' | 'all'
  onSelectFilter?: (f: 'active' | 'pending_approval' | 'deactivated' | 'all') => void
  onOpenUser?: (u: AdminUserSummary) => void
}) {
  return render(
    <UserTable
      users={opts?.users ?? usersFixture()}
      selectedFilter={opts?.selectedFilter ?? 'active'}
      onSelectFilter={opts?.onSelectFilter ?? (() => {})}
      onOpenUser={opts?.onOpenUser ?? (() => {})}
    />,
  )
}

describe('UserTable', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it('COLUMNS: renders a real table with the four sortable columns', () => {
    renderTable()
    expect(screen.getByRole('columnheader', { name: /Vorname/ })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /Nachname/ })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /E-Mail/ })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /Status/ })).toBeInTheDocument()
  })

  it('FILTER_CHIPS: the filter chips include Aktiv/Pending/Deaktiviert/Alle and drive selection', async () => {
    const onSelectFilter = vi.fn()
    const user = userEvent.setup()
    renderTable({ onSelectFilter })

    expect(screen.getByRole('button', { name: 'Aktiv' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Pending' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Deaktiviert' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Alle' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Alle' }))
    expect(onSelectFilter).toHaveBeenCalledWith('all')
  })

  it('GROUP_TAGS: the inline user-group tag renders beside the name', () => {
    renderTable()
    const row = screen.getByText('Tim').closest('tr')!
    expect(within(row).getByText('Gruppe Ost')).toBeInTheDocument()
  })

  it('SORT: clicking the E-Mail header sorts the rows asc then desc', async () => {
    const user = userEvent.setup()
    renderTable()

    // Ascending: lena@ < max@ < tim@
    await user.click(screen.getByRole('button', { name: /Nach E-Mail sortieren/ }))
    const rowsAsc = screen.getAllByRole('row')
    expect(within(rowsAsc[1]).getByText('lena@gear.local')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /E-Mail/ })).toHaveAttribute('aria-sort', 'ascending')

    // Descending
    await user.click(screen.getByRole('button', { name: /Nach E-Mail sortieren/ }))
    const rowsDesc = screen.getAllByRole('row')
    expect(within(rowsDesc[1]).getByText('tim@gear.local')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /E-Mail/ })).toHaveAttribute('aria-sort', 'descending')

    // Third click turns sorting off
    await user.click(screen.getByRole('button', { name: /Nach E-Mail sortieren/ }))
    expect(screen.getByRole('columnheader', { name: /E-Mail/ })).toHaveAttribute('aria-sort', 'none')
  })

  it('EMPTY: shows a German empty state for the active filter', () => {
    renderTable({ users: [] })
    expect(screen.getByText('Keine aktiven Benutzer.')).toBeInTheDocument()
  })

  it('ROW_CLICK: activating a row opens the user detail', async () => {
    const onOpenUser = vi.fn()
    const user = userEvent.setup()
    renderTable({ onOpenUser })

    await user.click(screen.getByText('Tim').closest('tr')!)
    expect(onOpenUser).toHaveBeenCalledWith(expect.objectContaining({ id: 'u-tim' }))
  })

  it('SINGLE_ACTIVATION: each row is ONE interactive surface (no per-cell buttons)', () => {
    // Effort 2 finding 8: the spreadsheet must not render a button per cell —
    // only the row itself is the activation target.
    renderTable()
    const firstRow = screen.getByText('Tim').closest('tr')!
    const rowButtons = firstRow.querySelectorAll('button')
    expect(rowButtons).toHaveLength(0)
    expect(firstRow).toHaveAttribute('tabindex', '0')
  })
})
