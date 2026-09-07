import { useState } from 'react'
import { ApiError, assignQualificationUsers } from '../auth/qualifications.ts'
import type { Qualification, QualificationAssignee, QualificationAssignResult, QualificationRosterUser } from '../auth/qualifications.ts'
import styles from './AssigneeEditor.module.css'

interface AssigneeEditorProps {
  qualification: Qualification
  /** The full user roster (id + display name) from the list round-trip. */
  users: QualificationRosterUser[]
  /** The currently assigned volunteers (pre-checked set). */
  assignees: QualificationAssignee[]
  onSaved: (result: QualificationAssignResult) => void
  onCancel: () => void
  /** Invoked when a save answers 403 (revoked role): the parent clears the
      cached admin flag and leaves the admin module. */
  onForbidden: () => void
}

type Feedback =
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string }
  | null

// AssigneeEditor is the per-qualification volunteer assignment editor (Story
// 2.7, UX-DR6/UX-DR8/UX-DR9): a checkbox list of the user roster with the
// current assignees pre-checked. Saving REPLACES the assignee set atomically
// (delete-then-insert on the server); removing a volunteer revokes eligibility
// immediately (AD-7/FR-22). Inline German feedback (errors, success), ≥48px
// targets, keyboard/focus/SR (role="alert"/"status").
//
// The editor is mounted only when the caller may act on the qualification
// (qualifications.manage, matching the server gate, AD-6). The server remains
// the source of truth.
export function AssigneeEditor({ qualification, users, assignees, onSaved, onCancel, onForbidden }: AssigneeEditorProps) {
  const [checked, setChecked] = useState<Set<string>>(() => new Set(assignees.map((a) => a.id)))
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  // Finding 4: the checkbox list renders the roster fetched at page load, but
  // the checked set comes from the freshly-fetched assignees. An assignee
  // absent from the roster (e.g. a volunteer created after load and assigned
  // elsewhere) must still be VISIBLE so it can be seen and unchecked — merge
  // the roster with any assignees not present in it instead of silently hiding
  // them (or silently dropping them from the submitted set).
  const rosterIds = new Set(users.map((u) => u.id))
  const extraAssignees = assignees.filter((a) => !rosterIds.has(a.id))
  const rows = [
    ...users.map((u) => ({ id: u.id, name: u.name })),
    ...extraAssignees.map((a) => ({ id: a.id, name: a.name })),
  ]

  function toggle(id: string) {
    setChecked((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
    setFeedback(null)
  }

  function submit() {
    setBusy(true)
    setFeedback(null)
    void (async () => {
      try {
        const result = await assignQualificationUsers(qualification.id, [...checked])
        onSaved(result)
      } catch (err) {
        if (err instanceof ApiError && err.status === 403) {
          onForbidden()
          return
        }
        setFeedback({
          kind: 'error',
          message: err instanceof ApiError ? err.message : 'Die Zuweisung konnte nicht gespeichert werden.',
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
      <h3 className={styles.title}>Zugewiesen an „{qualification.name}“</h3>
      <p className={styles.description}>
        Wähle die Personen, die diese Qualifikation besitzen. Gespeicherte Änderungen gelten sofort.
      </p>

      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          className={feedback.kind === 'error' ? styles.feedbackError : styles.feedbackSuccess}
        >
          {feedback.message}
        </p>
      )}

      {rows.length === 0 ? (
        <div className={styles.empty}>
          <p>Es sind noch keine Personen vorhanden.</p>
        </div>
      ) : (
        <fieldset className={styles.assigneeGroup}>
          <legend className={styles.legend}>Zugewiesene Personen</legend>
          <ul className={styles.list}>
            {rows.map((row) => (
              <li key={row.id} className={styles.row}>
                <label className={styles.checkRow}>
                  <input
                    type="checkbox"
                    className={styles.checkbox}
                    checked={checked.has(row.id)}
                    onChange={() => toggle(row.id)}
                  />
                  <span className={styles.checkLabel}>{row.name}</span>
                </label>
              </li>
            ))}
          </ul>
        </fieldset>
      )}

      <div className={styles.actions}>
        <button type="submit" className={styles.saveButton} disabled={busy}>
          {busy ? 'Wird gespeichert...' : 'Speichern'}
        </button>
        <button type="button" className={styles.cancelButton} onClick={onCancel} disabled={busy}>
          Abbrechen
        </button>
      </div>
    </form>
  )
}