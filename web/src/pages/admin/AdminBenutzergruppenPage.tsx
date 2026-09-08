import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { GroupRolesEditor } from '../../components/GroupRolesEditor.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listRoles } from '../../auth/roles.ts'
import type { RoleGroup } from '../../auth/roles.ts'
import { ApiError } from '../../auth/users.ts'
import {
  listUsers,
  listUserGroups,
  createUserGroup,
  listUserGroupMembers,
  assignUserGroupMembers,
  deleteUserGroup,
} from '../../auth/users.ts'
import type { AdminUserSummary, UserGroup } from '../../auth/users.ts'
import styles from './AdminBenutzergruppenPage.module.css'

// AdminBenutzergruppenPage is the real "Benutzergruppen" surface (Story 2.6 +
// Effort 2): organisational teams (AD-12). It owns user-group CREATION, member
// assignment and per-group role assignment — the group management moved out of
// the Benutzer page into this dedicated surface so it is reachable directly
// from the admin nav between "Benutzer" and "Rollen". Membership alone grants
// no permission, but a team may hold roles (Spec 2.9) whose permissions the
// members inherit.
//
// The whole page is gated on `user_groups.manage` (matching the server
// sub-mount, AD-6); the server remains the source of truth. A 403 (revoked
// role) clears the cached admin flag and leaves the admin module (review
// finding 2.1-6); a 401 (expired session) clears auth and redirects to /login.
export function AdminBenutzergruppenPage() {
  const navigate = useNavigate()
  const [userGroups, setUserGroups] = useState<UserGroup[]>([])
  const [users, setUsers] = useState<AdminUserSummary[]>([])
  const [roles, setRoles] = useState<RoleGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [groupName, setGroupName] = useState('')
  const [groupFeedback, setGroupFeedback] = useState('')
  const [groupBusy, setGroupBusy] = useState(false)
  // Group member editor + delete confirmation state (findings 5 & 6).
  const [memberEditor, setMemberEditor] = useState<{ group: UserGroup; members: Set<string> } | null>(null)
  const [memberBusy, setMemberBusy] = useState(false)
  const [confirmDeleteGroup, setConfirmDeleteGroup] = useState<UserGroup | null>(null)
  // Group role-assignment editor (Effort 2): the group being edited.
  const [rolesEditor, setRolesEditor] = useState<{ group: UserGroup } | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const groupData = await listUserGroups()
      setUserGroups(groupData)
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
      setLoadError('Benutzergruppen konnten nicht geladen werden.')
    }
    // The member editor needs the full user list to offer checkboxes. The
    // caller holds user_groups.manage but not necessarily users.view, so a 403
    // here means "no directory access" — the member editor simply has no users
    // to offer (the group list itself still works). Never log the caller out.
    try {
      const userData = await listUsers()
      setUsers(userData)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setUsers([])
    }
    // The group role-assignment editor needs the permission-group catalog
    // regardless of roles.* codes (review fix 1). A user_groups.manage-only
    // holder is server-denied on GET /groups, which is expected — the editor
    // simply has no catalog. Never log the caller out for an expected 403.
    try {
      const roleData = await listRoles()
      setRoles(roleData.groups)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        clearAuthState()
        navigate('/login', { replace: true })
        return
      }
      setRoles([])
    }
    setLoading(false)
  }, [navigate])

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

  function handleForbidden() {
    adminForbiddenHandled({ status: 403 })
    navigate('/')
  }

  function handleUnauthorized() {
    clearAuthState()
    navigate('/login', { replace: true })
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
      if (err instanceof ApiError && err.status === 401) {
        handleUnauthorized()
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
      if (err instanceof ApiError && err.status === 401) {
        handleUnauthorized()
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
      if (err instanceof ApiError && err.status === 401) {
        handleUnauthorized()
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
          <h2 className={styles.title}>Benutzergruppen</h2>
          <p className={styles.description}>
            Teams anlegen, Mitglieder zuordnen und Team-Rollen vergeben.
          </p>

          {loadError && (
            <p role="alert" className={styles.feedbackError}>
              {loadError}
            </p>
          )}
          {groupFeedback && (
            <p
              role={groupFeedback.startsWith('Benutzergruppe') || groupFeedback.includes('Mitglieder') || groupFeedback.includes('Rollen von') ? 'status' : 'alert'}
              className={groupFeedback.startsWith('Benutzergruppe') || groupFeedback.includes('Mitglieder') || groupFeedback.includes('Rollen von') ? styles.feedbackSuccess : styles.feedbackError}
            >
              {groupFeedback}
            </p>
          )}

          {loading ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Benutzergruppen werden geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : (
            <>
              <section aria-label="Benutzergruppen" className={styles.groupsSection}>
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
                      <button type="button" className={styles.newGroupButton} onClick={() => void saveMembers()} disabled={memberBusy}>
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
            </>
          )}
        </main>
      </div>
    </div>
  )
}