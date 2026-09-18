import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { deleteUserAccount, getDsgvoReport, listDeletedAccounts, listUsers, purgeDeletedAccount } from '../../auth/users.ts'
import type { AccessReport, AdminUserSummary, DeletedAccountRow } from '../../auth/users.ts'
import styles from './AdminDsgvoPage.module.css'

// DSGVO permission codes (AD-6): the tabs are gated per code — Datenauskunft
// by dsgvo.access_report, Konto löschen by dsgvo.delete. The server is the
// gate; these only drive client-side visibility.
const DSGVO_ACCESS_REPORT_PERMISSION = 'dsgvo.access_report'
const DSGVO_DELETE_PERMISSION = 'dsgvo.delete'

type Tab = 'report' | 'delete'
type Feedback = { kind: 'success' | 'error'; message: string } | null

// formatDate renders an RFC3339 timestamp as a German date + time; an empty or
// invalid value renders "–" so a missing field never shows "Invalid Date".
function formatDate(iso?: string | null): string {
  if (!iso) return '–'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '–'
  return d.toLocaleString('de-DE')
}

// displayNameOf renders the picker/header label of a user (Vorname Nachname,
// falling back to the email), never "undefined".
function displayNameOf(user: AdminUserSummary | undefined, fallback: string): string {
  if (!user) return fallback
  return `${user.vorname} ${user.nachname}`.trim() || user.email
}

// downloadReport triggers a client-side JSON download of the loaded report (the
// exact payload that was rendered — no second server call, no second audit).
// The server's attachment Content-Disposition serves direct browser visits the
// same artifact.
function downloadReport(report: AccessReport, userName: string) {
  const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `dsgvo-datenauskunft-${encodeURIComponent(userName)}.json`
  a.click()
  URL.revokeObjectURL(url)
}

// AdminDsgvoPage is the DSGVO surface (Story 3.3 + 3.4, FR-24/AD-6): a tab bar
// over "Datenauskunft" and "Konto löschen". Each tab is gated by its OWN
// permission code (AD-6) — Datenauskunft by dsgvo.access_report, Konto löschen
// by dsgvo.delete. The report tab lets a holder pick a user, generate the
// data-access report (the server orchestrates the User + Tool module exports)
// and renders it read-only (profile, roles/groups/grants/qualifications, auth
// history incl. sessions + login-attempt state, inspection summary +
// per-inspection rows) with a JSON download. The delete tab (Story 3.4) is the
// heavy two-step account-deletion flow plus the "Gelöschte Konten" on-demand
// purge list. Inline German feedback, skeleton loading, 401 → login, 403 →
// leave the admin module (the server remains the source of truth).
export function AdminDsgvoPage() {
  const navigate = useNavigate()
  const perms = getPermissions()
  const canReport = perms.includes(DSGVO_ACCESS_REPORT_PERMISSION)
  const canDelete = perms.includes(DSGVO_DELETE_PERMISSION)
  const [activeTab, setActiveTab] = useState<Tab>(() => (canReport ? 'report' : 'delete'))

  const handleApiError = useCallback(
    (err: unknown): boolean => {
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
    },
    [navigate],
  )

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(perms)} />
        <main className={styles.main}>
          <h2 className={styles.title}>DSGVO</h2>
          <p className={styles.description}>Datenauskünfte und Löschungen nach Datenschutz.</p>

          <div className={styles.tabs} role="tablist" aria-label="DSGVO">
            {canReport && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'report'}
                className={activeTab === 'report' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('report')}
              >
                Datenauskunft
              </button>
            )}
            {canDelete && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'delete'}
                className={activeTab === 'delete' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('delete')}
              >
                Konto löschen
              </button>
            )}
          </div>

          {activeTab === 'report' && canReport ? (
            <ReportTab onApiError={handleApiError} />
          ) : activeTab === 'delete' && canDelete ? (
            <DeleteTab onApiError={handleApiError} />
          ) : (
            // Blank-page fallback (only reachable if the route gating drifts):
            // a caller holding NEITHER DSGVO code still gets a German notice.
            <p role="status" className={styles.emptyHint}>
              Keine Berechtigung für die DSGVO-Funktionen.
            </p>
          )}
        </main>
      </div>
    </div>
  )
}

