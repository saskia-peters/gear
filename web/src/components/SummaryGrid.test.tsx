// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { SummaryGrid } from './SummaryGrid.tsx'

describe('SummaryGrid', () => {
  it('renders all four status categories with default 0 counts', () => {
    render(<SummaryGrid counts={{}} onToggleFilter={vi.fn()} />)

    expect(screen.getByText('Einsatzbereit')).toBeInTheDocument()
    expect(screen.getByText('Ausstehend')).toBeInTheDocument()
    expect(screen.getByText('Überfällig')).toBeInTheDocument()
    expect(screen.getByText('Außer Betrieb')).toBeInTheDocument()

    const zeros = screen.getAllByText('0')
    expect(zeros).toHaveLength(4)
  })

  it('renders provided counts correctly', () => {
    render(
      <SummaryGrid
        counts={{
          einsatzbereit: 12,
          ausstehend: 3,
          ueberfaellig: 1,
          ausserBetrieb: 2,
        }}
        onToggleFilter={vi.fn()}
      />,
    )

    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('cards are tappable buttons that call onToggleFilter with the stable status CODE, not the label', async () => {
    const user = userEvent.setup()
    const handleToggle = vi.fn()
    render(
      <SummaryGrid
        counts={{ einsatzbereit: 1, ausserBetrieb: 1, ueberfaellig: 2 }}
        onToggleFilter={handleToggle}
      />,
    )

    await user.click(screen.getByRole('button', { name: '1 Einsatzbereit' }))
    expect(handleToggle).toHaveBeenCalledWith('green')

    await user.click(screen.getByRole('button', { name: /Außer Betrieb/ }))
    expect(handleToggle).toHaveBeenCalledWith('oos')

    await user.click(screen.getByRole('button', { name: '2 Überfällig' }))
    expect(handleToggle).toHaveBeenCalledWith('red')
  })

  it('zero-count cards are DISABLED and never toggle a filter — patch 3', async () => {
    const user = userEvent.setup()
    const handleToggle = vi.fn()
    render(
      <SummaryGrid
        counts={{ einsatzbereit: 0, ausserBetrieb: 1 }}
        onToggleFilter={handleToggle}
      />,
    )

    const zeroGreen = screen.getByRole('button', { name: '0 Einsatzbereit' })
    expect(zeroGreen).toBeDisabled()
    await user.click(zeroGreen)
    expect(handleToggle).not.toHaveBeenCalled()

    // A non-zero card stays tappable.
    await user.click(screen.getByRole('button', { name: '1 Außer Betrieb' }))
    expect(handleToggle).toHaveBeenCalledWith('oos')
  })
})
