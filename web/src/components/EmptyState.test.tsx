// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { EmptyState } from './EmptyState.tsx'

describe('EmptyState', () => {
  it('renders standard German empty message with status role', () => {
    render(<EmptyState />)

    const statusContainer = screen.getByRole('status')
    expect(statusContainer).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Keine Werkzeuge vorhanden' })).toBeInTheDocument()
    expect(screen.getByText('Aktuell sind keine Werkzeuge in dieser Ansicht vorhanden.')).toBeInTheDocument()
  })

  it('renders custom message and description when provided', () => {
    render(
      <EmptyState
        message="Keine überfälligen Werkzeuge"
        description="Alle Werkzeuge sind ordnungsgemäß geprüft."
      />,
    )

    expect(screen.getByRole('heading', { level: 2, name: 'Keine überfälligen Werkzeuge' })).toBeInTheDocument()
    expect(screen.getByText('Alle Werkzeuge sind ordnungsgemäß geprüft.')).toBeInTheDocument()
  })
})
