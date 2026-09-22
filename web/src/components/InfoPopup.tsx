import { useEffect, useId, useRef, useState } from 'react'
import styles from './InfoPopup.module.css'

export interface InfoPopupProps {
  /** Short German title rendered as the popover heading (e.g. the setting name). */
  title: string
  /** German description explaining the setting (read-only help). */
  description: string
}

// InfoPopup is a lightweight accessible popover (Story 5-2b): a "?" trigger
// button toggles a read-only help popup. It closes on Escape, on an
// outside-pointerdown, on the close button, or by toggling the trigger again.
// A11y contract: `aria-expanded` on the trigger, `aria-controls` pointing at
// the popover, the popover uses `role="dialog"` with a labelled heading and an
// `aria-describedby` description. Focus moves into the popover (close button)
// on open and returns to the trigger on close. Net-new — no Modal/Tooltip
// component existed to reuse.
export function InfoPopup({ title, description }: InfoPopupProps) {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const dialogId = useId()
  const titleId = useId()
  const descId = useId()
  // Tracks whether the popup has ever been opened, so the close branch only
  // returns focus to the trigger AFTER an open — never on initial mount (21
  // popups mount together on the System tab; a mount-time focus() would hijack
  // the page focus).
  const wasOpened = useRef(false)

  useEffect(() => {
    if (!open) return

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    function onPointerDown(e: PointerEvent) {
      const t = e.target as Node
      if (
        popoverRef.current &&
        !popoverRef.current.contains(t) &&
        triggerRef.current &&
        !triggerRef.current.contains(t)
      ) {
        setOpen(false)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    document.addEventListener('pointerdown', onPointerDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.removeEventListener('pointerdown', onPointerDown)
    }
  }, [open])

  useEffect(() => {
    if (open) {
      // Focus the close button on open so keyboard users land inside the
      // dialog; Escape/outside/close then dismiss it.
      wasOpened.current = true
      popoverRef.current?.querySelector<HTMLButtonElement>('button')?.focus()
    } else if (wasOpened.current) {
      triggerRef.current?.focus()
    }
  }, [open])

  // Viewport flip: the popover is anchored left by default; when it would
  // overflow the right edge of the viewport (e.g. a "?" in the last table
  // column) align it to the trigger's right edge instead.
  useEffect(() => {
    if (!open) return
    const el = popoverRef.current
    const trigger = triggerRef.current
    if (!el || !trigger) return
    const triggerRect = trigger.getBoundingClientRect()
    const rect = el.getBoundingClientRect()
    if (triggerRect.right + rect.width > window.innerWidth) {
      el.style.left = 'auto'
      el.style.right = '0'
    } else {
      el.style.left = '0'
      el.style.right = 'auto'
    }
  }, [open])

  return (
    <span className={styles.wrap}>
      <button
        ref={triggerRef}
        type="button"
        className={styles.trigger}
        aria-expanded={open}
        aria-controls={dialogId}
        aria-label={`Erklärung zu ${title}`}
        onClick={() => setOpen((v) => !v)}
      >
        ?
      </button>
      {open && (
        <div ref={popoverRef} id={dialogId} role="dialog" aria-labelledby={titleId} aria-describedby={descId} className={styles.popover}>
          <div className={styles.header}>
            <h3 id={titleId} className={styles.title}>
              {title}
            </h3>
            <button type="button" className={styles.close} aria-label="Schließen" onClick={() => setOpen(false)}>
              ×
            </button>
          </div>
          <p id={descId} className={styles.description}>
            {description}
          </p>
        </div>
      )}
    </span>
  )
}