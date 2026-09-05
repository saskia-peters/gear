// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { FilterChips } from './FilterChips.tsx'
import { FILTER_OPTIONS, type FilterStatus } from '../types/filters.ts'

describe('FilterChips', () => {
  it('renders all filter options with active state on selected item', () => {
    const handleSelect = vi.fn()
    render(<FilterChips selectedFilter="Alle" onSelectFilter={handleSelect} />)

    FILTER_OPTIONS.forEach((option) => {
      const chip = screen.getByRole('button', { name: option })
      expect(chip).toBeInTheDocument()
      if (option === 'Alle') {
        expect(chip).toHaveAttribute('aria-pressed', 'true')
      } else {
        expect(chip).toHaveAttribute('aria-pressed', 'false')
      }
    })
  })

  it('calls onSelectFilter when an inactive chip is clicked', async () => {
    const user = userEvent.setup()
    let currentFilter: FilterStatus = 'Alle'
    const handleSelect = vi.fn((filter: FilterStatus) => {
      currentFilter = filter
    })

    const { rerender } = render(
      <FilterChips selectedFilter={currentFilter} onSelectFilter={handleSelect} />,
    )

    const ueberfaelligChip = screen.getByRole('button', { name: 'Überfällig' })
    await user.click(ueberfaelligChip)

    expect(handleSelect).toHaveBeenCalledWith('Überfällig')

    rerender(
      <FilterChips selectedFilter={currentFilter} onSelectFilter={handleSelect} />,
    )
    expect(screen.getByRole('button', { name: 'Überfällig' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })
})
