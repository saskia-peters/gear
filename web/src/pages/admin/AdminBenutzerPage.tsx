import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { UserEditor } from '../../components/UserEditor.tsx'
import { UserDetail } from '../../components/UserDetail.tsx'
import { adminForbiddenHandled, getPermissions, hasPermission } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listRoles } from '../../auth/roles.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../../auth/roles.ts'
import { ApiError } from '../../auth/users.ts'
import {
  listUsers,
  listUserGroups,
  createUserGroup,
  getUserDetail,
  listUserGroupMembers,
  assignUserGroupMembers,
  deleteUserGroup,
  userStatusLabel,
} from '../../auth/users.ts'
import type { AdminUserDetail, AdminUserSummary, UserGroup } from '../../auth/users.ts'
import styles from './AdminBenutzerPage.module.css'

type View = { kind: 'list' } | { kind: 'detail'; user: AdminUserDetail } | { kind: 'edit'; user: AdminUserDetail | null }

// AdminBenutzerPage is the real "Benutzer" surface (Story 2.6, UX-DR6/UX-DR8/
// UX-DR9): a user list with status badges (aktiv/pending/deaktiviert), a user
// detail (roles + user groups + direct grants + qualification view), a
// create/edit form and a deactivate flow with confirmation ("→ Sofort kein
// Login"). It also manages the organisational user groups (teams, AD-12) that
// the editor assigns — create, delete (with confirmation) and member
// assignment.
//
// The "Neuer Benutzer"/"Bearbeiten"/"Deaktivieren" actions are gated
// client-side on `users.manage` and the user-group actions on
// `user_groups.manage`, matching the server's per-action codes (AD-6); the
// server remains the source of truth. Each list fetch is gated on the caller's
// permissions and isolated so one 403 cannot abort the user list (finding 2):
// listUsers always runs (the page is already gated on users.*), listRoles only
// when the caller holds any roles.* code, listUserGroups only with
// user_groups.manage. A 403 on listUsers (revoked role) clears the cached admin
// flag and leaves the admin module (review finding 2.1-6).
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
  const [groupName, setGroupName] = useState('')
  const [groupFeedback, setGroupFeedback] = useState('')
  const [groupBusy, setGroupBusy] = useState(false)
  // Group member editor + delete confirmation state (findings 5 & 6).
  const [memberEditor, setMemberEditor] = useState<{ group: UserGroup; members: Set<string> } | null>(null)
  const [memberBusy, setMemberBusy] = useState(false)
  const [confirmDeleteGroup, setConfirmDeleteGroup] = useState<UserGroup | null>(null)

  const canManage = hasPermission('users.manage')
  const canManageGroups = hasPermission('user_groups.manage')
  const canViewRoles =
    hasPermission('roles.create') || hasPermission('roles.edit') || hasPermission('roles.assign')

  const load = useCallback(async () => {
    // finding 9: every load resets the loading and error states so a failed
    // reload never leaves stale error text and the skeleton shows.
    setLoading(true)
    setLoadError('')
    try {
      setUsers(await listUsers())
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        // Revocation downgrade (review finding 2.1-6): drop the admin flag and
        // leave the admin module.
        adminForbiddenHandled({ status: 403 })
        navigate('/')
        return
      }
      setLoadError('Benutzer konnten nicht geladen werden.')
    }
    // Roles/catalog are only needed by the editor and require a roles.* code
    // (finding 2): skip the fetch entirely when the caller holds none, and
    // treat a 403 as "no roles" rather than aborting the page.
    if (canViewRoles) {
      try {
        const roleData = await listRoles()
        setRoles(roleData.groups)
        setCatalog(roleData.available_permissions)
      } catch (err) {
        if (err instanceof ApiError && err.status === 403) {
          adminForbiddenHandled({ status: 403 })
          navigate('/')
          return
        }
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
        setUserGroups([])
      }
    } else {
      setUserGroups([])
    }
    setLoading(false)
  }, [canViewRoles, canManageGroups, navigate])

  // finding 9: one load() used by both mount and the action handlers. The
// initial call is deferred so the effect's synchronous scope performs no
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
      setLoadError('Der Benutzer konnte nicht geladen werden.')
    }
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
  async function openMemberEditor(group: UserGroup) {
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
      setGroupFeedback(err instanceof ApiError && err.message ? err.message : 'Die Benutzergruppe konnte nicht gelöscht werden.')
    } finally {
      setGroupBusy(false)
    }
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
              onEdit={() => openEdit(view.user)}
              onBack={backToList}
              onDeactivated={handleDeactivated}
              onForbidden={handleForbidden}
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

              {users.length === 0 ? (
                <div className={styles.empty}>
                  <p>Noch keine Benutzer vorhanden.</p>
                </div>
              ) : (
                <ul className={styles.list}>
                  {users.map((user) => (
                    <li key={user.id} className={styles.row}>
                      <button type="button" className={styles.rowMain} onClick={() => void openDetail(user)}>
                        <span className={styles.userName}>
                          {user.vorname} {user.nachname}
                        </span>
                        <span className={styles.userEmail}>{user.email}</span>
                      </button>
                      <div className={styles.rowActions}>
                        <span className={`${styles.statusBadge} ${styles[`status-${user.status}`]}`}>
                          {userStatusLabel(user.status)}
                        </span>
                        <button
                          type="button"
                          className={styles.editButton}
                          onClick={() => void openDetail(user)}
                        >
                          Öffnen
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
              )}

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
                            className={styles.groupDeleteButton}
                            onClick={() => setConfirmDeleteGroup(group)}
                          >
                            Löschen
                          </button>
                        </div>
                      </li>
                    ))}
                  </ul>

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