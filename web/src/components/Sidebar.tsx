import { NavLink } from 'react-router-dom'
import { getPermissions } from '../auth/authState.ts'
import { hasAnyAdminCode } from '../auth/permissions.ts'
import styles from './Sidebar.module.css'

// Sidebar is the authenticated app shell's module navigation (Story 1.8): the
// "GEAR" module is always present; the "ADMIN" module is only shown when the
// caller's server-authoritative resolved permission set (GET
// /api/v1/auth/me/permissions, cached in authState) contains any admin-module
// code (Story 2.3, FR-19). The server resolves the set; the client never
// derives it. On narrow screens it collapses to a horizontal bar at the top.
export function Sidebar() {
  const isAdmin = hasAnyAdminCode(getPermissions())

  return (
    <nav className={styles.sidebar} aria-label="Module">
      <ul className={styles.list}>
        <li>
          <NavLink
            to="/"
            end
            className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ''}`}
          >
            GEAR
          </NavLink>
        </li>
        {isAdmin && (
          <li>
            <NavLink
              to="/admin"
              className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ''}`}
            >
              ADMIN
            </NavLink>
          </li>
        )}
      </ul>
    </nav>
  )
}