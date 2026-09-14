// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { PromptDialog } from './PromptDialog.tsx'

function renderDialog(overrides: Partial<Parameters<typeof PromptDialog>[0]> = {}) {
  const props = {
    title: 'Test-E-Mail senden',
    label: 'Empfängeradresse',
    placeholder: 'z. B. max@beispiel.de',
    submitLabel: 'Senden',
    cancelLabel: 'Abbrechen',
    emptyMessage: 'Bitte gib eine Empfängeradresse ein.',
    onSubmit: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  }
  render(<PromptDialog {...props} />)
  return props
}

describe('PromptDialog', () => {
  afterEach(() => {
    cleanup()
  })

  it('DIALOG: renders a labelled modal with the input, submit and cancel', () => {
    renderDialog()
    expect(screen.getByRole('dialog', { name: 'Test-E-Mail senden' })).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByLabelText('Empfängeradresse')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Senden' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Abbrechen' })).toBeInTheDocument()
  })

  it('FOCUS: the input receives focus on open', () => {
    renderDialog()
    expect(screen.getByLabelText('Empfängeradresse')).toHaveFocus()
  })

  it('SUBMIT: entering a value and clicking Senden calls onSubmit with the trimmed value', async () => {
    const props = renderDialog()
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Empfängeradresse'), '  ziel@example.com  ')
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(props.onSubmit).toHaveBeenCalledWith('ziel@example.com')
    expect(props.onClose).not.toHaveBeenCalled()
  })

  it('EMPTY: an empty value shows the inline error and never submits', async () => {
    const props = renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib eine Empfängeradresse ein.')
    expect(props.onSubmit).not.toHaveBeenCalled()
  })

  it('VALIDATE: a custom validator error is shown and nothing is submitted', async () => {
    const validate = vi.fn((value: string) => (value.includes('@') ? null : 'Bitte gib eine gültige Empfängeradresse an.'))
    const props = renderDialog({ validate })
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Empfängeradresse'), 'kein-at')
    await user.click(screen.getByRole('button', { name: 'Senden' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Bitte gib eine gültige Empfängeradresse an.')
    expect(props.onSubmit).not.toHaveBeenCalled()
  })

  it('ESCAPE: pressing Escape calls onClose', async () => {
    const props = renderDialog()
    const user = userEvent.setup()
    await user.keyboard('{Escape}')
    expect(props.onClose).toHaveBeenCalledTimes(1)
  })

  it('CANCEL: the Abbrechen button calls onClose', async () => {
    const props = renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Abbrechen' }))
    expect(props.onClose).toHaveBeenCalledTimes(1)
  })

  it('CLOSE: the close button calls onClose', async () => {
    const props = renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Schließen' }))
    expect(props.onClose).toHaveBeenCalledTimes(1)
  })

  it('BUSY: the form is disabled while the action is in flight', () => {
    renderDialog({ busy: true })
    expect(screen.getByLabelText('Empfängeradresse')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Senden' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Abbrechen' })).toBeDisabled()
  })
})