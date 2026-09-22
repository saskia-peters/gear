// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { FilterChips } from './FilterChips.tsx'
import { FILTER_OPTIONS, type StatusCode } from '../types/filters.ts'

describe('FilterChips (Story 6.1 multi-select, code-keyed)', () => {
  it('renders all filter options with "Alle" active when nothing is selected', () => {
    render(<FilterChips selectedFilters={new Set()} onToggleFilter={vi.fn()} onClear={vi.fn()} />)

    FILTER_OPTIONS.forEach((option) => {
      const chip = screen.getByRole('button', { name: option })
      expect(chip).toBeInTheDocument()
      expect(chip).toHaveAttribute('aria-pressed', option === 'Alle' ? 'true' : 'false')
    })
  })

  it('marks the selected codes active and calls onToggleFilter with the CODE, never the German label', async () => {
    const user = userEvent.setup()
    const handleToggle = vi.fn()
    render(
      <FilterChips
        selectedFilters={new Set<StatusCode>(['red'])}
        onToggleFilter={handleToggle}
        onClear={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: 'Überfällig' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Einsatzbereit' })).toHaveAttribute('aria-pressed', 'false')
    // Any active status makes "Alle" inactive.
    expect(screen.getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'false')

    await user.click(screen.getByRole('button', { name: 'Ausstehend' }))
    expect(handleToggle).toHaveBeenCalledWith('orange')
  })

  it('"Alle" clears the selection via onClear', async () => {
    const user = userEvent.setup()
    const handleClear = vi.fn()
    render(
      <FilterChips
        selectedFilters={new Set<StatusCode>(['red', 'green'])}
        onToggleFilter={vi.fn()}
        onClear={handleClear}
      />,
    )

    expect(screen.getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'false')
    await user.click(screen.getByRole('button', { name: 'Alle' }))
    expect(handleClear).toHaveBeenCalled()
  })

  it('deselects an already-active chip (toggle off) — patch 9', async () => {
    const user = userEvent.setup()
    // A controlled harness: the parent toggles the code out on tap.
    let selected = new Set<StatusCode>(['red'])
    const handleToggle = vi.fn((code: StatusCode) => {
      const next = new Set(selected)
      if (next.has(code)) {
        next.delete(code)
      } else {
        next.add(code)
      }
      selected = next
    })
    const renderChips = () => (
      <FilterChips selectedFilters={selected} onToggleFilter={handleToggle} onClear={() => { selected = new Set() }} />
    )
    const { rerender } = render(renderChips())

    const chip = screen.getByRole('button', { name: 'Überfällig' })
    expect(chip).toHaveAttribute('aria-pressed', 'true')

    await user.click(chip)
    expect(handleToggle).toHaveBeenCalledWith('red')

    rerender(renderChips())
    expect(screen.getByRole('button', { name: 'Überfällig' })).toHaveAttribute('aria-pressed', 'false')
  })
})
