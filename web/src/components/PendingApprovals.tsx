import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { adminForbiddenHandled, authHeaders } from '../auth/authState.ts'
import styles from './PendingApprovals.module.css'

interface PendingUser {
  id: string
  vorname: string
  nachname: string
  email: string
  created_at?: string
}

type Feedback =
  | { kind: 'success'; message: string }
  | { kind: 'error'; message: string }
  | null

// The server returns the confirmation message in the approve/reject response
// body (core.MsgUserApproved / core.MsgUserRejected). We use it verbatim so
// microcopy lives in exactly one place; these strings are only a fallback if
// the response omits it.
const DEFAULT_APPROVE_MESSAGE = 'Antrag freigegeben.'
const DEFAULT_REJECT_MESSAGE = 'Antrag abgelehnt.'

// PendingApprovals is the live "Ausstehende Anträge" widget on the Verwaltung
// landing (Story 2.4, FR-20/UX-DR6/UX-DR8). It fetches the pending-approval
// list from the admin API and renders each request as a row (Vorname,
// Nachname, E-Mail) with "Freigeben" / "Ablehnen" CTAs, plus German inline
// feedback: a loading skeleton while fetching, the "Keine ausstehenden
// Anträge" empty state, and success/error announcements. It only mounts when
// the caller holds users.approve (AdminPage gating, matching the server's
// /users/* gate); the server is the source of truth for authorization (AD-6).
//
// The destructive reject is guarded by a lightweight inline two-step confirm:
// clicking "Ablehnen" swaps the row's actions to "Wirklich ablehnen?" with a
// danger "Ja, ablehnen" button and an "Abbrechen"; only the confirm button
// posts the reject.
//
// Accessibility floor (EXPERIENCE.md): every interactive control is ≥48px
// tall, fully keyboard-operable, in a sensible focus order (person info, then
// actions), and feedback is announced to screen readers — a visually-hidden
// live region announces load completion (list ready or empty), success via
// role="status", errors via role="alert".
export function PendingApprovals() {
  const navigate = useNavigate()
  const [users, setUsers] = useState<PendingUser[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [busyId, setBusyId] = useState<string | null>(null)
  const [confirmId, setConfirmId] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [loadAnnouncement, setLoadAnnouncement] = useState('')

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const res = await fetch('/api/v1/admin/users/pending', { headers: authHeaders() })
        if (cancelled) return
        // Revocation downgrade (review finding 2.1-6): a 403 means the admin
        // role is gone — drop the cached admin flag and leave the admin module.
        if (adminForbiddenHandled(res)) {
          navigate('/')
          return
        }
        if (res.ok) {
          const data = await res.json().catch(() => null)
          const list = Array.isArray(data?.users) ? data.users : []
          setUsers(list)
          setLoadAnnouncement(list.length === 0 ? 'Keine ausstehenden Anträge.' : 'Ausstehende Anträge geladen.')
        } else {
          setLoadError('Ausstehende Anträge konnten nicht geladen werden.')
          setLoadAnnouncement('Ausstehende Anträge konnten nicht geladen werden.')
        }
      } catch {
        if (!cancelled) {
          setLoadError('Ausstehende Anträge konnten nicht geladen werden.')
          setLoadAnnouncement('Ausstehende Anträge konnten nicht geladen werden.')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [navigate])

  async function act(id: string, action: 'approve' | 'reject') {
    setBusyId(id)
    setFeedback(null)
    try {
      const res = await fetch(`/api/v1/admin/users/${id}/${action}`, {
        method: 'POST',
        headers: authHeaders(),
      })
      const data = await res.json().catch(() => null)
      if (adminForbiddenHandled(res)) {
        navigate('/')
        return
      }
      if (res.ok) {
        setUsers((prev) => prev.filter((u) => u.id !== id))
        setConfirmId(null)
        // Use the server's confirmation message (single source of truth for
        // microcopy); fall back to a generic German string only if absent.
        const serverMessage =
          typeof data?.message === 'string' && data.message !== ''
            ? data.message
            : action === 'approve'
              ? DEFAULT_APPROVE_MESSAGE
              : DEFAULT_REJECT_MESSAGE
        setFeedback({ kind: 'success', message: serverMessage })
      } else {
        setFeedback({
          kind: 'error',
          message: data?.error?.message || 'Aktion fehlgeschlagen. Bitte versuche es erneut.',
        })
      }
    } catch {
      setFeedback({
        kind: 'error',
        message: 'Verbindung zum Server fehlgeschlagen. Bitte prüfe deine Internetverbindung.',
      })
    } finally {
      setBusyId(null)
    }
  }

  const startReject = (id: string) => {
    setConfirmId(id)
    setFeedback(null)
  }

  const cancelReject = () => setConfirmId(null)

  return (
    <div className={styles.widget}>
      {/* Visually-hidden live region (a11y): announces when loading completes,
          whether the list is ready or empty (the skeleton sets aria-busy but
          provides no completion announcement). */}
      <p className={styles.visuallyHidden} role="status" aria-live="polite">
        {loadAnnouncement}
      </p>

      {feedback && (
        <p
          role={feedback.kind === 'error' ? 'alert' : 'status'}
          className={feedback.kind === 'error' ? styles.feedbackError : styles.feedbackSuccess}
        >
          {feedback.message}
        </p>
      )}

      {loadError && (
        <p role="alert" className={styles.feedbackError}>
          {loadError}
        </p>
      )}

      {loading ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="Ausstehende Anträge werden geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : loadError ? null : users.length === 0 ? (
        <div className={styles.empty}>
          <h3 className={styles.emptyTitle}>Keine ausstehenden Anträge</h3>
          <p className={styles.emptyDescription}>Neue Freigaben von Mitgliedern erscheinen hier.</p>
        </div>
      ) : (
        <ul className={styles.list}>
          {users.map((user) => (
            <li key={user.id} className={styles.row}>
              <div className={styles.person}>
                <span className={styles.name}>
                  {user.vorname} {user.nachname}
                </span>
                <span className={styles.email}>{user.email}</span>
              </div>
              {confirmId === user.id ? (
                <div className={styles.actions}>
                  <span className={styles.confirmPrompt}>Wirklich ablehnen?</span>
                  <div className={styles.confirmActions}>
                    <button
                      type="button"
                      className={styles.confirmRejectButton}
                      onClick={() => act(user.id, 'reject')}
                      disabled={busyId !== null}
                    >
                      {busyId === user.id ? 'Wird abgelehnt...' : 'Ja, ablehnen'}
                    </button>
                    <button
                      type="button"
                      className={styles.cancelButton}
                      onClick={cancelReject}
                      disabled={busyId !== null}
                    >
                      Abbrechen
                    </button>
                  </div>
                </div>
              ) : (
                <div className={styles.actions}>
                  <button
                    type="button"
                    className={styles.approveButton}
                    onClick={() => act(user.id, 'approve')}
                    disabled={busyId !== null}
                  >
                    {busyId === user.id ? 'Wird freigegeben...' : 'Freigeben'}
                  </button>
                  <button
                    type="button"
                    className={styles.rejectButton}
                    onClick={() => startReject(user.id)}
                    disabled={busyId !== null}
                  >
                    Ablehnen
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}