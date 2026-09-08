import { useCallback, useEffect, useMemo, useState } from 'react'
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
// Layout (UX): the "Neue Benutzergruppe" form sits at the top, then the group
// list — each group with "Bearbeiten" (opens the group detail) and "Löschen".
// The detail first shows the CURRENT members as a list, each with a small
// "Von Gruppe entfernen" button, and a "Benutzer hinzufügen" button at the top
// that reveals the AVAILABLE users (with their emails) to add. Team-role
// assignment stays reachable from the detail via the roles editor.
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
  // Detail view: the group being inspected + its current member ids.
  const [detailGroup, setDetailGroup] = useState<UserGroup | null>(null)
  const [memberIds, setMemberIds] = useState<string[]>([])
  const [memberBusy, setMemberBusy] = useState(false)
  const [showAddUsers, setShowAddUsers] = useState(false)
  const [showRolesEditor, setShowRolesEditor] = useState(false)
  const [confirmDeleteGroup, setConfirmDeleteGroup] = useState<UserGroup | null>(null)
  // Available-users search + sort (name/email columns).
  const [availableSearch, setAvailableSearch] = useState('')
  const [availableSort, setAvailableSort] = useState<{ key: 'vorname' | 'nachname' | 'email'; dir: 'asc' | 'desc' } | null>(null)

  // Resolve member ids to user objects (name + email) for display. A member not
  // present in the fetched user list (e.g. the caller lacks users.view) falls
  // back to the raw id so the member still shows up.
  const usersById = useMemo(() => {
    const map = new Map<string, AdminUserSummary>()
    for (const u of users) map.set(u.id, u)
    return map
  }, [users])

  const currentMembers = useMemo(
    () => memberIds.map((id) => usersById.get(id) ?? { id, vorname: id, nachname: '', email: '', status: 'active' as const }),
    [memberIds, usersById],
  )

  const availableUsers = useMemo(() => {
    const memberSet = new Set(memberIds)
    return users.filter((u) => !memberSet.has(u.id))
  }, [users, memberIds])

  // The available list is searchable across all columns and sortable by
  // Vorname / Nachname / E-Mail. Search is case-insensitive substring over the
  // full name and the email; sorting uses the German collation like UserTable.
  const filteredAvailableUsers = useMemo(() => {
    const query = availableSearch.trim().toLocaleLowerCase('de')
    const filtered = query === ''
      ? availableUsers
      : availableUsers.filter((u) =>
          `${u.vorname} ${u.nachname} ${u.email}`.toLocaleLowerCase('de').includes(query),
        )
    if (availableSort === null) return filtered
    const { key, dir } = availableSort
    return [...filtered].sort((a, b) => {
      const cmp = a[key].localeCompare(b[key], 'de', { sensitivity: 'base' })
      return dir === 'asc' ? cmp : -cmp
    })
  }, [availableUsers, availableSearch, availableSort])

  function cycleAvailableSort(key: 'vorname' | 'nachname' | 'email') {
    setAvailableSort((prev) => {
      if (prev === null || prev.key !== key) {
        return { key, dir: 'asc' }
      }
      if (prev.dir === 'asc') {
        return { key, dir: 'desc' }
      }
      return null
    })
  }

  function ariaSort(key: 'vorname' | 'nachname' | 'email'): 'ascending' | 'descending' | 'none' {
    if (availableSort === null || availableSort.key !== key) return 'none'
    return availableSort.dir === 'asc' ? 'ascending' : 'descending'
  }

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
    // The detail's member list and the add-users list need the user directory.
    // The caller holds user_groups.manage but not necessarily users.view, so a
    // 403 here means "no directory access" — members fall back to ids and the
    // add list is empty (the group list itself still works). Never log out.
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

  // Open the detail view for a group, loading its current member ids.
  async function openDetail(group: UserGroup) {
    setDetailGroup(group)
    setMemberIds([])
    setShowAddUsers(false)
    setShowRolesEditor(false)
    setAvailableSearch('')
    setAvailableSort(null)
    setGroupFeedback('')
    setMemberBusy(true)
    try {
      const ids = await listUserGroupMembers(group.id)
      setMemberIds(ids)
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

  function backToList() {
    setDetailGroup(null)
    setMemberIds([])
    setShowAddUsers(false)
    setShowRolesEditor(false)
    setAvailableSearch('')
    setAvailableSort(null)
    setGroupFeedback('')
  }

  // Add ONE user to the group via the replace-set endpoint.
  async function addUser(user: AdminUserSummary) {
    if (!detailGroup) return
    setMemberBusy(true)
    setGroupFeedback('')
    try {
      await assignUserGroupMembers(detailGroup.id, [...memberIds, user.id])
      setGroupFeedback(`„${user.vorname} ${user.nachname}“ wurde zu „${detailGroup.name}“ hinzugefügt.`)
      const ids = await listUserGroupMembers(detailGroup.id)
      setMemberIds(ids)
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        handleForbidden()
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        handleUnauthorized()
        return
      }
      setGroupFeedback(err instanceof ApiError && err.message ? err.message : 'Der Benutzer konnte nicht hinzugefügt werden.')
    } finally {
      setMemberBusy(false)
    }
  }

  // Remove ONE user from the group via the replace-set endpoint.
  async function removeUser(user: AdminUserSummary) {
    if (!detailGroup) return
    setMemberBusy(true)
    setGroupFeedback('')
    try {
      await assignUserGroupMembers(detailGroup.id, memberIds.filter((id) => id !== user.id))
      setGroupFeedback(`„${user.vorname} ${user.nachname}“ wurde aus „${detailGroup.name}“ entfernt.`)
      const ids = await listUserGroupMembers(detailGroup.id)
      setMemberIds(ids)
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        handleForbidden()
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        handleUnauthorized()
        return
      }
      setGroupFeedback(err instanceof ApiError && err.message ? err.message : 'Der Benutzer konnte nicht entfernt werden.')
    } finally {
      setMemberBusy(false)
    }
  }

  function handleGroupRolesSaved(message: string) {
    setGroupFeedback(message)
    setShowRolesEditor(false)
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

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(getPermissions())} />
        <main className={styles.main}>
          {detailGroup ? (
            <>
              <h2 className={styles.title}>{detailGroup.name}</h2>
              <p className={styles.description}>
                Mitglieder verwalten und Team-Rollen vergeben.
              </p>

              <div className={styles.detailActions}>
                <button type="button" className={styles.backButton} onClick={backToList}>
                  ← Zurück zur Liste
                </button>
                <button
                  type="button"
                  className={styles.groupActionButton}
                  onClick={() => {
                    setShowAddUsers((v) => !v)
                    setShowRolesEditor(false)
                  }}
                  disabled={memberBusy}
                >
                  Benutzer hinzufügen
                </button>
                <button
                  type="button"
                  className={styles.groupActionButton}
                  onClick={() => {
                    setShowRolesEditor((v) => !v)
                    setShowAddUsers(false)
                  }}
                >
                  Rollen
                </button>
              </div>

              {groupFeedback && (
                <p
                  role={groupFeedback.startsWith('„') ? 'status' : groupFeedback.includes('hinzugefügt') || groupFeedback.includes('entfernt') || groupFeedback.includes('Rollen von') ? 'status' : 'alert'}
                  className={groupFeedback.startsWith('„') || groupFeedback.includes('hinzugefügt') || groupFeedback.includes('entfernt') || groupFeedback.includes('Rollen von') ? styles.feedbackSuccess : styles.feedbackError}
                >
                  {groupFeedback}
                </p>
              )}

              {showRolesEditor && (
                <GroupRolesEditor
                  groupId={detailGroup.id}
                  groupName={detailGroup.name}
                  roles={roles}
                  onSaved={handleGroupRolesSaved}
                  onForbidden={handleForbidden}
                  onUnauthorized={handleUnauthorized}
                  onCancel={() => setShowRolesEditor(false)}
                />
              )}

              {showAddUsers && (
                <section aria-label="Benutzer hinzufügen" className={styles.addUsersSection}>
                  <h3 className={styles.memberEditorTitle}>Verfügbare Benutzer</h3>
                  {users.length === 0 ? (
                    <p className={styles.emptyNote}>
                      Keine Benutzer verfügbar (kein Verzeichniszugriff oder keine Benutzer vorhanden).
                    </p>
                  ) : (
                    <>
                      <div className={styles.searchRow}>
                        <label className={styles.label} htmlFor="available-search">
                          Suchen
                        </label>
                        <input
                          id="available-search"
                          type="search"
                          className={styles.input}
                          value={availableSearch}
                          onChange={(e) => setAvailableSearch(e.target.value)}
                          placeholder="Name oder E-Mail…"
                        />
                      </div>
                      {filteredAvailableUsers.length === 0 ? (
                        <p className={styles.emptyNote}>
                          {availableUsers.length === 0
                            ? 'Alle Benutzer sind dieser Benutzergruppe bereits zugeordnet.'
                            : 'Keine Benutzer entsprechen der Suche.'}
                        </p>
                      ) : (
                        <table className={styles.availableTable}>
                          <thead>
                            <tr>
                              {([
                                { key: 'vorname', label: 'Vorname' },
                                { key: 'nachname', label: 'Nachname' },
                                { key: 'email', label: 'E-Mail' },
                              ] as const).map((col) => (
                                <th key={col.key} scope="col" aria-sort={ariaSort(col.key)}>
                                  <button
                                    type="button"
                                    className={styles.sortButton}
                                    onClick={() => cycleAvailableSort(col.key)}
                                    aria-label={`Nach ${col.label} sortieren${ariaSort(col.key) === 'ascending' ? ' (absteigend)' : ariaSort(col.key) === 'descending' ? ' (aufsteigend)' : ''}`}
                                  >
                                    {col.label}
                                    <span className={styles.sortIndicator} aria-hidden="true">
                                      {ariaSort(col.key) === 'ascending' ? '▲' : ariaSort(col.key) === 'descending' ? '▼' : ''}
                                    </span>
                                  </button>
                                </th>
                              ))}
                              <th scope="col">
                                <span className={styles.srOnly}>Aktion</span>
                              </th>
                            </tr>
                          </thead>
                          <tbody>
                            {filteredAvailableUsers.map((user) => (
                              <tr key={user.id} className={styles.availableRow}>
                                <td className={styles.availableCell}>{user.vorname}</td>
                                <td className={styles.availableCell}>{user.nachname}</td>
                                <td className={styles.availableCell}>{user.email}</td>
                                <td className={styles.availableActionCell}>
                                  <button
                                    type="button"
                                    className={styles.smallAddButton}
                                    onClick={() => void addUser(user)}
                                    disabled={memberBusy}
                                  >
                                    Hinzufügen
                                  </button>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      )}
                    </>
                  )}
                </section>
              )}

              <section aria-label="Mitglieder" className={styles.membersSection}>
                <h3 className={styles.memberEditorTitle}>Mitglieder</h3>
                {memberBusy && memberIds.length === 0 ? (
                  <p className={styles.emptyNote}>Mitglieder werden geladen…</p>
                ) : currentMembers.length === 0 ? (
                  <p className={styles.emptyNote}>Keine Benutzer in dieser Benutzergruppe.</p>
                ) : (
                  <ul className={styles.memberList}>
                    {currentMembers.map((user) => (
                      <li key={user.id} className={styles.memberRow}>
                        <div className={styles.userInfo}>
                          <span className={styles.userName}>
                            {user.vorname} {user.nachname}
                          </span>
                          <span className={styles.userEmail}>{user.email}</span>
                        </div>
                        <button
                          type="button"
                          className={styles.smallRemoveButton}
                          onClick={() => void removeUser(user)}
                          disabled={memberBusy}
                        >
                          Von Gruppe entfernen
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            </>
          ) : loading ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Benutzergruppen werden geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : (
            <>
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
                  role={groupFeedback.startsWith('Benutzergruppe') ? 'status' : 'alert'}
                  className={groupFeedback.startsWith('Benutzergruppe') ? styles.feedbackSuccess : styles.feedbackError}
                >
                  {groupFeedback}
                </p>
              )}

              <section aria-label="Neue Benutzergruppe anlegen" className={styles.groupForm}>
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
              </section>

              <section aria-label="Benutzergruppen" className={styles.groupsSection}>
                {userGroups.length === 0 ? (
                  <p className={styles.emptyNote}>Noch keine Benutzergruppen.</p>
                ) : (
                  <ul className={styles.groupsList}>
                    {userGroups.map((group) => (
                      <li key={group.id} className={styles.groupRow}>
                        <span className={styles.groupName}>{group.name}</span>
                        <div className={styles.groupRowActions}>
                          <button
                            type="button"
                            className={styles.groupActionButton}
                            onClick={() => void openDetail(group)}
                          >
                            Bearbeiten
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
              </section>
            </>
          )}
        </main>
      </div>
    </div>
  )
}