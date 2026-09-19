import { useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Header } from '../../components/Header.tsx'
import { AdminNav } from '../../components/AdminNav.tsx'
import { adminForbiddenHandled, clearAuthState, getPermissions } from '../../auth/authState.ts'
import { filteredAdminNav } from '../../auth/permissions.ts'
import {
  BACKUP_SETTINGS_PERMISSION,
  SMTP_SETTINGS_PERMISSION,
  SCHEDULES_PERMISSION,
  SYSTEM_SETTINGS_PERMISSION,
} from '../../auth/settings.ts'
import styles from './AdminEinstellungenPage.module.css'
import { BackupSettingsTab } from './BackupSettingsTab.tsx'
import { EmailSettingsTab } from './EmailSettingsTab.tsx'
import { ScheduleSettingsTab } from './ScheduleSettingsTab.tsx'
import { SystemSettingsTab } from './SystemSettingsTab.tsx'
import type { Tab } from './settingsTabTypes.ts'

// AdminEinstellungenPage is the Einstellungen surface (Story 3.1 + 3.2 + 4.1 +
// 5-2b, FR-28/FR-29/FR-30/UX-DR6/UX-DR8/UX-DR9): a tab bar over the E-Mail,
// Backup, Zeitpläne and System settings surfaces. Each tab is gated by its OWN
// permission code (AD-6) — E-Mail by admin.settings.email, Backup by
// admin.settings.backup, Zeitpläne by schedules.manage, System by
// admin.settings.system — so a holder of only one code sees only that surface.
// Credentials are write-only: GET exposes only *configured booleans and saving
// with an empty credential omits it so the server keeps the existing encrypted
// one (NFR-S4). Inline German feedback, sticky actions, ≥48px targets,
// 401→login, 403→leave the admin module. The server remains the source of
// truth.
export function AdminEinstellungenPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const perms = getPermissions()
  const canEmail = perms.includes(SMTP_SETTINGS_PERMISSION)
  const canBackup = perms.includes(BACKUP_SETTINGS_PERMISSION)
  const canSchedules = perms.includes(SCHEDULES_PERMISSION)
  const canSystem = perms.includes(SYSTEM_SETTINGS_PERMISSION)
  // The schedule editor returns via router state (Spec 4-6 review 1/4): the
  // carried tab puts a multi-tab holder back on Zeitpläne, and the carried
  // message is shown once as a success notice. The state is cleared after the
  // first render so it never reappears on a later remount.
  const carried = location.state as { tab?: Tab | ''; message?: string } | null
  const [activeTab, setActiveTab] = useState<Tab>(() => {
    const fromTab = carried?.tab
    if (fromTab === 'email' && canEmail) return 'email'
    if (fromTab === 'backup' && canBackup) return 'backup'
    if (fromTab === 'schedules' && canSchedules) return 'schedules'
    if (fromTab === 'system' && canSystem) return 'system'
    return canEmail ? 'email' : canBackup ? 'backup' : canSchedules ? 'schedules' : 'system'
  })
  const [notice] = useState<string | null>(carried?.message ?? null)

  useEffect(() => {
    // Drop the carried state after the first render: the success notice stays
    // visible for this view, but a later remount/navigation must not re-show it.
    if (notice) {
      navigate(location.pathname, { replace: true, state: null })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // handleApiError inspects an API error: a 403 clears the cached admin flag
  // and leaves the admin module; a 401 (expired/revoked session) clears the
  // client auth state and redirects to /login. Returns true when the error was
  // handled (redirect), false otherwise.
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
          <h2 className={styles.title}>Einstellungen</h2>
          <p className={styles.description}>
            E-Mail-Versand, Backup-Ziele, Zeitpläne und System-Einstellungen. Änderungen gelten sofort, ohne Neubereitstellung.
          </p>

          {notice && (
            <p role="status" className={styles.feedbackSuccess}>
              {notice}
            </p>
          )}

          <div className={styles.tabs} role="tablist" aria-label="Einstellungen">
            {canEmail && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'email'}
                className={activeTab === 'email' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('email')}
              >
                E-Mail
              </button>
            )}
            {canBackup && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'backup'}
                className={activeTab === 'backup' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('backup')}
              >
                Backup
              </button>
            )}
            {canSchedules && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'schedules'}
                className={activeTab === 'schedules' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('schedules')}
              >
                Zeitpläne
              </button>
            )}
            {canSystem && (
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === 'system'}
                className={activeTab === 'system' ? styles.tabActive : styles.tab}
                onClick={() => setActiveTab('system')}
              >
                System
              </button>
            )}
          </div>

          {activeTab === 'email' && canEmail ? (
            <EmailSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'backup' && canBackup ? (
            <BackupSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'schedules' && canSchedules ? (
            <ScheduleSettingsTab onApiError={handleApiError} />
          ) : activeTab === 'system' && canSystem ? (
            <SystemSettingsTab onApiError={handleApiError} />
          ) : null}
        </main>
      </div>
    </div>
  )
}