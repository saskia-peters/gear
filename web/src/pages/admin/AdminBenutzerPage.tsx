import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { UserEditor } from '../../components/UserEditor.tsx'
import { UserDetail } from '../../components/UserDetail.tsx'
import { UserTable } from '../../components/UserTable.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions, hasPermission } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listRoles } from '../../auth/roles.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../../auth/roles.ts'
import { ApiError } from '../../auth/users.ts'
import {
  DEFAULT_USER_STATUS_FILTER,
  listUsers,
  listUserGroups,
  getUserDetail,
} from '../../auth/users.ts'
import type { AdminUserDetail, AdminUserSummary, UserGroup, UserStatusFilter } from '../../auth/users.ts'
import styles from './AdminBenutzerPage.module.css'

type View = { kind: 'list' } | { kind: 'detail'; user: AdminUserDetail } | { kind: 'edit'; user: AdminUserDetail | null }

// AdminBenutzerPage is the real "Benutzer" surface (Story 2.6 + Effort 2,
// UX-DR6/UX-DR8/UX-DR9): a compact SORTABLE spreadsheet table (Vorname ·
// Nachname · E-Mail · Status) with status filter chips (default aktiv, driving
// ?status= on ListUsers) and inline user-group tags; a user detail (three
// source sections + a collapsed "Alle Berechtigungen" provenance view) with an
// editable Qualifikationen section for users.qualifications.manage holders and
// an editable Benutzergruppen membership list (Effort 2); a create/edit form
// and a deactivate flow with confirmation ("→ Sofort kein Login"). User-group
// CREATION and team management (members + team roles) live on the dedicated
// AdminBenutzergruppenPage, reached from the admin nav between Benutzer and
// Rollen.
//
// The "Neuer Benutzer"/"Bearbeiten"/"Deaktivieren" actions are gated
// client-side on `users.manage`, the user-detail group membership editing on
// `user_groups.manage`, and qualification editing on
// `users.qualifications.manage` — matching the server's per-action codes
// (AD-6); the server remains the source of truth. Each list fetch is gated on
// the caller's permissions and isolated so one 403 cannot abort the user list
// (finding 2). A 403 (revoked role) clears the cached admin flag and leaves
// the admin module; a 401 (expired session) clears auth and redirects to
// /login (Effort 2 I/O 401_EXPIRED).
export function AdminBenutzerPage() {
  const navigate = useNavigate()
  const [users, setUsers] = useState<AdminUserSummary[]>([])
  const [roles, setRoles] = useState<RoleGroup[]>([])
  const [userGroups, setUserGroups] = useState<UserGroup[]>([])
  const [catalog, setCatalog] = useState<PermissionCatalogEntry[]>([])
  const [view, setView] = useState<View>({ kind: 'list' })
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [feedback, setFeedback] = useState('')
  const [statusFilter, setStatusFilter] = useState<UserStatusFilter>(DEFAULT_USER_STATUS_FILTER)

  const canManage = hasPermission('users.manage')
  const canManageGroups = hasPermission('user_groups.manage')
  const canManageQualifications = hasPermission('users.qualifications.manage')
  const canViewRoles =
    hasPermission('roles.create') || hasPermission('roles.edit') || hasPermission('roles.assign')

  const load = useCallback(async () => {
    // finding 9: every load resets the loading and error states so a failed
    // reload never leaves stale error text and the skeleton shows.
    setLoading(true)
    setLoadError('')
    try {
      setUsers(await listUsers(statusFilter === 'all' ? undefined : statusFilter))
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        // Revocation downgrade (review finding 2.1-6): drop the admin flag and
        // leave the admin module.
        adminForbiddenHandled({ status: 403 })
        navigate('/')
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        // 401_EXPIRED (Effort 2): clear auth state and redirect to login.
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setLoadError('Benutzer konnten nicht geladen werden.')
    }
    // Roles/catalog are needed by the user editor (requires a roles.* code).
    // Skip when the caller holds none; a 403 is treated as "no roles" rather
    // than aborting the page.
    if (canViewRoles) {
      try {
        const roleData = await listRoles()
        setRoles(roleData.groups)
        setCatalog(roleData.available_permissions)
      } catch (err) {
        if (err instanceof ApiError && err.status === 403 && canViewRoles) {
          // Genuine revocation: the caller legitimately held a roles.* code but
          // the server now denies — clear the admin flag and leave the module.
          adminForbiddenHandled({ status: 403 })
          navigate('/')
          return
        }
        if (err instanceof ApiError && err.status === 401) {
          clearAuthState()
          navigate('/login', { replace: true })
          return
        }
        setRoles([])
        setCatalog([])
      }
    } else {
      setRoles([])
      setCatalog([])
    }
    // User groups are still needed on the Benutzer surface: the user detail's
    // editable Benutzergruppen checkboxes (Effort 2) list every group. Skip
    // when the caller lacks user_groups.manage so a users.manage holder without
    // it never breaks the page (finding 2).
    if (canManageGroups) {
      try {
        setUserGroups(await listUserGroups())
      } catch (err) {
        if (err instanceof ApiError && err.status === 403) {
          adminForbiddenHandled({ status: 403 })
          navigate('/')
          return
        }
        if (err instanceof ApiError && err.status === 401) {
          clearAuthState()
          navigate('/login', { replace: true })
          return
        }
        setUserGroups([])
      }
    } else {
      setUserGroups([])
    }
    setLoading(false)
  }, [canViewRoles, canManageGroups, navigate, statusFilter])

  // finding 9: one load() used by both mount and the action handlers. The// initial call is deferred so the effect's synchronous scope performs no
// setState (react-hooks/set-state-in-effect); loading defaults to true and
// load() resets the states on every subsequent invocation.
  useEffect(() => {
    const id = window.setTimeout(() => {
      void load()
    }, 0)
    return () => window.clearTimeout(id)
  }, [load])

  function openCreate() {
    setView({ kind: 'edit', user: null })
    setFeedback('')
  }

  function openEdit(user: AdminUserDetail) {
    setView({ kind: 'edit', user })
    setFeedback('')
  }

  async function openDetail(summary: AdminUserSummary) {
    try {
      const detail = await getUserDetail(summary.id)
      setView({ kind: 'detail', user: detail })
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        adminForbiddenHandled({ status: 403 })
        navigate('/')
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setLoadError('Der Benutzer konnte nicht geladen werden.')
    }
  }

  // refreshDetail re-fetches the currently open user detail (Effort 2): after a
  // qualification assign/revoke/expiry write, so the view stays open and the
  // assignment/status is refreshed in place.
  async function refreshDetail() {
    if (view.kind !== 'detail') return
    try {
      const detail = await getUserDetail(view.user.id)
      setView({ kind: 'detail', user: detail })
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        adminForbiddenHandled({ status: 403 })
        navigate('/')
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setLoadError('Der Benutzer konnte nicht neu geladen werden.')
    }
  }

  function handleUnauthorized() {
    clearAuthState()
    navigate('/login', { replace: true })
  }

  function handleFilterChange(filter: UserStatusFilter) {
    setStatusFilter(filter)
    setFeedback('')
  }

  function backToList() {
    setView({ kind: 'list' })
    setFeedback('')
    setLoadError('')
  }

  function handleForbidden() {
    adminForbiddenHandled({ status: 403 })
    navigate('/')
  }

  async function handleSaved(_saved: AdminUserDetail, message: string) {
    setFeedback(message)
    setView({ kind: 'list' })
    await load()
  }

  function handleDeactivated(message: string) {
    setFeedback(message)
    setView({ kind: 'list' })
    void load()
  }

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(getPermissions())} />
        <main className={styles.main}>
          <h2 className={styles.title}>Benutzer</h2>
          <p className={styles.description}>
            Mitglieder verwalten, Teams-Zuordnung einsehen und Zugänge deaktivieren.
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

          {view.kind === 'edit' ? (
            <UserEditor
              key={view.user?.id ?? 'new'}
              user={view.user}
              roles={roles}
              userGroups={userGroups}
              availablePermissions={catalog}
              onSaved={handleSaved}
              onCancel={backToList}
              onForbidden={handleForbidden}
            />
          ) : view.kind === 'detail' ? (
            <UserDetail
              user={view.user}
              canManage={canManage}
              canManageQualifications={canManageQualifications}
              canManageGroups={canManageGroups}
              userGroups={userGroups}
              onEdit={() => openEdit(view.user)}
              onBack={backToList}
              onRefreshDetail={() => void refreshDetail()}
              onDeactivated={handleDeactivated}
              onForbidden={handleForbidden}
              onUnauthorized={handleUnauthorized}
            />
          ) : loading ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Benutzer werden geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : (
            <>
              <div className={styles.toolbar}>
                {canManage && (
                  <button type="button" className={styles.newButton} onClick={openCreate}>
                    Neuer Benutzer
                  </button>
                )}
              </div>

              <UserTable
                users={users}
                selectedFilter={statusFilter}
                onSelectFilter={handleFilterChange}
                onOpenUser={(user) => void openDetail(user)}
              />
            </>
          )}
        </main>
      </div>
    </div>
  )
}