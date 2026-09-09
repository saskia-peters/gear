import { useState } from 'react'
import { ApiError, createQualification, updateQualification } from '../auth/qualifications.ts'
import type { Qualification, QualificationExpiryKind } from '../auth/qualifications.ts'
import styles from './QualificationEditor.module.css'

interface QualificationEditorProps {
  qualification: Qualification | null
  onSaved: (qualification: Qualification, message: string) => void
  onCancel: () => void
  /** Invoked when a create/update save answers 403 (revoked role): the parent
      clears the cached admin flag and leaves the admin module. */
  onForbidden: () => void
}

type Feedback =
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string }
  | null

// QualificationEditor is the create/edit form for a qualification (Story 2.7,
// UX-DR6/UX-DR8/UX-DR9). It captures a name, an optional description and the
// expiry model via a single checkbox: "Unbegrenzt gültig" (never expires,
// FR-22). A qualification itself has NO valid-until date (2026-09-08 rework) —
// for a non-unlimited (fixed) qualification the per-user valid-until is set
// when it is assigned to a user (Spec 2.9), not here. Saving a new
// qualification POSTs; editing an existing one PUTs with the same body. Inline
// German feedback (validation, errors, success), ≥48px targets,
// keyboard/focus/SR (role="alert"/"status").
//
// The editor is mounted only when the caller may act on the qualification: both
// "Neue Qualifikation" and "Bearbeiten" require qualifications.manage (matching
// the server gate, AD-6). The server remains the source of truth.
export function QualificationEditor({ qualification, onSaved, onCancel, onForbidden }: QualificationEditorProps) {
  const isEdit = qualification !== null
  const [name, setName] = useState(qualification?.name ?? '')
  const [description, setDescription] = useState(qualification?.description ?? '')
  const [unlimited, setUnlimited] = useState(qualification ? qualification.expiry_kind === 'unlimited' : true)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  function submit() {
    const trimmed = name.trim()
    if (trimmed === '') {
      setFeedback({ kind: 'error', message: 'Bitte gib einen Namen für die Qualifikation an.' })
      return
    }

    setBusy(true)
    setFeedback(null)
    const expiryKind: QualificationExpiryKind = unlimited ? 'unlimited' : 'fixed'
    const input = {
      name: trimmed,
      description: description.trim(),
      expiry_kind: expiryKind,
    }
    void (async () => {
      try {
        const saved = isEdit
          ? await updateQualification(qualification!.id, input)
          : await createQualification(input)
        // Finding 5: the success microcopy comes from the server
        // (server-authoritative), with a generic fallback only if absent.
        onSaved(saved.qualification, saved.message || (isEdit ? 'Qualifikation gespeichert.' : 'Qualifikation erstellt.'))
      } catch (err) {
        if (err instanceof ApiError && err.status === 403) {
          // Server-side revocation downgrade: the parent clears the cached
          // admin flag and navigates away (review finding 2.1-6).
          onForbidden()
          return
        }
        setFeedback({
          kind: 'error',
          message: err instanceof ApiError ? err.message : 'Die Qualifikation konnte nicht gespeichert werden.',
        })
      } finally {
        setBusy(false)
      }
    })()
  }

  return (
    <form
      className={styles.editor}
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <h3 className={styles.title}>
        {isEdit ? `Qualifikation „${qualification!.name}“ bearbeiten` : 'Neue Qualifikation'}
      </h3>

      {/* Sticky action bar (Effort 2): always visible at the top. */}
      <div className={styles.stickyActions}>
        <button type="submit" className={styles.saveButton} disabled={busy}>
          {busy ? 'Wird gespeichert...' : 'Speichern'}
        </button>
        <button type="button" className={styles.cancelButton} onClick={onCancel} disabled={busy}>
          Abbrechen
        </button>
      </div>

      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          className={feedback.kind === 'error' ? styles.feedbackError : styles.feedbackSuccess}
        >
          {feedback.message}
        </p>
      )}

      <div className={styles.field}>
        <label className={styles.label} htmlFor="qualification-name">
          Name
        </label>
        <input
          id="qualification-name"
          className={styles.input}
          value={name}
          onChange={(e) => {
            setName(e.target.value)
            setFeedback(null)
          }}
          maxLength={120}
          autoFocus={!isEdit}
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label} htmlFor="qualification-description">
          Beschreibung
        </label>
        <input
          id="qualification-description"
          className={styles.input}
          value={description}
          onChange={(e) => {
            setDescription(e.target.value)
            setFeedback(null)
          }}
        />
      </div>

      <div className={styles.expiryGroup}>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            className={styles.checkbox}
            checked={unlimited}
            onChange={(e) => {
              setUnlimited(e.target.checked)
              setFeedback(null)
            }}
          />
          <span className={styles.checkLabel}>
            Unbegrenzt gültig
            <span className={styles.hint}>
              Läuft nie ab. Ohne diese Markierung wird das Ablaufdatum bei der Zuweisung an einen Benutzer festgelegt.
            </span>
          </span>
        </label>
      </div>
    </form>
  )
}