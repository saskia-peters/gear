import { useEffect, useId, useRef, useState } from 'react'
import styles from './PromptDialog.module.css'

export interface PromptDialogProps {
  /** Heading shown in the dialog. */
  title: string
  /** German label of the text input. */
  label: string
  /** Placeholder of the text input (optional). */
  placeholder?: string
  /** Submit-button label. */
  submitLabel: string
  /** Cancel-button label. */
  cancelLabel: string
  /** Error shown when the trimmed value is empty. */
  emptyMessage: string
  /** Optional semantic validator; returns an error message or null. */
  validate?: (value: string) => string | null
  /** Called with the trimmed value when the admin confirms. */
  onSubmit: (value: string) => void
  /** Called on dismiss (close button, Escape, cancel). */
  onClose: () => void
  /** True while the action is in flight — disables the form. */
  busy?: boolean
}

// PromptDialog is a lightweight accessible single-input modal (first use: the
// SMTP test-recipient prompt). Net-new — no Modal component existed to reuse.
// A11y contract: role="dialog" + aria-modal with a labelled input, submit /
// cancel, inline error, focus into the input on open and back to the opener on
// close, Escape to dismiss. Closes on the close button, Escape, or cancel.
export function PromptDialog({
  title,
  label,
  placeholder = '',
  submitLabel,
  cancelLabel,
  emptyMessage,
  validate,
  onSubmit,
  onClose,
  busy = false,
}: PromptDialogProps) {
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const titleId = useId()
  const inputId = useId()

  // Focus the input on mount; return focus to the element that opened the
  // dialog on unmount.
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    inputRef.current?.focus()
    return () => {
      previous?.focus?.()
    }
  }, [])

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  function submit() {
    const trimmed = value.trim()
    if (trimmed === '') {
      setError(emptyMessage)
      return
    }
    const customError = validate?.(trimmed) ?? null
    if (customError) {
      setError(customError)
      return
    }
    onSubmit(trimmed)
  }

  return (
    <div className={styles.overlay}>
      <div className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <div className={styles.header}>
          <h3 id={titleId} className={styles.title}>
            {title}
          </h3>
          <button type="button" className={styles.close} aria-label="Schließen" onClick={onClose}>
            ×
          </button>
        </div>
        <form
          className={styles.form}
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <div className={styles.field}>
            <label className={styles.label} htmlFor={inputId}>
              {label}
            </label>
            <input
              id={inputId}
              ref={inputRef}
              className={styles.input}
              type="text"
              inputMode="email"
              autoComplete="off"
              value={value}
              placeholder={placeholder}
              disabled={busy}
              onChange={(e) => {
                setValue(e.target.value)
                setError('')
              }}
            />
          </div>
          {error && (
            <p role="alert" className={styles.error}>
              {error}
            </p>
          )}
          <div className={styles.actions}>
            <button type="submit" className={styles.submit} disabled={busy}>
              {submitLabel}
            </button>
            <button type="button" className={styles.cancel} disabled={busy} onClick={onClose}>
              {cancelLabel}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}