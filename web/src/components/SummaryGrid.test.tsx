// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { SummaryGrid } from './SummaryGrid.tsx'

describe('SummaryGrid', () => {
  it('renders all four status categories with default 0 counts', () => {
    render(<SummaryGrid />)

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
      />,
    )

    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })
})