// ReportTab is the Datenauskunft surface (Story 3.3, FR-24/AD-8): a user
// picker (the server-authoritative user list), a "Bericht erstellen" action and
// the assembled report rendered read-only with a JSON download. Empty sections
// render German notes ("Keine …"), never an error (REPORT_NO_AUTH /
// REPORT_NO_INSPECTIONS).
function ReportTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const [users, setUsers] = useState<AdminUserSummary[]>([])
  const [usersLoaded, setUsersLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [report, setReport] = useState<AccessReport | null>(null)
  const [targetName, setTargetName] = useState('')
  // Cancellation guard for the report fetch: mounted tracks the component
  // lifetime (a late response must never setState after unmount), requestSeq
  // tracks per-request staleness (a late response for a PREVIOUS selection must
  // never render over the current one).
  const mounted = useRef(true)
  const requestSeq = useRef(0)

  useEffect(() => {
    let cancelled = false
    // Reset the mounted flag at the START of each effect run: React StrictMode
    // double-invokes effects in dev (mount → cleanup → mount), so the previous
    // run's cleanup set mounted.current = false; without this reset the guard in
    // generate() would silently drop every response (and busy would never clear).
    mounted.current = true
    async function run() {
      try {
        const rows = await listUsers()
        if (cancelled) return
        setUsers(rows)
        if (rows.length === 1) setSelectedId(rows[0].id)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die Benutzerliste konnte nicht geladen werden.')
        }
      } finally {
        if (!cancelled) setUsersLoaded(true)
      }
    }
    void run()
    return () => {
      cancelled = true
      mounted.current = false
    }
  }, [onApiError])

  async function generate() {
    if (!selectedId) {
      setFeedback({ kind: 'error', message: 'Bitte wähle einen Benutzer aus.' })
      return
    }
    const seq = ++requestSeq.current
    setBusy(true)
    setFeedback(null)
    try {
      const data = await getDsgvoReport(selectedId)
      if (!mounted.current || requestSeq.current !== seq) return
      const user = users.find((u) => u.id === selectedId)
      setReport(data)
      setTargetName(displayNameOf(user, selectedId))
    } catch (err) {
      if (!mounted.current || requestSeq.current !== seq) return
      if (onApiError(err)) return
      setReport(null)
      setFeedback({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Der Bericht konnte nicht erstellt werden.',
      })
    } finally {
      if (mounted.current && requestSeq.current === seq) setBusy(false)
    }
  }

  return (
    <>
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

      {!usersLoaded ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="Benutzerliste wird geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <>
          <div className={styles.picker}>
            <label className={styles.label} htmlFor="dsgvo-user">
              Benutzer
            </label>
            <div className={styles.pickerRow}>
              <select
                id="dsgvo-user"
                className={styles.select}
                value={selectedId}
                onChange={(e) => {
                  setSelectedId(e.target.value)
                  setFeedback(null)
                }}
              >
                <option value="">— Bitte wählen —</option>
                {users.map((u) => (
                  <option key={u.id} value={u.id}>
                    {`${displayNameOf(u, u.id)} (${u.email})`}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className={styles.saveButton}
                disabled={busy || selectedId === ''}
                onClick={() => void generate()}
              >
                {busy ? 'Bericht wird erstellt...' : 'Bericht erstellen'}
              </button>
            </div>
          </div>

          {report && (
            <ReportView report={report} targetName={targetName} />
          )}
        </>
      )}
    </>
  )
}

// ReportView renders the assembled data-access report read-only. Each section
// falls back to a German empty note when it has no data (REPORT_NO_AUTH /
// REPORT_NO_INSPECTIONS). The user/tools sections are null-guarded so a
// defensive server failure (a `null` section) never crashes the render.
function ReportView({ report, targetName }: { report: AccessReport; targetName: string }) {
  const user = report.user
  const tools = report.tools
  if (!user || !tools) {
    return (
      <div className={styles.report} role="region" aria-label="Datenauskunft">
        <p role="alert" className={styles.feedbackError}>
          Der Bericht ist unvollständig und kann nicht angezeigt werden.
        </p>
      </div>
    )
  }
  return (
    <div className={styles.report} role="region" aria-label="Datenauskunft">
      <div className={styles.reportHeader}>
        <div>
          <h3 className={styles.reportTitle}>Datenauskunft für {targetName}</h3>
          <p className={styles.reportMeta}>Erstellt am {formatDate(report.generated_at)}</p>
        </div>
        <button type="button" className={styles.rowButton} onClick={() => downloadReport(report, targetName)}>
          Herunterladen (JSON)
        </button>
      </div>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>Profil</h4>
        <dl className={styles.grid}>
          <div>
            <dt className={styles.dt}>E-Mail</dt>
            <dd className={styles.dd}>{user.profile.email}</dd>
          </div>
          {user.profile.pending_email ? (
            <div>
              <dt className={styles.dt}>E-Mail-Änderung (ausstehend)</dt>
              <dd className={styles.dd}>{user.profile.pending_email}</dd>
            </div>
          ) : null}
          <div>
            <dt className={styles.dt}>Anzeigename</dt>
            <dd className={styles.dd}>{user.profile.display_name}</dd>
          </div>
          <div>
            <dt className={styles.dt}>Vorname</dt>
            <dd className={styles.dd}>{user.profile.first_name || '–'}</dd>
          </div>
          <div>
            <dt className={styles.dt}>Nachname</dt>
            <dd className={styles.dd}>{user.profile.last_name || '–'}</dd>
          </div>
          <div>
            <dt className={styles.dt}>Status</dt>
            <dd className={styles.dd}>{user.profile.state}</dd>
          </div>
          <div>
            <dt className={styles.dt}>MFA aktiv</dt>
            <dd className={styles.dd}>{user.profile.is_mfa_enabled ? 'Ja' : 'Nein'}</dd>
          </div>
          <div>
            <dt className={styles.dt}>Registriert am</dt>
            <dd className={styles.dd}>{formatDate(user.profile.created_at)}</dd>
          </div>
          <div>
            <dt className={styles.dt}>Zuletzt geändert</dt>
            <dd className={styles.dd}>{formatDate(user.profile.updated_at)}</dd>
          </div>
        </dl>
        {Object.keys(user.profile.attributes ?? {}).length > 0 && (
          <ul className={styles.tagList} aria-label="Attribute">
            {Object.entries(user.profile.attributes ?? {}).map(([key, value]) => (
              <li key={key} className={styles.tag}>
                {key}: {String(value)}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>Rollen und Berechtigungen</h4>
        <p className={styles.emptyNote}>
          {user.roles.length === 0 && user.user_groups.length === 0 && user.direct_grants.length === 0
            ? 'Keine Rollen, Gruppen oder direkten Berechtigungen.'
            : ''}
        </p>
        {user.roles.length > 0 && (
          <p className={styles.inline}>
            <strong>Rollen:</strong>{' '}
            {user.roles.map((r) => r.name).join(', ') || 'Keine Rollen.'}
          </p>
        )}
        {user.user_groups.length > 0 && (
          <p className={styles.inline}>
            <strong>Gruppen:</strong>{' '}
            {user.user_groups.map((g) => g.name).join(', ') || 'Keine Gruppen.'}
          </p>
        )}
        {user.direct_grants.length > 0 && (
          <p className={styles.inline}>
            <strong>Direkte Berechtigungen:</strong>{' '}
            {user.direct_grants.map((g) => g.code).join(', ')}
          </p>
        )}
      </section>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>Qualifikationen</h4>
        {user.qualifications.length === 0 ? (
          <p className={styles.emptyNote}>Keine Qualifikationen.</p>
        ) : (
          <ul className={styles.list}>
            {user.qualifications.map((q) => (
              <li key={q.id} className={styles.row}>
                <span className={styles.rowName}>{q.name}</span>
                <span className={styles.rowMeta}>
                  {q.status} · gültig bis {formatDate(q.expires_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>Anmeldeverlauf</h4>
        {user.sessions.length === 0 ? (
          <p className={styles.emptyNote}>Keine Sitzungen.</p>
        ) : (
          <ul className={styles.list}>
            {user.sessions.map((s) => (
              <li key={s.id} className={styles.row}>
                <span className={styles.rowMeta}>
                  Erstellt {formatDate(s.created_at)} · gültig bis {formatDate(s.expires_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
        {!user.login_attempts ? (
          <p className={styles.emptyNote}>Keine Anmeldeversuche.</p>
        ) : (
          <p className={styles.inline}>
            <strong>Anmeldeversuche:</strong> {user.login_attempts.failed_count} fehlgeschlagen
            {user.login_attempts.lockout_until
              ? ` · gesperrt bis ${formatDate(user.login_attempts.lockout_until)}`
              : ' · nicht gesperrt'}
          </p>
        )}
      </section>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>Prüfungen</h4>
        {tools.summary.length === 0 ? (
          <p className={styles.emptyNote}>Keine Prüfungen in der Zusammenfassung.</p>
        ) : (
          <ul className={styles.list}>
            {tools.summary.map((s) => (
              <li key={s.tool_id} className={styles.row}>
                <span className={styles.rowName}>{s.tool_name}</span>
                <span className={styles.rowMeta}>
                  {s.inspection_count} Prüfungen · {s.fail_count} fehlgeschlagen
                </span>
              </li>
            ))}
          </ul>
        )}
        {tools.inspections.length === 0 ? (
          <p className={styles.emptyNote}>Keine Prüfungen.</p>
        ) : (
          <ul className={styles.list}>
            {tools.inspections.map((insp) => (
              <li key={insp.id} className={styles.row}>
                <span className={styles.rowName}>
                  {insp.tool_name} · {insp.overall_result === 'pass' ? 'bestanden' : 'fehlgeschlagen'}
                </span>
                <span className={styles.rowMeta}>
                  {formatDate(insp.submitted_at)}
                  {insp.notes ? ` · ${insp.notes}` : ''}
                </span>
                {insp.items.length > 0 && (
                  <span className={styles.rowMeta}>
                    {insp.items.map((item) => `${item.label}: ${item.result}`).join(' · ')}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        {tools.reinstatements.length === 0 ? (
          <p className={styles.emptyNote}>Keine Wiederherstellungen.</p>
        ) : (
          <ul className={styles.list}>
            {tools.reinstatements.map((r) => (
              <li key={r.id} className={styles.row}>
                <span className={styles.rowName}>{r.tool_name}</span>
                <span className={styles.rowMeta}>
                  {formatDate(r.created_at)} · {r.reason}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

// DeleteTab is the Konto-löschen surface (Story 3.4, FR-24/UX-DR7/DR8): the
// heavy TWO-STEP delete flow plus the "Gelöschte Konten" purge list. Step one
// picks a user; step two requires the user's display_name typed EXACTLY AND a
// mandatory non-empty Begründung — "Endgültig löschen" stays disabled until both
// are valid, a name mismatch shows an inline German error and blocks deletion,
// and the button is `busy` during the call. The deletion NEVER hard-deletes:
// the account becomes a scrubbed tombstone and its personal data moves into the
// archive. The second section lists the archived accounts (name/email/date/
// reason) with a per-row "Jetzt endgültig löschen" purge action (window.confirm)
// — the ONLY hard delete, admin-initiated on demand. 401 → login, 403 → leave
// the admin module.
function DeleteTab({ onApiError }: { onApiError: (err: unknown) => boolean }) {
  const [users, setUsers] = useState<AdminUserSummary[]>([])
  const [usersLoaded, setUsersLoaded] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [typedName, setTypedName] = useState('')
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [deleted, setDeleted] = useState<DeletedAccountRow[]>([])
  const [deletedLoaded, setDeletedLoaded] = useState(false)
  const [deletedError, setDeletedError] = useState('')

  const selectedUser = users.find((u) => u.id === selectedId)
  const expectedName = displayNameOf(selectedUser, selectedId)
  const nameMatches = typedName.trim() !== '' && typedName.trim() === expectedName
  const reasonValid = reason.trim() !== ''
  const canSubmit = selectedId !== '' && nameMatches && reasonValid && !busy

  // Load both the user picker and the archived-accounts list on mount.
  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const [rows, archived] = await Promise.all([listUsers(), listDeletedAccounts()])
        if (cancelled) return
        // Story 3.4 Never rule: a `deleted` tombstone is non-existent to the
        // admin surface — even if one leaks into the payload, it is never
        // offered as a selectable delete target (the server already excludes
        // them from ListUsers; this is the defensive client filter).
        const liveUsers = rows.filter((u) => u.status !== ('deleted' as AdminUserSummary['status']))
        setUsers(liveUsers)
        setDeleted(archived)
        if (liveUsers.length === 1) setSelectedId(liveUsers[0].id)
      } catch (err) {
        if (cancelled) return
        if (!onApiError(err)) {
          setLoadError('Die Benutzerliste konnte nicht geladen werden.')
        }
      } finally {
        if (!cancelled) {
          setUsersLoaded(true)
          setDeletedLoaded(true)
        }
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [onApiError])

  async function refreshDeleted() {
    try {
      const archived = await listDeletedAccounts()
      setDeleted(archived)
      setDeletedLoaded(true)
      setDeletedError('')
    } catch (err) {
      if (onApiError(err)) return
      setDeletedError('Die gelöschten Konten konnten nicht geladen werden.')
    }
  }

  async function performDelete() {
    if (!selectedId) {
      setFeedback({ kind: 'error', message: 'Bitte wähle einen Benutzer aus.' })
      return
    }
    if (!nameMatches) {
      setFeedback({ kind: 'error', message: 'Der eingegebene Name stimmt nicht mit dem Benutzer überein.' })
      return
    }
    if (!reasonValid) {
      setFeedback({ kind: 'error', message: 'Bitte gib eine Begründung an.' })
      return
    }
    setBusy(true)
    setFeedback(null)
    try {
      const result = await deleteUserAccount(selectedId, reason.trim())
      setFeedback({ kind: 'success', message: result.message })
      // The deleted user leaves the picker; the account appears in the archive.
      const remaining = users.filter((u) => u.id !== selectedId)
      setUsers(remaining)
      setSelectedId('')
      setTypedName('')
      setReason('')
      await refreshDeleted()
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({ kind: 'error', message: err instanceof Error ? err.message : 'Das Konto konnte nicht gelöscht werden.' })
    } finally {
      setBusy(false)
    }
  }

  async function performPurge(account: DeletedAccountRow) {
    const confirmed = window.confirm(
      `Soll „${account.display_name}“ (${account.email}) endgültig gelöscht werden? Diese Aktion kann nicht rückgängig gemacht werden.`,
    )
    if (!confirmed) return
    setBusy(true)
    setFeedback(null)
    try {
      const result = await purgeDeletedAccount(account.id)
      setDeleted((prev) => prev.filter((a) => a.id !== account.id))
      setFeedback({ kind: 'success', message: result.message })
    } catch (err) {
      if (onApiError(err)) return
      setFeedback({ kind: 'error', message: err instanceof Error ? err.message : 'Der Account konnte nicht endgültig gelöscht werden.' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
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

      <p className={styles.warning} role="note">
        Unumkehrbar · Audit-Pflicht
      </p>

      {!usersLoaded ? (
        <div className={styles.skeleton} aria-busy="true" aria-label="Benutzerliste wird geladen">
          <div className={styles.skeletonRow} aria-hidden="true" />
          <div className={styles.skeletonRow} aria-hidden="true" />
        </div>
      ) : (
        <>
          <div className={styles.deleteForm}>
            <label className={styles.label} htmlFor="dsgvo-delete-user">
              Benutzer
            </label>
            <select
              id="dsgvo-delete-user"
              className={styles.select}
              value={selectedId}
              disabled={busy}
              onChange={(e) => {
                setSelectedId(e.target.value)
                setTypedName('')
                setReason('')
                setFeedback(null)
              }}
            >
              <option value="">— Bitte wählen —</option>
              {users.map((u) => (
                <option key={u.id} value={u.id}>
                  {`${displayNameOf(u, u.id)} (${u.email})`}
                </option>
              ))}
            </select>

            {selectedUser && (
              <>
                <p className={styles.confirmHint}>
                  Gib den Namen exakt ein, um die Löschung zu bestätigen:
                </p>
                <p className={styles.confirmName}>{expectedName}</p>
                <label className={styles.label} htmlFor="dsgvo-delete-name">
                  Name bestätigen
                </label>
                <input
                  id="dsgvo-delete-name"
                  className={typedName.trim() !== '' && !nameMatches ? `${styles.textInput} ${styles.inputError}` : styles.textInput}
                  type="text"
                  value={typedName}
                  disabled={busy}
                  onChange={(e) => {
                    setTypedName(e.target.value)
                    setFeedback(null)
                  }}
                  placeholder={expectedName}
                  autoComplete="off"
                />
                {typedName.trim() !== '' && !nameMatches && (
                  <p role="alert" className={styles.fieldError}>
                    Der Name stimmt nicht überein.
                  </p>
                )}
                <label className={styles.label} htmlFor="dsgvo-delete-reason">
                  Begründung
                </label>
                <textarea
                  id="dsgvo-delete-reason"
                  className={styles.textarea}
                  value={reason}
                  disabled={busy}
                  onChange={(e) => {
                    setReason(e.target.value)
                    setFeedback(null)
                  }}
                  placeholder="Warum wird das Konto gelöscht?"
                  rows={3}
                />
                {reason.trim() === '' && reason !== '' && (
                  <p role="alert" className={styles.fieldError}>
                    Bitte gib eine Begründung an.
                  </p>
                )}
                <button
                  type="button"
                  className={styles.dangerButton}
                  disabled={!canSubmit}
                  onClick={() => void performDelete()}
                >
                  {busy ? 'Konto wird gelöscht...' : 'Endgültig löschen'}
                </button>
              </>
            )}
          </div>

          <section className={styles.deletedSection} aria-label="Gelöschte Konten">
            <h3 className={styles.sectionTitle}>Gelöschte Konten</h3>
            {deletedError && (
              <p role="alert" className={styles.feedbackError}>
                {deletedError}
              </p>
            )}
            {!deletedLoaded ? (
              <div className={styles.skeleton} aria-busy="true" aria-label="Gelöschte Konten werden geladen">
                <div className={styles.skeletonRow} aria-hidden="true" />
              </div>
            ) : deleted.length === 0 ? (
              <p className={styles.emptyNote}>Keine gelöschten Konten.</p>
            ) : (
              <ul className={styles.list}>
                {deleted.map((account) => (
                  <li key={account.id} className={styles.row}>
                    <span className={styles.rowName}>{account.display_name || account.email}</span>
                    <span className={styles.rowMeta}>
                      {account.email} · gelöscht am {formatDate(account.deleted_at)}
                      {account.reason ? ` · ${account.reason}` : ''}
                    </span>
                    <button
                      type="button"
                      className={styles.rowButton}
                      disabled={busy}
                      onClick={() => void performPurge(account)}
                    >
                      Jetzt endgültig löschen
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </>
  )
}