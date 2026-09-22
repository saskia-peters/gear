// @vitest-environment jsdom
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, afterEach } from 'vitest'
import { InfoPopup } from './InfoPopup.tsx'

function renderPopup() {
  return render(<InfoPopup title="OTP-Länge" description="Anzahl der Zeichen eines Einmalpassworts." />)
}

describe('InfoPopup', () => {
  afterEach(() => {
    cleanup()
  })

  it('TRIGGER: the "?" button is closed by default with aria-expanded=false', () => {
    renderPopup()
    const trigger = screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' })
    expect(trigger).toHaveTextContent('?')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('OPEN: clicking the trigger opens a labelled dialog with the description', async () => {
    const user = userEvent.setup()
    renderPopup()
    await user.click(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' }))

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAccessibleName('OTP-Länge')
    expect(dialog).toHaveAccessibleDescription('Anzahl der Zeichen eines Einmalpassworts.')
    expect(screen.getByRole('heading', { name: 'OTP-Länge' })).toBeInTheDocument()
    expect(screen.getByText('Anzahl der Zeichen eines Einmalpassworts.')).toBeInTheDocument()
    // aria-expanded flips to true and aria-controls points at the dialog.
    const trigger = screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' })
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(trigger.getAttribute('aria-controls')).toBe(dialog.id)
  })

  it('CLOSE_BUTTON: the close button dismisses the dialog and restores aria-expanded=false', async () => {
    const user = userEvent.setup()
    renderPopup()
    await user.click(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Schließen' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('ESCAPE: pressing Escape closes the dialog', async () => {
    const user = userEvent.setup()
    renderPopup()
    await user.click(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('OUTSIDE_CLICK: a pointer-down outside the popover closes it', async () => {
    const user = userEvent.setup()
    renderPopup()
    await user.click(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await user.pointer({ keys: '[MouseLeft]', target: document.body })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('TOGGLE: clicking the trigger again closes the dialog', async () => {
    const user = userEvent.setup()
    renderPopup()
    const trigger = screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' })
    await user.click(trigger)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(trigger)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('NO_MOUNT_FOCUS: mounting does NOT steal focus (21 popups mount together on the System tab)', () => {
    renderPopup()
    expect(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' })).not.toHaveFocus()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('FOCUS_OPEN: opening moves focus into the dialog (the close button)', async () => {
    const user = userEvent.setup()
    renderPopup()
    await user.click(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' }))

    expect(screen.getByRole('button', { name: 'Schließen' })).toHaveFocus()
  })

  it('FOCUS_CLOSE: dismissing returns focus to the trigger', async () => {
    const user = userEvent.setup()
    renderPopup()
    await user.click(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' }))
    expect(screen.getByRole('button', { name: 'Schließen' })).toHaveFocus()

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Erklärung zu OTP-Länge' })).toHaveFocus()
  })
})