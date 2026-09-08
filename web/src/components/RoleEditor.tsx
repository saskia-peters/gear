import { useState } from 'react'
import { ApiError, createRole, updateRole } from '../auth/roles.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../auth/roles.ts'
import styles from './RoleEditor.module.css'

interface RoleEditorProps {
  role: RoleGroup | null
  availablePermissions: PermissionCatalogEntry[]
  onSaved: (role: RoleGroup, message: string) => void
  onCancel: () => void
  /** Invoked when a create/update save answers 403 (revoked role): the parent
      clears the cached admin flag and leaves the admin module. */
  onForbidden: () => void
}

type Feedback =
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string }
  | null

// RoleEditor is the create/edit form for a permission group (Story 2.5,
// UX-DR6/UX-DR8/UX-DR9). It captures a name, an optional description and an
// ADDITIVE checkbox grid over the 22 base codes (checked = granted; no deny
// permissions, FR-6/AD-12). Saving a new role POSTs; editing an existing role
// PUTs with the same body. Inline German feedback (validation, errors,
// success), ≥48px targets, keyboard/focus/SR (role="alert"/"status").
//
// The editor is mounted only when the caller may act on the role: "Neue Rolle"
// requires roles.create, "Bearbeiten" requires roles.edit (matching the server
// gate, AD-6). The server remains the source of truth.
export function RoleEditor({ role, availablePermissions, onSaved, onCancel, onForbidden }: RoleEditorProps) {
  const isEdit = role !== null
  const [name, setName] = useState(role?.name ?? '')
  const [description, setDescription] = useState(role?.description ?? '')
  const [checked, setChecked] = useState<Set<string>>(
    () => new Set(role?.permissions ?? []),
  )
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)

  function toggle(code: string) {
    setChecked((prev) => {
      const next = new Set(prev)
      if (next.has(code)) {
        next.delete(code)
      } else {
        next.add(code)
      }
      return next
    })
    setFeedback(null)
  }

  async function submit() {
    const trimmed = name.trim()
    if (trimmed === '') {
      setFeedback({ kind: 'error', message: 'Bitte gib einen Namen für die Rolle an.' })
      return
    }
    setBusy(true)
    setFeedback(null)
    const input = {
      name: trimmed,
      description: description.trim(),
      permissions: [...checked],
    }
    try {
      const saved = isEdit
        ? await updateRole(role!.id, input)
        : await createRole(input)
      const message = isEdit ? 'Rolle gespeichert.' : 'Rolle erstellt.'
      onSaved(saved, message)
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        // Server-side revocation downgrade: the parent clears the cached admin
        // flag and navigates away (review finding 2.1-6).
        onForbidden()
        return
      }
      setFeedback({
        kind: 'error',
        message: err instanceof ApiError ? err.message : 'Die Rolle konnte nicht gespeichert werden.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className={styles.editor}
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <h3 className={styles.title}>{isEdit ? `Rolle „${role!.name}“ bearbeiten` : 'Neue Rolle'}</h3>

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
        <label className={styles.label} htmlFor="role-name">
          Name
        </label>
        <input
          id="role-name"
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
        <label className={styles.label} htmlFor="role-description">
          Beschreibung
        </label>
        <input
          id="role-description"
          className={styles.input}
          value={description}
          onChange={(e) => {
            setDescription(e.target.value)
            setFeedback(null)
          }}
        />
      </div>

      <fieldset className={styles.permsGroup}>
        <legend className={styles.label}>
          Berechtigungen
          <span className={styles.hint}>Angekreuzte Berechtigungen werden vergeben.</span>
        </legend>
        <div className={styles.grid}>
          {availablePermissions.map((perm) => (
            <label key={perm.code} className={styles.checkRow}>
              <input
                type="checkbox"
                className={styles.checkbox}
                checked={checked.has(perm.code)}
                onChange={() => toggle(perm.code)}
              />
              <span className={styles.checkLabel}>{perm.label}</span>
            </label>
          ))}
        </div>
      </fieldset>
    </form>
  )
}
