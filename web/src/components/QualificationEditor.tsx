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

// toDateInputValue converts an ISO timestamp (server) to a local YYYY-MM-DD
// value for the date picker.
function toDateInputValue(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// QualificationEditor is the create/edit form for a qualification (Story 2.7,
// UX-DR6/UX-DR8/UX-DR9). It captures a name, an optional description and the
// expiry model: either unbegrenzt (never expires, FR-22) or a fixed validity
// period with an expires_at date picker. Saving a new qualification POSTs;
// editing an existing one PUTs with the same body. Inline German feedback
// (validation, errors, success), ≥48px targets, keyboard/focus/SR
// (role="alert"/"status").
//
// The editor is mounted only when the caller may act on the qualification: both
// "Neue Qualifikation" and "Bearbeiten" require qualifications.manage (matching
// the server gate, AD-6). The server remains the source of truth.
export function QualificationEditor({ qualification, onSaved, onCancel, onForbidden }: QualificationEditorProps) {
  const isEdit = qualification !== null
  const [name, setName] = useState(qualification?.name ?? '')
  const [description, setDescription] = useState(qualification?.description ?? '')
  const [expiryKind, setExpiryKind] = useState<QualificationExpiryKind>(qualification?.expiry_kind ?? 'unlimited')
  const [expiresAt, setExpiresAt] = useState(() => toDateInputValue(qualification?.expires_at ?? null))
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  function submit() {
    const trimmed = name.trim()
    if (trimmed === '') {
      setFeedback({ kind: 'error', message: 'Bitte gib einen Namen für die Qualifikation an.' })
      return
    }
    if (expiryKind === 'fixed') {
      if (expiresAt === '') {
        setFeedback({ kind: 'error', message: 'Bitte wähle ein Ablaufdatum aus.' })
        return
      }
      // Finding 10: validate against END-of-day (T23:59:59 local) — the same
      // instant that is serialized for the server — so client and server agree
      // that a qualification expiring "today" is still valid.
      if (new Date(`${expiresAt}T23:59:59`).getTime() <= Date.now()) {
        setFeedback({ kind: 'error', message: 'Bitte wähle ein Ablaufdatum in der Zukunft.' })
        return
      }
    }

    setBusy(true)
    setFeedback(null)
    const input = {
      name: trimmed,
      description: description.trim(),
      expiry_kind: expiryKind,
      expires_at: expiryKind === 'fixed' ? new Date(`${expiresAt}T23:59:59`).toISOString() : null,
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

      <fieldset className={styles.expiryGroup}>
        <legend className={styles.label}>Gültigkeit</legend>
        <label className={styles.radioRow}>
          <input
            type="radio"
            name="expiry-kind"
            className={styles.radio}
            checked={expiryKind === 'unlimited'}
            onChange={() => {
              setExpiryKind('unlimited')
              setFeedback(null)
            }}
          />
          <span className={styles.radioLabel}>
            Unbegrenzt gültig
            <span className={styles.hint}>Läuft nie ab.</span>
          </span>
        </label>
        <label className={styles.radioRow}>
          <input
            type="radio"
            name="expiry-kind"
            className={styles.radio}
            checked={expiryKind === 'fixed'}
            onChange={() => {
              setExpiryKind('fixed')
              setFeedback(null)
            }}
          />
          <span className={styles.radioLabel}>Gültig bis</span>
        </label>
        {expiryKind === 'fixed' && (
          <div className={styles.field}>
            <label className={styles.label} htmlFor="qualification-expires-at">
              Ablaufdatum
            </label>
            <input
              id="qualification-expires-at"
              type="date"
              className={styles.input}
              value={expiresAt}
              onChange={(e) => {
                setExpiresAt(e.target.value)
                setFeedback(null)
              }}
            />
          </div>
        )}
      </fieldset>
    </form>
  )
}