import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { RoleEditor } from '../../components/RoleEditor.tsx'
import { adminForbiddenHandled, getPermissions, hasPermission } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import { listRoles } from '../../auth/roles.ts'
import type { PermissionCatalogEntry, RoleGroup } from '../../auth/roles.ts'
import styles from './AdminRollenPage.module.css'

// AdminRollenPage is the real "Rollen" surface (Story 2.5, UX-DR6/UX-DR8/
// UX-DR9): a role list (the four base roles + any custom named groups, each
// with a permission count and a base-role badge), a "Neue Rolle" action and an
// editor (create/edit) with the additive 23-code checkbox grid. Editing takes
// effect immediately on the next request because permission resolution is live
// (AD-2/AD-6/FR-6).
//
// The "Neue Rolle" action is gated client-side on `roles.create` and each row's
// "Bearbeiten" on `roles.edit`, matching the server's per-action codes (AD-6);
// the server remains the source of truth. A 403 (revoked role) clears the
// cached admin flag and leaves the admin module (review finding 2.1-6).
export function AdminRollenPage() {
  const navigate = useNavigate()
  const [groups, setGroups] = useState<RoleGroup[]>([])
  const [catalog, setCatalog] = useState<PermissionCatalogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [feedback, setFeedback] = useState('')
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRole, setEditingRole] = useState<RoleGroup | null>(null)

  const canCreate = hasPermission('roles.create')
  const canEdit = hasPermission('roles.edit')

  async function load() {
    try {
      const data = await listRoles()
      setGroups(data.groups)
      setCatalog(data.available_permissions)
    } catch (err) {
      if (err instanceof Error && 'status' in err && (err as { status: number }).status === 403) {
        // Revocation downgrade (review finding 2.1-6): drop the admin flag and
        // leave the admin module.
        adminForbiddenHandled({ status: 403 })
        navigate('/')
        return
      }
      setLoadError('Rollen konnten nicht geladen werden.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    async function run() {
      try {
        const data = await listRoles()
        if (cancelled) return
        setGroups(data.groups)
        setCatalog(data.available_permissions)
      } catch (err) {
        if (cancelled) return
        if (err instanceof Error && 'status' in err && (err as { status: number }).status === 403) {
          adminForbiddenHandled({ status: 403 })
          navigate('/')
          return
        }
        setLoadError('Rollen konnten nicht geladen werden.')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [navigate])

  function openCreate() {
    setEditingRole(null)
    setEditorOpen(true)
    setFeedback('')
  }

  function openEdit(role: RoleGroup) {
    setEditingRole(role)
    setEditorOpen(true)
    setFeedback('')
  }

  function closeEditor() {
    setEditorOpen(false)
    setEditingRole(null)
  }

  // Server-side revocation downgrade (review finding 2.1-6): a 403 from a save
  // means the caller's role was revoked — drop the cached admin flag and leave
  // the admin module, exactly like the list-load 403 path.
  function handleForbidden() {
    adminForbiddenHandled({ status: 403 })
    navigate('/')
  }

  async function handleSaved(_saved: RoleGroup, message: string) {
    setFeedback(message)
    setEditorOpen(false)
    setEditingRole(null)
    await load()
  }

  return (
    <div className={styles.page}>
      <Header />
      <div className={styles.body}>
        <AdminNav entries={filteredAdminNav(getPermissions())} />
        <main className={styles.main}>
          <h2 className={styles.title}>Rollen</h2>
          <p className={styles.description}>
            Rollen ansehen und anpassen. Änderungen gelten sofort.
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
            <RoleEditor
              key={editingRole?.id ?? 'new'}
              role={editingRole}
              availablePermissions={catalog}
              onSaved={handleSaved}
              onCancel={closeEditor}
              onForbidden={handleForbidden}
            />
          ) : loading ? (
            <div className={styles.skeleton} aria-busy="true" aria-label="Rollen werden geladen">
              <div className={styles.skeletonRow} aria-hidden="true" />
              <div className={styles.skeletonRow} aria-hidden="true" />
            </div>
          ) : (
            <>
              <div className={styles.toolbar}>
                {canCreate && (
                  <button type="button" className={styles.newButton} onClick={openCreate}>
                    Neue Rolle
                  </button>
                )}
              </div>

              {groups.length === 0 ? (
                <div className={styles.empty}>
                  <p>Noch keine Rollen vorhanden.</p>
                </div>
              ) : (
                <ul className={styles.list}>
                  {groups.map((group) => (
                    <li key={group.id} className={styles.row}>
                      <div className={styles.roleInfo}>
                        <div className={styles.roleName}>
                          {group.name}
                          {group.is_base_role && (
                            <span className={styles.baseBadge}>Basisrolle</span>
                          )}
                        </div>
                        {group.description !== '' && (
                          <span className={styles.roleDescription}>{group.description}</span>
                        )}
                        <span className={styles.roleCount}>
                          {group.permissions.length} Berechtigung
                          {group.permissions.length === 1 ? '' : 'en'}
                        </span>
                      </div>
                      <div className={styles.rowActions}>
                        {canEdit && (
                          <button
                            type="button"
                            className={styles.editButton}
                            onClick={() => openEdit(group)}
                          >
                            Bearbeiten
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
