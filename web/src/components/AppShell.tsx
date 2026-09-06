import type { ReactNode } from 'react'
import { Sidebar } from './Sidebar.tsx'
import styles from './AppShell.module.css'

// AppShell is the authenticated application shell (Story 1.8): a left module
// sidebar plus the page content. Every authenticated route is wrapped in it so
// the GEAR/ADMIN module navigation is persistent across the dashboard, profile,
// MFA and password pages.
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className={styles.shell}>
      <Sidebar />
      <div className={styles.content}>{children}</div>
    </div>
  )
}