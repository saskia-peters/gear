import { useEffect, useState } from 'react'
import { ApiError, assignUserGroupRoles, listUserGroupRoles } from '../auth/users.ts'
import type { RoleGroupRef } from '../auth/users.ts'
import styles from './GroupRolesEditor.module.css'

interface GroupRolesEditorProps {
  groupId: string
  groupName: string
  /** The full role catalog to check against (permission groups). */
  roles: RoleGroupRef[]
  onSaved: (message: string) => void
  onForbidden: () => void
  /** Invoked when a 401 answers an in-page fetch (expired session → login). */
  onUnauthorized: () => void
  onCancel: () => void
}

type Feedback = { kind: 'error' | 'success'; message: string } | null

// GroupRolesEditor is the role-assignment editor for an organisational user
// group (Effort 2, Spec 2.9): a checkbox list of permission groups (roles),
// save replaces the group's role set atomically via
// GET/POST /user-groups/{id}/roles. Members inherit the new roles on the next
// resolution. Gated by user_groups.manage (the parent only renders it for
// holders). The server remains authoritative — this is only the editor surface.
export function GroupRolesEditor({ groupId, groupName, roles, onSaved, onForbidden, onUnauthorized, onCancel }: GroupRolesEditorProps) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(true)
  const [feedback, setFeedback] = useState<Feedback>(null)

  useEffect(() => {
    let cancelled = false
    listUserGroupRoles(groupId)
      .then((current) => {
        if (cancelled) return
        setSelected(new Set(current.map((r) => r.id)))
        setBusy(false)
      })
      .catch((err) => {
        if (cancelled) return
        setBusy(false)
        if (err instanceof ApiError && err.status === 403) {
          onForbidden()
          return
        }
        if (err instanceof ApiError && err.status === 401) {
          onUnauthorized()
          return
        }
        setFeedback({ kind: 'error', message: 'Die Rollen der Benutzergruppe konnten nicht geladen werden.' })
      })
    return () => {
      cancelled = true
    }
  }, [groupId, onForbidden, onUnauthorized])

  function toggle(roleId: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(roleId)) {
        next.delete(roleId)
      } else {
        next.add(roleId)
      }
      return next
    })
  }

  async function save() {
    setBusy(true)
    setFeedback(null)
    try {
      await assignUserGroupRoles(groupId, [...selected])
      onSaved(`Rollen von „${groupName}“ aktualisiert. Mitglieder erben sie ab der nächsten Anfrage.`)
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        onForbidden()
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setFeedback({ kind: 'error', message: err instanceof ApiError && err.message ? err.message : 'Die Rollen konnten nicht gespeichert werden.' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={styles.editor}>
      <h4 className={styles.title}>Rollen von „{groupName}“</h4>
      <p className={styles.hint}>
        Mitglieder der Benutzergruppe erben die zugewiesenen Rollen.
      </p>

      {feedback && (
        <p
          role={feedback.kind === 'success' ? 'status' : 'alert'}
          className={feedback.kind === 'success' ? styles.feedbackSuccess : styles.feedbackError}
        >
          {feedback.message}
        </p>
      )}

      {roles.length === 0 ? (
        <p className={styles.emptyNote}>Keine Rollen verfügbar.</p>
      ) : (
        <ul className={styles.roleList}>
          {roles.map((role) => (
            <li key={role.id} className={styles.roleRow}>
              <label className={styles.roleLabel}>
                <input
                  type="checkbox"
                  className={styles.checkbox}
                  checked={selected.has(role.id)}
                  onChange={() => toggle(role.id)}
                  disabled={busy}
                />
                <span className={styles.roleName}>
                  {role.name}
                  {role.is_base_role ? <span className={styles.baseTag}>Basis</span> : null}
                </span>
              </label>
            </li>
          ))}
        </ul>
      )}

      <div className={styles.actions}>
        <button type="button" className={styles.saveButton} onClick={() => void save()} disabled={busy}>
          {busy ? 'Wird gespeichert...' : 'Speichern'}
        </button>
        <button type="button" className={styles.cancelButton} onClick={onCancel} disabled={busy}>
          Abbrechen
        </button>
      </div>
    </div>
  )
}
