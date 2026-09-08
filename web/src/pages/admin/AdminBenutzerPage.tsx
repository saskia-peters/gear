import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { UserEditor } from '../../components/UserEditor.tsx'
import { UserDetail } from '../../components/UserDetail.tsx'
import { UserTable } from '../../components/UserTable.tsx'
import { GroupRolesEditor } from '../../components/GroupRolesEditor.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions, hasPermission } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listRoles } from '../../auth/roles.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../../auth/roles.ts'
import { ApiError } from '../../auth/users.ts'
import {
  DEFAULT_USER_STATUS_FILTER,
  listUsers,
  listUserGroups,
  createUserGroup,
  getUserDetail,
  listUserGroupMembers,
  assignUserGroupMembers,
  deleteUserGroup,
} from '../../auth/users.ts'
import type { AdminUserDetail, AdminUserSummary, UserGroup, UserStatusFilter } from '../../auth/users.ts'
import styles from './AdminBenutzerPage.module.css'

type View = { kind: 'list' } | { kind: 'detail'; user: AdminUserDetail } | { kind: 'edit'; user: AdminUserDetail | null }

// AdminBenutzerPage is the real "Benutzer" surface (Story 2.6 + Effort 2,
// UX-DR6/UX-DR8/UX-DR9): a compact SORTABLE spreadsheet table (Vorname ·
// Nachname · E-Mail · Status) with status filter chips (default aktiv, driving
// ?status= on ListUsers) and inline user-group tags; a user detail (three
// source sections + a collapsed "Alle Berechtigungen" provenance view) with an
// editable Qualifikationen section for users.qualifications.manage holders; a
// create/edit form and a deactivate flow with confirmation ("→ Sofort kein
// Login"). It also manages the organisational user groups (teams, AD-12): a
// member editor AND a per-group role-assignment editor (Effort 2).
//
// The "Neuer Benutzer"/"Bearbeiten"/"Deaktivieren" actions are gated
// client-side on `users.manage`, the user-group actions on
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
  const [groupName, setGroupName] = useState('')
  const [groupFeedback, setGroupFeedback] = useState('')
  const [groupBusy, setGroupBusy] = useState(false)
  // Group member editor + delete confirmation state (findings 5 & 6).
  const [memberEditor, setMemberEditor] = useState<{ group: UserGroup; members: Set<string> } | null>(null)
  const [memberBusy, setMemberBusy] = useState(false)
  const [confirmDeleteGroup, setConfirmDeleteGroup] = useState<UserGroup | null>(null)
  // Group role-assignment editor (Effort 2): the group being edited + its
  // current role ids.
  const [rolesEditor, setRolesEditor] = useState<{ group: UserGroup } | null>(null)

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
    // Roles/catalog are needed by BOTH the user editor (requires a roles.*
    // code) AND the group role-assignment editor (requires only
    // user_groups.manage — the GroupRolesEditor's checkbox list needs the
    // catalog regardless of roles.*). So fetch roles whenever the caller can
    // view roles OR manage groups; skip only when neither applies. A 403 is
    // treated as "no roles" rather than aborting the page.
    const needsRoles = canViewRoles || canManageGroups
    if (needsRoles) {
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
        // No roles.* code (user_groups.manage-only holder): GET /groups is
        // server-denied, which is expected — the group editor simply has no
        // catalog to offer. Never log the caller out for an expected 403.
        setRoles([])
        setCatalog([])
      }
    } else {
      setRoles([])
      setCatalog([])
    }
    // User groups need user_groups.manage (finding 2): skip when absent so a
    // users.manage holder without it never breaks the page.
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

  async function handleCreateGroup() {
    const name = groupName.trim()
    if (name === '') {
      setGroupFeedback('Bitte gib einen Namen für die Benutzergruppe an.')
      return
    }
    setGroupBusy(true)
    setGroupFeedback('')
    try {
      const group = await createUserGroup({ name, description: '' })
      setGroupName('')
      setGroupFeedback(`Benutzergruppe „${group.name}“ erstellt.`)
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        handleForbidden()
        return
      }
      setGroupFeedback(err instanceof ApiError && err.message ? err.message : 'Die Benutzergruppe konnte nicht erstellt werden.')
    } finally {
      setGroupBusy(false)
    }
  }

  // finding 6: open the member editor for a group, pre-checking current members.
  // Opening one overlay closes the others (they are mutually exclusive).
  async function openMemberEditor(group: UserGroup) {
    setRolesEditor(null)
    setConfirmDeleteGroup(null)
    setMemberBusy(true)
    setGroupFeedback('')
    try {
      const ids = await listUserGroupMembers(group.id)
      setMemberEditor({ group, members: new Set(ids) })
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        handleForbidden()
        return
      }
      setGroupFeedback('Die Mitglieder konnten nicht geladen werden.')
    } finally {
      setMemberBusy(false)
    }
  }

  function toggleMember(userID: string) {
    setMemberEditor((prev) => {
      if (!prev) return prev
      const next = new Set(prev.members)
      if (next.has(userID)) {
        next.delete(userID)
      } else {
        next.add(userID)
      }
      return { ...prev, members: next }
    })
  }

  // finding 6: save the member set via the existing assign endpoint.
  async function saveMembers() {
    if (!memberEditor) return
    setMemberBusy(true)
    setGroupFeedback('')
    try {
      await assignUserGroupMembers(memberEditor.group.id, [...memberEditor.members])
      setGroupFeedback(`Mitglieder von „${memberEditor.group.name}“ aktualisiert.`)
      setMemberEditor(null)
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        handleForbidden()
        return
      }
      setGroupFeedback(err instanceof ApiError && err.message ? err.message : 'Die Mitglieder konnten nicht gespeichert werden.')
    } finally {
      setMemberBusy(false)
    }
  }

  // finding 5: delete a group after explicit confirmation.
  async function handleConfirmDeleteGroup() {
    if (!confirmDeleteGroup) return
    setGroupBusy(true)
    setGroupFeedback('')
    try {
      await deleteUserGroup(confirmDeleteGroup.id)
      setGroupFeedback(`Benutzergruppe „${confirmDeleteGroup.name}“ gelöscht.`)
      setConfirmDeleteGroup(null)
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        handleForbidden()
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        handleUnauthorized()
        return
      }
      setGroupFeedback(err instanceof ApiError && err.message ? err.message : 'Die Benutzergruppe konnte nicht gelöscht werden.')
    } finally {
      setGroupBusy(false)
    }
  }

  // Effort 2: open the role-assignment editor for a group. Opening one overlay
  // closes the others (they are mutually exclusive).
  function openRolesEditor(group: UserGroup) {
    setMemberEditor(null)
    setConfirmDeleteGroup(null)
    setRolesEditor({ group })
    setGroupFeedback('')
  }

  function handleGroupRolesSaved(message: string) {
    setGroupFeedback(message)
    setRolesEditor(null)
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
            Mitglieder verwalten, Teams pflegen und Zugänge deaktivieren.
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

              {canManageGroups && (
                <section aria-label="Benutzergruppen" className={styles.groupsSection}>
                  <h3 className={styles.groupsTitle}>Benutzergruppen</h3>
                  <p className={styles.groupsHint}>
                    Organisatorische Teams (vergeben keine Rechte).
                  </p>
                  {groupFeedback && (
                    <p
                      role={groupFeedback.startsWith('Benutzergruppe') || groupFeedback.includes('Mitglieder') ? 'status' : 'alert'}
                      className={groupFeedback.startsWith('Benutzergruppe') || groupFeedback.includes('Mitglieder') ? styles.feedbackSuccess : styles.feedbackError}
                    >
                      {groupFeedback}
                    </p>
                  )}
                  <ul className={styles.groupsList}>
                    {userGroups.map((group) => (
                      <li key={group.id} className={styles.groupRow}>
                        <span className={styles.groupName}>{group.name}</span>
                        <div className={styles.groupRowActions}>
                          <button
                            type="button"
                            className={styles.groupActionButton}
                            onClick={() => void openMemberEditor(group)}
                            disabled={memberBusy}
                          >
                            Mitglieder
                          </button>
                          <button
                            type="button"
                            className={styles.groupActionButton}
                            onClick={() => openRolesEditor(group)}
                          >
                            Rollen
                          </button>
                          <button
                            type="button"
                            className={styles.groupDeleteButton}
                            onClick={() => {
                              setMemberEditor(null)
                              setRolesEditor(null)
                              setConfirmDeleteGroup(group)
                            }}
                          >
                            Löschen
                          </button>
                        </div>
                      </li>
                    ))}
                  </ul>
                  {userGroups.length === 0 && (
                    <p className={styles.emptyNote}>Noch keine Benutzergruppen.</p>
                  )}

                  {confirmDeleteGroup && (
                    <div className={styles.confirmBox} role="alert">
                      <p className={styles.confirmText}>
                        Benutzergruppe „{confirmDeleteGroup.name}“ löschen?
                      </p>
                      <div className={styles.confirmActions}>
                        <button type="button" className={styles.confirmDeleteButton} onClick={() => void handleConfirmDeleteGroup()} disabled={groupBusy}>
                          {groupBusy ? 'Wird gelöscht...' : 'Ja, löschen'}
                        </button>
                        <button
                          type="button"
                          className={styles.cancelButton}
                          onClick={() => setConfirmDeleteGroup(null)}
                          disabled={groupBusy}
                        >
                          Abbrechen
                        </button>
                      </div>
                    </div>
                  )}

                  {memberEditor && (
                    <div className={styles.memberEditor}>
                      <h4 className={styles.memberEditorTitle}>
                        Mitglieder von „{memberEditor.group.name}“
                      </h4>
                      {users.length === 0 ? (
                        <p className={styles.emptyNote}>Keine Benutzer verfügbar.</p>
                      ) : (
                        <ul className={styles.memberList}>
                          {users.map((user) => (
                            <li key={user.id} className={styles.memberRow}>
                              <label className={styles.memberLabel}>
                                <input
                                  type="checkbox"
                                  className={styles.checkbox}
                                  checked={memberEditor.members.has(user.id)}
                                  onChange={() => toggleMember(user.id)}
                                />
                                <span className={styles.memberName}>
                                  {user.vorname} {user.nachname}
                                </span>
                              </label>
                            </li>
                          ))}
                        </ul>
                      )}
                      <div className={styles.confirmActions}>
                        <button type="button" className={styles.newButton} onClick={() => void saveMembers()} disabled={memberBusy}>
                          {memberBusy ? 'Wird gespeichert...' : 'Speichern'}
                        </button>
                        <button
                          type="button"
                          className={styles.cancelButton}
                          onClick={() => setMemberEditor(null)}
                          disabled={memberBusy}
                        >
                          Abbrechen
                        </button>
                      </div>
                    </div>
                  )}

                  {rolesEditor && (
                    <GroupRolesEditor
                      groupId={rolesEditor.group.id}
                      groupName={rolesEditor.group.name}
                      roles={roles}
                      onSaved={handleGroupRolesSaved}
                      onForbidden={handleForbidden}
                      onUnauthorized={handleUnauthorized}
                      onCancel={() => setRolesEditor(null)}
                    />
                  )}

                  <div className={styles.groupForm}>
                    <label className={styles.label} htmlFor="group-name">
                      Neue Benutzergruppe
                    </label>
                    <div className={styles.groupFormRow}>
                      <input
                        id="group-name"
                        className={styles.input}
                        value={groupName}
                        onChange={(e) => {
                          setGroupName(e.target.value)
                          setGroupFeedback('')
                        }}
                        maxLength={120}
                        placeholder="z. B. Gruppe Ost"
                      />
                      <button
                        type="button"
                        className={styles.newGroupButton}
                        onClick={() => void handleCreateGroup()}
                        disabled={groupBusy}
                      >
                        {groupBusy ? 'Wird erstellt...' : 'Erstellen'}
                      </button>
                    </div>
                  </div>
                </section>
              )}
            </>
          )}
        </main>
      </div>
    </div>
  )
}