import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { QualificationEditor } from '../../components/QualificationEditor.tsx'
import { AssigneeEditor } from '../../components/AssigneeEditor.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions, hasPermission } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listQualifications, listQualificationAssignees, qualificationStatusLabel } from '../../auth/qualifications.ts'
import type { Qualification, QualificationAssignee, QualificationAssignResult, QualificationRosterUser } from '../../auth/qualifications.ts'
import styles from './AdminQualifikationenPage.module.css'

// AdminQualifikationenPage is the real "Qualifikationen" surface (Story 2.7,
// UX-DR6/UX-DR8/UX-DR9): a qualification list with per-qualification status
// badges (Unbegrenzt / Gültig / Bald ablaufend / Abgelaufen, FR-22/AD-7), a
// "Neue Qualifikation" action, a create/edit form (name, description,
// expiry-kind radio, expires_at date picker) and a per-qualification assignee
// editor (checkbox list of volunteers). Assigning/removing takes effect
// immediately on the next check because resolution is live (AD-7/FR-22).
//
// The whole surface is gated client-side on `qualifications.manage` (matching
// the server gate, AD-6); the server remains the source of truth. A 403
// (revoked role) clears the cached admin flag and leaves the admin module
// (review finding 2.1-6).
export function AdminQualifikationenPage() {
  const navigate = useNavigate()
  const [qualifications, setQualifications] = useState<Qualification[]>([])
  const [users, setUsers] = useState<QualificationRosterUser[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [feedback, setFeedback] = useState('')
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingQualification, setEditingQualification] = useState<Qualification | null>(null)
  const [assigneeEditor, setAssigneeEditor] = useState<{ qualification: Qualification; assignees: QualificationAssignee[] } | null>(null)

  const canManage = hasPermission('qualifications.manage')

  // handleLoadError inspects an API error from a load/assignee fetch (findings
  // 3 & 11): a 403 clears the cached admin flag and leaves the admin module
  // (review finding 2.1-6); a 401 (expired/revoked session) clears the client
  // auth state and redirects to /login instead of surfacing a generic error.
  // Returns true when the error was handled (redirect), false otherwise.
  const handleLoadError = useCallback((err: unknown): boolean => {
    const status = err instanceof Error && 'status' in err ? (err as { status: number }).status : 0
    if (status === 403) {
      adminForbiddenHandled({ status: 403 })
      navigate('/')
      return true
    }
    if (status === 401) {
      clearAuthState()
      navigate('/login', { replace: true })
      return true
    }
    return false
  }, [navigate])

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const data = await listQualifications()
        if (cancelled) return
        setQualifications(data.qualifications)
        setUsers(data.users)
      } catch (err) {
        if (cancelled) return
        if (!handleLoadError(err)) {
          setLoadError('Qualifikationen konnten nicht geladen werden.')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [navigate, handleLoadError])

  async function load() {
    // Finding 3: clear a stale load error (and re-arm the loading state) at the
    // start of every load so a failed load followed by a successful reload no
    // longer keeps showing the old error banner.
    setLoadError('')
    setLoading(true)
    try {
      const data = await listQualifications()
      setQualifications(data.qualifications)
      setUsers(data.users)
    } catch (err) {
      if (!handleLoadError(err)) {
        setLoadError('Qualifikationen konnten nicht geladen werden.')
      }
    } finally {
      setLoading(false)
    }
  }

  function openCreate() {
    setEditingQualification(null)
    setEditorOpen(true)
    setFeedback('')
  }

  function openEdit(qualification: Qualification) {
    setEditingQualification(qualification)
    setEditorOpen(true)
    setFeedback('')
  }

  function closeEditor() {
    setEditorOpen(false)
    setEditingQualification(null)
  }

  // Server-side revocation downgrade (review finding 2.1-6): a 403 from a save
  // means the caller's role was revoked — drop the cached admin flag and leave
  // the admin module, exactly like the list-load 403 path.
  function handleForbidden() {
    adminForbiddenHandled({ status: 403 })
    navigate('/')
  }

  async function handleSaved(_saved: Qualification, message: string) {
    setFeedback(message)
    setEditorOpen(false)
    setEditingQualification(null)
    await load()
  }

  async function openAssignees(qualification: Qualification) {
    try {
      const assignees = await listQualificationAssignees(qualification.id)
      setAssigneeEditor({ qualification, assignees })
      setFeedback('')
    } catch (err) {
      if (!handleLoadError(err)) {
        setLoadError('Zugewiesene Personen konnten nicht geladen werden.')
      }
    }
  }

  async function handleAssigneesSaved(_result: QualificationAssignResult) {
    setFeedback(_result.message)
    setAssigneeEditor(null)
    await load()
  }

  function closeAssignees() {
    setAssigneeEditor(null)
  }

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(getPermissions())} />
        <main className={styles.main}>
          <h2 className={styles.title}>Qualifikationen</h2>
          <p className={styles.description}>
            Qualifikationen pflegen und Personen zuweisen. Änderungen gelten sofort.
          </p>

          {feedback && (
            <p role="status" className={styles.feedbackSuccess}>
              {feedback}
            </p>
          )}
          {loadError && (
            <p role="alert" className={styles.feedbackError}>
              {loadError}
            </p>
          )}

          {editorOpen ? (
            <QualificationEditor
              key={editingQualification?.id ?? 'new'}
              qualification={editingQualification}
              onSaved={handleSaved}
              onCancel={closeEditor}
              onForbidden={handleForbidden}
            />
          ) : assigneeEditor ? (
            <AssigneeEditor
              key={assigneeEditor.qualification.id}
              qualification={assigneeEditor.qualification}
              users={users}
              assignees={assigneeEditor.assignees}
              onSaved={handleAssigneesSaved}
              onCancel={closeAssignees}
              onForbidden={handleForbidden}
            />
          ) : loading ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Qualifikationen werden geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : (
            <>
              <div className={styles.toolbar}>
                {canManage && (
                  <button type="button" className={styles.newButton} onClick={openCreate}>
                    Neue Qualifikation
                  </button>
                )}
              </div>

              {qualifications.length === 0 ? (
                <div className={styles.empty}>
                  <p>Noch keine Qualifikationen vorhanden.</p>
                </div>
              ) : (
                <ul className={styles.list}>
                  {qualifications.map((qualification) => (
                    <li key={qualification.id} className={styles.row}>
                      <div className={styles.qualInfo}>
                        <div className={styles.qualNameLine}>
                          <span className={styles.qualName}>{qualification.name}</span>
                          <span className={`${styles.qualStatus} ${styles[`qual-${qualification.status}`]}`}>
                            {qualificationStatusLabel(qualification.status)}
                          </span>
                        </div>
                        {qualification.description !== '' && (
                          <span className={styles.qualDescription}>{qualification.description}</span>
                        )}
                        {qualification.expiry_kind === 'fixed' && qualification.expires_at && (
                          <span className={styles.qualExpiry}>
                            Gültig bis {new Date(qualification.expires_at).toLocaleDateString('de-DE')}
                          </span>
                        )}
                      </div>
                      <div className={styles.rowActions}>
                        {canManage && (
                          <button
                            type="button"
                            className={styles.editButton}
                            onClick={() => openEdit(qualification)}
                          >
                            Bearbeiten
                          </button>
                        )}
                        {canManage && (
                          <button
                            type="button"
                            className={styles.assignButton}
                            onClick={() => void openAssignees(qualification)}
                          >
                            Zuweisung
                          </button>
                        )}
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </main>
      </div>
    </div>
  )
}